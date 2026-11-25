package core

import (
	"context"
	"errors"
	"path"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samc1213/gtfs-analyze/log"
	"github.com/samc1213/gtfs-analyze/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
)

func newTestDb(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := InitializeSqliteDatabase(filepath.Join(t.TempDir(), "test.db"), log.Silent)
	require.NoError(t, err)
	t.Cleanup(func() { closeDb(db, log.New(log.Silent)) })
	return db
}

func countRows(t *testing.T, db *gorm.DB, value any) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(value).Count(&count).Error)
	return count
}

// Regression test: vehicle_positions used to be keyed by entity id, so each poll overwrote the last
func TestVehiclePositionHistoryAccumulates(t *testing.T) {
	db := newTestDb(t)
	ctx := context.Background()
	tracker := NewRtFeedTracker()

	first := loadRtFeed(t, "VehiclePosition_RTD_2023_05_23.pb")
	changes := tracker.Diff(first)
	require.NoError(t, WriteRtChangesToDatabase(ctx, changes, db))
	tracker.Commit(changes)

	second := nextSnapshot(first, 30)
	for _, entity := range second.Entity {
		entity.Vehicle.Timestamp = proto.Uint64(entity.Vehicle.GetTimestamp() + 30)
	}
	changes = tracker.Diff(second)
	require.NoError(t, WriteRtChangesToDatabase(ctx, changes, db))
	tracker.Commit(changes)

	assert.EqualValues(t, 2*331, countRows(t, db, &model.VehiclePosition{}))
	var snapshots int64
	require.NoError(t, db.Model(&model.VehiclePosition{}).Distinct("message_timestamp").Count(&snapshots).Error)
	assert.EqualValues(t, 2, snapshots)
}

func TestWriteTripUpdatesAndAlerts(t *testing.T) {
	db := newTestDb(t)
	ctx := context.Background()
	tracker := NewRtFeedTracker()

	require.NoError(t, WriteRtChangesToDatabase(ctx, tracker.Diff(loadRtFeed(t, "TripUpdate_RTD_2026_11_24.pb")), db))
	assert.EqualValues(t, 814, countRows(t, db, &model.TripUpdate{}))
	assert.EqualValues(t, 22281, countRows(t, db, &model.StopTimeUpdate{}))

	require.NoError(t, WriteRtChangesToDatabase(ctx, tracker.Diff(loadRtFeed(t, "Alerts_RTD_2026_11_24.pb")), db))
	assert.EqualValues(t, 91, countRows(t, db, &model.Alert{}))

	var alert model.Alert
	require.NoError(t, db.Preload("InformedEntities").Preload("ActivePeriods").Where("entity_id = ?", "63945").First(&alert).Error)
	require.Len(t, alert.InformedEntities, 1)
	assert.Equal(t, "12875", alert.InformedEntities[0].StopId)
	require.Len(t, alert.ActivePeriods, 1)
}

func TestWriteStaticFeed(t *testing.T) {
	if testing.Short() {
		t.Skip("writes a full RTD feed; takes ~1 minute")
	}
	feed, err := ParseStaticGtfsFromPath(path.Join(getTestFilesPath(), "google_transit_rtd_2023_05_12.zip"))
	require.NoError(t, err)
	db := newTestDb(t)

	require.NoError(t, WriteStaticGtfsFeedToDatabase(context.Background(), feed, db))

	assert.EqualValues(t, len(feed.StopTime), countRows(t, db, &model.StopTime{}))
	assert.EqualValues(t, len(feed.Trip), countRows(t, db, &model.Trip{}))
	assert.EqualValues(t, len(feed.Stop), countRows(t, db, &model.Stop{}))
	exists, err := doesFeedAlreadyExist(context.Background(), feed, db)
	require.NoError(t, err)
	assert.True(t, exists)

	// Nullable columns are stored as NULL, not empty strings
	var emptyHeadsigns int64
	require.NoError(t, db.Model(&model.StopTime{}).Where("stop_headsign = ''").Count(&emptyHeadsigns).Error)
	assert.Zero(t, emptyHeadsigns)
}

func TestPollKeepsGoingAfterErrorsAndStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var runs atomic.Int32
	job := storeJob{
		name:     "flaky",
		interval: 5 * time.Millisecond,
		run: func(ctx context.Context) error {
			if runs.Add(1) <= 2 {
				return errors.New("temporary failure")
			}
			if runs.Load() >= 5 {
				cancel()
			}
			return nil
		},
	}

	done := make(chan struct{})
	go func() {
		poll(ctx, job, log.New(log.Silent))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("poll did not stop after the context was cancelled")
	}
	assert.GreaterOrEqual(t, runs.Load(), int32(5))
}

func TestStoreRequiresAFeed(t *testing.T) {
	err := Store(context.Background(), StoreConfig{DbPath: filepath.Join(t.TempDir(), "x.db"), LogLevel: log.Silent})
	assert.ErrorContains(t, err, "nothing to store")
}
