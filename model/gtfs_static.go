package model

import (
	"errors"
	"regexp"
	"strconv"
	"time"
)

// See https://gtfs.org/schedule/reference for reference
// This model is meant to be a direct copy of the GTFS reference schema,
// except that each entity has a Version tag. This allows us to keep multiple versions
// of a given GTFS schema in the same SQL tables

type RouteType int8

const (
	Tram       RouteType = 0
	Subway     RouteType = 1
	Rail       RouteType = 2
	Bus        RouteType = 3
	Ferry      RouteType = 4
	Cable      RouteType = 5
	AerialLift RouteType = 6
	Funicular  RouteType = 7
	Trolleybus RouteType = 11
	Monorail   RouteType = 12
)

type LocationType int8

const (
	StopLocationType LocationType = 0
	Station          LocationType = 1
	EntranceExit     LocationType = 2
	GenericNode      LocationType = 3
	BoardingArea     LocationType = 4
)

type ContinuousPickupDropoff int8

const (
	ContinuousStopping       ContinuousPickupDropoff = 0
	NoContinuousStopping     ContinuousPickupDropoff = 1
	MustPhoneAgency          ContinuousPickupDropoff = 2
	MustCoordinateWithDriver ContinuousPickupDropoff = 3
)

type DirectionId int8

const (
	OutboundTravel DirectionId = 0
	InboundTravel  DirectionId = 1
)

type WheelchairAccessible int8

const (
	WheelchairNoInfo     WheelchairAccessible = 0
	AtLeastOneWheelchair WheelchairAccessible = 1
	NoWheelchairs        WheelchairAccessible = 2
)

type BikesAllowed int8

const (
	BikesNoInfo    BikesAllowed = 0
	AtLeastOneBike BikesAllowed = 1
	NoBikes        BikesAllowed = 2
)

type PickupDropoffType int8

const (
	RegularPickupDropoff              PickupDropoffType = 0
	NoPickupDropoff                   PickupDropoffType = 1
	PhoneAgencyPickupDropoff          PickupDropoffType = 2
	CoordinateWithDriverPickupDropoff PickupDropoffType = 3
)

type Agency struct {
	Version  string    `gorm:"primaryKey;not null;default:null"`
	FeedInfo *FeedInfo `gorm:"foreignKey:Version;belongsTo"`
	Id       string    `csv_parse:"agency_id" gorm:"primaryKey;not null;default:null"`
	Name     string    `csv_parse:"agency_name" gorm:"default:null"`
	Url      string    `csv_parse:"agency_url" gorm:"default:null"`
	Timezone string    `csv_parse:"agency_timezone" gorm:"default:null"`
	Language string    `csv_parse:"agency_lang" gorm:"default:null"`
	Phone    string    `csv_parse:"agency_phone" gorm:"default:null"`
	FareUrl  string    `csv_parse:"agency_fare_url" gorm:"default:null"`
	Email    string    `csv_parse:"agency_email" gorm:"default:null"`
}

type Stop struct {
	Version            string       `gorm:"primaryKey;not null;default:null"`
	FeedInfo           *FeedInfo    `gorm:"foreignKey:Version;belongsTo"`
	Id                 string       `csv_parse:"stop_id" gorm:"primaryKey;not null;default:null"`
	Code               string       `csv_parse:"stop_code" gorm:"default:null"`
	Name               string       `csv_parse:"stop_name" gorm:"default:null"`
	TtsName            string       `csv_parse:"tts_stop_name" gorm:"default:null"`
	Description        string       `csv_parse:"stop_desc" gorm:"default:null"`
	Latitude           float64      `csv_parse:"stop_lat"` // Use 64 bits to provide better native support for PostGIS, etc. However 32 bits provides plenty of precision
	Longitude          float64      `csv_parse:"stop_lon"` // Use 64 bits to provide better native support for PostGIS, etc. However 32 bits provides plenty of precision
	ZoneId             string       `csv_parse:"zone_id" gorm:"default:null"`
	Url                string       `csv_parse:"stop_url" gorm:"default:null"`
	LocationType       LocationType `csv_parse:"location_type"`
	ParentStationId    string       `csv_parse:"parent_station" gorm:"default:null"`
	ParentStation      *Stop        `gorm:"foreignKey:ParentStationId"`
	Timezone           string       `csv_parse:"stop_timezone" gorm:"default:null"`
	WheelchairBoarding int8         `csv_parse:"wheelchair_boarding"`
}

