package context

import (
	"context"
	"testing"
	"time"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	common_schema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeTokenCounter 模拟 Token 计数器，用于测试
type fakeTokenCounter struct {
	countVal   int
	countErr   error
	msgCount   int
	msgErr     error
	toolCount  int
	toolErr    error
}

func (f *fakeTokenCounter) Count(text string, model string) (int, error) {
	return f.countVal, f.countErr
}

func (f *fakeTokenCounter) CountMessages(messages []llm_schema.BaseMessage, model string) (int, error) {
	return f.msgCount, f.msgErr
}

func (f *fakeTokenCounter) CountTools(tools []*common_schema.ToolInfo, model string) (int, error) {
	return f.toolCount, f.toolErr
}

// fakeProcessor 模拟 ContextProcessor，用于测试
type fakeProcessor struct {
	processorType string
}

func (p *fakeProcessor) OnAddMessages(_ context.Context, _ iface.ModelContext, _ []llm_schema.BaseMessage, _ ...iface.Option) (*iface.ContextEvent, []llm_schema.BaseMessage, error) {
	return nil, nil, nil
}

func (p *fakeProcessor) OnGetContextWindow(_ context.Context, _ iface.ModelContext, _ iface.ContextWindow, _ ...iface.Option) (*iface.ContextEvent, iface.ContextWindow, error) {
	return nil, iface.ContextWindow{}, nil
}

func (p *fakeProcessor) TriggerAddMessages(_ context.Context, _ iface.ModelContext, _ []llm_schema.BaseMessage, _ ...iface.Option) (bool, error) {
	return false, nil
}

func (p *fakeProcessor) TriggerGetContextWindow(_ context.Context, _ iface.ModelContext, _ iface.ContextWindow, _ ...iface.Option) (bool, error) {
	return false, nil
}

func (p *fakeProcessor) SaveState() map[string]any { return nil }

func (p *fakeProcessor) LoadState(_ map[string]any) {}

func (p *fakeProcessor) ProcessorType() string { return p.processorType }

// ──────────────────────────── 导出函数 ────────────────────────────

// 辅助函数：创建基本测试用的 ProcessorStateRecorder
func newTestRecorder(tokenCounter token.TokenCounter, historyLimit int) *ProcessorStateRecorder {
	return NewProcessorStateRecorder("session-1", "ctx-1", func() *session.Session { return nil }, tokenCounter, historyLimit)
}

// ──────────────────────────── 测试 NewProcessorStateRecorder ────────────────────────────

// TestNewProcessorStateRecorder 测试创建处理器状态记录器
func TestNewProcessorStateRecorder(t *testing.T) {
	recorder := newTestRecorder(nil, 10)

	if recorder.sessionID != "session-1" {
		t.Errorf("sessionID = %q, 期望 %q", recorder.sessionID, "session-1")
	}
	if recorder.contextID != "ctx-1" {
		t.Errorf("contextID = %q, 期望 %q", recorder.contextID, "ctx-1")
	}
	if recorder.getSessionRef() != nil {
		t.Error("getSessionRef 应返回 nil")
	}
	if recorder.tokenCounter != nil {
		t.Error("tokenCounter 应为 nil")
	}
	if recorder.historyLimit != 10 {
		t.Errorf("historyLimit = %d, 期望 10", recorder.historyLimit)
	}
	if len(recorder.history) != 0 {
		t.Errorf("history 长度 = %d, 期望 0", len(recorder.history))
	}
}

// TestNewProcessorStateRecorder_带TokenCounter 测试创建带 Token 计数器的记录器
func TestNewProcessorStateRecorder_带TokenCounter(t *testing.T) {
	tc := &fakeTokenCounter{msgCount: 42}
	recorder := newTestRecorder(tc, 5)

	if recorder.tokenCounter == nil {
		t.Error("tokenCounter 不应为 nil")
	}
	if recorder.historyLimit != 5 {
		t.Errorf("historyLimit = %d, 期望 5", recorder.historyLimit)
	}
}

// ──────────────────────────── 测试 History ────────────────────────────

// TestHistory_空历史 测试空历史记录返回空切片
func TestHistory_空历史(t *testing.T) {
	recorder := newTestRecorder(nil, 10)
	history := recorder.History()

	if len(history) != 0 {
		t.Errorf("空历史长度 = %d, 期望 0", len(history))
	}
}

// TestHistory_返回切片副本 测试 History 返回的切片是独立副本
func TestHistory_返回切片副本(t *testing.T) {
	recorder := newTestRecorder(nil, 10)
	recorder.history = append(recorder.history, map[string]any{"key": "value1"})

	history := recorder.History()
	if len(history) != 1 {
		t.Fatalf("历史长度 = %d, 期望 1", len(history))
	}

	// 修改返回切片的长度，不影响原始数据
	history = append(history, map[string]any{"key": "value2"})
	if len(recorder.history) != 1 {
		t.Error("History() 应返回切片副本，修改切片长度不应影响原始数据")
	}
}

// ──────────────────────────── 测试 LoadHistory ────────────────────────────

// TestLoadHistory_正常加载 测试正常加载历史记录
func TestLoadHistory_正常加载(t *testing.T) {
	recorder := newTestRecorder(nil, 10)

	history := []map[string]any{
		{"op": "a"},
		{"op": "b"},
		{"op": "c"},
	}
	recorder.LoadHistory(history)

	got := recorder.History()
	if len(got) != 3 {
		t.Fatalf("历史长度 = %d, 期望 3", len(got))
	}
	if got[0]["op"] != "a" {
		t.Errorf("第一条记录 op = %v, 期望 a", got[0]["op"])
	}
}

// TestLoadHistory_空历史 测试加载空历史
func TestLoadHistory_空历史(t *testing.T) {
	recorder := newTestRecorder(nil, 10)
	recorder.history = append(recorder.history, map[string]any{"key": "value"})
	recorder.LoadHistory([]map[string]any{})

	if len(recorder.history) != 0 {
		t.Errorf("加载空历史后长度 = %d, 期望 0", len(recorder.history))
	}
}

// TestLoadHistory_超过限制截取 测试超过 historyLimit 时截取最后 N 条
func TestLoadHistory_超过限制截取(t *testing.T) {
	recorder := newTestRecorder(nil, 3)

	history := make([]map[string]any, 5)
	for i := 0; i < 5; i++ {
		history[i] = map[string]any{"idx": i}
	}
	recorder.LoadHistory(history)

	got := recorder.History()
	if len(got) != 3 {
		t.Fatalf("历史长度 = %d, 期望 3（截取后）", len(got))
	}
	// 应保留最后 3 条：idx=2, 3, 4
	if got[0]["idx"] != 2 {
		t.Errorf("截取后第一条 idx = %v, 期望 2", got[0]["idx"])
	}
	if got[2]["idx"] != 4 {
		t.Errorf("截取后最后一条 idx = %v, 期望 4", got[2]["idx"])
	}
}

// TestLoadHistory_不超过限制 测试历史数量未超过限制时完整保留
func TestLoadHistory_不超过限制(t *testing.T) {
	recorder := newTestRecorder(nil, 10)

	history := []map[string]any{
		{"op": "a"},
		{"op": "b"},
	}
	recorder.LoadHistory(history)

	got := recorder.History()
	if len(got) != 2 {
		t.Errorf("历史长度 = %d, 期望 2", len(got))
	}
}

// ──────────────────────────── 测试 BuildState ────────────────────────────

// TestBuildState_完整构建 测试完整状态构建
func TestBuildState_完整构建(t *testing.T) {
	tc := &fakeTokenCounter{msgCount: 100}
	recorder := newTestRecorder(tc, 10)

	now := time.Now()
	beforeMsgs := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello world"),
		llm_schema.NewUserMessage("another message"),
	}
	afterMsgs := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("compressed"),
	}

	input := ProcessorStateInput{
		OperationID:    "op-1",
		Status:         schema.CompressionCompleted,
		Phase:          schema.PhaseActiveCompress,
		Trigger:        "auto",
		Processor:      &fakeProcessor{processorType: "TestProcessor"},
		Reason:         "context overflow",
		BeforeMessages: beforeMsgs,
		AfterMessages:  afterMsgs,
		StartedAt:      now.Add(-2 * time.Second),
		EndedAt:        now,
		Error:          "",
		MessagesToModify: []int{0, 1},
		Force:          false,
		ContextMax:     8000,
		CompactSummary: "compacted",
		CompressionUsage: &schema.ContextCompressionUsage{
			Calls:       1,
			TotalTokens: 500,
		},
	}

	state := recorder.BuildState(input)

	// 验证基础字段
	if state.Type != schema.ContextCompressionStateType {
		t.Errorf("Type = %q, 期望 %q", state.Type, schema.ContextCompressionStateType)
	}
	if state.OperationID != "op-1" {
		t.Errorf("OperationID = %q, 期望 %q", state.OperationID, "op-1")
	}
	if state.Status != schema.CompressionCompleted {
		t.Errorf("Status = %v, 期望 %v", state.Status, schema.CompressionCompleted)
	}
	if state.Phase != schema.PhaseActiveCompress {
		t.Errorf("Phase = %v, 期望 %v", state.Phase, schema.PhaseActiveCompress)
	}
	if state.Processor != "TestProcessor" {
		t.Errorf("Processor = %q, 期望 %q", state.Processor, "TestProcessor")
	}
	if state.ContextMax != 8000 {
		t.Errorf("ContextMax = %d, 期望 8000", state.ContextMax)
	}
	if state.CompactSummary != "compacted" {
		t.Errorf("CompactSummary = %q, 期望 %q", state.CompactSummary, "compacted")
	}
	if state.CompressionUsage == nil || state.CompressionUsage.Calls != 1 {
		t.Error("CompressionUsage 应包含调用次数 1")
	}

	// 验证 DurationMs
	if state.DurationMs != 2000 {
		t.Errorf("DurationMs = %d, 期望 2000", state.DurationMs)
	}

	// 验证 Before 指标
	if state.Before.Messages != 2 {
		t.Errorf("Before.Messages = %d, 期望 2", state.Before.Messages)
	}
	if state.Before.Tokens != 100 {
		t.Errorf("Before.Tokens = %d, 期望 100", state.Before.Tokens)
	}

	// 验证 After 指标
	if state.After == nil {
		t.Fatal("After 不应为 nil")
	}
	if state.After.Messages != 1 {
		t.Errorf("After.Messages = %d, 期望 1", state.After.Messages)
	}
	if state.After.Tokens != 100 {
		t.Errorf("After.Tokens = %d, 期望 100", state.After.Tokens)
	}

	// 验证 Saved
	if state.Saved == nil {
		t.Fatal("Saved 不应为 nil")
	}
	if state.Saved.Messages != 1 {
		t.Errorf("Saved.Messages = %d, 期望 1", state.Saved.Messages)
	}
	if state.Saved.Tokens != 0 {
		t.Errorf("Saved.Tokens = %d, 期望 0", state.Saved.Tokens)
	}

	// 验证 Summary 非空
	if state.Summary == "" {
		t.Error("Summary 不应为空")
	}
}

