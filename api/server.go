// Package api serves a read-only JSON API over a gtfs-analyze SQLite database,
// for the web UI in web/.
package api

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/samc1213/gtfs-analyze/log"
	"gorm.io/gorm"
)

// Largest time window /api/positions will return, to bound response size
const maxPositionsWindow = time.Hour

type Server struct {
	db          *gorm.DB
	logger      log.Interface
	allowOrigin string

	// The simplified route network is expensive to build, so it is cached per feed version
	networkLock    sync.Mutex
	networkVersion string
	networkJson    []byte
}

// NewServer returns an http.Handler for the API. allowOrigin is sent as
// Access-Control-Allow-Origin so a separately hosted web app can call it; empty disables CORS.
func NewServer(db *gorm.DB, logger log.Interface, allowOrigin string) http.Handler {
	server := &Server{db: db, logger: logger, allowOrigin: allowOrigin}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/meta", server.handleMeta)
	mux.HandleFunc("GET /api/routes", server.handleRoutes)
	mux.HandleFunc("GET /api/network", server.handleNetwork)
	mux.HandleFunc("GET /api/positions", server.handlePositions)
	mux.HandleFunc("GET /api/trips/{tripId}", server.handleTrip)
	mux.HandleFunc("GET /api/activity", server.handleActivity)
	return server.withMiddleware(mux)
}

// An error with an HTTP status to report to the client
type httpError struct {
	status  int
	message string
}

func (e *httpError) Error() string { return e.message }

func badRequest(format string, args ...any) error {
	return &httpError{status: http.StatusBadRequest, message: fmt.Sprintf(format, args...)}
}

func notFound(format string, args ...any) error {
	return &httpError{status: http.StatusNotFound, message: fmt.Sprintf(format, args...)}
}

func (server *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if server.allowOrigin != "" {
			w.Header().Set("Access-Control-Allow-Origin", server.allowOrigin)
		}
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Add("Vary", "Accept-Encoding")
			gzipWriter := gzip.NewWriter(w)
			defer gzipWriter.Close()
			w = gzipResponseWriter{ResponseWriter: w, writer: gzipWriter}
		}
		next.ServeHTTP(w, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	writer *gzip.Writer
}

func (w gzipResponseWriter) Write(data []byte) (int, error) {
	return w.writer.Write(data)
}

func (server *Server) writeJson(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		server.logger.Warning("Writing response: %s", err)
	}
}

func (server *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var httpErr *httpError
	if errors.As(err, &httpErr) {
		http.Error(w, httpErr.message, httpErr.status)
		return
	}
	server.logger.Error("%s %s: %s", r.Method, r.URL.Path, err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

func parseUnixParam(r *http.Request, name string) (int64, error) {
	value := r.URL.Query().Get(name)
	if value == "" {
		return 0, badRequest("missing query parameter %q (unix seconds)", name)
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, badRequest("query parameter %q must be unix seconds", name)
	}
	return parsed, nil
}

// latestFeedVersion is the most recently downloaded static feed
func (server *Server) latestFeedVersion(r *http.Request) (string, error) {
	var versions []string
	err := server.db.WithContext(r.Context()).Raw("SELECT version FROM feed_infos ORDER BY download_time DESC LIMIT 1").Scan(&versions).Error
	if err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "", notFound("no static GTFS feed has been stored yet")
	}
	return versions[0], nil
}

// feedVersionForDate is the most recently downloaded static feed valid on serviceDate,
// falling back to the latest feed if none claims to cover that date.
func (server *Server) feedVersionForDate(r *http.Request, serviceDate time.Time) (string, error) {
	var versions []string
	err := server.db.WithContext(r.Context()).Raw(`SELECT version FROM feed_infos
		WHERE (start_date IS NULL OR start_date <= ?) AND (end_date IS NULL OR end_date >= ?)
		ORDER BY download_time DESC LIMIT 1`, serviceDate, serviceDate).Scan(&versions).Error
	if err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return server.latestFeedVersion(r)
	}
	return versions[0], nil
}

func (server *Server) agencyLocation(r *http.Request, version string) (*time.Location, error) {
	var timezones []string
	err := server.db.WithContext(r.Context()).Raw("SELECT timezone FROM agencies WHERE version = ? LIMIT 1", version).Scan(&timezones).Error
	if err != nil {
		return nil, err
	}
	if len(timezones) == 0 || timezones[0] == "" {
		return time.UTC, nil
	}
	return time.LoadLocation(timezones[0])
}