type Route struct {
	Version           string                  `gorm:"primaryKey;not null;default:null"`
	FeedInfo          *FeedInfo               `gorm:"foreignKey:Version;belongsTo"`
	Id                string                  `csv_parse:"route_id" gorm:"primaryKey;not null;default:null"`
	AgencyId          string                  `csv_parse:"agency_id" gorm:"default:null"`
	ShortName         string                  `csv_parse:"route_short_name" gorm:"default:null"`
	LongName          string                  `csv_parse:"route_long_name" gorm:"default:null"`
	Description       string                  `csv_parse:"route_desc" gorm:"default:null"`
	Type              RouteType               `csv_parse:"route_type"`
	Url               string                  `csv_parse:"route_url" gorm:"default:null"`
	Color             string                  `csv_parse:"route_color" gorm:"default:null"`
	TextColor         string                  `csv_parse:"route_text_color" gorm:"default:null"`
	SortOrder         int32                   `csv_parse:"route_sort_order"`
	ContinuousPickup  ContinuousPickupDropoff `csv_parse:"continuous_pickup"`
	ContinuousDropoff ContinuousPickupDropoff `csv_parse:"continuous_drop_off"`
	NetworkId         string                  `csv_parse:"network_id"`
}

type Trip struct {
	Version              string    `gorm:"primaryKey;not null;default:null"`
	FeedInfo             *FeedInfo `gorm:"foreignKey:Version;belongsTo"`
	Id                   string    `csv_parse:"trip_id" gorm:"primaryKey;not null;default:null"`
	RouteId              string    `csv_parse:"route_id"`
	Route                *Route
	ServiceId            string               `csv_parse:"service_id" gorm:"default: null"`
	Headsign             string               `csv_parse:"trip_headsign" gorm:"default: null"`
	ShortName            string               `csv_parse:"trip_short_name" gorm:"default: null"`
	DirectionId          DirectionId          `csv_parse:"direction_id"`
	BlockId              string               `csv_parse:"block_id"`
	ShapeId              string               `csv_parse:"shape_id"`
	WheelchairAccessible WheelchairAccessible `csv_parse:"wheelchair_accessible"`
	BikesAllowed         BikesAllowed         `csv_parse:"bikes_allowed"`
}

// Store arrival and departure times as "seconds after midnight", to handle
// cases where
type ArrivalDepartureTime int

const HOURS_TO_MINUTES = 60
const MINUTES_TO_SECONDS = 60

// ArrivalDepartureTime can be greater than 24:00:00, in cases where the time is
// after midnight on the date in question. Since it's hard to store time like this,
// we convert the time to the time in seconds after migdnight
func (custom *ArrivalDepartureTime) ConvertFromCsv(input string) error {
	if input == "" {
		return nil
	}
	re2 := regexp.MustCompile(`(?P<hour>[0-9]{2})\:(?P<minute>[0-9]{2})\:(?P<second>[0-9]{2})`)
	matches := re2.FindStringSubmatch(input)
	if len(matches) != 4 {
		return errors.New("Invalid ArrivalDepartureTime " + input)
	}
	var hour int64
	var minute int64
	var second int64
	hour, err := strconv.ParseInt(matches[1], 10, 32)
	if err != nil {
		return errors.New("Invalid ArrivalDepartureTime " + input)
	}
	minute, err = strconv.ParseInt(matches[2], 10, 32)
	if err != nil {
		return errors.New("Invalid ArrivalDepartureTime " + input)
	}
	second, err = strconv.ParseInt(matches[3], 10, 32)
	if err != nil {
		return errors.New("Invalid ArrivalDepartureTime " + input)
	}
	*custom = ArrivalDepartureTime(hour*HOURS_TO_MINUTES*MINUTES_TO_SECONDS + minute*MINUTES_TO_SECONDS + second)

	return nil
}

func NewArrivalTime(date time.Time) ArrivalDepartureTime {
	year, month, day := date.Date()
	var baseTime time.Time
	// If someone just provided a time of day, subtract from 0-0-0
	if year == 0 && month == 0 && day == 0 {
		baseTime = time.Time{}
	} else {
		baseTime = time.Date(year, month, day, 0, 0, 0, 0, date.Location())
	}
	return ArrivalDepartureTime(date.Sub(baseTime).Seconds())
}