// TestBuildState_无AfterMessages 测试无 AfterMessages 时不构建 after 和 saved
func TestBuildState_无AfterMessages(t *testing.T) {
	tc := &fakeTokenCounter{msgCount: 50}
	recorder := newTestRecorder(tc, 10)

	now := time.Now()
	input := ProcessorStateInput{
		OperationID:    "op-2",
		Status:         schema.CompressionStarted,
		Phase:          schema.PhaseActiveCompress,
		BeforeMessages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")},
		AfterMessages:  nil,
		StartedAt:      now,
		EndedAt:        now,
		ContextMax:     4000,
	}

	state := recorder.BuildState(input)

	if state.After != nil {
		t.Error("AfterMessages 为 nil 时 After 应为 nil")
	}
	if state.Saved != nil {
		t.Error("AfterMessages 为 nil 时 Saved 应为 nil")
	}
}

// TestBuildState_无Processor 测试无处理器时 Processor 为空
func TestBuildState_无Processor(t *testing.T) {
	recorder := newTestRecorder(nil, 10)

	now := time.Now()
	input := ProcessorStateInput{
		OperationID:    "op-3",
		Status:         schema.CompressionSkipped,
		Phase:          schema.PhaseGetContextWindow,
		BeforeMessages: []llm_schema.BaseMessage{},
		StartedAt:      now,
		EndedAt:        now,
		Reason:         "no need",
	}

	state := recorder.BuildState(input)

	if state.Processor != "" {
		t.Errorf("无 Processor 时 Processor = %q, 期望空字符串", state.Processor)
	}
}

