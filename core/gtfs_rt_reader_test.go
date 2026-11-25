package core

import (
	"os"
	"path"
	"testing"

	"github.com/samc1213/gtfs-analyze/gtfs_realtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func loadRtFeed(t *testing.T, fileName string) *gtfs_realtime.FeedMessage {
	t.Helper()
	protoBytes, err := os.ReadFile(path.Join(getTestFilesPath(), fileName))
	require.NoError(t, err)
	message, err := ParseRtFeed(protoBytes)
	require.NoError(t, err)
	return message
}

// nextSnapshot returns a copy of message with a later header timestamp, as the next poll would
func nextSnapshot(message *gtfs_realtime.FeedMessage, secondsLater uint64) *gtfs_realtime.FeedMessage {
	next := proto.Clone(message).(*gtfs_realtime.FeedMessage)
	next.Header.Timestamp = proto.Uint64(message.Header.GetTimestamp() + secondsLater)
	return next
}

func TestParseRtFeedRejectsGarbage(t *testing.T) {
	_, err := ParseRtFeed([]byte("<html>not a protobuf</html>"))
	assert.Error(t, err)
}

func TestParseRtdRtVehiclePosition(t *testing.T) {
	message := loadRtFeed(t, "VehiclePosition_RTD_2023_05_23.pb")
	changes := NewRtFeedTracker().Diff(message)
	require.NotNil(t, changes)
	assert.Empty(t, changes.EntityErrors)
	assert.Equal(t, 331, len(changes.VehiclePositions))
	entityIds := make(map[string]struct{}, len(changes.VehiclePositions))
	for _, position := range changes.VehiclePositions {
		assert.EqualValues(t, 1684864397, position.MessageTimestamp)
		// All the entity ids should be unique within a snapshot
		assert.NotContains(t, entityIds, position.EntityId)
		entityIds[position.EntityId] = struct{}{}
	}
}

func TestVehiclePositionsStoredOnlyWhenChanged(t *testing.T) {
	tracker := NewRtFeedTracker()
	first := loadRtFeed(t, "VehiclePosition_RTD_2023_05_23.pb")
	changes := tracker.Diff(first)
	tracker.Commit(changes)

	// Same snapshot again: already stored
	assert.Nil(t, tracker.Diff(first))

	// Newer snapshot where one vehicle moved
	second := nextSnapshot(first, 30)
	second.Entity[0].Vehicle.Position.Latitude = proto.Float32(second.Entity[0].Vehicle.Position.GetLatitude() + 0.01)
	changes = tracker.Diff(second)
	require.NotNil(t, changes)
	require.Len(t, changes.VehiclePositions, 1)
	assert.Equal(t, second.Entity[0].GetId(), changes.VehiclePositions[0].EntityId)
	assert.Equal(t, second.Header.GetTimestamp(), changes.VehiclePositions[0].MessageTimestamp)
	tracker.Commit(changes)

	// A vehicle that drops out of the feed and comes back is stored again
	third := nextSnapshot(second, 30)
	returning := third.Entity[1]
	third.Entity = third.Entity[2:]
	changes = tracker.Diff(third)
	assert.Empty(t, changes.VehiclePositions)
	tracker.Commit(changes)
	fourth := nextSnapshot(third, 30)
	fourth.Entity = append(fourth.Entity, returning)
	changes = tracker.Diff(fourth)
	require.Len(t, changes.VehiclePositions, 1)
	assert.Equal(t, returning.GetId(), changes.VehiclePositions[0].EntityId)
}

func TestUncommittedChangesAreDetectedAgain(t *testing.T) {
	tracker := NewRtFeedTracker()
	first := loadRtFeed(t, "VehiclePosition_RTD_2023_05_23.pb")
	// Simulate a failed database write: Diff without Commit
	assert.Len(t, tracker.Diff(first).VehiclePositions, 331)
	assert.Len(t, tracker.Diff(nextSnapshot(first, 30)).VehiclePositions, 331)
}

