package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/samc1213/gtfs-analyze/gtfs_realtime"
	"github.com/samc1213/gtfs-analyze/model"
	"google.golang.org/protobuf/proto"
)

// Upper bound on a GTFS-RT response body, to protect against a misbehaving server
const maxRtFeedBytes = 64 << 20

// FetchRtFeed downloads and decodes a GTFS-RT FeedMessage from url.
func FetchRtFeed(ctx context.Context, client *http.Client, url string) (*gtfs_realtime.FeedMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}

	protoBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxRtFeedBytes))
	if err != nil {
		return nil, fmt.Errorf("GET %s: reading body: %w", url, err)
	}
	return ParseRtFeed(protoBytes)
}

// ParseRtFeed decodes GTFS-RT protobuf bytes into a FeedMessage.
func ParseRtFeed(protoBytes []byte) (*gtfs_realtime.FeedMessage, error) {
	var message gtfs_realtime.FeedMessage
	if err := proto.Unmarshal(protoBytes, &message); err != nil {
		return nil, fmt.Errorf("decoding GTFS-RT feed: %w", err)
	}
	if message.Header == nil {
		return nil, errors.New("GTFS-RT feed has no header")
	}
	return &message, nil
}

func parseRtStartDate(startDate string) (time.Time, error) {
	if startDate == "" {
		return time.Time{}, nil
	}
	return time.Parse("20060102", startDate)
}

func parseRtStartTime(startTime string) (model.ArrivalDepartureTime, error) {
	var result model.ArrivalDepartureTime
	err := result.ConvertFromCsv(startTime)
	return result, err
}

func convertVehiclePosition(entityId string, vehicle *gtfs_realtime.VehiclePosition, messageTimestamp uint64) (model.VehiclePosition, error) {
	var result model.VehiclePosition
	result.EntityId = entityId
	result.MessageTimestamp = messageTimestamp

	trip := vehicle.GetTrip()
	result.TripId = trip.GetTripId()
	result.RouteId = trip.GetRouteId()
	result.DirectionId = model.DirectionId(trip.GetDirectionId())
	startTime, err := parseRtStartTime(trip.GetStartTime())
	if err != nil {
		return result, err
	}
	result.StartTime = startTime
	startDate, err := parseRtStartDate(trip.GetStartDate())
	if err != nil {
		return result, err
	}
	result.StartDate = startDate
	result.ScheduleRelationship = model.ScheduleRelationship(trip.GetScheduleRelationship().Number())

	descriptor := vehicle.GetVehicle()
	result.VehicleId = descriptor.GetId()
	result.VehicleLabel = descriptor.GetLabel()
	result.LicensePlate = descriptor.GetLicensePlate()
	result.WheelchairAccessible = model.WheelchairAccessible(descriptor.GetWheelchairAccessible().Number())

	position := vehicle.GetPosition()
	result.Latitude = float64(position.GetLatitude())
	result.Longitude = float64(position.GetLongitude())
	result.Bearing = float64(position.GetBearing())
	result.Odometer = position.GetOdometer()
	result.Speed = float64(position.GetSpeed())

	result.CurrentStopSequence = int32(vehicle.GetCurrentStopSequence())
	result.StopId = vehicle.GetStopId()
	result.CurrentStatus = model.VehicleStopStatus(vehicle.GetCurrentStatus().Number())
	result.PositionTimestamp = vehicle.GetTimestamp()
	result.CongestionLevel = model.CongestionLevel(vehicle.GetCongestionLevel().Number())
	result.OccupancyStatus = model.OccupancyStatus(vehicle.GetOccupancyStatus().Number())
	result.OccupancyPercentage = vehicle.GetOccupancyPercentage()

	return result, nil
}