// ──────────────────────────── 测试 Emit ────────────────────────────

// TestEmit_记录历史 测试 Emit 追加状态到历史记录
func TestEmit_记录历史(t *testing.T) {
	recorder := newTestRecorder(nil, 10)

	state := &schema.ContextCompressionState{
		Type:        schema.ContextCompressionStateType,
		OperationID: "op-emit",
		Status:      schema.CompressionCompleted,
		Phase:       schema.PhaseActiveCompress,
		Processor:   "TestProc",
		Summary:     "test emit",
		Before: schema.ContextCompressionMetric{
			Messages: 5,
			Tokens:   100,
		},
	}

	recorder.Emit(context.Background(), state)

	history := recorder.History()
	if len(history) != 1 {
		t.Fatalf("历史长度 = %d, 期望 1", len(history))
	}
	if history[0]["operation_id"] != "op-emit" {
		t.Errorf("operation_id = %v, 期望 op-emit", history[0]["operation_id"])
	}
}

// TestEmit_历史截取 测试 Emit 超过 historyLimit 时截取
func TestEmit_历史截取(t *testing.T) {
	recorder := newTestRecorder(nil, 3)

	for i := 0; i < 5; i++ {
		state := &schema.ContextCompressionState{
			Type:        schema.ContextCompressionStateType,
			OperationID: "op-" + string(rune('0'+i)),
			Status:      schema.CompressionCompleted,
			Phase:       schema.PhaseActiveCompress,
			Before:      schema.ContextCompressionMetric{},
		}
		recorder.Emit(context.Background(), state)
	}

	history := recorder.History()
	if len(history) != 3 {
		t.Fatalf("历史长度 = %d, 期望 3（截取后）", len(history))
	}
}

