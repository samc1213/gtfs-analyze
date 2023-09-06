package core

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/samc1213/gtfs-analyze/infra"
	"github.com/samc1213/gtfs-analyze/log"
	"github.com/samc1213/gtfs-analyze/model"
)

type InternalStopTime struct {
	StopId            string
	StopTime          time.Time
	ActualArrivalTime time.Time
}

type InternalTrip struct {
	Id                  string
	StopTimes           []InternalStopTime
	HaveStartedTracking bool
}

type EasyLookupFeed struct {
	CalendarByServiceId map[string]*model.Calendar
	StopTimesByTripId   map[string][]*model.StopTime
}

type OtpCalculation struct {
	TripsByDate    map[infra.Date]map[string]*InternalTrip
	Feed           *model.GtfsStaticFeed
	EasyLookupFeed *EasyLookupFeed
	Location       *time.Location
	Lock           sync.Mutex
}

type InternalVehiclePosition struct {
	TripId        string
	StopId        string
	CurrentStatus model.VehicleStopStatus
	PositionTime  time.Time
}

func CalculateOtpForTimeRange(sqliteDbPath string, startTime time.Time, endTime time.Time, onTimeThreshold time.Duration, logLevel log.Level) (*OtpSummary, error) {
	logger := log.New(logLevel)
	logger.Debug("Caluclating Otp for time range %s to %s with threshold %s", startTime.String(), endTime.String(), onTimeThreshold.String())

	db, err := InitializeSqliteDatabase(sqliteDbPath, logLevel)
	if err != nil {
		return nil, err
	}
	var vehiclePositions []model.VehiclePosition
	tx := db.Where("position_timestamp >= ? AND position_timestamp <= ?", startTime.Unix(), endTime.Unix()).Find(&vehiclePositions)
	if tx.Error != nil {
		return nil, tx.Error
	}

	logger.Debug("Found %d VechilePosition updates", len(vehiclePositions))

	// TODO: Need to update the static feed depending on the day we're looking at
	year, month, day := startTime.Date()
	feed, err := GetFeedOnDate(year, month, day, db)
	if err != nil {
		return nil, err
	}

	calculation, err := CreateOtpCalculation(feed)
	if err != nil {
		return nil, err
	}

	calculation.OnNewPositionData(vehiclePositions, logger)

	return calculation.SummarizeOnTimePerformanceByTrip(onTimeThreshold, startTime, endTime, logger), nil
}

func CreateOtpCalculation(feed *model.GtfsStaticFeed) (*OtpCalculation, error) {
	if feed == nil {
		return nil, errors.New("must provide a non-nil feed")
	}
	easyLookup := EasyLookupFeed{}
	easyLookup.CalendarByServiceId = make(map[string]*model.Calendar)
	for calendarIdx := range feed.Calendar {
		easyLookup.CalendarByServiceId[feed.Calendar[calendarIdx].ServiceId] = &feed.Calendar[calendarIdx]
	}
	easyLookup.StopTimesByTripId = make(map[string][]*model.StopTime)
	for stopTimeIdx := range feed.StopTime {
		tripId := feed.StopTime[stopTimeIdx].TripId
		easyLookup.StopTimesByTripId[tripId] = append(easyLookup.StopTimesByTripId[tripId], &feed.StopTime[stopTimeIdx])
	}
	// Sort stop times by order/stopsequence
	for tripId := range easyLookup.StopTimesByTripId {
		sort.Slice(easyLookup.StopTimesByTripId[tripId], func(i int, j int) bool {
			return easyLookup.StopTimesByTripId[tripId][i].StopSequence < easyLookup.StopTimesByTripId[tripId][j].StopSequence
		})
	}

	if len(feed.Agency) < 1 {
		return nil, errors.New("cannot lookup agency timezone")
	}

	location, err := time.LoadLocation(feed.Agency[0].Timezone)
	if err != nil {
		return nil, err
	}

	return &OtpCalculation{Feed: feed, EasyLookupFeed: &easyLookup, TripsByDate: make(map[infra.Date]map[string]*InternalTrip), Location: location}, nil
}

