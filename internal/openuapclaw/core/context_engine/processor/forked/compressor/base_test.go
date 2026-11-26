package compressor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	cesupport "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/compressor/support"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// newBaseCompressor 构造带自定义分段钩子的基座处理器（带模型配置，executor 就绪——
// PY trigger_get_context_window base.py:184 要求 executor 非空）。
func newBaseCompressor(t *testing.T, mutate func(*PrefixCompactProcessorConfig)) *PrefixCompactProcessor {
	t.Helper()
	cfg := NewPrefixCompactProcessorConfig()
	if mutate != nil {
		mutate(cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	mcc, err := llm_schema.NewModelClientConfig("openai", "test-key", "https://mock.test",
		llm_schema.WithVerifySSL(false))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ModelClient = mcc
	cfg.Model = llm_schema.NewModelRequestConfig()
	return NewPrefixCompactProcessor(cfg)
}

// buildSimpleSpan 分段钩子：首条保留，中间压缩，末条保护。
func buildSimpleSpan(messages []llm_schema.BaseMessage) PrefixCompactSpan {
	if len(messages) < 3 {
		return PrefixCompactSpan{}
	}
	return PrefixCompactSpan{
		PreservedPrefix:    messages[:1],
		MessagesToCompress: messages[1 : len(messages)-1],
		ProtectedTail:      messages[len(messages)-1:],
	}
}

// bigWindow 构造足量消息窗口（char 兜底计数可越过阈值）
func bigWindow() iface.ContextWindow {
	return iface.ContextWindow{
		SystemMessages: []llm_schema.BaseMessage{llm_schema.NewSystemMessage(strings.Repeat("sys", 900))},
		ContextMessages: []llm_schema.BaseMessage{
			llm_schema.NewUserMessage(strings.Repeat("u1", 900)),
			llm_schema.NewAssistantMessage(strings.Repeat("a1", 900)),
			llm_schema.NewUserMessage(strings.Repeat("u2", 900)),
		},
	}
}

// TestPrefixCompactProcessor_Trigger_未达阈值不触发 低于绝对阈值返回 false 并写 debug。
// Python: base.py:193-204 below_threshold 分支。
func TestPrefixCompactProcessor_Trigger_未达阈值不触发(t *testing.T) {
	ws := t.TempDir()
	mc := &compressorFakeContext{sessionID: "s1", workspaceDir: ws}
	p := newBaseCompressor(t, func(c *PrefixCompactProcessorConfig) {
		c.EnableCompressionDump = true
		c.CompressionDumpDir = ws
	})
	p.buildSpanHook = buildSimpleSpan
	// 小窗口（3 条短消息 ≈ 12 tokens << 400）
	small := iface.ContextWindow{ContextMessages: []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hi"),
		llm_schema.NewAssistantMessage("hello"),
		llm_schema.NewUserMessage("ok"),
	}}
	triggered, err := p.TriggerGetContextWindow(context.Background(), mc, small)
	if err != nil {
		t.Fatal(err)
	}
	if triggered {
		t.Error("未达阈值不应触发")
	}
}

// TestPrefixCompactProcessor_Trigger_无可压缩跨度不触发 has_target=false 分支。
// Python: base.py:206-214 no_compressible_span。
func TestPrefixCompactProcessor_Trigger_无可压缩跨度不触发(t *testing.T) {
	mc := &compressorFakeContext{sessionID: "s1", workspaceDir: t.TempDir()}
	p := newBaseCompressor(t, nil)
	p.buildSpanHook = func([]llm_schema.BaseMessage) PrefixCompactSpan { return PrefixCompactSpan{} }
	triggered, err := p.TriggerGetContextWindow(context.Background(), mc, bigWindow())
	if err != nil {
		t.Fatal(err)
	}
	if triggered {
		t.Error("无压缩跨度不应触发")
	}
}

// TestPrefixCompactProcessor_Trigger_目标低于最小值不触发 min_target 分支。
// Python: base.py:220-238 target_below_min。
func TestPrefixCompactProcessor_Trigger_目标低于最小值不触发(t *testing.T) {
	mc := &compressorFakeContext{sessionID: "s1", workspaceDir: t.TempDir()}
	p := newBaseCompressor(t, func(c *PrefixCompactProcessorConfig) {
		c.MinTargetContextRatio = 0.5 // context_max=1000 → min=500
	})
	// 总量越过阈值（~740 ≥ 400）但压缩目标只有 ~30 tokens < 500
	p.buildSpanHook = func(messages []llm_schema.BaseMessage) PrefixCompactSpan {
		if len(messages) < 3 {
			return PrefixCompactSpan{}
		}
		return PrefixCompactSpan{
			PreservedPrefix:    messages[:1],
			MessagesToCompress: messages[1:2],
			ProtectedTail:      messages[2:],
		}
	}
	smallTarget := iface.ContextWindow{
		SystemMessages: []llm_schema.BaseMessage{llm_schema.NewSystemMessage(strings.Repeat("sys", 1200))},
		ContextMessages: []llm_schema.BaseMessage{
			llm_schema.NewUserMessage(strings.Repeat("p", 750)),
			llm_schema.NewUserMessage("tiny"),
			llm_schema.NewUserMessage(strings.Repeat("t", 120)),
		},
	}
	triggered, err := p.TriggerGetContextWindow(context.Background(), mc, smallTarget)
	if err != nil {
		t.Fatal(err)
	}
	if triggered {
		t.Error("压缩目标低于最小值不应触发")
	}
}

// TestPrefixCompactProcessor_Trigger_超阈值触发 全链通过返回 true。
// Python: base.py:241-258 reached_threshold。
func TestPrefixCompactProcessor_Trigger_超阈值触发(t *testing.T) {
	mc := &compressorFakeContext{sessionID: "s1", workspaceDir: t.TempDir()}
	p := newBaseCompressor(t, nil)
	p.buildSpanHook = buildSimpleSpan
	triggered, err := p.TriggerGetContextWindow(context.Background(), mc, bigWindow())
	if err != nil {
		t.Fatal(err)
	}
	if !triggered {
		t.Error("超阈值应触发")
	}
}

// TestPrefixCompactProcessor_DebugRecord落盘 threshold_check 写入 dump 目录。
// Python: base.py:861 _write_context_debug。
func TestPrefixCompactProcessor_DebugRecord落盘(t *testing.T) {
	ws := t.TempDir()
	mc := &compressorFakeContext{sessionID: "s1", workspaceDir: ws}
	p := newBaseCompressor(t, func(c *PrefixCompactProcessorConfig) {
		c.EnableCompressionDump = true
		c.CompressionDumpDir = ws
	})
	p.buildSpanHook = buildSimpleSpan
	if _, err := p.TriggerGetContextWindow(context.Background(), mc, bigWindow()); err != nil {
		t.Fatal(err)
	}
	found := false
	_ = filepath.WalkDir(ws, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			found = true
		}
		return nil
	})
	if !found {
		t.Error("dump 目录应产生至少一条 debug 记录")
	}
}

