package agent_teams

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	ceinterface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/token"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
)

// ──────────────────────────── 测试 ────────────────────────────

// fakeCompactTarget CompactContextTarget 测试替身：GetCurrentContext 返回
// 预置消息，SetMessages 经 fakeModelContext 记录回写结果；model 为 nil 时走
// skipped 分支。
type fakeCompactTarget struct {
	msgs   []llmschema.BaseMessage
	engine ceinterface.ContextEngine
}

func (f *fakeCompactTarget) GetCurrentContext(_ context.Context, _ string, _ string) ([]llmschema.BaseMessage, error) {
	return f.msgs, nil
}

func (f *fakeCompactTarget) ContextEngineForCheckpoint() ceinterface.ContextEngine { return f.engine }

func (f *fakeCompactTarget) CompactionModel() *llm.Model { return nil }

// fakeModelContext 最小 ModelContext 替身：只记录 SetMessages，其余为 no-op。
type fakeModelContext struct {
	lastSet    []llmschema.BaseMessage
	setHistory bool
}

func (m *fakeModelContext) Len() int { return len(m.lastSet) }
func (m *fakeModelContext) GetMessages(size int, withHistory bool) ([]llmschema.BaseMessage, error) {
	return m.lastSet, nil
}
func (m *fakeModelContext) SetMessages(messages []llmschema.BaseMessage, withHistory bool) {
	m.lastSet = messages
	m.setHistory = withHistory
}
func (m *fakeModelContext) PopMessages(size int, withHistory bool) []llmschema.BaseMessage {
	return nil
}
func (m *fakeModelContext) ClearMessages(_ context.Context, _ bool, _ ...ceinterface.Option) error {
	return nil
}
func (m *fakeModelContext) AddMessages(_ context.Context, messages []llmschema.BaseMessage, _ ...ceinterface.Option) ([]llmschema.BaseMessage, error) {
	return messages, nil
}
func (m *fakeModelContext) GetContextWindow(_ context.Context, _ []llmschema.BaseMessage,
	_ []schema.ToolInfoInterface, _ int, _ int, _ ...ceinterface.Option) (*ceinterface.ContextWindow, error) {
	return nil, nil
}
func (m *fakeModelContext) Statistic() *ceinterface.ContextStats { return &ceinterface.ContextStats{} }
func (m *fakeModelContext) SessionID() string                    { return "default_session_id" }
func (m *fakeModelContext) ContextID() string                    { return "default_context_id" }
func (m *fakeModelContext) TokenCounter() token.TokenCounter     { return nil }
func (m *fakeModelContext) ReloaderTool() tool.Tool              { return nil }
func (m *fakeModelContext) WorkspaceDir() string                 { return "" }
func (m *fakeModelContext) SetSessionRef(_ sessioninterfaces.SessionFacade)  {}
func (m *fakeModelContext) GetSessionRef() sessioninterfaces.SessionFacade   { return nil }
func (m *fakeModelContext) OffloadMessages(_ string, _ []llmschema.BaseMessage) {}
func (m *fakeModelContext) SaveState() map[string]any            { return nil }
func (m *fakeModelContext) LoadState(_ map[string]any)           {}
func (m *fakeModelContext) CompressContext(_ context.Context, _ ...ceinterface.CompressContextOption) (*ceinterface.CompressContextResult, error) {
	return nil, nil
}

// fakeCompactEngine 最小 ContextEngine 替身：GetContext 返回固定 ModelContext。
type fakeCompactEngine struct{ mc ceinterface.ModelContext }

func (e *fakeCompactEngine) CreateContext(_ context.Context, _ string, _ sessioninterfaces.SessionFacade, _ ...ceinterface.CreateContextOption) (ceinterface.ModelContext, error) {
	return e.mc, nil
}
func (e *fakeCompactEngine) GetContext(_ string, _ string) ceinterface.ModelContext { return e.mc }
func (e *fakeCompactEngine) CompressContext(_ context.Context, _ string, _ sessioninterfaces.SessionFacade, _ ...ceinterface.CompressContextOption) (*ceinterface.CompressContextResult, error) {
	return nil, nil
}
func (e *fakeCompactEngine) ClearContext(_ context.Context, _ ...ceinterface.ClearContextOption) error {
	return nil
}
func (e *fakeCompactEngine) SaveContexts(_ context.Context, _ sessioninterfaces.SessionFacade, _ []string) (map[string]any, error) {
	return nil, nil
}
func (e *fakeCompactEngine) UpdateModelContext(_ string, _ int) {}
func (e *fakeCompactEngine) RecoverFromModelContextOverflow(_ context.Context, _ sessioninterfaces.SessionFacade,
	_ string, _ bool, _ int) bool {
	return false
}

