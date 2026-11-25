package api

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/samc1213/gtfs-analyze/core"
	"github.com/samc1213/gtfs-analyze/log"
	"github.com/samc1213/gtfs-analyze/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var denver, _ = time.LoadLocation("America/Denver")

// 2026-11-24, a Tuesday
var serviceDate = time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)

func localTime(hour, minute int) int64 {
	return time.Date(2026, 11, 24, hour, minute, 0, 0, denver).Unix()
}

func ptr[T any](value T) *T { return &value }

func newTestServer(t *testing.T) (http.Handler, *gorm.DB) {
	t.Helper()
	db, err := core.InitializeSqliteDatabase(filepath.Join(t.TempDir(), "api.db"), log.Silent)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDb, err := db.DB(); err == nil {
			sqlDb.Close()
		}
	})

	feed := &model.GtfsStaticFeed{
		Agency: []model.Agency{{Id: "RTD", Name: "Regional Transportation District", Timezone: "America/Denver"}},
		Route:  []model.Route{{Id: "15", ShortName: "15", LongName: "East Colfax", Type: model.Bus, Color: "0076CE"}},
		Trip:   []model.Trip{{Id: "T1", RouteId: "15", ServiceId: "WK", Headsign: "Aurora", ShapeId: "S1"}},
		Stop: []model.Stop{
			{Id: "A", Name: "Civic Center", Latitude: 39.74, Longitude: -104.99},
			{Id: "B", Name: "Colfax & York", Latitude: 39.74, Longitude: -104.96},
			{Id: "C", Name: "Colfax & Colorado", Latitude: 39.74, Longitude: -104.94},
		},
		StopTime: []model.StopTime{
			{TripId: "T1", StopId: "A", StopSequence: 1, ArrivalTime: 8 * 3600},
			{TripId: "T1", StopId: "B", StopSequence: 2, ArrivalTime: 8*3600 + 10*60},
			{TripId: "T1", StopId: "C", StopSequence: 3, ArrivalTime: 8*3600 + 20*60},
		},
		Shape: []model.Shape{
			// Collinear middle points are removed by simplification
			{Id: "S1", Sequence: 1, Latitude: 39.74, Longitude: -104.99},
			{Id: "S1", Sequence: 2, Latitude: 39.74, Longitude: -104.98},
			{Id: "S1", Sequence: 3, Latitude: 39.74, Longitude: -104.97},
			{Id: "S1", Sequence: 4, Latitude: 39.75, Longitude: -104.96},
			{Id: "S1", Sequence: 5, Latitude: 39.74, Longitude: -104.94},
		},
		FeedInfo: model.FeedInfo{
			Version:      "v1",
			StartDate:    time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC),
			EndDate:      time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC),
			DownloadTime: time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC),
		},
	}
	for i := range feed.Agency {
		feed.Agency[i].Version = "v1"
	}
	for i := range feed.Route {
		feed.Route[i].Version = "v1"
	}
	for i := range feed.Trip {
		feed.Trip[i].Version = "v1"
	}
	for i := range feed.Stop {
		feed.Stop[i].Version = "v1"
	}
	for i := range feed.StopTime {
		feed.StopTime[i].Version = "v1"
	}
	for i := range feed.Shape {
		feed.Shape[i].Version = "v1"
	}
	require.NoError(t, core.WriteStaticGtfsFeedToDatabase(context.Background(), feed, db))

	positions := []model.VehiclePosition{
		{EntityId: "bus1", VehicleLabel: "6001", TripId: "T1", RouteId: "15", StartDate: serviceDate, Latitude: 39.74, Longitude: -104.99, PositionTimestamp: uint64(localTime(8, 1)), MessageTimestamp: uint64(localTime(8, 1))},
		{EntityId: "bus1", VehicleLabel: "6001", TripId: "T1", RouteId: "15", StartDate: serviceDate, Latitude: 39.74, Longitude: -104.97, PositionTimestamp: uint64(localTime(8, 6)), MessageTimestamp: uint64(localTime(8, 6))},
		{EntityId: "bus2", TripId: "T9", RouteId: "0", Latitude: 39.70, Longitude: -104.98, PositionTimestamp: uint64(localTime(8, 6)), MessageTimestamp: uint64(localTime(8, 6))},
	}

	// Stop B: predicted on time at 8:00, then 3 minutes late at 8:05
	updates := []model.StopTimeUpdate{
		{TripId: "T1", StartDate: serviceDate, StopSequence: ptr(uint32(2)), StopId: "B", ArrivalTime: ptr(localTime(8, 10)), MessageTimestamp: uint64(localTime(8, 0))},
		{TripId: "T1", StartDate: serviceDate, StopSequence: ptr(uint32(2)), StopId: "B", ArrivalTime: ptr(localTime(8, 13)), MessageTimestamp: uint64(localTime(8, 5))},
		{TripId: "T1", StartDate: serviceDate, StopSequence: ptr(uint32(3)), StopId: "C", ScheduleRelationship: model.StopSkipped, MessageTimestamp: uint64(localTime(8, 5))},
	}
	require.NoError(t, core.WriteRtChangesToDatabase(context.Background(), &core.RtChanges{VehiclePositions: positions, StopTimeUpdates: updates}, db))

	return NewServer(db, log.New(log.Silent), "*"), db
}