type metaResponse struct {
	AgencyName  string `json:"agencyName"`
	Timezone    string `json:"timezone"`
	FeedVersion string `json:"feedVersion"`
	// Range of stored vehicle positions, unix seconds. Null if there are none yet.
	EarliestPosition *int64 `json:"earliestPosition"`
	LatestPosition   *int64 `json:"latestPosition"`
	// Map center, from the stops' bounding box
	Center *[2]float64 `json:"center"`
}

func (server *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	response := metaResponse{}
	db := server.db.WithContext(r.Context())

	version, err := server.latestFeedVersion(r)
	var httpErr *httpError
	if err != nil && !errors.As(err, &httpErr) {
		server.writeError(w, r, err)
		return
	}
	if version != "" {
		response.FeedVersion = version
		var agency struct {
			Name     string
			Timezone string
		}
		if err := db.Raw("SELECT name, timezone FROM agencies WHERE version = ? LIMIT 1", version).Scan(&agency).Error; err != nil {
			server.writeError(w, r, err)
			return
		}
		response.AgencyName, response.Timezone = agency.Name, agency.Timezone

		// The median stop, rather than the middle of the bounding box: a few far-flung
		// stops (RTD has some ~150 km away) would otherwise drag the center out of town
		var latitudes, longitudes []float64
		if err := db.Raw("SELECT latitude FROM stops WHERE version = ? AND latitude != 0 ORDER BY latitude", version).Scan(&latitudes).Error; err != nil {
			server.writeError(w, r, err)
			return
		}
		if err := db.Raw("SELECT longitude FROM stops WHERE version = ? AND longitude != 0 ORDER BY longitude", version).Scan(&longitudes).Error; err != nil {
			server.writeError(w, r, err)
			return
		}
		if len(latitudes) > 0 && len(longitudes) > 0 {
			response.Center = &[2]float64{longitudes[len(longitudes)/2], latitudes[len(latitudes)/2]}
		}
	}

	var positionRange struct {
		Earliest *int64
		Latest   *int64
	}
	// Bounded by snapshot times, not vehicle times: a vehicle with a broken clock or a
	// days-old last report would otherwise stretch the range far beyond the collected data
	if err := db.Raw(`SELECT MIN(message_timestamp) AS earliest, MAX(message_timestamp) AS latest
		FROM vehicle_positions WHERE message_timestamp > 0`).Scan(&positionRange).Error; err != nil {
		server.writeError(w, r, err)
		return
	}
	response.EarliestPosition, response.LatestPosition = positionRange.Earliest, positionRange.Latest

	server.writeJson(w, response)
}

type routeResponse struct {
	Id        string `json:"id"`
	ShortName string `json:"shortName"`
	LongName  string `json:"longName"`
	Type      int    `json:"type"`
	Color     string `json:"color"`
	TextColor string `json:"textColor"`
}

func (server *Server) handleRoutes(w http.ResponseWriter, r *http.Request) {
	version, err := server.latestFeedVersion(r)
	if err != nil {
		server.writeError(w, r, err)
		return
	}
	routes := []routeResponse{}
	err = server.db.WithContext(r.Context()).Raw(`SELECT id, COALESCE(short_name, '') AS short_name, COALESCE(long_name, '') AS long_name,
		type, COALESCE(color, '') AS color, COALESCE(text_color, '') AS text_color
		FROM routes WHERE version = ? ORDER BY sort_order, short_name, id`, version).Scan(&routes).Error
	if err != nil {
		server.writeError(w, r, err)
		return
	}
	server.writeJson(w, routes)
}

// A vehicle position, kept compact because replay requests many of them
type positionResponse struct {
	EntityId     string  `json:"entityId"`
	VehicleLabel string  `json:"label"`
	TripId       string  `json:"tripId"`
	RouteId      string  `json:"routeId"`
	StartDate    string  `json:"startDate"` // YYYYMMDD, empty if not provided
	Latitude     float64 `json:"lat"`
	Longitude    float64 `json:"lon"`
	Bearing      float64 `json:"bearing"`
	Status       int     `json:"status"` // 0 incoming at, 1 stopped at, 2 in transit to
	StopId       string  `json:"stopId"`
	Timestamp    int64   `json:"t"`
}