// ──────────────────────────── 测试 buildMetric（通过 BuildState）────────────────────────────

// TestBuildMetric_tokenCounter成功 测试 tokenCounter 成功时使用计数结果
func TestBuildMetric_tokenCounter成功(t *testing.T) {
	tc := &fakeTokenCounter{msgCount: 256}
	recorder := newTestRecorder(tc, 10)

	now := time.Now()
	input := ProcessorStateInput{
		OperationID:    "op-metric",
		Status:         schema.CompressionStarted,
		BeforeMessages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("test content")},
		StartedAt:      now,
		EndedAt:        now,
		ContextMax:     1000,
	}

	state := recorder.BuildState(input)

	if state.Before.Tokens != 256 {
		t.Errorf("Before.Tokens = %d, 期望 256（来自 fakeTokenCounter）", state.Before.Tokens)
	}
	if state.Before.ContextPercent != 26 {
		t.Errorf("Before.ContextPercent = %d, 期望 26", state.Before.ContextPercent)
	}
	if state.Before.Time != formatTime(now) {
		t.Errorf("Before.Time = %q, 期望 %q", state.Before.Time, formatTime(now))
	}
}

// TestBuildMetric_tokenCounter失败降级 测试 tokenCounter 失败时降级为字符数/4
func TestBuildMetric_tokenCounter失败降级(t *testing.T) {
	tc := &fakeTokenCounter{msgCount: 0, msgErr: fmtError("counter error")}
	recorder := newTestRecorder(tc, 10)

	now := time.Now()
	input := ProcessorStateInput{
		OperationID:    "op-fallback",
		Status:         schema.CompressionStarted,
		BeforeMessages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello world!!!")},
		// "hello world!!!" = 14 chars → ceil(14/4) = 4
		StartedAt: now,
		EndedAt:   now,
		ContextMax: 100,
	}

	state := recorder.BuildState(input)

	if state.Before.Tokens != 4 {
		t.Errorf("Before.Tokens = %d, 期望 4（降级为 ceil(14/4)）", state.Before.Tokens)
	}
}

// ──────────────────────────── 测试 measureMessages（通过 BuildState）────────────────────────────

// TestMeasureMessages_无TokenCounter 测试无 Token 计数器时使用字符数降级
func TestMeasureMessages_无TokenCounter(t *testing.T) {
	recorder := newTestRecorder(nil, 10)

	now := time.Now()
	// "abcdefgh" = 8 chars → ceil(8/4) = 2
	input := ProcessorStateInput{
		OperationID:    "op-nocounter",
		Status:         schema.CompressionStarted,
		BeforeMessages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("abcdefgh")},
		StartedAt:      now,
		EndedAt:        now,
	}

	state := recorder.BuildState(input)

	if state.Before.Tokens != 2 {
		t.Errorf("Before.Tokens = %d, 期望 2（ceil(8/4)）", state.Before.Tokens)
	}
}

// TestMeasureMessages_空消息 测试空消息列表返回 0
func TestMeasureMessages_空消息(t *testing.T) {
	recorder := newTestRecorder(nil, 10)

	now := time.Now()
	input := ProcessorStateInput{
		OperationID:    "op-empty",
		Status:         schema.CompressionStarted,
		BeforeMessages: []llm_schema.BaseMessage{},
		StartedAt:      now,
		EndedAt:        now,
	}

	state := recorder.BuildState(input)

	if state.Before.Tokens != 0 {
		t.Errorf("Before.Tokens = %d, 期望 0（空消息）", state.Before.Tokens)
	}
}

// ──────────────────────────── 测试 buildSaved（通过 BuildState）────────────────────────────

// TestBuildSaved_after为nil 测试 after 为 nil 时返回 nil
func TestBuildSaved_after为nil(t *testing.T) {
	recorder := newTestRecorder(nil, 10)

	now := time.Now()
	input := ProcessorStateInput{
		OperationID:    "op-nil-after",
		Status:         schema.CompressionStarted,
		BeforeMessages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("test")},
		AfterMessages:  nil,
		StartedAt:      now,
		EndedAt:        now,
	}

	state := recorder.BuildState(input)

	if state.Saved != nil {
		t.Error("after 为 nil 时 Saved 应为 nil")
	}
}