func TestTripUpdatesStoredOnlyWhenChanged(t *testing.T) {
	tracker := NewRtFeedTracker()
	first := loadRtFeed(t, "TripUpdate_RTD_2026_11_24.pb")
	changes := tracker.Diff(first)
	require.NotNil(t, changes)
	assert.Empty(t, changes.EntityErrors)
	assert.Len(t, changes.TripUpdates, 814)
	assert.Len(t, changes.StopTimeUpdates, 22281)
	stopTimeUpdate := changes.StopTimeUpdates[0]
	assert.Equal(t, "116031397", stopTimeUpdate.TripId)
	assert.EqualValues(t, 1, *stopTimeUpdate.StopSequence)
	assert.Equal(t, "26175", stopTimeUpdate.StopId)
	assert.Nil(t, stopTimeUpdate.ArrivalTime)
	assert.EqualValues(t, 1795563062, *stopTimeUpdate.DepartureTime)
	tracker.Commit(changes)

	// One stop's prediction slips by a minute: only that prediction is stored
	second := nextSnapshot(first, 30)
	stu := second.Entity[0].TripUpdate.StopTimeUpdate[3]
	stu.Arrival.Time = proto.Int64(stu.Arrival.GetTime() + 60)
	changes = tracker.Diff(second)
	assert.Empty(t, changes.TripUpdates)
	require.Len(t, changes.StopTimeUpdates, 1)
	assert.EqualValues(t, 4, *changes.StopTimeUpdates[0].StopSequence)
	tracker.Commit(changes)

	// The trip is cancelled: the trip-level change is stored
	third := nextSnapshot(second, 30)
	third.Entity[0].TripUpdate.Trip.ScheduleRelationship = gtfs_realtime.TripDescriptor_CANCELED.Enum()
	changes = tracker.Diff(third)
	require.Len(t, changes.TripUpdates, 1)
	assert.Empty(t, changes.StopTimeUpdates)
}

func TestAlertsStoredOnlyWhenChanged(t *testing.T) {
	tracker := NewRtFeedTracker()
	first := loadRtFeed(t, "Alerts_RTD_2026_11_24.pb")
	changes := tracker.Diff(first)
	require.NotNil(t, changes)
	require.Len(t, changes.Alerts, 91)
	alert := changes.Alerts[0]
	assert.Equal(t, "63945", alert.EntityId)
	assert.Contains(t, alert.HeaderText, "Colfax Ave & Elm St")
	require.Len(t, alert.InformedEntities, 1)
	assert.Equal(t, "15", alert.InformedEntities[0].RouteId)
	require.Len(t, alert.ActivePeriods, 1)
	assert.EqualValues(t, 1830247140, *alert.ActivePeriods[0].End)
	tracker.Commit(changes)

	second := nextSnapshot(first, 30)
	assert.Empty(t, tracker.Diff(second).Alerts)

	second.Entity[0].Alert.HeaderText.Translation[0].Text = proto.String("Stop reopened")
	changes = tracker.Diff(second)
	require.Len(t, changes.Alerts, 1)
	assert.Equal(t, "Stop reopened", changes.Alerts[0].HeaderText)
}

func TestDifferentialFeedKeepsUnmentionedEntities(t *testing.T) {
	tracker := NewRtFeedTracker()
	first := loadRtFeed(t, "VehiclePosition_RTD_2023_05_23.pb")
	tracker.Commit(tracker.Diff(first))

	// A differential update mentioning a single, unchanged vehicle stores nothing,
	// and doesn't make the tracker forget the other vehicles
	second := nextSnapshot(first, 30)
	second.Header.Incrementality = gtfs_realtime.FeedHeader_DIFFERENTIAL.Enum()
	second.Entity = second.Entity[:1]
	changes := tracker.Diff(second)
	assert.Empty(t, changes.VehiclePositions)
	tracker.Commit(changes)

	third := nextSnapshot(first, 60)
	assert.Empty(t, tracker.Diff(third).VehiclePositions)
}