func get(t *testing.T, handler http.Handler, url string, response any) int {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, url, nil))
	if recorder.Code == http.StatusOK && response != nil {
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), response), recorder.Body.String())
	}
	return recorder.Code
}

func TestMeta(t *testing.T) {
	handler, _ := newTestServer(t)
	var meta metaResponse
	require.Equal(t, http.StatusOK, get(t, handler, "/api/meta", &meta))
	assert.Equal(t, "Regional Transportation District", meta.AgencyName)
	assert.Equal(t, "America/Denver", meta.Timezone)
	assert.Equal(t, "v1", meta.FeedVersion)
	assert.EqualValues(t, localTime(8, 1), *meta.EarliestPosition)
	assert.EqualValues(t, localTime(8, 6), *meta.LatestPosition)
	assert.Equal(t, &[2]float64{-104.96, 39.74}, meta.Center)
}

func TestRoutesAndNetwork(t *testing.T) {
	handler, _ := newTestServer(t)
	var routes []routeResponse
	require.Equal(t, http.StatusOK, get(t, handler, "/api/routes", &routes))
	require.Len(t, routes, 1)
	assert.Equal(t, routeResponse{Id: "15", ShortName: "15", LongName: "East Colfax", Type: 3, Color: "0076CE"}, routes[0])

	var network geoJsonFeatureCollection
	require.Equal(t, http.StatusOK, get(t, handler, "/api/network", &network))
	require.Len(t, network.Features, 1)
	feature := network.Features[0]
	assert.Equal(t, "15", feature.Properties.RouteId)
	assert.Equal(t, "0076CE", feature.Properties.Color)
	assert.Equal(t, [][2]float64{{-104.99, 39.74}, {-104.97, 39.74}, {-104.96, 39.75}, {-104.94, 39.74}}, feature.Geometry.Coordinates)
}

func TestPositions(t *testing.T) {
	handler, _ := newTestServer(t)
	url := func(query string) string { return "/api/positions?" + query }

	var positions []positionResponse
	require.Equal(t, http.StatusOK, get(t, handler, url("from="+itoa(localTime(8, 0))+"&to="+itoa(localTime(8, 10))), &positions))
	require.Len(t, positions, 3)
	assert.Equal(t, "6001", positions[0].VehicleLabel)
	assert.Equal(t, "20261124", positions[0].StartDate)
	// Falls back to the entity id when there's no vehicle label or id
	assert.Equal(t, "bus2", positions[2].VehicleLabel)
	assert.Equal(t, "", positions[2].StartDate)

	require.Equal(t, http.StatusOK, get(t, handler, url("route=15&from="+itoa(localTime(8, 5))+"&to="+itoa(localTime(8, 10))), &positions))
	require.Len(t, positions, 1)
	assert.InDelta(t, -104.97, positions[0].Longitude, 1e-9)

	assert.Equal(t, http.StatusBadRequest, get(t, handler, url("from=1"), nil))
	assert.Equal(t, http.StatusBadRequest, get(t, handler, url("from=100&to=50"), nil))
	assert.Equal(t, http.StatusBadRequest, get(t, handler, url("from=0&to=7200"), nil))
}