type positionRow struct {
	EntityId          string
	VehicleId         *string
	VehicleLabel      *string
	TripId            string
	RouteId           string
	StartDate         time.Time
	Latitude          float64
	Longitude         float64
	Bearing           float64
	CurrentStatus     int
	StopId            string
	PositionTimestamp int64
}

// handlePositions returns every stored vehicle position with a timestamp in [from, to],
// ordered by time. Optional route filters by route id.
func (server *Server) handlePositions(w http.ResponseWriter, r *http.Request) {
	from, err := parseUnixParam(r, "from")
	if err != nil {
		server.writeError(w, r, err)
		return
	}
	to, err := parseUnixParam(r, "to")
	if err != nil {
		server.writeError(w, r, err)
		return
	}
	if to < from {
		server.writeError(w, r, badRequest("to must not be before from"))
		return
	}
	if time.Duration(to-from)*time.Second > maxPositionsWindow {
		server.writeError(w, r, badRequest("window must be at most %s", maxPositionsWindow))
		return
	}

	query := server.db.WithContext(r.Context()).Table("vehicle_positions").
		Select("entity_id, vehicle_id, vehicle_label, trip_id, route_id, start_date, latitude, longitude, bearing, current_status, stop_id, position_timestamp").
		Where("position_timestamp BETWEEN ? AND ?", from, to).
		Order("position_timestamp, id")
	if route := r.URL.Query().Get("route"); route != "" {
		query = query.Where("route_id = ?", route)
	}
	var rows []positionRow
	if err := query.Scan(&rows).Error; err != nil {
		server.writeError(w, r, err)
		return
	}

	positions := make([]positionResponse, len(rows))
	for i, row := range rows {
		label := row.EntityId
		if row.VehicleLabel != nil && *row.VehicleLabel != "" {
			label = *row.VehicleLabel
		} else if row.VehicleId != nil && *row.VehicleId != "" {
			label = *row.VehicleId
		}
		startDate := ""
		if !row.StartDate.IsZero() {
			startDate = row.StartDate.UTC().Format("20060102")
		}
		positions[i] = positionResponse{
			EntityId:     row.EntityId,
			VehicleLabel: label,
			TripId:       row.TripId,
			RouteId:      row.RouteId,
			StartDate:    startDate,
			Latitude:     row.Latitude,
			Longitude:    row.Longitude,
			Bearing:      row.Bearing,
			Status:       row.CurrentStatus,
			StopId:       row.StopId,
			Timestamp:    row.PositionTimestamp,
		}
	}
	server.writeJson(w, positions)
}

type activityResponse struct {
	From       int64 `json:"from"`
	BucketSize int64 `json:"bucketSize"` // seconds
	// Vehicles in service (on a trip) seen in each bucket, starting at From
	Vehicles []int `json:"vehicles"`
}

// handleActivity counts vehicles in service over [from, to], in at most `buckets`
// (default 240) equal buckets of at least a minute. Used to draw the UI's timeline.
func (server *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	from, err := parseUnixParam(r, "from")
	if err != nil {
		server.writeError(w, r, err)
		return
	}
	to, err := parseUnixParam(r, "to")
	if err != nil {
		server.writeError(w, r, err)
		return
	}
	if to < from {
		server.writeError(w, r, badRequest("to must not be before from"))
		return
	}
	buckets := int64(240)
	if value := r.URL.Query().Get("buckets"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 1 || parsed > 2000 {
			server.writeError(w, r, badRequest("buckets must be between 1 and 2000"))
			return
		}
		buckets = parsed
	}
	bucketSize := (to - from + buckets) / buckets
	if bucketSize < 60 {
		bucketSize = 60
	}

	var rows []struct {
		Bucket   int64
		Vehicles int
	}
	err = server.db.WithContext(r.Context()).Raw(`SELECT (position_timestamp - ?) / ? AS bucket, COUNT(DISTINCT entity_id) AS vehicles
		FROM vehicle_positions WHERE position_timestamp BETWEEN ? AND ? AND trip_id != ''
		GROUP BY bucket ORDER BY bucket`, from, bucketSize, from, to).Scan(&rows).Error
	if err != nil {
		server.writeError(w, r, err)
		return
	}

	response := activityResponse{From: from, BucketSize: bucketSize, Vehicles: make([]int, (to-from)/bucketSize+1)}
	for _, row := range rows {
		if row.Bucket >= 0 && row.Bucket < int64(len(response.Vehicles)) {
			response.Vehicles[row.Bucket] = row.Vehicles
		}
	}
	server.writeJson(w, response)
}
