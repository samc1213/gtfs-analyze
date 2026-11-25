package model

import "time"

// See https://gtfs.org/realtime/reference for reference.
//
// Realtime tables are append-only histories. Every row has its own auto-increment
// Id, so the same feed entity can appear many times (one row per observed change).
// MessageTimestamp is the GTFS-RT FeedHeader timestamp of the snapshot in which the
// change was first observed.

type ScheduleRelationship int8

const (
	Scheduled ScheduleRelationship = iota
	Added
	Unscheduled
	Canceled
	Duplicated
	Deleted
)

type StopTimeScheduleRelationship int8

const (
	StopScheduled StopTimeScheduleRelationship = iota
	StopSkipped
	StopNoData
	StopUnscheduled
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

// Values match the GTFS-RT Alert.Cause enum
type AlertCause int8

// Values match the GTFS-RT Alert.Effect enum
type AlertEffect int8

// Values match the GTFS-RT Alert.SeverityLevel enum
type AlertSeverityLevel int8

type VehiclePosition struct {
	Id uint64 `gorm:"primaryKey;autoIncrement"`
	// Feed-unique id of the entity this update came from. Not unique in this table.
	EntityId         string `gorm:"index;not null"`
	MessageTimestamp uint64 `gorm:"index"`
	// Start Trip Object
	TripId      string // No foreign key to the trips.txt file, since GTFS-RT and GTFS static are distinct feeds and this FK could fail
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
	PositionTimestamp   uint64 `gorm:"index"`
	CongestionLevel     CongestionLevel
	OccupancyStatus     OccupancyStatus
	OccupancyPercentage uint32
	// Omit multicarriagedetails since it is many to one
}

// TripUpdate holds the trip-level fields of a GTFS-RT TripUpdate. A row is written
// when a trip is first seen or when any of its trip-level fields change.
// The per-stop predictions are stored separately in StopTimeUpdate.
type TripUpdate struct {
	Id               uint64 `gorm:"primaryKey;autoIncrement"`
	EntityId         string `gorm:"not null"`
	MessageTimestamp uint64 `gorm:"index"`
	// Start Trip Object
	TripId               string  `gorm:"index:idx_trip_updates_trip"`
	RouteId              string  `gorm:"default:null"`
	DirectionId          *uint32 // nullable: not all feeds populate direction
	StartTime            ArrivalDepartureTime
	StartDate            time.Time `gorm:"index:idx_trip_updates_trip"`
	ScheduleRelationship ScheduleRelationship
	// End Trip Object
	VehicleId    string  `gorm:"default:null"`
	VehicleLabel string  `gorm:"default:null"`
	Timestamp    *uint64 // When the vehicle's realtime progress was measured, if provided
	Delay        *int32  // Trip-level delay in seconds, if provided
}

// StopTimeUpdate is a single stop's prediction from a GTFS-RT TripUpdate. A row is
// written when a prediction is first seen or when any of its values change, so the
// rows for a (trip, start date, stop sequence) form a history of the prediction.
type StopTimeUpdate struct {
	Id               uint64 `gorm:"primaryKey;autoIncrement"`
	MessageTimestamp uint64 `gorm:"index"`
	// Trip identifiers, denormalized from the parent TripUpdate for easy querying
	TripId    string    `gorm:"index:idx_stop_time_updates_trip"`
	StartDate time.Time `gorm:"index:idx_stop_time_updates_trip"`
	RouteId   string    `gorm:"default:null"`
	// Stop identifiers. At least one is set.
	StopSequence *uint32
	StopId       string `gorm:"default:null"`
	// Arrival/departure StopTimeEvents. Times are POSIX seconds, delays are seconds.
	ArrivalTime          *int64
	ArrivalDelay         *int32
	ArrivalUncertainty   *int32
	DepartureTime        *int64
	DepartureDelay       *int32
	DepartureUncertainty *int32
	ScheduleRelationship StopTimeScheduleRelationship
}

// Alert is one version of a GTFS-RT Alert. A new row is written when an alert is
// first seen or when any of its content changes (detected via ContentHash).
// Translated strings are stored in a single language: English if present, else the first translation.
type Alert struct {
	Id               uint64 `gorm:"primaryKey;autoIncrement"`
	EntityId         string `gorm:"index;not null"`
	MessageTimestamp uint64 `gorm:"index"`
	// SHA-256 of the deterministic protobuf encoding of the alert
	ContentHash      string `gorm:"not null"`
	Cause            AlertCause
	Effect           AlertEffect
	SeverityLevel    AlertSeverityLevel
	HeaderText       string                `gorm:"default:null"`
	DescriptionText  string                `gorm:"default:null"`
	Url              string                `gorm:"default:null"`
	CauseDetail      string                `gorm:"default:null"`
	EffectDetail     string                `gorm:"default:null"`
	ActivePeriods    []AlertActivePeriod   `gorm:"foreignKey:AlertId"`
	InformedEntities []AlertInformedEntity `gorm:"foreignKey:AlertId"`
}

type AlertActivePeriod struct {
	Id      uint64  `gorm:"primaryKey;autoIncrement"`
	AlertId uint64  `gorm:"index;not null"`
	Start   *uint64 // POSIX seconds; null means open-ended
	End     *uint64 // POSIX seconds; null means open-ended
}

type AlertInformedEntity struct {
	Id          uint64 `gorm:"primaryKey;autoIncrement"`
	AlertId     uint64 `gorm:"index;not null"`
	AgencyId    string `gorm:"default:null"`
	RouteId     string `gorm:"default:null"`
	RouteType   *int32
	DirectionId *uint32
	TripId      string `gorm:"default:null"`
	StopId      string `gorm:"default:null"`
}
