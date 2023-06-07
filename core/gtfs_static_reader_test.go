package core

import (
	"path"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func getTestFilesPath() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return path.Join(path.Dir(thisFile), "test_files")
}

// This test takes nearly 30 seconds to run - TODO: figure out what's slow in parsing and improve parsing performance if possible
func TestParseRtdStatic(t *testing.T) {
	staticFeed, err := ParseStaticGtfsFromPath(path.Join(getTestFilesPath(), "google_transit_rtd_2023_05_12.zip"))
	assert.NoError(t, err)
	assert.Equal(t, 1, len(staticFeed.Agency))
	assert.Equal(t, "Regional Transportation District", staticFeed.Agency[0].Name)
	assert.Equal(t, 19, len(staticFeed.Calendar))
	assert.Equal(t, "ca084dac096878a7d8fbf6f3f7dc1203", staticFeed.FeedInfo.Version)
}