// mkMsgs 构造 user/assistant 交替消息。
func mkMsgs(n int) []llmschema.BaseMessage {
	msgs := make([]llmschema.BaseMessage, 0, n)
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			msgs = append(msgs, llmschema.NewUserMessage(strings.Repeat("u", i+1)))
		} else {
			msgs = append(msgs, llmschema.NewAssistantMessage(strings.Repeat("a", i+1)))
		}
	}
	return msgs
}

// TestCompactContext_Split越界Skip split_at<=0 或 >=len 时不做任何回写。
// Python: fork_compact.py:64-68
func TestCompactContext_Split越界Skip(t *testing.T) {
	for _, split := range []int{0, -1, 4, 100} {
		mc := &fakeModelContext{}
		target := &fakeCompactTarget{msgs: mkMsgs(4), engine: &fakeCompactEngine{mc: mc}}
		CompactContext(context.Background(), target, split, "", "before")
		require.Nil(t, mc.lastSet, "split=%d 越界不应回写", split)
	}
}

// TestCompactContext_Before压缩头部 压缩头部成摘要消息置顶、尾部逐字保留。
// Python: fork_compact.py:80-83 + else 分支（kept=tail, compress=head）
func TestCompactContext_Before压缩头部(t *testing.T) {
	mc := &fakeModelContext{}
	target := &fakeCompactTarget{msgs: mkMsgs(4), engine: &fakeCompactEngine{mc: mc}}
	CompactContext(context.Background(), target, 2, "", "before")
	require.NotNil(t, mc.lastSet)
	// 无模型 → 摘要为 skipped 文案；结果 = [summary, msgs[2], msgs[3]]
	require.Len(t, mc.lastSet, 3)
	require.Contains(t, mc.lastSet[0].GetContent().Text(),
		"[compaction skipped: no model available]")
	require.Contains(t, mc.lastSet[0].GetContent().Text(),
		"[Compacted context — earlier conversation summarised]")
	require.Equal(t, "uuu", mc.lastSet[1].GetContent().Text())
	require.Equal(t, "aaaa", mc.lastSet[2].GetContent().Text())
	require.True(t, mc.setHistory, "全量替换（Python set_messages）")
}

// TestCompactContext_After压缩尾部保留头部 头部延伸过闭合 ToolMessage 块。
// Python: fork_compact.py:73-79
func TestCompactContext_After压缩尾部保留头部(t *testing.T) {
	msgs := []llmschema.BaseMessage{
		llmschema.NewUserMessage("u1"),
		llmschema.NewAssistantMessage("a1"),
		llmschema.NewToolMessage("call-1", "result-1"),
		llmschema.NewToolMessage("call-2", "result-2"), // 闭合块延续
		llmschema.NewAssistantMessage("a2"),
		llmschema.NewUserMessage("u2"),
	}
	mc := &fakeModelContext{}
	target := &fakeCompactTarget{msgs: msgs, engine: &fakeCompactEngine{mc: mc}}
	CompactContext(context.Background(), target, 2, "", "after")
	require.NotNil(t, mc.lastSet)
	// 头部延伸过 ToolMessage 块：u1,a1,t1,t2 保留；t2 之后压缩成摘要置尾。
	require.Len(t, mc.lastSet, 5)
	require.Equal(t, "u1", mc.lastSet[0].GetContent().Text())
	require.Equal(t, "a1", mc.lastSet[1].GetContent().Text())
	require.Equal(t, "result-1", mc.lastSet[2].GetContent().Text())
	require.Equal(t, "result-2", mc.lastSet[3].GetContent().Text())
	require.Contains(t, mc.lastSet[4].GetContent().Text(),
		"[compaction skipped: no model available]")
}

// TestCompactContext_剥SystemMessage 压缩前剥掉全部 SystemMessage。
// Python: fork_compact.py:61-62
func TestCompactContext_剥SystemMessage(t *testing.T) {
	msgs := append([]llmschema.BaseMessage{llmschema.NewSystemMessage("sys")},
		mkMsgs(4)...)
	mc := &fakeModelContext{}
	target := &fakeCompactTarget{msgs: msgs, engine: &fakeCompactEngine{mc: mc}}
	// split=2 按剥除后的列表计（u,aa,uuu,aaaa 中 split=2 → 压缩 u,aa）
	CompactContext(context.Background(), target, 2, "", "before")
	require.Len(t, mc.lastSet, 3) // summary + uuu + aaaa
	require.Equal(t, "uuu", mc.lastSet[1].GetContent().Text())
}

// TestRenderMessageTexts 空内容消息被跳过、角色标签格式对齐。
// Python: _render_message_texts (fork_compact.py:140-147)
func TestRenderMessageTexts(t *testing.T) {
	out := renderMessageTexts([]llmschema.BaseMessage{
		llmschema.NewUserMessage("hello"),
		llmschema.NewAssistantMessage(""),
	})
	require.Contains(t, out, "[user]\nhello")
	require.NotContains(t, out, "[assistant]")
}