func convertTripUpdate(entityId string, tripUpdate *gtfs_realtime.TripUpdate, messageTimestamp uint64) (model.TripUpdate, []model.StopTimeUpdate, error) {
	var result model.TripUpdate
	result.EntityId = entityId
	result.MessageTimestamp = messageTimestamp

	trip := tripUpdate.GetTrip()
	result.TripId = trip.GetTripId()
	result.RouteId = trip.GetRouteId()
	if trip != nil {
		result.DirectionId = trip.DirectionId
	}
	startTime, err := parseRtStartTime(trip.GetStartTime())
	if err != nil {
		return result, nil, err
	}
	result.StartTime = startTime
	startDate, err := parseRtStartDate(trip.GetStartDate())
	if err != nil {
		return result, nil, err
	}
	result.StartDate = startDate
	result.ScheduleRelationship = model.ScheduleRelationship(trip.GetScheduleRelationship().Number())

	result.VehicleId = tripUpdate.GetVehicle().GetId()
	result.VehicleLabel = tripUpdate.GetVehicle().GetLabel()
	result.Timestamp = tripUpdate.Timestamp
	result.Delay = tripUpdate.Delay

	stopTimeUpdates := make([]model.StopTimeUpdate, len(tripUpdate.StopTimeUpdate))
	for i, stu := range tripUpdate.StopTimeUpdate {
		arrivalTime, arrivalDelay, arrivalUncertainty := stopTimeEventFields(stu.GetArrival())
		departureTime, departureDelay, departureUncertainty := stopTimeEventFields(stu.GetDeparture())
		stopTimeUpdates[i] = model.StopTimeUpdate{
			MessageTimestamp:     messageTimestamp,
			TripId:               result.TripId,
			StartDate:            result.StartDate,
			RouteId:              result.RouteId,
			StopSequence:         stu.StopSequence,
			StopId:               stu.GetStopId(),
			ArrivalTime:          arrivalTime,
			ArrivalDelay:         arrivalDelay,
			ArrivalUncertainty:   arrivalUncertainty,
			DepartureTime:        departureTime,
			DepartureDelay:       departureDelay,
			DepartureUncertainty: departureUncertainty,
			ScheduleRelationship: model.StopTimeScheduleRelationship(stu.GetScheduleRelationship().Number()),
		}
	}

	return result, stopTimeUpdates, nil
}

// stopTimeEventFields returns the optional fields of a StopTimeEvent, all nil if the event is absent
func stopTimeEventFields(event *gtfs_realtime.TripUpdate_StopTimeEvent) (eventTime *int64, delay *int32, uncertainty *int32) {
	if event == nil {
		return nil, nil, nil
	}
	return event.Time, event.Delay, event.Uncertainty
}

// convertAlert converts an alert, including a hash of its content (used to detect changes).
func convertAlert(entityId string, alert *gtfs_realtime.Alert, messageTimestamp uint64) (model.Alert, error) {
	var result model.Alert
	contentBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(alert)
	if err != nil {
		return result, err
	}
	hash := sha256.Sum256(contentBytes)

	result.EntityId = entityId
	result.MessageTimestamp = messageTimestamp
	result.ContentHash = hex.EncodeToString(hash[:])
	result.Cause = model.AlertCause(alert.GetCause().Number())
	result.Effect = model.AlertEffect(alert.GetEffect().Number())
	result.SeverityLevel = model.AlertSeverityLevel(alert.GetSeverityLevel().Number())
	result.HeaderText = pickTranslation(alert.GetHeaderText())
	result.DescriptionText = pickTranslation(alert.GetDescriptionText())
	result.Url = pickTranslation(alert.GetUrl())
	result.CauseDetail = pickTranslation(alert.GetCauseDetail())
	result.EffectDetail = pickTranslation(alert.GetEffectDetail())

	for _, period := range alert.ActivePeriod {
		result.ActivePeriods = append(result.ActivePeriods, model.AlertActivePeriod{Start: period.Start, End: period.End})
	}
	for _, informed := range alert.InformedEntity {
		result.InformedEntities = append(result.InformedEntities, model.AlertInformedEntity{
			AgencyId:    informed.GetAgencyId(),
			RouteId:     informed.GetRouteId(),
			RouteType:   informed.RouteType,
			DirectionId: informed.DirectionId,
			TripId:      informed.GetTrip().GetTripId(),
			StopId:      informed.GetStopId(),
		})
	}
	return result, nil
}

// pickTranslation returns the English translation if there is one, else the first translation.
func pickTranslation(translated *gtfs_realtime.TranslatedString) string {
	translations := translated.GetTranslation()
	if len(translations) == 0 {
		return ""
	}
	for _, translation := range translations {
		language := strings.ToLower(translation.GetLanguage())
		if language == "en" || strings.HasPrefix(language, "en-") {
			return translation.GetText()
		}
	}
	return translations[0].GetText()
}
