package api

import (
	"net/http"
	"time"

	"github.com/samc1213/gtfs-analyze/model"
)

type tripStopResponse struct {
	Sequence  int32   `json:"sequence"`
	StopId    string  `json:"stopId"`
	Name      string  `json:"name"`
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lon"`
	// Unix seconds. Null when the schedule doesn't give a time for this stop.
	ScheduledArrival *int64 `json:"scheduledArrival"`
	// Latest realtime prediction as of the requested time, unix seconds. Null if none.
	PredictedArrival *int64 `json:"predictedArrival"`
	Skipped          bool   `json:"skipped"`
}

type tripResponse struct {
	TripId         string `json:"tripId"`
	ServiceDate    string `json:"serviceDate"` // YYYYMMDD
	RouteId        string `json:"routeId"`
	RouteShortName string `json:"routeShortName"`
	RouteLongName  string `json:"routeLongName"`
	RouteColor     string `json:"routeColor"`
	RouteTextColor string `json:"routeTextColor"`
	Headsign       string `json:"headsign"`
	DirectionId    int    `json:"directionId"`
	// Predicted minus scheduled arrival at the next stop (or the last stop, if the
	// trip is over), in seconds. Positive is late. Null without predictions.
	DelaySeconds *int64             `json:"delaySeconds"`
	Stops        []tripStopResponse `json:"stops"`
	// The trip's path as [lon, lat] pairs. Empty if the feed has no shape for it.
	Shape [][2]float64 `json:"shape"`
}

