// T11 单测：forked offloader 触发/卸载/保护名单。
package offloader

import (
	"context"
	"strings"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/token"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// fakeContext 最小 ModelContext（窗口 + 消息缓冲 + workspace）
type fakeContext struct {
	iface.ModelContext
	sessionID    string
	workspaceDir string
	messages     []llm_schema.BaseMessage
	windowAccess float64
	hasAccess    bool
}

func (f *fakeContext) SessionID() string    { return f.sessionID }
func (f *fakeContext) ContextID() string    { return f.sessionID + "-ctx" }
func (f *fakeContext) WorkspaceDir() string { return f.workspaceDir }
func (f *fakeContext) Len() int             { return len(f.messages) }
func (f *fakeContext) GetMessages(int, bool) ([]llm_schema.BaseMessage, error) {
	return f.messages, nil
}
func (f *fakeContext) SetMessages(messages []llm_schema.BaseMessage, _ bool) { f.messages = messages }
func (f *fakeContext) TokenCounter() token.TokenCounter                      { return nil }
func (f *fakeContext) ModelName() string                                     { return "" }
func (f *fakeContext) ContextWindowTokens() int                              { return 1000 }
func (f *fakeContext) ModelContextWindowTokens() map[string]int              { return map[string]int{} }

// LastWindowAccessAt/SetWindowAccessAt 实现 TTL 能力面（D-24.15-a6）
func (f *fakeContext) LastWindowAccessAt() (float64, bool) { return f.windowAccess, f.hasAccess }
func (f *fakeContext) SetWindowAccessAt(ts float64)        { f.windowAccess, f.hasAccess = ts, true }

// bigToolMessage 超大工具结果（带 tool_call 配对）
func pairMessages(content string, toolName string, argsJSON string) ([]llm_schema.BaseMessage, llm_schema.BaseMessage) {
	assistant := llm_schema.NewAssistantMessage("")
	assistant.ToolCalls = []*llm_schema.ToolCall{llm_schema.NewToolCall("call-1", toolName, argsJSON)}
	tool := llm_schema.NewToolMessage("call-1", content)
	return []llm_schema.BaseMessage{assistant}, tool
}

// TestTriggerAddMessages 超阈触发。
func TestTriggerAddMessages(t *testing.T) {
	mo := NewMessageSummaryOffloader(NewMessageSummaryOffloaderConfig())
	mc := &fakeContext{sessionID: "s1", workspaceDir: t.TempDir()}
	_, tool := pairMessages(strings.Repeat("x", 9000), "bash", "{}")
	triggered, err := mo.TriggerAddMessages(context.Background(), mc, []llm_schema.BaseMessage{tool})
	if err != nil {
		t.Fatal(err)
	}
	if !triggered {
		t.Error("超阈工具结果应触发")
	}
	// 小内容不触发
	small := llm_schema.NewToolMessage("call-1", "ok")
	if triggered, _ := mo.TriggerAddMessages(context.Background(), mc, []llm_schema.BaseMessage{small}); triggered {
		t.Error("阈下内容不应触发")
	}
}

// TestOnAddMessages_OffloadPlaceholder 超阈卸载产出占位消息。
func TestOnAddMessages_OffloadPlaceholder(t *testing.T) {
	ws := t.TempDir()
	mo := NewMessageSummaryOffloader(NewMessageSummaryOffloaderConfig())
	assistant, tool := pairMessages(strings.Repeat("y", 9000), "bash", "{}")
	mc := &fakeContext{sessionID: "s1", workspaceDir: ws, messages: assistant}
	event, processed, err := mo.OnAddMessages(context.Background(), mc, []llm_schema.BaseMessage{tool})
	if err != nil {
		t.Fatal(err)
	}
	if event == nil || len(processed) != 1 {
		t.Fatalf("应产出事件与新列表, event=%v", event)
	}
	text := messageContent(processed[0])
	if !strings.Contains(text, "Content truncated and offloaded") {
		t.Errorf("应含 head/tail 预览描述: %q", text[:min(120, len(text))])
	}
	if len(text) >= 9000 {
		t.Error("内容应被裁剪为预览")
	}
}

// TestProtectedToolMessage 保护工具不处理。
func TestProtectedToolMessage(t *testing.T) {
	mo := NewMessageSummaryOffloader(NewMessageSummaryOffloaderConfig())
	assistant, tool := pairMessages(strings.Repeat("z", 9000), "read_file", `{"file_path":"/a/SKILL.md"}`)
	mc := &fakeContext{sessionID: "s1", workspaceDir: t.TempDir(), messages: assistant}
	_, processed, err := mo.OnAddMessages(context.Background(), mc, []llm_schema.BaseMessage{tool})
	if err != nil {
		t.Fatal(err)
	}
	if messageContent(processed[0]) != strings.Repeat("z", 9000) {
		t.Error("保护工具（read_file:*SKILL.md 模式未配置时不匹配名前缀——默认名单 read_file 精确名匹配应放行原文）")
	}
	// 默认名单 read_file 精确匹配 → 应跳过
	if !mo.isProtectedToolMessage(tool, mc.messages) {
		t.Error("read_file 应在默认保护名单")
	}
}

// TestTriggerGetContextWindow_TTL TTL 判定链。
func TestTriggerGetContextWindow_TTL(t *testing.T) {
	mo := NewMessageSummaryOffloader(NewMessageSummaryOffloaderConfig())
	mc := &fakeContext{sessionID: "s1", workspaceDir: t.TempDir(), windowAccess: 0, hasAccess: true}
	mc.messages = []llm_schema.BaseMessage{llm_schema.NewToolMessage("c", strings.Repeat("q", 3000))}
	// now=400s，距上次 400s > ttl 300 → 触发（覆盖包级时间源）
	previous := nowFunc
	nowFunc = func() float64 { return 400 }
	defer func() { nowFunc = previous }()
	mo.rulePipeline = newPipelineWithNow(nowFunc, mo.Config())
	triggered, err := mo.TriggerGetContextWindow(context.Background(), mc, iface.ContextWindow{})
	if err != nil {
		t.Fatal(err)
	}
	if !triggered {
		t.Error("TTL 空闲超时 + 占用超阈应触发")
	}
	// 未记录过访问 → 不触发
	mc.hasAccess = false
	if triggered, _ := mo.TriggerGetContextWindow(context.Background(), mc, iface.ContextWindow{}); triggered {
		t.Error("无历史访问不应触发")
	}
}
