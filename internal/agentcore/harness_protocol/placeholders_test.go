package harness_protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestHarnessCheckpoint_九字段 验证字段齐备与常量。
// Python: checkpoints.py:15,28-45 HarnessCheckpoint
func TestHarnessCheckpoint_九字段(t *testing.T) {
	assert.Equal(t, 4*1024*1024, MaxCheckpointBytes)
	c := HarnessCheckpoint{
		Provider:      "claude",
		SchemaVersion: "1",
		AgentID:       "a1",
		HostSessionID: "s1",
		CheckpointID:  "cp1",
		Sequence:      3,
		Data:          map[string]any{"k": "v"},
	}
	assert.Equal(t, "cp1", c.CheckpointID)
	assert.Equal(t, 3, c.Sequence)
}

// TestCheckpointReason_值集 验证 5 个发布原因。
// Python: checkpoints.py:18-26 CheckpointReason
func TestCheckpointReason_值集(t *testing.T) {
	got := []CheckpointReason{
		CheckpointReasonSessionActivated,
		CheckpointReasonTurnCompleted,
		CheckpointReasonStateChanged,
		CheckpointReasonPeriodic,
		CheckpointReasonProviderRequested,
	}
	want := []string{"session_activated", "turn_completed", "state_changed", "periodic", "provider_requested"}
	assert.Len(t, got, 5)
	for i, r := range got {
		assert.Equal(t, want[i], string(r))
	}
}

// TestCheckpointSaveReceipt_三字段 验证回执字段。
// Python: checkpoints.py:66-79 CheckpointSaveReceipt
func TestCheckpointSaveReceipt_三字段(t *testing.T) {
	r := CheckpointSaveReceipt{CheckpointID: "cp1", Sequence: 1, StorageRevision: "rev1"}
	assert.Equal(t, "rev1", r.StorageRevision)
}
