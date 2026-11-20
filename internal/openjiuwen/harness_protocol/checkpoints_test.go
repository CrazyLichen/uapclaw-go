package harness_protocol

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewHarnessCheckpoint_五必填字段逐项拒绝 验证步骤 1：
// 5 个必填字段各自为空时报对应文案。
// Python: checkpoints.py:47-56
func TestNewHarnessCheckpoint_五必填字段逐项拒绝(t *testing.T) {
	base := CheckpointOptions{
		Provider:      "acme",
		SchemaVersion: "1",
		AgentID:       "member-a",
		HostSessionID: "session-a",
		CheckpointID:  "checkpoint-1",
		Sequence:      1,
	}
	cases := []struct {
		name    string
		mutate  func(*CheckpointOptions)
		wantMsg string
	}{
		{"provider", func(o *CheckpointOptions) { o.Provider = "" }, "checkpoint provider must not be empty"},
		{"schema_version", func(o *CheckpointOptions) { o.SchemaVersion = "" }, "checkpoint schema_version must not be empty"},
		{"agent_id", func(o *CheckpointOptions) { o.AgentID = "" }, "checkpoint agent_id must not be empty"},
		{"host_session_id", func(o *CheckpointOptions) { o.HostSessionID = "" }, "checkpoint host_session_id must not be empty"},
		{"checkpoint_id", func(o *CheckpointOptions) { o.CheckpointID = "" }, "checkpoint checkpoint_id must not be empty"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := base
			c.mutate(&opts)
			_, err := NewHarnessCheckpoint(opts)
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.wantMsg)
		})
	}
}

// TestNewHarnessCheckpoint_步骤2与3 验证 sequence 非负与 optional ID 空串拒绝。
// Python: checkpoints.py:57-60
func TestNewHarnessCheckpoint_步骤2与3(t *testing.T) {
	base := CheckpointOptions{
		Provider:      "acme",
		SchemaVersion: "1",
		AgentID:       "member-a",
		HostSessionID: "session-a",
		CheckpointID:  "checkpoint-1",
		Sequence:      1,
	}

	t.Run("sequence 负值", func(t *testing.T) {
		opts := base
		opts.Sequence = -1
		_, err := NewHarnessCheckpoint(opts)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "checkpoint sequence must be non-negative")
	})

	t.Run("provider_session_id 空串", func(t *testing.T) {
		opts := base
		empty := ""
		opts.ProviderSessionID = &empty
		_, err := NewHarnessCheckpoint(opts)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "optional checkpoint ids must not be empty strings")
	})

	t.Run("revision 空串", func(t *testing.T) {
		opts := base
		empty := ""
		opts.Revision = &empty
		_, err := NewHarnessCheckpoint(opts)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "optional checkpoint ids must not be empty strings")
	})

	// nil 表示「未提供」，不是空串，必须通过
	t.Run("nil 允许", func(t *testing.T) {
		opts := base
		opts.ProviderSessionID = nil
		opts.Revision = nil
		_, err := NewHarnessCheckpoint(opts)
		require.NoError(t, err)
	})
}

