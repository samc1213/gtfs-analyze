package core

import (
	"fmt"
	"testing"
	"time"

	"github.com/samc1213/gtfs-analyze/log"
	"github.com/samc1213/gtfs-analyze/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockLogger struct {
	mock.Mock
}

func TestOtpSingleTrip(t *testing.T) {
	feed, tripOneId, stopOneId, stopTwoId, tripDate := createStaticFeed()
	calculation, err := CreateOtpCalculation(feed)
	assert.NoError(t, err)
	logger := log.New(log.Info)
	simulateInTransitToStop(tripDate, 8*time.Hour, tripOneId, stopOneId, calculation, logger)
	stopOne := &calculation.TripsByDate[tripDate][tripOneId].StopTimes[0]
	stopTwo := &calculation.TripsByDate[tripDate][tripOneId].StopTimes[1]
	assert.Equal(t, stopOneId, stopOne.StopId)
	assert.Equal(t, stopTwoId, stopTwo.StopId)
	assert.Equal(t, tripDate.Add(8*time.Hour+30*time.Minute), stopOne.StopTime)
	assert.Equal(t, tripDate.Add(8*time.Hour+45*time.Minute), stopTwo.StopTime)

	// Haven't gotten to stop one yet
	assert.Zero(t, stopOne.ActualArrivalTime)
	assert.Zero(t, stopTwo.ActualArrivalTime)

	// At stop one at 8:32
	stopOneArrivalTime := 8*time.Hour + 32*time.Minute
	simulateStop(tripDate, stopOneArrivalTime, tripOneId, stopOneId, calculation, logger)

	assert.Equal(t, tripDate.Add(stopOneArrivalTime), stopOne.ActualArrivalTime)
	assert.Zero(t, stopTwo.ActualArrivalTime)

	// At stop two at 8:44
	stopTwoArrivalTime := 8*time.Hour + 44*time.Minute
	simulateStop(tripDate, stopTwoArrivalTime, tripOneId, stopTwoId, calculation, logger)

	assert.Equal(t, tripDate.Add(stopOneArrivalTime), stopOne.ActualArrivalTime)
	assert.Equal(t, tripDate.Add(stopTwoArrivalTime), stopTwo.ActualArrivalTime)

	otpByTrip := calculation.SummarizeOnTimePerformanceByTrip(8*time.Minute, logger)
	assert.EqualValues(t, "TripId", otpByTrip.GroupBy)
	assert.Contains(t, otpByTrip.OtpSummaries, OtpSummaryEntry{Name: tripOneId, OnTimePerformance: 1})

	// Simulate being late at both stops on next day
	tripDate = time.Date(2023, 6, 9, 0, 0, 0, 0, time.Local)
	stopOneArrivalTime = 8*time.Hour + 40*time.Minute
	simulateStop(tripDate, stopOneArrivalTime, tripOneId, stopOneId, calculation, logger)
	stopTwoArrivalTime = 8*time.Hour + 55*time.Minute
	simulateStop(tripDate, stopTwoArrivalTime, tripOneId, stopTwoId, calculation, logger)

	otpByTrip = calculation.SummarizeOnTimePerformanceByTrip(8*time.Minute, logger)
	assert.EqualValues(t, "TripId", otpByTrip.GroupBy)
	assert.Contains(t, otpByTrip.OtpSummaries, OtpSummaryEntry{Name: tripOneId, OnTimePerformance: 0.5})
}

func createStaticFeed() (*model.GtfsStaticFeed, string, string, string, time.Time) {
	feed := model.GtfsStaticFeed{}
	weekdayServiceId := "wkdayService"
	weekdayCalendar := model.Calendar{Monday: model.ServiceIsAvailable,
		Tuesday:   model.ServiceIsAvailable,
		Wednesday: model.ServiceIsAvailable,
		Thursday:  model.ServiceIsAvailable,
		Friday:    model.ServiceIsAvailable,
		Saturday:  model.ServiceIsNotAvailable,
		Sunday:    model.ServiceIsNotAvailable,
		ServiceId: weekdayServiceId}
	feed.Calendar = append(feed.Calendar, weekdayCalendar)
	routeId := "route15"
	route15 := model.Route{Id: routeId}
	feed.Route = append(feed.Route, route15)
	tripOneId := "trip1"
	tripOne := model.Trip{Id: tripOneId, RouteId: routeId, ServiceId: weekdayServiceId}
	feed.Trip = append(feed.Trip, tripOne)
	stopOneId := "stop1"
	stopTwoId := "stop2"
	tripDate := time.Date(2023, 6, 8, 0, 0, 0, 0, time.Local)
	tripOneStopOneArrivalTime := model.NewArrivalTime(time.Time{}.Add(8*time.Hour + 30*time.Minute))
	tripOneStopOne := model.StopTime{TripId: tripOneId,
		StopId:      stopOneId,
		ArrivalTime: tripOneStopOneArrivalTime}
	tripOneStopTwoArrivalTime := model.NewArrivalTime(time.Time{}.Add(8*time.Hour + 45*time.Minute))
	tripOneStopTwo := model.StopTime{TripId: tripOneId,
		StopId:      stopTwoId,
		ArrivalTime: tripOneStopTwoArrivalTime}
	feed.StopTime = append(feed.StopTime, tripOneStopOne, tripOneStopTwo)
	return &feed, tripOneId, stopOneId, stopTwoId, tripDate
}

func simulateStop(tripDate time.Time, arrivalTime time.Duration, tripId string, stopId string, calculation *OtpCalculation, logger log.Interface) {
	simulateStopInner(tripDate, arrivalTime, tripId, stopId, calculation, model.StoppedAt, logger)
}

func simulateInTransitToStop(tripDate time.Time, arrivalTime time.Duration, tripId string, stopId string, calculation *OtpCalculation, logger log.Interface) {
	simulateStopInner(tripDate, arrivalTime, tripId, stopId, calculation, model.InTransitTo, logger)
}

func simulateStopInner(tripDate time.Time, arrivalTime time.Duration, tripId string, stopId string, calculation *OtpCalculation, currentStatus model.VehicleStopStatus, logger log.Interface) {
	positionTime := tripDate.Add(arrivalTime)
	position := InternalVehiclePosition{TripId: tripId, StopId: stopId, CurrentStatus: currentStatus, PositionTime: positionTime}
	positionData := make([]InternalVehiclePosition, 1)
	positionData[0] = position
	calculation.onNewPositionData(positionData, logger)
}

func TestOtpSummaryPrint(t *testing.T) {
	summary := OtpSummary{}
	summary.GroupBy = "TripId"
	summary.OtpSummaries = make([]OtpSummaryEntry, 2)
	summary.OtpSummaries[0] = OtpSummaryEntry{Name: "trip1", OnTimePerformance: 0.835}
	summary.OtpSummaries[1] = OtpSummaryEntry{Name: "trip2", OnTimePerformance: 0.993}
	fmt.Println(summary.PrettyPrint())
}

func TestOtpTimeRange(t *testing.T) {
	summary, _ := CalculateOtpForTimeRange("/home/sam/Downloads/rtd.db", time.Unix(1692661211, 0), time.Unix(1692662655, 0), 7*time.Minute, log.Info)
	fmt.Println(summary.PrettyPrint())
}
