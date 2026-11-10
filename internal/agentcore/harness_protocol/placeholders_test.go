package harness_protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestEventBufferConfig_默认值 验证容量 1024 与默认溢出策略。
// Python: events.py:98-106 EventBufferConfig（capacity=1024, overflow=COALESCE_OR_BLOCK）
func TestEventBufferConfig_默认值(t *testing.T) {
	c := EventBufferConfig{Capacity: 1024, Overflow: EventOverflowPolicyCoalesceOrBlock}
	assert.Equal(t, 1024, c.Capacity)
	assert.Equal(t, EventOverflowPolicy("coalesce_or_block"), c.Overflow)
}

// TestEventOverflowPolicy_值集 验证 3 个取值。
// Python: events.py:87-95 EventOverflowPolicy
func TestEventOverflowPolicy_值集(t *testing.T) {
	assert.Equal(t, EventOverflowPolicy("block"), EventOverflowPolicyBlock)
	assert.Equal(t, EventOverflowPolicy("coalesce_or_block"), EventOverflowPolicyCoalesceOrBlock)
	assert.Equal(t, EventOverflowPolicy("drop_best_effort_or_block"), EventOverflowPolicyDropBestEffortOrBlock)
}

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

// TestErrEventStreamDone_哨兵 验证游标耗尽哨兵可作为 error 使用。
// Python: stream.py:12-22 __anext__ 抛 StopAsyncIteration
func TestErrEventStreamDone_哨兵(t *testing.T) {
	assert.EqualError(t, ErrEventStreamDone, "harness event stream exhausted")
}