func (calculation *OtpCalculation) populateTripsForDate(date infra.Date, logger log.Interface) {
	for _, trip := range calculation.Feed.Trip {
		calendar, ok := calculation.EasyLookupFeed.CalendarByServiceId[trip.ServiceId]
		if !ok {
			logger.Warning("Cannot find calendar with service id %s, required for trip %s", trip.ServiceId, trip.Id)
			continue
		}
		if doesTripRunOnDate(date, calendar) {
			stopTimes, ok := calculation.EasyLookupFeed.StopTimesByTripId[trip.Id]
			if !ok {
				logger.Warning("Cannot find stop times for trip %s", trip.Id)
				continue
			}
			internalStopTimes := make([]InternalStopTime, len(stopTimes))
			for stopTimeIdx := range stopTimes {
				stopTime := stopTimes[stopTimeIdx]
				internalStopTimes[stopTimeIdx] = InternalStopTime{
					StopId:   stopTime.StopId,
					StopTime: time.Date(date.Year, date.Month, date.Day, 0, 0, 0, 0, calculation.Location).Add(time.Duration(*&stopTime.ArrivalTime) * time.Second),
				}
			}
			tripsForDate := calculation.TripsByDate[date]
			if tripsForDate == nil {
				calculation.TripsByDate[date] = make(map[string]*InternalTrip)
				tripsForDate = calculation.TripsByDate[date]
			}
			tripsForDate[trip.Id] = &InternalTrip{Id: trip.Id, StopTimes: internalStopTimes}
		}
	}
}

func doesTripRunOnDate(date infra.Date, calendar *model.Calendar) bool {
	switch date.Weekday() {
	case time.Monday:
		return calendar.Monday == model.ServiceIsAvailable
	case time.Tuesday:
		return calendar.Tuesday == model.ServiceIsAvailable
	case time.Wednesday:
		return calendar.Wednesday == model.ServiceIsAvailable
	case time.Thursday:
		return calendar.Thursday == model.ServiceIsAvailable
	case time.Friday:
		return calendar.Friday == model.ServiceIsAvailable
	case time.Saturday:
		return calendar.Saturday == model.ServiceIsAvailable
	case time.Sunday:
		return calendar.Sunday == model.ServiceIsAvailable
	}
	return false
}

type OtpSummaryEntry struct {
	Name              string // This value depends on the grouping logic. Could be a route id, trip id,
	OnTimePerformance float64
}

type GroupBy string

const (
	TripId GroupBy = "TripId"
)

type OtpSummary struct {
	GroupBy      GroupBy
	OtpSummaries []OtpSummaryEntry
}

func (summary *OtpSummary) PrettyPrint() string {
	sort.Slice(summary.OtpSummaries, func(i, j int) bool { return summary.OtpSummaries[i].Name < summary.OtpSummaries[j].Name })
	builder := strings.Builder{}
	writer := tabwriter.NewWriter(&builder, 0, 0, 0, ' ', tabwriter.Debug)
	fmt.Fprintf(writer, "%s\tOTP\n", summary.GroupBy)
	for _, summary := range summary.OtpSummaries {
		fmt.Fprintf(writer, "%s\t%.2f\n", summary.Name, summary.OnTimePerformance*100)
	}
	writer.Flush()
	return builder.String()
}

// Summarize on time performance by trip id, using the following calculation:
// `on-time performance = # of trip stops where service on time / # of total trip stops`,
// where "service on time" means that the service arrived at the stop within onTimeThreshold amount
// of time
func (calculation *OtpCalculation) SummarizeOnTimePerformanceByTrip(onTimeThreshold time.Duration, startTime time.Time, endTime time.Time, logger log.Interface) *OtpSummary {
	calculation.Lock.Lock()
	defer calculation.Lock.Unlock()

	numStopsOnTimeByTripId := make(map[string]int)
	numStopsTotalByTripId := make(map[string]int)

	for _, tripIdToTrip := range calculation.TripsByDate {
		for tripId, trip := range tripIdToTrip {
			// For now we do not include trips that have not been tracked at all (could be an issue with GTFS-RT)
			if trip.HaveStartedTracking {
				for _, stopTime := range trip.StopTimes {
					if stopTime.StopTime.After(startTime) && stopTime.StopTime.Before(endTime) {
						numStopsTotalByTripId[tripId] += 1

						if stopTime.ActualArrivalTime.Sub(stopTime.StopTime).Abs() < onTimeThreshold.Abs() {
							numStopsOnTimeByTripId[tripId] += 1
						}
					}
				}
			}
		}
	}

	summary := OtpSummary{}
	summary.GroupBy = TripId
	summary.OtpSummaries = make([]OtpSummaryEntry, len(numStopsTotalByTripId))
	summariesIdx := 0
	for tripId, numStopsTotal := range numStopsTotalByTripId {
		numStopsOnTime, ok := numStopsOnTimeByTripId[tripId]
		otp := 0.0
		if ok {
			otp = float64(numStopsOnTime) / float64(numStopsTotal)
		}
		summary.OtpSummaries[summariesIdx] = OtpSummaryEntry{Name: tripId, OnTimePerformance: otp}
		summariesIdx++
	}
	return &summary
}