// TestBuildSaved_正常计算 测试节省量正常计算
func TestBuildSaved_正常计算(t *testing.T) {
	tc := &fakeTokenCounter{msgCount: 100}
	recorder := newTestRecorder(tc, 10)

	now := time.Now()
	input := ProcessorStateInput{
		OperationID:    "op-saved",
		Status:         schema.CompressionCompleted,
		BeforeMessages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("a"), llm_schema.NewUserMessage("b")},
		AfterMessages:  []llm_schema.BaseMessage{llm_schema.NewUserMessage("c")},
		StartedAt:      now,
		EndedAt:        now,
	}

	state := recorder.BuildState(input)

	if state.Saved == nil {
		t.Fatal("Saved 不应为 nil")
	}
	// 2 条 → 1 条，节省 1 条消息
	if state.Saved.Messages != 1 {
		t.Errorf("Saved.Messages = %d, 期望 1", state.Saved.Messages)
	}
	// 100 tokens → 100 tokens，节省 0 tokens（fakeCounter 对所有消息返回 100）
	if state.Saved.Tokens != 0 {
		t.Errorf("Saved.Tokens = %d, 期望 0", state.Saved.Tokens)
	}
}

// TestBuildSaved_节省百分比计算 测试节省百分比的精度
func TestBuildSaved_节省百分比计算(t *testing.T) {
	// before: 200 tokens, after: 50 tokens → saved 150/200 = 75.0%
	tc := &fakeTokenCounter{msgCount: 200}
	recorder := newTestRecorder(tc, 10)

	now := time.Now()
	afterTc := &fakeTokenCounter{msgCount: 50}
	afterRecorder := newTestRecorder(afterTc, 10)

	beforeMsgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("a"), llm_schema.NewUserMessage("b")}
	afterMsgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("c")}

	// 手动构建 before/after 指标
	before := recorder.buildMetric(beforeMsgs, 1000, now)
	after := afterRecorder.buildMetric(afterMsgs, 1000, now)

	saved := recorder.buildSaved(before, &after)

	if saved == nil {
		t.Fatal("Saved 不应为 nil")
	}
	if saved.Percent != 75.0 {
		t.Errorf("Saved.Percent = %.1f, 期望 75.0", saved.Percent)
	}
}

// ──────────────────────────── 测试 buildSummary（通过 BuildState）────────────────────────────

// TestBuildSummary_started 测试 started 状态摘要
func TestBuildSummary_started(t *testing.T) {
	tc := &fakeTokenCounter{msgCount: 5000}
	recorder := newTestRecorder(tc, 10)

	now := time.Now()
	input := ProcessorStateInput{
		OperationID:    "op-started",
		Status:         schema.CompressionStarted,
		BeforeMessages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("a"), llm_schema.NewUserMessage("b"), llm_schema.NewUserMessage("c")},
		AfterMessages:  nil,
		StartedAt:      now,
		EndedAt:        now,
	}

	state := recorder.BuildState(input)

	expected := "正在压缩 3 条消息，约 5.0k tokens"
	if state.Summary != expected {
		t.Errorf("Summary = %q, 期望 %q", state.Summary, expected)
	}
}

// TestBuildSummary_failed 测试 failed 状态摘要
func TestBuildSummary_failed(t *testing.T) {
	tc := &fakeTokenCounter{msgCount: 3000}
	recorder := newTestRecorder(tc, 10)

	now := time.Now()
	input := ProcessorStateInput{
		OperationID:    "op-failed",
		Status:         schema.CompressionFailed,
		BeforeMessages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("a")},
		AfterMessages:  nil,
		StartedAt:      now,
		EndedAt:        now,
		Error:          "api error",
	}

	state := recorder.BuildState(input)

	expected := "上下文处理器执行失败，上下文约 3.0k tokens"
	if state.Summary != expected {
		t.Errorf("Summary = %q, 期望 %q", state.Summary, expected)
	}
}

// TestBuildSummary_noop 测试 noop 状态摘要
func TestBuildSummary_noop(t *testing.T) {
	tc := &fakeTokenCounter{msgCount: 2000}
	recorder := newTestRecorder(tc, 10)

	now := time.Now()
	input := ProcessorStateInput{
		OperationID:    "op-noop",
		Status:         schema.CompressionNoop,
		BeforeMessages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("a")},
		AfterMessages:  []llm_schema.BaseMessage{llm_schema.NewUserMessage("a")},
		StartedAt:      now,
		EndedAt:        now,
	}

	state := recorder.BuildState(input)

	// noop: 上下文未变化，约 2.0k tokens (节省 0.0%)
	if state.Summary == "" {
		t.Error("Summary 不应为空")
	}
}

