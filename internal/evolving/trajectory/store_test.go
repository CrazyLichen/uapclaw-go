package trajectory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── InMemoryTrajectoryStore 测试 ────────────────────────────

// TestNewInMemoryTrajectoryStore 创建内存存储
func TestNewInMemoryTrajectoryStore(t *testing.T) {
	store := NewInMemoryTrajectoryStore()
	assert.NotNil(t, store)
	assert.NotNil(t, store.data)
}

// TestInMemoryTrajectoryStore_SaveAndLoad 保存和加载
func TestInMemoryTrajectoryStore_SaveAndLoad(t *testing.T) {
	store := NewInMemoryTrajectoryStore()
	traj := &Trajectory{
		ExecutionID: "exec-1",
		Source:      "online",
		SessionID:   "sess-1",
		Steps:       []*TrajectoryStep{},
	}

	// 保存后加载
	store.Save(traj, "")
	loaded := store.Load("exec-1", "")
	assert.NotNil(t, loaded)
	assert.Equal(t, "exec-1", loaded.ExecutionID)
	assert.Equal(t, "online", loaded.Source)

	// 不存在的 ID
	nilResult := store.Load("not-exist", "")
	assert.Nil(t, nilResult)
}

// TestInMemoryTrajectoryStore_SaveWithVersion 版本隔离
func TestInMemoryTrajectoryStore_SaveWithVersion(t *testing.T) {
	store := NewInMemoryTrajectoryStore()
	traj := &Trajectory{
		ExecutionID: "exec-1",
		Source:      "online",
		Steps:       []*TrajectoryStep{},
	}

	// 保存到 v1 版本
	store.Save(traj, "v1")
	// v1 版本能加载
	loaded := store.Load("exec-1", "v1")
	assert.NotNil(t, loaded)
	// default 版本不能加载
	defaultLoaded := store.Load("exec-1", "")
	assert.Nil(t, defaultLoaded)
}

// TestInMemoryTrajectoryStore_Query 查询过滤
func TestInMemoryTrajectoryStore_Query(t *testing.T) {
	store := NewInMemoryTrajectoryStore()
	store.Save(&Trajectory{ExecutionID: "exec-1", Source: "online", SessionID: "sess-1", Steps: []*TrajectoryStep{}}, "")
	store.Save(&Trajectory{ExecutionID: "exec-2", Source: "offline", CaseID: "case-1", Steps: []*TrajectoryStep{}}, "")
	store.Save(&Trajectory{ExecutionID: "exec-3", Source: "online", SessionID: "sess-2", Steps: []*TrajectoryStep{}}, "")

	// 按来源过滤
	results := store.Query("", map[string]any{"source": "online"})
	assert.Len(t, results, 2)

	// 按 session_id 过滤
	results = store.Query("", map[string]any{"session_id": "sess-1"})
	assert.Len(t, results, 1)
	assert.Equal(t, "exec-1", results[0].ExecutionID)

	// 按 case_id 过滤
	results = store.Query("", map[string]any{"case_id": "case-1"})
	assert.Len(t, results, 1)
	assert.Equal(t, "exec-2", results[0].ExecutionID)

	// 无匹配
	results = store.Query("", map[string]any{"source": "nonexist"})
	assert.Len(t, results, 0)

	// 无过滤
	results = store.Query("", nil)
	assert.Len(t, results, 3)
}

// TestInMemoryTrajectoryStore_Query版本隔离 不同版本查询
func TestInMemoryTrajectoryStore_Query版本隔离(t *testing.T) {
	store := NewInMemoryTrajectoryStore()
	store.Save(&Trajectory{ExecutionID: "exec-1", Source: "online", Steps: []*TrajectoryStep{}}, "v1")
	store.Save(&Trajectory{ExecutionID: "exec-2", Source: "offline", Steps: []*TrajectoryStep{}}, "v2")

	// v1 版本查询
	results := store.Query("v1", nil)
	assert.Len(t, results, 1)
	assert.Equal(t, "exec-1", results[0].ExecutionID)

	// v2 版本查询
	results = store.Query("v2", nil)
	assert.Len(t, results, 1)
	assert.Equal(t, "exec-2", results[0].ExecutionID)

	// default 版本为空
	results = store.Query("", nil)
	assert.Len(t, results, 0)
}

