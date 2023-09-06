package core

import (
	"fmt"
	"testing"
	"time"

	"github.com/samc1213/gtfs-analyze/infra"
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
	tripDateInLocation := time.Date(tripDate.Year, tripDate.Month, tripDate.Day, 0, 0, 0, 0, calculation.Location)
	simulateInTransitToStop(tripDateInLocation, 8*time.Hour, tripOneId, stopOneId, calculation, logger)
	stopOne := &calculation.TripsByDate[tripDate][tripOneId].StopTimes[0]
	stopTwo := &calculation.TripsByDate[tripDate][tripOneId].StopTimes[1]
	assert.Equal(t, stopOneId, stopOne.StopId)
	assert.Equal(t, stopTwoId, stopTwo.StopId)
	assert.Equal(t, tripDateInLocation.Add(8*time.Hour+30*time.Minute), stopOne.StopTime)
	assert.Equal(t, tripDateInLocation.Add(8*time.Hour+45*time.Minute), stopTwo.StopTime)

	// Haven't gotten to stop one yet
	assert.Zero(t, stopOne.ActualArrivalTime)
	assert.Zero(t, stopTwo.ActualArrivalTime)

	// At stop one at 8:32
	stopOneArrivalTime := 8*time.Hour + 32*time.Minute
	simulateStop(tripDateInLocation, stopOneArrivalTime, tripOneId, stopOneId, calculation, logger)

	assert.Equal(t, tripDateInLocation.Add(stopOneArrivalTime), stopOne.ActualArrivalTime)
	assert.Zero(t, stopTwo.ActualArrivalTime)

	// At stop two at 8:44
	stopTwoArrivalTime := 8*time.Hour + 44*time.Minute
	simulateStop(tripDateInLocation, stopTwoArrivalTime, tripOneId, stopTwoId, calculation, logger)

	assert.Equal(t, tripDateInLocation.Add(stopOneArrivalTime), stopOne.ActualArrivalTime)
	assert.Equal(t, tripDateInLocation.Add(stopTwoArrivalTime), stopTwo.ActualArrivalTime)

	startTime := tripDateInLocation.Add(stopOneArrivalTime).Add(-time.Hour)
	endTime := tripDateInLocation.Add(stopTwoArrivalTime).Add(time.Hour)

	otpByTrip := calculation.SummarizeOnTimePerformanceByTrip(8*time.Minute, startTime, endTime, logger)
	assert.EqualValues(t, "TripId", otpByTrip.GroupBy)
	assert.Contains(t, otpByTrip.OtpSummaries, OtpSummaryEntry{Name: tripOneId, OnTimePerformance: 1})

	// Simulate being late at both stops on next day
	tripDate = infra.Date{Year: 2023, Month: 6, Day: 9}
	tripDateInLocation = time.Date(tripDate.Year, tripDate.Month, tripDate.Day, 0, 0, 0, 0, calculation.Location)

	stopOneArrivalTime = 8*time.Hour + 40*time.Minute
	simulateStop(tripDateInLocation, stopOneArrivalTime, tripOneId, stopOneId, calculation, logger)
	stopTwoArrivalTime = 8*time.Hour + 55*time.Minute
	simulateStop(tripDateInLocation, stopTwoArrivalTime, tripOneId, stopTwoId, calculation, logger)

	endTime = tripDateInLocation.Add(stopTwoArrivalTime).Add(time.Hour)
	otpByTrip = calculation.SummarizeOnTimePerformanceByTrip(8*time.Minute, startTime, endTime, logger)
	assert.EqualValues(t, "TripId", otpByTrip.GroupBy)
	assert.Contains(t, otpByTrip.OtpSummaries, OtpSummaryEntry{Name: tripOneId, OnTimePerformance: 0.5})
}

// If we filter the time range to only surround stop 1, the OTP should be wholly based on stop 1's timeliness (100%)
func TestOtpStopsOutsideTimeRange(t *testing.T) {
	feed, tripOneId, stopOneId, _, tripDate := createStaticFeed()
	calculation, err := CreateOtpCalculation(feed)
	tripDateInLocation := time.Date(tripDate.Year, tripDate.Month, tripDate.Day, 0, 0, 0, 0, calculation.Location)
	assert.NoError(t, err)
	logger := log.New(log.Info)
	stopOneTime := 8*time.Hour + 30*time.Minute
	// Stop stop one at 8:00
	simulateStop(tripDateInLocation, stopOneTime, tripOneId, stopOneId, calculation, logger)
	startTime := tripDateInLocation.Add(stopOneTime).Add(-5 * time.Minute)
	endTime := tripDateInLocation.Add(stopOneTime).Add(5 * time.Minute)
	otp := calculation.SummarizeOnTimePerformanceByTrip(7*time.Minute, startTime, endTime, logger)
	assert.EqualValues(t, 1, otp.OtpSummaries[0].OnTimePerformance)
}

func createStaticFeed() (*model.GtfsStaticFeed, string, string, string, infra.Date) {
	feed := model.GtfsStaticFeed{}
	timeZone := "America/Denver"
	feed.Agency = append(feed.Agency, model.Agency{Timezone: timeZone})
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
	tripDate := infra.Date{Year: 2023, Month: 6, Day: 8}
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

// Integration Test - do not run
// func TestOtpTimeRange(t *testing.T) {
// 	summary, _ := CalculateOtpForTimeRange("/home/sam/Downloads/rtd.db", time.Unix(1692661211, 0), time.Unix(1692662655, 0), 7*time.Minute, log.Info)
// 	fmt.Println(summary.PrettyPrint())
// }