func TestTripUsesLatestPredictionAsOfTime(t *testing.T) {
	handler, _ := newTestServer(t)

	// At 8:03 the latest prediction for stop B was "on time"
	var trip tripResponse
	require.Equal(t, http.StatusOK, get(t, handler, "/api/trips/T1?date=20261124&at="+itoa(localTime(8, 3)), &trip))
	assert.Equal(t, "Aurora", trip.Headsign)
	assert.Equal(t, "15", trip.RouteShortName)
	require.Len(t, trip.Stops, 3)
	assert.Equal(t, "Colfax & York", trip.Stops[1].Name)
	assert.EqualValues(t, localTime(8, 10), *trip.Stops[1].ScheduledArrival)
	assert.EqualValues(t, localTime(8, 10), *trip.Stops[1].PredictedArrival)
	assert.EqualValues(t, 0, *trip.DelaySeconds)
	assert.False(t, trip.Stops[2].Skipped)
	assert.Len(t, trip.Shape, 5)

	// At 8:06 it had slipped 3 minutes, and stop C was skipped
	require.Equal(t, http.StatusOK, get(t, handler, "/api/trips/T1?date=20261124&at="+itoa(localTime(8, 6)), &trip))
	assert.EqualValues(t, localTime(8, 13), *trip.Stops[1].PredictedArrival)
	assert.EqualValues(t, 180, *trip.DelaySeconds)
	assert.True(t, trip.Stops[2].Skipped)

	// Without a date, the service date is the agency-local date of `at`
	require.Equal(t, http.StatusOK, get(t, handler, "/api/trips/T1?at="+itoa(localTime(8, 6)), &trip))
	assert.Equal(t, "20261124", trip.ServiceDate)
	assert.EqualValues(t, 180, *trip.DelaySeconds)

	assert.Equal(t, http.StatusNotFound, get(t, handler, "/api/trips/nope?date=20261124", nil))
	assert.Equal(t, http.StatusBadRequest, get(t, handler, "/api/trips/T1?date=24-11-2026", nil))
}

func TestGzipAndCors(t *testing.T) {
	handler, _ := newTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "/api/routes", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	assert.Equal(t, "*", recorder.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "gzip", recorder.Header().Get("Content-Encoding"))
	reader, err := gzip.NewReader(recorder.Body)
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	var routes []routeResponse
	require.NoError(t, json.Unmarshal(body, &routes))
	assert.Len(t, routes, 1)
}

func TestNoStaticFeedYet(t *testing.T) {
	db, err := core.InitializeSqliteDatabase(filepath.Join(t.TempDir(), "empty.db"), log.Silent)
	require.NoError(t, err)
	defer func() {
		if sqlDb, err := db.DB(); err == nil {
			sqlDb.Close()
		}
	}()
	handler := NewServer(db, log.New(log.Silent), "")
	assert.Equal(t, http.StatusNotFound, get(t, handler, "/api/routes", nil))
	var meta metaResponse
	require.Equal(t, http.StatusOK, get(t, handler, "/api/meta", &meta))
	assert.Nil(t, meta.LatestPosition)
}

func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}

func TestActivity(t *testing.T) {
	handler, _ := newTestServer(t)
	var activity activityResponse
	from := localTime(8, 0)
	require.Equal(t, http.StatusOK, get(t, handler, "/api/activity?buckets=10&from="+itoa(from)+"&to="+itoa(from+599), &activity))
	assert.EqualValues(t, 60, activity.BucketSize)
	require.Len(t, activity.Vehicles, 10)
	// bus1 at 8:01; bus1 and bus2 at 8:06
	assert.Equal(t, []int{0, 1, 0, 0, 0, 0, 2, 0, 0, 0}, activity.Vehicles)

	assert.Equal(t, http.StatusBadRequest, get(t, handler, "/api/activity?buckets=0&from=1&to=2", nil))
}