// TestPrefixCompactProcessor_OnGetContextWindow_无执行体返回nil executor 未初始化直接返回。
// Python: base.py:258-261。
func TestPrefixCompactProcessor_OnGetContextWindow_无执行体返回nil(t *testing.T) {
	mc := &compressorFakeContext{sessionID: "s1", workspaceDir: t.TempDir()}
	p := newBaseCompressor(t, nil)
	cw := bigWindow()
	event, out, err := p.OnGetContextWindow(context.Background(), mc, cw)
	if err != nil || event != nil {
		t.Errorf("无执行体应返回 nil event, got %v err %v", event, err)
	}
	_ = out
}

// TestAdjustKeepRecentForToolBoundaries 工具配对不拆散。
// Python: base.py:101。
func TestAdjustKeepRecentForToolBoundaries(t *testing.T) {
	// 场景：keep_recent=1 只保住 ToolMessage，需外扩到携带 tool_call 的 AssistantMessage
	assistant := llm_schema.NewAssistantMessage("with tool")
	assistant.ToolCalls = []*llm_schema.ToolCall{{ID: "tc1"}}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("q"),
		assistant,
		llm_schema.NewToolMessage("tc1", "result"),
	}
	got := AdjustKeepRecentForToolBoundaries(messages, 1)
	// PY 语义：起点回退到 assistant 所在下标 1，保护 [assistant, tool] 两条 → len-start=2
	if got != 2 {
		t.Errorf("应外扩到工具配对起点（want 2, got %d）", got)
	}
	// 空/负值
	if got := AdjustKeepRecentForToolBoundaries(nil, 2); got != 2 {
		t.Errorf("空消息应原样返回, got %d", got)
	}
	if got := AdjustKeepRecentForToolBoundaries(messages, 0); got != 0 {
		t.Errorf("keep_recent<=0 应返回 0, got %d", got)
	}
}

