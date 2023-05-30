package core

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/samc1213/gtfs-analyze/gtfs_realtime"
	"github.com/samc1213/gtfs-analyze/model"
	"google.golang.org/protobuf/proto"
)

func ParseRtGtfsFromUrl(vehiclePositionUrl string) ([]model.VehiclePosition, error) {
	client := http.Client{}
	protoBytes, err := parseProtoFromUrl(&client, vehiclePositionUrl)
	if err != nil {
		return nil, err
	}
	vehiclePosition, err := convertVehiclePositionProtoToModel(protoBytes)
	if err != nil {
		return nil, err
	}

	return vehiclePosition, nil
}

func convertVehiclePositionProtoToModel(protoBytes []byte) ([]model.VehiclePosition, error) {
	var vehiclePositionProto gtfs_realtime.FeedMessage
	proto.Unmarshal(protoBytes, &vehiclePositionProto)

	vehiclePositions := make([]model.VehiclePosition, len(vehiclePositionProto.Entity))

	for i, entity := range vehiclePositionProto.Entity {
		if entity.Vehicle == nil {
			return nil, errors.New("vehicle should not be null for vehicle position update")
		}
		vehiclePosition := &vehiclePositions[i]
		vehiclePosition.MessageTimestamp = *vehiclePositionProto.Header.Timestamp

		vehicle := entity.Vehicle

		vehiclePosition.Id = entity.GetId()
		vehiclePosition.TripId = vehicle.Trip.GetTripId()
		vehiclePosition.RouteId = vehicle.Trip.GetRouteId()
		vehiclePosition.DirectionId = model.DirectionId(vehicle.Trip.GetDirectionId())
		vehiclePosition.StartTime.ConvertFromCsv(vehicle.Trip.GetStartTime())
		if vehicle.Trip.GetStartDate() != "" {
			startDate, err := time.Parse("20060102", vehicle.Trip.GetStartDate())
			if err != nil {
				return nil, err
			}
			vehiclePosition.StartDate = startDate
		}
		vehiclePosition.ScheduleRelationship = model.ScheduleRelationship(vehicle.Trip.GetScheduleRelationship().Number())

		vehiclePosition.VehicleId = vehicle.Vehicle.GetId()
		vehiclePosition.VehicleLabel = vehicle.Vehicle.GetLabel()
		vehiclePosition.LicensePlate = vehicle.Vehicle.GetLicensePlate()
		vehiclePosition.WheelchairAccessible = model.WheelchairAccessible(vehicle.Vehicle.GetWheelchairAccessible().Number())

		vehiclePosition.Latitude = float64(vehicle.Position.GetLatitude())
		vehiclePosition.Longitude = float64(vehicle.Position.GetLongitude())
		vehiclePosition.Bearing = float64(vehicle.Position.GetBearing())
		vehiclePosition.Odometer = vehicle.Position.GetOdometer()
		vehiclePosition.Speed = float64(vehicle.Position.GetSpeed())

		vehiclePosition.CurrentStopSequence = int32(vehicle.GetCurrentStopSequence())
		vehiclePosition.StopId = vehicle.GetStopId()
		vehiclePosition.CurrentStatus = model.VehicleStopStatus(vehicle.GetCurrentStatus().Number())
		vehiclePosition.PositionTimestamp = vehicle.GetTimestamp()
		vehiclePosition.CongestionLevel = model.CongestionLevel(vehicle.GetCongestionLevel().Number())
		vehiclePosition.OccupancyStatus = model.OccupancyStatus(vehicle.GetOccupancyStatus())
		vehiclePosition.OccupancyPercentage = vehicle.GetOccupancyPercentage()
	}

	return vehiclePositions, nil
}

func parseProtoFromUrl(client *http.Client, url string) ([]byte, error) {
	protoResp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer protoResp.Body.Close()

	protoBytes, err := io.ReadAll(protoResp.Body)
	if err != nil {
		return nil, err
	}

	return protoBytes, nil
}
