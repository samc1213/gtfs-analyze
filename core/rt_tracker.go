package core

import (
	"fmt"

	"github.com/samc1213/gtfs-analyze/gtfs_realtime"
	"github.com/samc1213/gtfs-analyze/model"
)

// RtFeedTracker turns successive GTFS-RT snapshots from a single feed URL into the
// set of rows that changed, so the database stores a history of changes rather than
// a copy of every snapshot.
//
// The tracker remembers the last committed snapshot in memory only. After a restart,
// the first snapshot is stored in full again.
//
// Usage: call Diff on each new FeedMessage, write the returned changes to the
// database, and call Commit only once the write succeeded. If the write fails, the
// same changes will be detected again on the next snapshot.
type RtFeedTracker struct {
	lastMessageTimestamp uint64
	state                rtState
}

type rtState struct {
	vehicles map[string]model.VehiclePosition // keyed by entity id
	trips    map[rtTripKey]model.TripUpdate
	stops    map[rtStopKey]model.StopTimeUpdate
	alerts   map[string]string // entity id -> content hash
}

type rtTripKey struct {
	tripId    string
	routeId   string
	startDate int64
	startTime model.ArrivalDepartureTime
}

type rtStopKey struct {
	trip         rtTripKey
	stopSequence int64 // -1 when the feed omits stop_sequence
	stopId       string
}

// RtChanges is the result of RtFeedTracker.Diff: rows that are new or changed since the last commit.
type RtChanges struct {
	MessageTimestamp uint64
	VehiclePositions []model.VehiclePosition
	TripUpdates      []model.TripUpdate
	StopTimeUpdates  []model.StopTimeUpdate
	Alerts           []model.Alert
	// Entities that could not be converted. They are skipped; the rest of the snapshot is still stored.
	EntityErrors []error

	nextState rtState
}

func (changes *RtChanges) RowCount() int {
	return len(changes.VehiclePositions) + len(changes.TripUpdates) + len(changes.StopTimeUpdates) + len(changes.Alerts)
}

func NewRtFeedTracker() *RtFeedTracker {
	return &RtFeedTracker{state: newRtState()}
}

func newRtState() rtState {
	return rtState{
		vehicles: make(map[string]model.VehiclePosition),
		trips:    make(map[rtTripKey]model.TripUpdate),
		stops:    make(map[rtStopKey]model.StopTimeUpdate),
		alerts:   make(map[string]string),
	}
}

func (state rtState) clone() rtState {
	result := newRtState()
	for k, v := range state.vehicles {
		result.vehicles[k] = v
	}
	for k, v := range state.trips {
		result.trips[k] = v
	}
	for k, v := range state.stops {
		result.stops[k] = v
	}
	for k, v := range state.alerts {
		result.alerts[k] = v
	}
	return result
}