// TestInMemoryTrajectoryStore_Save覆盖 相同ID覆盖
func TestInMemoryTrajectoryStore_Save覆盖(t *testing.T) {
	store := NewInMemoryTrajectoryStore()
	store.Save(&Trajectory{ExecutionID: "exec-1", Source: "online", Steps: []*TrajectoryStep{}}, "")
	store.Save(&Trajectory{ExecutionID: "exec-1", Source: "offline", Steps: []*TrajectoryStep{}}, "")

	loaded := store.Load("exec-1", "")
	assert.NotNil(t, loaded)
	assert.Equal(t, "offline", loaded.Source)
}

// ──────────────────────────── FileTrajectoryStore 测试 ────────────────────────────

// TestNewFileTrajectoryStore 创建文件存储
func TestNewFileTrajectoryStore(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTrajectoryStore(dir)
	assert.NotNil(t, store)
	assert.Equal(t, dir, store.baseDir)
	// 目录应该已创建
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

// TestFileTrajectoryStore_SaveAndLoad 保存和加载
func TestFileTrajectoryStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTrajectoryStore(dir)

	traj := &Trajectory{
		ExecutionID: "exec-1",
		Source:      "online",
		SessionID:   "sess-1",
		Steps: []*TrajectoryStep{
			{
				Kind:      StepKindLLM,
				Detail: &LLMCallDetail{
					Model:    "qwen-max",
					Messages: []map[string]any{{"role": "user", "content": "hello"}},
					Response: map[string]any{"role": "assistant", "content": "hi"},
				},
				StartTimeMs: 1000,
				EndTimeMs:   2000,
			},
			{
				Kind: StepKindTool,
				Detail: &ToolCallDetail{
					ToolName:   "search",
					CallArgs:   map[string]any{"query": "test"},
					CallResult: map[string]any{"status": "ok"},
				},
			},
		},
		Cost: CostInfo{"input_tokens": 100, "output_tokens": 50},
		Meta: map[string]any{"member_id": "m1"},
	}

	// 保存
	store.Save(traj, "")
	// 加载
	loaded := store.Load("exec-1", "")
	require.NotNil(t, loaded)
	assert.Equal(t, "exec-1", loaded.ExecutionID)
	assert.Equal(t, "online", loaded.Source)
	assert.Equal(t, "sess-1", loaded.SessionID)
	assert.Len(t, loaded.Steps, 2)
	// LLM 步骤
	assert.Equal(t, StepKindLLM, loaded.Steps[0].Kind)
	llmDetail, ok := loaded.Steps[0].Detail.(*LLMCallDetail)
	require.True(t, ok)
	assert.Equal(t, "qwen-max", llmDetail.Model)
	// Tool 步骤
	assert.Equal(t, StepKindTool, loaded.Steps[1].Kind)
	toolDetail, ok := loaded.Steps[1].Detail.(*ToolCallDetail)
	require.True(t, ok)
	assert.Equal(t, "search", toolDetail.ToolName)
	// Cost
	assert.NotNil(t, loaded.Cost)
	assert.Equal(t, 100, loaded.Cost["input_tokens"])
	// Meta
	assert.Equal(t, "m1", loaded.Meta["member_id"])
}

// TestFileTrajectoryStore_Load不存在 加载不存在的轨迹
func TestFileTrajectoryStore_Load不存在(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTrajectoryStore(dir)

	// 不存在的 ID
	loaded := store.Load("not-exist", "")
	assert.Nil(t, loaded)
}

// TestFileTrajectoryStore_SaveWithVersion 版本隔离
func TestFileTrajectoryStore_SaveWithVersion(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTrajectoryStore(dir)

	traj := &Trajectory{ExecutionID: "exec-1", Source: "online", Steps: []*TrajectoryStep{}}
	store.Save(traj, "v1")

	// v1 版本能加载
	loaded := store.Load("exec-1", "v1")
	assert.NotNil(t, loaded)

	// default 版本不能加载
	defaultLoaded := store.Load("exec-1", "")
	assert.Nil(t, defaultLoaded)

	// 检查文件名
	expectedPath := filepath.Join(dir, "trajectories_v1.jsonl")
	_, err := os.Stat(expectedPath)
	assert.NoError(t, err)
}

// TestFileTrajectoryStore_Query 查询过滤
func TestFileTrajectoryStore_Query(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTrajectoryStore(dir)

	store.Save(&Trajectory{ExecutionID: "exec-1", Source: "online", SessionID: "sess-1", Steps: []*TrajectoryStep{}}, "")
	store.Save(&Trajectory{ExecutionID: "exec-2", Source: "offline", CaseID: "case-1", Steps: []*TrajectoryStep{}}, "")

	// 按来源过滤
	results := store.Query("", map[string]any{"source": "online"})
	assert.Len(t, results, 1)
	assert.Equal(t, "exec-1", results[0].ExecutionID)

	// 按来源过滤 offline
	results = store.Query("", map[string]any{"source": "offline"})
	assert.Len(t, results, 1)
	assert.Equal(t, "exec-2", results[0].ExecutionID)

	// 无匹配
	results = store.Query("", map[string]any{"source": "nonexist"})
	assert.Len(t, results, 0)

	// 无过滤
	results = store.Query("", nil)
	assert.Len(t, results, 2)
}