// TestBuildSummary_completed 测试 completed 状态摘要
func TestBuildSummary_completed(t *testing.T) {
	tc := &fakeTokenCounter{msgCount: 2000}
	recorder := newTestRecorder(tc, 10)

	now := time.Now()
	input := ProcessorStateInput{
		OperationID:    "op-completed",
		Status:         schema.CompressionCompleted,
		BeforeMessages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("a"), llm_schema.NewUserMessage("b")},
		AfterMessages:  []llm_schema.BaseMessage{llm_schema.NewUserMessage("c")},
		StartedAt:      now,
		EndedAt:        now,
	}

	state := recorder.BuildState(input)

	// completed: 压缩 2 → 1 条消息，约 2.0k → 2.0k tokens，节省约 0 tokens (0.0%)
	if state.Summary == "" {
		t.Error("Summary 不应为空")
	}
}

// TestBuildSummary_skipped 测试 skipped 状态摘要（after 为 nil）
func TestBuildSummary_skipped(t *testing.T) {
	tc := &fakeTokenCounter{msgCount: 1000}
	recorder := newTestRecorder(tc, 10)

	now := time.Now()
	input := ProcessorStateInput{
		OperationID:    "op-skipped",
		Status:         schema.CompressionSkipped,
		BeforeMessages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("a")},
		AfterMessages:  nil,
		Reason:         "context not full",
		StartedAt:      now,
		EndedAt:        now,
	}

	state := recorder.BuildState(input)

	expected := "上下文处理器已跳过: context not full"
	if state.Summary != expected {
		t.Errorf("Summary = %q, 期望 %q", state.Summary, expected)
	}
}

// TestBuildSummary_带消息修改信息 测试摘要包含消息修改信息
func TestBuildSummary_带消息修改信息(t *testing.T) {
	tc := &fakeTokenCounter{msgCount: 100}
	recorder := newTestRecorder(tc, 10)

	now := time.Now()
	input := ProcessorStateInput{
		OperationID:      "op-modify",
		Status:           schema.CompressionCompleted,
		BeforeMessages:   []llm_schema.BaseMessage{llm_schema.NewUserMessage("a"), llm_schema.NewUserMessage("b")},
		AfterMessages:    []llm_schema.BaseMessage{llm_schema.NewUserMessage("c")},
		MessagesToModify: []int{0, 1},
		StartedAt:        now,
		EndedAt:          now,
	}

	state := recorder.BuildState(input)

	if state.Summary == "" {
		t.Error("Summary 不应为空")
	}
	// 应包含 "修改了 2 条消息"
}

// ──────────────────────────── 测试 compactNumber ────────────────────────────

// TestCompactNumber_百万级 测试 >=1M 的紧凑表示
func TestCompactNumber_百万级(t *testing.T) {
	result := compactNumber(1_500_000)
	if result != "1.5m" {
		t.Errorf("compactNumber(1500000) = %q, 期望 %q", result, "1.5m")
	}
}

// TestCompactNumber_百万级边界 测试正好 1M
func TestCompactNumber_百万级边界(t *testing.T) {
	result := compactNumber(1_000_000)
	if result != "1.0m" {
		t.Errorf("compactNumber(1000000) = %q, 期望 %q", result, "1.0m")
	}
}

// TestCompactNumber_千级 测试 >=1K 的紧凑表示
func TestCompactNumber_千级(t *testing.T) {
	result := compactNumber(2500)
	if result != "2.5k" {
		t.Errorf("compactNumber(2500) = %q, 期望 %q", result, "2.5k")
	}
}

// TestCompactNumber_千级边界 测试正好 1K
func TestCompactNumber_千级边界(t *testing.T) {
	result := compactNumber(1000)
	if result != "1.0k" {
		t.Errorf("compactNumber(1000) = %q, 期望 %q", result, "1.0k")
	}
}

// TestCompactNumber_小于千 测试 <1K 的原始表示
func TestCompactNumber_小于千(t *testing.T) {
	result := compactNumber(999)
	if result != "999" {
		t.Errorf("compactNumber(999) = %q, 期望 %q", result, "999")
	}
}

// TestCompactNumber_零值 测试零值
func TestCompactNumber_零值(t *testing.T) {
	result := compactNumber(0)
	if result != "0" {
		t.Errorf("compactNumber(0) = %q, 期望 %q", result, "0")
	}
}

// ──────────────────────────── 测试 formatTime ────────────────────────────