func (calculation *OtpCalculation) onNewPositionData(positionData []InternalVehiclePosition, logger log.Interface) {
	calculation.Lock.Lock()
	defer calculation.Lock.Unlock()

	for _, position := range positionData {
		date := calculation.inferTripDate(&position)
		tripsByTripId, ok := calculation.TripsByDate[date]
		if !ok {
			calculation.populateTripsForDate(date, logger)
			tripsByTripId = calculation.TripsByDate[date]
		}
		trip, ok := tripsByTripId[position.TripId]
		if !ok {
			logger.Warning("No trip found for position data with trip id %s on date %s", position.TripId, date.String())
			continue
		}
		if position.CurrentStatus == model.StoppedAt {
			calculation.markArrivalTimeForAllStopsPriorAndIncluding(trip, position.StopId, position.PositionTime)
		}
		if position.CurrentStatus == model.IncomingAt || position.CurrentStatus == model.InTransitTo {
			calculation.markArrivalTimeForAllStopsPrior(trip, position.StopId, position.PositionTime)
		}
	}
}

// TODO: Utilize the static feed and the time of this VehiclePosition update to decide what date
// this trip is on. This current implementation works for trips before midnight, but doesn't work
// for anything after midnight UTC
func (calculation *OtpCalculation) inferTripDate(position *InternalVehiclePosition) infra.Date {
	y, m, d := position.PositionTime.Date()
	return infra.Date{y, m, d}
}

func (calculation *OtpCalculation) OnNewPositionData(positionData []model.VehiclePosition, logger log.Interface) {
	internalPositions := make([]InternalVehiclePosition, len(positionData))
	for i, position := range positionData {
		internalPositions[i] = InternalVehiclePosition{TripId: position.TripId, StopId: position.StopId, CurrentStatus: position.CurrentStatus, PositionTime: time.Unix(int64(position.PositionTimestamp), 0)}
	}
	calculation.onNewPositionData(internalPositions, logger)
}

func (calculation *OtpCalculation) markArrivalTimeForAllStopsPriorAndIncluding(trip *InternalTrip, stopId string, positionTime time.Time) error {
	return calculation.markArrivalTimeInternal(trip, stopId, positionTime, true)
}

func (calculation *OtpCalculation) markArrivalTimeForAllStopsPrior(trip *InternalTrip, stopId string, positionTime time.Time) error {
	return calculation.markArrivalTimeInternal(trip, stopId, positionTime, false)
}

func (calculation *OtpCalculation) markArrivalTimeInternal(trip *InternalTrip, stopId string, positionTime time.Time, includeThisStop bool) error {
	var providedStop *InternalStopTime
	var providedStopIdx int
	for stopIdx := range trip.StopTimes {
		stop := &trip.StopTimes[stopIdx]
		if stop.StopId == stopId {
			providedStop = stop
			providedStopIdx = stopIdx
			break
		}
	}
	if providedStop == nil {
		return fmt.Errorf("could not find stop with id %s on trip %s", stopId, trip.Id)
	}
	var startMarkTimeIdx int
	if includeThisStop {
		startMarkTimeIdx = providedStopIdx
	} else {
		startMarkTimeIdx = providedStopIdx - 1
	}
	for stopIdx := startMarkTimeIdx; stopIdx >= 0; stopIdx-- {
		stop := &trip.StopTimes[stopIdx]
		// Once we reach a previously-marked arrival, stop
		if !stop.ActualArrivalTime.IsZero() {
			break
		}
		stop.ActualArrivalTime = positionTime
		// Only run once for the first track
		if !trip.HaveStartedTracking {
			trip.HaveStartedTracking = true
			break
		}
	}
	return nil
}