// TestNewHarnessCheckpoint_步骤4到6 验证 data 冻结与回写。
// Python: checkpoints.py:61-65
func TestNewHarnessCheckpoint_步骤4到6(t *testing.T) {
	src := map[string]any{"conversation_id": "conversation-a"}
	cp, err := NewHarnessCheckpoint(CheckpointOptions{
		Provider:      "claude-code",
		SchemaVersion: "1",
		AgentID:       "team-a_member-a",
		HostSessionID: "session-a",
		CheckpointID:  "checkpoint-1",
		Sequence:      1,
		Data:          src,
	})
	require.NoError(t, err)
	assert.Equal(t, "conversation-a", cp.Data["conversation_id"])

	// 冻结：改源 map 不影响结果（FreezeJsonObject 深拷贝）
	src["conversation_id"] = "mutated"
	assert.Equal(t, "conversation-a", cp.Data["conversation_id"])

	// 非 JSON 值被拒绝（NaN）
	_, err = NewHarnessCheckpoint(CheckpointOptions{
		Provider:      "acme",
		SchemaVersion: "1",
		AgentID:       "a",
		HostSessionID: "s",
		CheckpointID:  "c",
		Sequence:      1,
		Data:          map[string]any{"x": math.NaN()},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be finite")
}

// TestNewHarnessCheckpoint_步骤5尺寸 验证 4MiB 上限。
// Python: checkpoints.py:62-64
func TestNewHarnessCheckpoint_步骤5尺寸(t *testing.T) {
	mkOpts := func(payload string) CheckpointOptions {
		return CheckpointOptions{
			Provider:      "acme",
			SchemaVersion: "1",
			AgentID:       "member-a",
			HostSessionID: "session-a",
			CheckpointID:  "checkpoint-1",
			Sequence:      1,
			Data:          map[string]any{"payload": payload},
		}
	}

	// 先算出信封开销，避免手算边界
	emptySize, err := checkpointDataSize(map[string]any{"payload": ""})
	require.NoError(t, err)
	overhead := emptySize

	t.Run("恰好边界内", func(t *testing.T) {
		_, err := NewHarnessCheckpoint(mkOpts(strings.Repeat("x", MaxCheckpointBytes-overhead)))
		require.NoError(t, err, "恰好 4MiB 应通过")
	})

	t.Run("超限 1 字节", func(t *testing.T) {
		_, err := NewHarnessCheckpoint(mkOpts(strings.Repeat("x", MaxCheckpointBytes-overhead+1)))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "checkpoint data exceeds 4194304 bytes")
	})

	t.Run("全 ASCII 紧凑编码字节数", func(t *testing.T) {
		// {"payload":"xxx"} 紧凑编码 = 17 字节（与 Python ensure_ascii=False 一致）
		n, err := checkpointDataSize(map[string]any{"payload": "xxx"})
		require.NoError(t, err)
		assert.Equal(t, 17, n)
	})

	t.Run("HTML 不转义", func(t *testing.T) {
		// json.Marshal 默认会转义 <>& 得 58 字节；SetEscapeHTML(false) 得 33 字节
		n, err := checkpointDataSize(map[string]any{"html": "<a>&</a>", "cn": "中文"})
		require.NoError(t, err)
		assert.Equal(t, 33, n)
	})
}

// TestCheckpointDataSize_非ASCII偏差 登记 spec D5：
// 中文场景 Python 默认 ensure_ascii=True 得 21 字节，Go 得 15 字节。
func TestCheckpointDataSize_非ASCII偏差(t *testing.T) {
	n, err := checkpointDataSize(map[string]any{"cn": "中文"})
	require.NoError(t, err)
	// {"cn":"中文"} = 15 字节（Go，不转义非 ASCII）
	// Python 默认 = 21 字节（\u4e2d\u6587）
	assert.Equal(t, 15, n, "Go 侧不转义非 ASCII，比 Python 默认口径小")
}

// TestNewCheckpointSaveReceipt 验证回执两步校验。
// Python: checkpoints.py:76-80
func TestNewCheckpointSaveReceipt(t *testing.T) {
	t.Run("正常", func(t *testing.T) {
		r, err := NewCheckpointSaveReceipt("checkpoint-1", 1, "storage-1")
		require.NoError(t, err)
		assert.Equal(t, "checkpoint-1", r.CheckpointID)
		assert.Equal(t, 1, r.Sequence)
		assert.Equal(t, "storage-1", r.StorageRevision)
	})

	t.Run("checkpoint_id 空", func(t *testing.T) {
		_, err := NewCheckpointSaveReceipt("", 1, "storage-1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "checkpoint save receipt ids must not be empty")
	})

	t.Run("storage_revision 空", func(t *testing.T) {
		_, err := NewCheckpointSaveReceipt("checkpoint-1", 1, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "checkpoint save receipt ids must not be empty")
	})

	t.Run("sequence 负值", func(t *testing.T) {
		_, err := NewCheckpointSaveReceipt("checkpoint-1", -1, "storage-1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "checkpoint save receipt sequence must be non-negative")
	})
}

// TestHarnessCheckpoint_九字段 验证字段齐备与常量（15.1 占位用例迁入）。
// Python: checkpoints.py:15,28-45
func TestHarnessCheckpoint_九字段(t *testing.T) {
	assert.Equal(t, 4*1024*1024, MaxCheckpointBytes)
	cp, err := NewHarnessCheckpoint(CheckpointOptions{
		Provider:      "claude",
		SchemaVersion: "1",
		AgentID:       "a1",
		HostSessionID: "s1",
		CheckpointID:  "cp1",
		Sequence:      3,
		Data:          map[string]any{"k": "v"},
	})
	require.NoError(t, err)
	assert.Equal(t, "cp1", cp.CheckpointID)
	assert.Equal(t, 3, cp.Sequence)
	assert.Equal(t, "v", cp.Data["k"])
}

// TestCheckpointReason_值集 验证 5 个发布原因（15.1 占位用例迁入）。
// Python: checkpoints.py:18-26
func TestCheckpointReason_值集(t *testing.T) {
	got := []CheckpointReason{
		CheckpointReasonSessionActivated,
		CheckpointReasonTurnCompleted,
		CheckpointReasonStateChanged,
		CheckpointReasonPeriodic,
		CheckpointReasonProviderRequested,
	}
	want := []string{"session_activated", "turn_completed", "state_changed", "periodic", "provider_requested"}
	require.Len(t, got, 5)
	for i, r := range got {
		assert.Equal(t, want[i], string(r))
	}
}

// TestCheckpointSaveReceipt_三字段 验证回执字段（15.1 占位用例迁入）。
// Python: checkpoints.py:66-73
func TestCheckpointSaveReceipt_三字段(t *testing.T) {
	r, err := NewCheckpointSaveReceipt("cp1", 1, "rev1")
	require.NoError(t, err)
	assert.Equal(t, "rev1", r.StorageRevision)
}