// TestFindLastRealUserMessageIndex 内部前缀消息不算真实用户消息。
// Python: base.py:126/133。
func TestFindLastRealUserMessageIndex(t *testing.T) {
	internal := llm_schema.NewUserMessage(cesupport.InternalUserPrefixes[0] + "injected")
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("real"),
		llm_schema.NewAssistantMessage("ok"),
		internal,
	}
	if got := FindLastRealUserMessageIndex(messages); got != 0 {
		t.Errorf("内部前缀消息应被跳过（want 0, got %d）", got)
	}
	if got := FindLastRealUserMessageIndex(nil); got != -1 {
		t.Errorf("空消息应返回 -1, got %d", got)
	}
}

// TestWrapMemoryBlockWithRecallMarker 记忆块形状含召回标记。
// Python: base.py:766。
func TestWrapMemoryBlockWithRecallMarker(t *testing.T) {
	p := newBaseCompressor(t, nil)
	withID := p.wrapMemoryBlock("sum", "mem-1")
	for _, part := range []string{"<memory_block>", "<meaning>", "<conflict_policy>", "<summary>", "sum", "[[COMPRESSION_RECALL: id=mem-1]]", "</memory_block>"} {
		if !strings.Contains(withID, part) {
			t.Errorf("记忆块缺少 %s", part)
		}
	}
	without := p.wrapMemoryBlock("sum", "")
	if strings.Contains(without, "COMPRESSION_RECALL") {
		t.Error("无 memory_id 不应有召回标记")
	}
}

// TestExtractStateSnapshotOrRaw 快照抽取与回退。
// Python: base.py:783。
func TestExtractStateSnapshotOrRaw(t *testing.T) {
	if got := extractStateSnapshotOrRaw(""); got != "" {
		t.Errorf("空文本应返回空串, got %q", got)
	}
	if got := extractStateSnapshotOrRaw("plain"); got != "plain" {
		t.Errorf("无快照标签应返回原文, got %q", got)
	}
	if got := extractStateSnapshotOrRaw("before <state_snapshot> inner </state_snapshot>"); got != "inner" {
		t.Errorf("应抽取快照内容, got %q", got)
	}
	// 快照为空回退原文
	if got := extractStateSnapshotOrRaw("<state_snapshot>   </state_snapshot>"); got == "" {
		t.Error("空快照应回退原文")
	}
}

// TestBuildContextOverflowRetrySpan 预算收缩把溢出消息移入保护尾巴。
// Python: base.py:545。
func TestBuildContextOverflowRetrySpan(t *testing.T) {
	mc := &compressorFakeContext{sessionID: "s1", workspaceDir: t.TempDir()}
	p := newBaseCompressor(t, nil)
	cw := iface.ContextWindow{}
	span := PrefixCompactSpan{
		PreservedPrefix:    []llm_schema.BaseMessage{llm_schema.NewUserMessage(strings.Repeat("p", 600))},
		MessagesToCompress: []llm_schema.BaseMessage{llm_schema.NewUserMessage(strings.Repeat("c", 600))},
		ProtectedTail:      []llm_schema.BaseMessage{llm_schema.NewUserMessage("tail")},
	}
	// 极小预算 → 一条都放不下 → nil
	got := p.buildContextOverflowRetrySpan(mc, &cw, span, "prompt", 0.001)
	if got == nil {
		t.Log("极小预算返回 nil（符合预算耗尽路径）")
	} else if len(got.MessagesToCompress) >= len(span.MessagesToCompress) {
		t.Error("收缩后的压缩段应严格变小")
	}
}

// TestBuildPromptWithPreserveInstruction 保留指令拼接。
// Python: base.py:682。
func TestBuildPromptWithPreserveInstruction(t *testing.T) {
	p := newBaseCompressor(t, nil)
	if got := p.buildPrompt(PrefixCompactSpan{}, ""); got != "" {
		t.Errorf("无指令应返回默认提示词（空）, got %q", got)
	}
	got := p.buildPrompt(PrefixCompactSpan{}, "keep ids")
	if !strings.Contains(got, "keep ids") || !strings.Contains(got, "preservation priority") {
		t.Error("应拼接保留指令模板")
	}
}