// Diff computes the changes in message relative to the last committed snapshot.
// It returns nil if message is not newer than the last committed snapshot.
func (tracker *RtFeedTracker) Diff(message *gtfs_realtime.FeedMessage) *RtChanges {
	messageTimestamp := message.GetHeader().GetTimestamp()
	// A zero timestamp means the feed didn't provide one, so we can't detect duplicates at the message level
	if messageTimestamp != 0 && messageTimestamp <= tracker.lastMessageTimestamp {
		return nil
	}

	changes := &RtChanges{MessageTimestamp: messageTimestamp}
	previous := tracker.state
	// A FULL_DATASET snapshot replaces everything, so entities absent from it are forgotten.
	// A DIFFERENTIAL snapshot only updates the entities it mentions.
	differential := message.GetHeader().GetIncrementality() == gtfs_realtime.FeedHeader_DIFFERENTIAL
	if differential {
		changes.nextState = previous.clone()
	} else {
		changes.nextState = newRtState()
	}
	next := changes.nextState

	for _, entity := range message.Entity {
		entityId := entity.GetId()
		if entity.GetIsDeleted() {
			if differential {
				delete(next.vehicles, entityId)
				delete(next.alerts, entityId)
				// Trip/stop state is keyed by trip, not entity id; it is pruned when the trip stops appearing in a full snapshot
			}
			continue
		}

		if entity.Vehicle != nil {
			position, err := convertVehiclePosition(entityId, entity.Vehicle, messageTimestamp)
			if err != nil {
				changes.EntityErrors = append(changes.EntityErrors, fmt.Errorf("vehicle position entity %q: %w", entityId, err))
			} else {
				last, seen := next.vehicles[entityId]
				if !seen {
					last, seen = previous.vehicles[entityId]
				}
				if !seen || !sameVehiclePosition(last, position) {
					changes.VehiclePositions = append(changes.VehiclePositions, position)
				}
				next.vehicles[entityId] = position
			}
		}

		if entity.TripUpdate != nil {
			tripUpdate, stopTimeUpdates, err := convertTripUpdate(entityId, entity.TripUpdate, messageTimestamp)
			if err != nil {
				changes.EntityErrors = append(changes.EntityErrors, fmt.Errorf("trip update entity %q: %w", entityId, err))
			} else {
				tripKey := makeTripKey(&tripUpdate)
				last, seen := next.trips[tripKey]
				if !seen {
					last, seen = previous.trips[tripKey]
				}
				if !seen || !sameTripUpdate(last, tripUpdate) {
					changes.TripUpdates = append(changes.TripUpdates, tripUpdate)
				}
				next.trips[tripKey] = tripUpdate

				for _, stopTimeUpdate := range stopTimeUpdates {
					stopKey := makeStopKey(tripKey, &stopTimeUpdate)
					last, seen := next.stops[stopKey]
					if !seen {
						last, seen = previous.stops[stopKey]
					}
					if !seen || !sameStopTimeUpdate(last, stopTimeUpdate) {
						changes.StopTimeUpdates = append(changes.StopTimeUpdates, stopTimeUpdate)
					}
					next.stops[stopKey] = stopTimeUpdate
				}
			}
		}

		if entity.Alert != nil {
			alert, err := convertAlert(entityId, entity.Alert, messageTimestamp)
			if err != nil {
				changes.EntityErrors = append(changes.EntityErrors, fmt.Errorf("alert entity %q: %w", entityId, err))
			} else {
				lastHash, seen := next.alerts[entityId]
				if !seen {
					lastHash, seen = previous.alerts[entityId]
				}
				if !seen || lastHash != alert.ContentHash {
					changes.Alerts = append(changes.Alerts, alert)
				}
				next.alerts[entityId] = alert.ContentHash
			}
		}
	}

	return changes
}

// Commit records changes as stored. Call it only after the changes were written successfully.
func (tracker *RtFeedTracker) Commit(changes *RtChanges) {
	if changes == nil {
		return
	}
	tracker.state = changes.nextState
	if changes.MessageTimestamp > tracker.lastMessageTimestamp {
		tracker.lastMessageTimestamp = changes.MessageTimestamp
	}
}

func makeTripKey(tripUpdate *model.TripUpdate) rtTripKey {
	return rtTripKey{
		tripId:    tripUpdate.TripId,
		routeId:   tripUpdate.RouteId,
		startDate: tripUpdate.StartDate.Unix(),
		startTime: tripUpdate.StartTime,
	}
}

func makeStopKey(tripKey rtTripKey, stopTimeUpdate *model.StopTimeUpdate) rtStopKey {
	stopSequence := int64(-1)
	if stopTimeUpdate.StopSequence != nil {
		stopSequence = int64(*stopTimeUpdate.StopSequence)
	}
	return rtStopKey{trip: tripKey, stopSequence: stopSequence, stopId: stopTimeUpdate.StopId}
}

// sameVehiclePosition compares everything except the row id and the snapshot timestamp
func sameVehiclePosition(a, b model.VehiclePosition) bool {
	a.Id, b.Id = 0, 0
	a.MessageTimestamp, b.MessageTimestamp = 0, 0
	a.StartDate, b.StartDate = a.StartDate.UTC(), b.StartDate.UTC()
	return a == b
}

// sameTripUpdate compares the trip-level state. Timestamp is ignored because some
// feeds refresh it on every snapshot even when nothing else changed.
func sameTripUpdate(a, b model.TripUpdate) bool {
	return a.RouteId == b.RouteId &&
		equalPtr(a.DirectionId, b.DirectionId) &&
		a.ScheduleRelationship == b.ScheduleRelationship &&
		a.VehicleId == b.VehicleId &&
		a.VehicleLabel == b.VehicleLabel &&
		equalPtr(a.Delay, b.Delay)
}

func sameStopTimeUpdate(a, b model.StopTimeUpdate) bool {
	return equalPtr(a.ArrivalTime, b.ArrivalTime) &&
		equalPtr(a.ArrivalDelay, b.ArrivalDelay) &&
		equalPtr(a.ArrivalUncertainty, b.ArrivalUncertainty) &&
		equalPtr(a.DepartureTime, b.DepartureTime) &&
		equalPtr(a.DepartureDelay, b.DepartureDelay) &&
		equalPtr(a.DepartureUncertainty, b.DepartureUncertainty) &&
		a.ScheduleRelationship == b.ScheduleRelationship
}

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
