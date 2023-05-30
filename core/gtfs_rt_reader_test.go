package core

import (
	"io"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseRtdRtVehiclePosition(t *testing.T) {
	f, err := os.Open(path.Join(getTestFilesPath(), "VehiclePosition_RTD_2023_05_23.pb"))
	assert.NoError(t, err)
	protoBytes, err := io.ReadAll(f)
	assert.NoError(t, err)
	updates, err := convertVehiclePositionProtoToModel(protoBytes)
	assert.NoError(t, err)
	assert.Equal(t, 331, len(updates))
	updateIds := make(map[string]struct{}, len(updates))
	for _, update := range updates {
		assert.EqualValues(t, 1684864397, update.MessageTimestamp)
		// All the ids should be unique
		assert.NotContains(t, updateIds, update.Id)
		updateIds[update.Id] = struct{}{}
	}
}