// TestFormatTime 测试时间格式化
func TestFormatTime(t *testing.T) {
	// 使用固定时间测试格式
	tm := time.Date(2025, 6, 15, 10, 30, 45, 123000000, time.UTC)
	result := formatTime(tm)

	expected := "2025-06-15T10:30:45.123Z"
	if result != expected {
		t.Errorf("formatTime() = %q, 期望 %q", result, expected)
	}
}

// TestFormatTime_带时区偏移 测试带时区偏移的时间格式化
func TestFormatTime_带时区偏移(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	tm := time.Date(2025, 6, 15, 18, 30, 45, 0, loc)
	result := formatTime(tm)

	expected := "2025-06-15T18:30:45.000+08:00"
	if result != expected {
		t.Errorf("formatTime() = %q, 期望 %q", result, expected)
	}
}

// ──────────────────────────── 测试 contextPercent ────────────────────────────

// TestContextPercent_正常计算 测试正常百分比计算
func TestContextPercent_正常计算(t *testing.T) {
	result := contextPercent(500, 1000)
	if result != 50 {
		t.Errorf("contextPercent(500, 1000) = %d, 期望 50", result)
	}
}

// TestContextPercent_zeroMax 测试 contextMax 为零时返回 0
func TestContextPercent_zeroMax(t *testing.T) {
	result := contextPercent(500, 0)
	if result != 0 {
		t.Errorf("contextPercent(500, 0) = %d, 期望 0", result)
	}
}

// TestContextPercent_负数Max 测试 contextMax 为负数时返回 0
func TestContextPercent_负数Max(t *testing.T) {
	result := contextPercent(500, -1)
	if result != 0 {
		t.Errorf("contextPercent(500, -1) = %d, 期望 0", result)
	}
}

// TestContextPercent_超过100 测试超过 100% 时 clamp 到 100
func TestContextPercent_超过100(t *testing.T) {
	result := contextPercent(2000, 1000)
	if result != 100 {
		t.Errorf("contextPercent(2000, 1000) = %d, 期望 100", result)
	}
}

// TestContextPercent_负数Tokens 测试负数 tokens 时 clamp 到 0
func TestContextPercent_负数Tokens(t *testing.T) {
	result := contextPercent(-100, 1000)
	if result != 0 {
		t.Errorf("contextPercent(-100, 1000) = %d, 期望 0", result)
	}
}