// handleTrip describes one trip on one service date: its scheduled stops and, for each,
// the latest realtime prediction made at or before `at`.
//
// Query parameters: date (YYYYMMDD; defaults to the agency-local date of `at`, and is
// also the realtime start_date to match), at (unix seconds; defaults to now).
func (server *Server) handleTrip(w http.ResponseWriter, r *http.Request) {
	tripId := r.PathValue("tripId")
	at := time.Now().Unix()
	if r.URL.Query().Get("at") != "" {
		parsed, err := parseUnixParam(r, "at")
		if err != nil {
			server.writeError(w, r, err)
			return
		}
		at = parsed
	}

	dateParam := r.URL.Query().Get("date")
	var serviceDate time.Time // midnight UTC of the service date, as start dates are stored
	if dateParam != "" {
		parsed, err := time.Parse("20060102", dateParam)
		if err != nil {
			server.writeError(w, r, badRequest("date must be YYYYMMDD"))
			return
		}
		serviceDate = parsed
	}

	versionDate := serviceDate
	if versionDate.IsZero() {
		versionDate = time.Unix(at, 0).UTC()
	}
	version, err := server.feedVersionForDate(r, versionDate)
	if err != nil {
		server.writeError(w, r, err)
		return
	}
	location, err := server.agencyLocation(r, version)
	if err != nil {
		server.writeError(w, r, err)
		return
	}
	if serviceDate.IsZero() {
		y, m, d := time.Unix(at, 0).In(location).Date()
		serviceDate = time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}

	db := server.db.WithContext(r.Context())
	var trip struct {
		Id          string
		RouteId     string
		Headsign    *string
		DirectionId int
		ShapeId     string
		ShortName   *string
		LongName    *string
		Color       *string
		TextColor   *string
	}
	err = db.Raw(`SELECT t.id, t.route_id, t.headsign, t.direction_id, t.shape_id,
		r.short_name, r.long_name, r.color, r.text_color
		FROM trips t LEFT JOIN routes r ON r.version = t.version AND r.id = t.route_id
		WHERE t.version = ? AND t.id = ?`, version, tripId).Scan(&trip).Error
	if err != nil {
		server.writeError(w, r, err)
		return
	}
	if trip.Id == "" {
		server.writeError(w, r, notFound("trip %q is not in static feed version %s", tripId, version))
		return
	}

	var stopTimes []struct {
		StopSequence int32
		StopId       string
		ArrivalTime  *int64
		Name         *string
		Latitude     *float64
		Longitude    *float64
	}
	err = db.Raw(`SELECT st.stop_sequence, st.stop_id, st.arrival_time, s.name, s.latitude, s.longitude
		FROM stop_times st LEFT JOIN stops s ON s.version = st.version AND s.id = st.stop_id
		WHERE st.version = ? AND st.trip_id = ? ORDER BY st.stop_sequence`, version, tripId).Scan(&stopTimes).Error
	if err != nil {
		server.writeError(w, r, err)
		return
	}

	// Latest prediction per stop as of `at`. Rows are change events, so the last one wins.
	var updates []model.StopTimeUpdate
	err = db.Where("trip_id = ? AND start_date = ? AND message_timestamp <= ?", tripId, serviceDate, at).
		Order("id").Find(&updates).Error
	if err != nil {
		server.writeError(w, r, err)
		return
	}
	latestBySequence := make(map[int32]model.StopTimeUpdate)
	latestByStopId := make(map[string]model.StopTimeUpdate)
	for _, update := range updates {
		if update.StopSequence != nil {
			latestBySequence[int32(*update.StopSequence)] = update
		} else if update.StopId != "" {
			latestByStopId[update.StopId] = update
		}
	}

	// GTFS times are measured from "noon minus 12h" on the service date, which differs
	// from midnight on daylight saving changeover days
	y, m, d := serviceDate.Date()
	serviceDayStart := time.Date(y, m, d, 12, 0, 0, 0, location).Add(-12 * time.Hour).Unix()

	response := tripResponse{
		TripId:         trip.Id,
		ServiceDate:    serviceDate.Format("20060102"),
		RouteId:        trip.RouteId,
		RouteShortName: deref(trip.ShortName),
		RouteLongName:  deref(trip.LongName),
		RouteColor:     deref(trip.Color),
		RouteTextColor: deref(trip.TextColor),
		Headsign:       deref(trip.Headsign),
		DirectionId:    trip.DirectionId,
		Stops:          make([]tripStopResponse, len(stopTimes)),
		Shape:          [][2]float64{},
	}
	for i, stopTime := range stopTimes {
		stop := tripStopResponse{
			Sequence:  stopTime.StopSequence,
			StopId:    stopTime.StopId,
			Name:      deref(stopTime.Name),
			Latitude:  derefFloat(stopTime.Latitude),
			Longitude: derefFloat(stopTime.Longitude),
		}
		if stopTime.ArrivalTime != nil {
			scheduled := serviceDayStart + *stopTime.ArrivalTime
			stop.ScheduledArrival = &scheduled
		}
		update, found := latestBySequence[stopTime.StopSequence]
		if !found {
			update, found = latestByStopId[stopTime.StopId]
		}
		if found {
			stop.Skipped = update.ScheduleRelationship == model.StopSkipped
			if update.ArrivalTime != nil {
				stop.PredictedArrival = update.ArrivalTime
			} else if update.DepartureTime != nil {
				stop.PredictedArrival = update.DepartureTime
			}
		}
		response.Stops[i] = stop
	}
	response.DelaySeconds = tripDelay(response.Stops, at)

	if trip.ShapeId != "" {
		var points []struct{ Latitude, Longitude float64 }
		err = db.Raw("SELECT latitude, longitude FROM shapes WHERE version = ? AND id = ? ORDER BY sequence", version, trip.ShapeId).Scan(&points).Error
		if err != nil {
			server.writeError(w, r, err)
			return
		}
		for _, point := range points {
			response.Shape = append(response.Shape, [2]float64{point.Longitude, point.Latitude})
		}
	}

	server.writeJson(w, response)
}

// tripDelay is predicted minus scheduled arrival at the first stop predicted at or after
// `at`; or, if every prediction is in the past, at the last predicted stop.
func tripDelay(stops []tripStopResponse, at int64) *int64 {
	var last *int64
	for _, stop := range stops {
		if stop.Skipped || stop.PredictedArrival == nil || stop.ScheduledArrival == nil {
			continue
		}
		delay := *stop.PredictedArrival - *stop.ScheduledArrival
		if *stop.PredictedArrival >= at {
			return &delay
		}
		last = &delay
	}
	return last
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func derefFloat(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}