// TestFileTrajectoryStore_Query空文件 查询不存在的文件
func TestFileTrajectoryStore_Query空文件(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTrajectoryStore(dir)

	results := store.Query("", nil)
	assert.Len(t, results, 0)
}

// ──────────────────────────── 辅助函数测试 ────────────────────────────

// TestTrajectoryToDict 转换为字典
func TestTrajectoryToDict(t *testing.T) {
	traj := &Trajectory{
		ExecutionID: "exec-1",
		Source:      "online",
		Steps: []*TrajectoryStep{
			{
				Kind: StepKindLLM,
				Detail: &LLMCallDetail{
					Model:    "qwen-max",
					Messages: []map[string]any{{"role": "user", "content": "hello"}},
				},
			},
		},
	}

	dict := trajectoryToDict(traj)
	assert.Equal(t, "exec-1", dict["execution_id"])
	assert.Equal(t, "online", dict["source"])

	steps, ok := dict["steps"].([]any)
	require.True(t, ok)
	assert.Len(t, steps, 1)
}

// TestDictToTrajectory 从字典还原
func TestDictToTrajectory(t *testing.T) {
	data := map[string]any{
		"execution_id": "exec-1",
		"source":       "online",
		"session_id":   "sess-1",
		"case_id":      "case-1",
		"steps": []any{
			map[string]any{
				"kind":          "llm",
				"start_time_ms": float64(1000),
				"end_time_ms":   float64(2000),
				"detail": map[string]any{
					"model": "qwen-max",
					"messages": []any{
						map[string]any{"role": "user", "content": "hello"},
					},
				},
			},
			map[string]any{
				"kind": "tool",
				"detail": map[string]any{
					"tool_name":   "search",
					"call_args":   map[string]any{"query": "test"},
					"call_result": map[string]any{"status": "ok"},
				},
			},
		},
		"cost": map[string]any{"input_tokens": float64(100), "output_tokens": float64(50)},
		"meta": map[string]any{"member_id": "m1"},
	}

	traj := dictToTrajectory(data)
	require.NotNil(t, traj)
	assert.Equal(t, "exec-1", traj.ExecutionID)
	assert.Equal(t, "online", traj.Source)
	assert.Len(t, traj.Steps, 2)

	// LLM 步骤
	assert.Equal(t, StepKindLLM, traj.Steps[0].Kind)
	llmDetail, ok := traj.Steps[0].Detail.(*LLMCallDetail)
	require.True(t, ok)
	assert.Equal(t, "qwen-max", llmDetail.Model)

	// Tool 步骤
	assert.Equal(t, StepKindTool, traj.Steps[1].Kind)
	toolDetail, ok := traj.Steps[1].Detail.(*ToolCallDetail)
	require.True(t, ok)
	assert.Equal(t, "search", toolDetail.ToolName)
}

// TestDictToTrajectory_空Steps 无步骤
func TestDictToTrajectory_空Steps(t *testing.T) {
	data := map[string]any{
		"execution_id": "exec-1",
		"source":       "online",
	}

	traj := dictToTrajectory(data)
	require.NotNil(t, traj)
	assert.Equal(t, "exec-1", traj.ExecutionID)
	assert.Empty(t, traj.Steps)
}

// TestDictToTrajectory_无Detail detail 为空
func TestDictToTrajectory_无Detail(t *testing.T) {
	data := map[string]any{
		"execution_id": "exec-1",
		"steps": []any{
			map[string]any{
				"kind":          "llm",
				"start_time_ms": float64(1000),
			},
		},
	}

	traj := dictToTrajectory(data)
	require.NotNil(t, traj)
	assert.Len(t, traj.Steps, 1)
	assert.Nil(t, traj.Steps[0].Detail)
}

// TestToJSONCompatible JSON 兼容转换
func TestToJSONCompatible(t *testing.T) {
	// 基本类型
	assert.Equal(t, "hello", toJSONCompatible("hello"))
	// JSON 反序列化后 int 变为 float64
	assert.Equal(t, float64(42), toJSONCompatible(42))
	assert.Nil(t, toJSONCompatible(nil))

	// 结构体
	traj := &Trajectory{ExecutionID: "exec-1", Source: "online", Steps: []*TrajectoryStep{}}
	result := toJSONCompatible(traj)
	m, ok := result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "exec-1", m["execution_id"])
}