type StopTime struct {
	Version          string                  `gorm:"primaryKey;not null;default:null"`
	FeedInfo         *FeedInfo               `gorm:"foreignKey:Version;belongsTo"`
	TripId           string                  `csv_parse:"trip_id" gorm:"primaryKey;not null;default:null"`
	Trip             *Trip                   `gorm:"foreignKey:trip_id"`
	ArrivalTime      ArrivalDepartureTime    `csv_parse:"arrival_time" gorm:"default:null"`
	DepartureTime    ArrivalDepartureTime    `csv_parse:"departure_time" gorm:"default:null"`
	StopId           string                  `csv_parse:"stop_id" gorm:"not null;default:null"`
	Stop             *Stop                   `gorm:"foreignKey:stop_id"`
	StopSequence     int32                   `csv_parse:"stop_sequence" gorm:"primaryKey;not null;default:null"`
	StopHeadsign     string                  `csv_parse:"stop_headsign" gorm:"default:null"`
	PickupType       PickupDropoffType       `csv_parse:"pickup_type;default:0"`
	DropoffType      PickupDropoffType       `csv_parse:"dropoff_type;default:0"`
	ContinuousPickup ContinuousPickupDropoff `csv_parse:"continuous_pickup"`
}

type ServiceAvailable int8

const (
	ServiceIsNotAvailable ServiceAvailable = 0
	ServiceIsAvailable    ServiceAvailable = 1
)

type Calendar struct {
	Version   string           `gorm:"primaryKey;not null;default:null"`
	FeedInfo  *FeedInfo        `gorm:"foreignKey:Version;belongsTo"`
	ServiceId string           `csv_parse:"service_id" gorm:"primaryKey;not null;default:null"`
	Monday    ServiceAvailable `csv_parse:"monday" gorm:"not null"`
	Tuesday   ServiceAvailable `csv_parse:"tuesday" gorm:"not null"`
	Wednesday ServiceAvailable `csv_parse:"wednesday" gorm:"not null"`
	Thursday  ServiceAvailable `csv_parse:"thursday" gorm:"not null"`
	Friday    ServiceAvailable `csv_parse:"friday" gorm:"not null"`
	Saturday  ServiceAvailable `csv_parse:"saturday" gorm:"not null"`
	Sunday    ServiceAvailable `csv_parse:"sunday" gorm:"not null"`
	StartDate time.Time        `csv_parse:"start_date;timeLayout:20060102" gorm:"not null;default:null"`
	EndDate   time.Time        `csv_parse:"end_date;timeLayout:20060102" gorm:"not null;default:null"`
}

type FeedInfo struct {
	PublisherName   string    `csv_parse:"feed_publisher_name" gorm:"default:null"`
	PublisherUrl    string    `csv_parse:"feed_publisher_url" gorm:"default:null"`
	Language        string    `csv_parse:"feed_lang" gorm:"default:null"`
	DefaultLanguage string    `csv_parse:"default_lang" gorm:"default:null"`
	StartDate       time.Time `csv_parse:"feed_start_date;timeLayout:20060102" gorm:"default:null"`
	EndDate         time.Time `csv_parse:"feed_end_date;timeLayout:20060102" gorm:"default:null"`
	// While Version is optional, and the entire FeedInfo file is optional, this application
	// will generate a version if it is not included in the feed, in order to track changes to
	// the GTFS feed and save all historical versions throughout time
	Version      string    `csv_parse:"feed_version" gorm:"unique;primaryKey;not null;default:null"`
	DownloadTime time.Time `gorm:"default:null;not null"`
	ContactEmail string    `csv_parse:"feed_contact_email" gorm:"default:null"`
	ContactUrl   string    `csv_parse:"feed_contact_url" gorm:"default:null"`
}

type GtfsStaticFeed struct {
	Agency   []Agency
	Stop     []Stop
	Route    []Route
	Trip     []Trip
	StopTime []StopTime
	Calendar []Calendar
	FeedInfo FeedInfo
}

func GetAllModels() []interface{} {
	return []interface{}{
		&Agency{},
		&Stop{},
		&Route{},
		&Trip{},
		&StopTime{},
		&Calendar{},
		&FeedInfo{},
		&VehiclePosition{},
	}
}
