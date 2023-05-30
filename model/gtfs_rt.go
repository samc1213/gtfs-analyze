package model

import "time"

type ScheduleRelationship int8

const (
	Scheduled ScheduleRelationship = iota
	Added
	Unscheduled
	Canceled
	Duplicated
	Deleted
)

type VehicleStopStatus int8

const (
	IncomingAt VehicleStopStatus = iota
	StoppedAt
	InTransitTo
)

type CongestionLevel int8

const (
	UnknownCongestionLevel CongestionLevel = iota
	RunningSmoothly
	StopAndGo
	Congestion
	SevereCongestion
)

type OccupancyStatus int8

const (
	EmptyOccupancy OccupancyStatus = iota
	ManySeatsAvailable
	FewSeatsAvailable
	StandingRoomOnly
	CrushedStandingRoomOnly
	FullOccupancy
	NotAcceptingPassengers
	NoDataAvailable
	NotBoardable
)

type VehiclePosition struct {
	// Feed-unique id for this update
	Id               string `gorm:"primaryKey;not null;default:null"`
	MessageTimestamp uint64 `gorm:"index"`
	// Start Trip Object
	TripId      string //N No foreign key to the trips.txt file, since GTFS-RT and GTFS static are distinct feeds and this FK could fail
	RouteId     string
	DirectionId DirectionId
	// can be > 24 hours, so represent in seconds since midnight
	StartTime            ArrivalDepartureTime
	StartDate            time.Time
	ScheduleRelationship ScheduleRelationship
	// End Trip Object
	// Start VehicleDescriptor Object
	VehicleId            string `gorm:"default:null"`
	VehicleLabel         string `gorm:"default:null"`
	LicensePlate         string `gorm:"default:null"`
	WheelchairAccessible WheelchairAccessible
	// End VehicleDescriptor Object
	// Start Position Object
	Latitude  float64
	Longitude float64
	Bearing   float64
	Odometer  float64
	Speed     float64
	// End Position Object
	CurrentStopSequence int32
	StopId              string
	CurrentStatus       VehicleStopStatus
	PositionTimestamp   uint64
	CongestionLevel     CongestionLevel
	OccupancyStatus     OccupancyStatus
	OccupancyPercentage uint32
	// Omit multicarriagedetails since it is many to one
}