// TestMatchTrajectoryField 字段匹配
func TestMatchTrajectoryField(t *testing.T) {
	traj := &Trajectory{ExecutionID: "exec-1", Source: "online", CaseID: "case-1", SessionID: "sess-1"}

	assert.True(t, matchTrajectoryField(traj, "execution_id", "exec-1"))
	assert.True(t, matchTrajectoryField(traj, "source", "online"))
	assert.True(t, matchTrajectoryField(traj, "case_id", "case-1"))
	assert.True(t, matchTrajectoryField(traj, "session_id", "sess-1"))
	assert.False(t, matchTrajectoryField(traj, "source", "offline"))
	assert.False(t, matchTrajectoryField(traj, "unknown_field", "value"))
}

// TestRoundTrip 保存后加载完整往返
func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTrajectoryStore(dir)

	traj := &Trajectory{
		ExecutionID: "exec-roundtrip",
		Source:      "online",
		SessionID:   "sess-1",
		CaseID:      "case-1",
		Steps: []*TrajectoryStep{
			{
				Kind:        StepKindLLM,
				StartTimeMs: 1000,
				EndTimeMs:   2000,
				Detail: &LLMCallDetail{
					Model:    "qwen-max",
					Messages: []map[string]any{{"role": "user", "content": "hello"}},
					Response: map[string]any{"role": "assistant", "content": "hi"},
					Tools:    []map[string]any{{"type": "function", "function": map[string]any{"name": "search"}}},
					Usage:    map[string]any{"prompt_tokens": float64(10), "completion_tokens": float64(5)},
					Meta:     map[string]any{"key": "value"},
				},
				PromptTokenIDs:    []int{1, 2, 3},
				CompletionTokenIDs: []int{4, 5},
				Reward:            0.95,
				Meta:              map[string]any{"operator_id": "op-1"},
			},
			{
				Kind: StepKindTool,
				Detail: &ToolCallDetail{
					ToolName:        "search",
					CallArgs:        map[string]any{"query": "test"},
					CallResult:      map[string]any{"status": "ok"},
					ToolDescription: "Search tool",
					ToolCallID:      "call-1",
				},
				Error: map[string]any{"code": "timeout"},
				Meta:  map[string]any{"invoke_id": "inv-1"},
			},
		},
		Cost: CostInfo{"input_tokens": 100, "output_tokens": 50},
		Meta: map[string]any{"member_id": "m1", "member_count": float64(3)},
	}

	store.Save(traj, "v1")
	loaded := store.Load("exec-roundtrip", "v1")
	require.NotNil(t, loaded)

	// 验证基本字段
	assert.Equal(t, "exec-roundtrip", loaded.ExecutionID)
	assert.Equal(t, "online", loaded.Source)
	assert.Equal(t, "sess-1", loaded.SessionID)
	assert.Equal(t, "case-1", loaded.CaseID)

	// 验证步骤
	require.Len(t, loaded.Steps, 2)

	// LLM 步骤
	llmStep := loaded.Steps[0]
	assert.Equal(t, StepKindLLM, llmStep.Kind)
	assert.Equal(t, 1000, llmStep.StartTimeMs)
	assert.Equal(t, 2000, llmStep.EndTimeMs)
	llmDetail, ok := llmStep.Detail.(*LLMCallDetail)
	require.True(t, ok)
	assert.Equal(t, "qwen-max", llmDetail.Model)
	assert.Len(t, llmDetail.Messages, 1)
	assert.Equal(t, "user", llmDetail.Messages[0]["role"])
	assert.Equal(t, "assistant", llmDetail.Response["role"])
	assert.NotNil(t, llmDetail.Usage)

	// Tool 步骤
	toolStep := loaded.Steps[1]
	assert.Equal(t, StepKindTool, toolStep.Kind)
	toolDetail, ok := toolStep.Detail.(*ToolCallDetail)
	require.True(t, ok)
	assert.Equal(t, "search", toolDetail.ToolName)
	assert.Equal(t, "call-1", toolDetail.ToolCallID)

	// Cost
	assert.NotNil(t, loaded.Cost)
	assert.Equal(t, 100, loaded.Cost["input_tokens"])

	// Meta
	assert.Equal(t, "m1", loaded.Meta["member_id"])
}