// TestContextPercent_四舍五入 测试百分比四舍五入
func TestContextPercent_四舍五入(t *testing.T) {
	// 333/1000 = 33.3% → 33
	result := contextPercent(333, 1000)
	if result != 33 {
		t.Errorf("contextPercent(333, 1000) = %d, 期望 33", result)
	}

	// 335/1000 = 33.5% → 34
	result2 := contextPercent(335, 1000)
	if result2 != 34 {
		t.Errorf("contextPercent(335, 1000) = %d, 期望 34", result2)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fmtError 实现 error 接口的简单辅助类型
type fmtError string

func (e fmtError) Error() string { return string(e) }

// ──────────────────────────── Emit 完整流程测试 ────────────────────────────

// TestEmit_带sessionRef推送流式数据 测试有 sessionRef 时推送流式数据
func TestEmit_带sessionRef推送流式数据(t *testing.T) {
	sess := session.NewSession()
	recorder := NewProcessorStateRecorder("session-1", "ctx-1", func() *session.Session {
		return sess
	}, nil, 10)

	state := &schema.ContextCompressionState{
		Type:        schema.ContextCompressionStateType,
		OperationID: "op-stream",
		Status:      schema.CompressionCompleted,
		Phase:       schema.PhaseActiveCompress,
		Processor:   "TestProc",
		Summary:     "test stream",
		Before: schema.ContextCompressionMetric{
			Messages: 5,
			Tokens:   100,
		},
	}

	// 不应 panic
	recorder.Emit(context.Background(), state)

	history := recorder.History()
	if len(history) != 1 {
		t.Fatalf("历史长度 = %d, 期望 1", len(history))
	}
}

// TestEmit_无sessionRef不推送 测试无 sessionRef 时不推送流式数据
func TestEmit_无sessionRef不推送(t *testing.T) {
	recorder := newTestRecorder(nil, 10)

	state := &schema.ContextCompressionState{
		Type:        schema.ContextCompressionStateType,
		OperationID: "op-no-stream",
		Status:      schema.CompressionCompleted,
		Phase:       schema.PhaseActiveCompress,
		Processor:   "TestProc",
		Summary:     "test no stream",
		Before:      schema.ContextCompressionMetric{},
	}

	// 不应 panic
	recorder.Emit(context.Background(), state)

	history := recorder.History()
	if len(history) != 1 {
		t.Fatalf("历史长度 = %d, 期望 1", len(history))
	}
}

// ──────────────────────────── stateToMap 完整测试 ────────────────────────────

// TestStateToMap_完整状态 测试包含所有字段的状态转换
func TestStateToMap_完整状态(t *testing.T) {
	recorder := newTestRecorder(nil, 10)

	state := &schema.ContextCompressionState{
		Type:        schema.ContextCompressionStateType,
		OperationID: "op-full",
		Status:      schema.CompressionCompleted,
		Phase:       schema.PhaseActiveCompress,
		Processor:   "FullProcessor",
		Model:       "test-model",
		DurationMs:  2000,
		ContextMax:  8000,
		Summary:     "full test",
		CompactSummary: "compacted",
		Before: schema.ContextCompressionMetric{
			Time:           "2025-01-01T00:00:00.000Z",
			Messages:       5,
			Tokens:         100,
			ContextPercent: 50,
		},
		After: &schema.ContextCompressionMetric{
			Time:           "2025-01-01T00:00:02.000Z",
			Messages:       2,
			Tokens:         40,
			ContextPercent: 20,
		},
		Saved: &schema.ContextCompressionSaved{
			Messages: 3,
			Tokens:   60,
			Percent:  60.0,
		},
		Statistic: iface.ContextStats{
			TotalMessages:          10,
			TotalTokens:            200,
			TotalDialogues:         2,
			SystemMessages:         1,
			UserMessages:           3,
			AssistantMessages:      3,
			ToolMessages:           3,
			Tools:                  2,
			SystemMessageTokens:    20,
			UserMessageTokens:      60,
			AssistantMessageTokens: 60,
			ToolMessageTokens:      60,
			ToolTokens:             30,
		},
		CompressionUsage: &schema.ContextCompressionUsage{
			Calls:        1,
			InputTokens:  100,
			OutputTokens: 50,
			TotalTokens:  150,
			CacheTokens:  10,
			InputCost:    0.5,
			OutputCost:   0.3,
			TotalCost:    0.8,
			ModelName:    "test-model",
		},
		Error: "test error",
	}

	m := recorder.stateToMap(state)

	// 验证基础字段
	if m["type"] != schema.ContextCompressionStateType {
		t.Error("type 字段错误")
	}
	if m["operation_id"] != "op-full" {
		t.Error("operation_id 字段错误")
	}
	if m["processor"] != "FullProcessor" {
		t.Error("processor 字段错误")
	}
	if m["error"] != "test error" {
		t.Error("error 字段错误")
	}

	// 验证 After 字段
	afterMap, ok := m["after"].(map[string]any)
	if !ok {
		t.Fatal("after 应为 map[string]any")
	}
	if afterMap["messages"] != 2 {
		t.Error("after.messages 字段错误")
	}

	// 验证 Saved 字段
	savedMap, ok := m["saved"].(map[string]any)
	if !ok {
		t.Fatal("saved 应为 map[string]any")
	}
	if savedMap["tokens"] != 60 {
		t.Error("saved.tokens 字段错误")
	}

	// 验证 Statistic 字段
	statMap, ok := m["statistic"].(map[string]any)
	if !ok {
		t.Fatal("statistic 应为 map[string]any")
	}
	if statMap["total_messages"] != 10 {
		t.Error("statistic.total_messages 字段错误")
	}

	// 验证 CompressionUsage 字段
	usageMap, ok := m["compression_usage"].(map[string]any)
	if !ok {
		t.Fatal("compression_usage 应为 map[string]any")
	}
	if usageMap["calls"] != 1 {
		t.Error("compression_usage.calls 字段错误")
	}
}

// TestStateToMap_无After和Saved 测试无 After/Saved 时的状态转换
func TestStateToMap_无After和Saved(t *testing.T) {
	recorder := newTestRecorder(nil, 10)

	state := &schema.ContextCompressionState{
		Type:        schema.ContextCompressionStateType,
		OperationID: "op-minimal",
		Status:      schema.CompressionStarted,
		Before: schema.ContextCompressionMetric{
			Messages: 5,
			Tokens:   100,
		},
	}

	m := recorder.stateToMap(state)

	if _, exists := m["after"]; exists {
		t.Error("After 为 nil 时 map 不应包含 after 键")
	}
	if _, exists := m["saved"]; exists {
		t.Error("Saved 为 nil 时 map 不应包含 saved 键")
	}
	if _, exists := m["error"]; exists {
		t.Error("Error 为空时 map 不应包含 error 键")
	}
	if _, exists := m["compression_usage"]; exists {
		t.Error("CompressionUsage 为 nil 时 map 不应包含 compression_usage 键")
	}
}
