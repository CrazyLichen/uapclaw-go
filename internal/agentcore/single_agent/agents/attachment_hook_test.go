package agents

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
)

// TestSyncPromptAttachments_无管理器时跳过 验证 nil manager 不报错。
func TestSyncPromptAttachments_无管理器时跳过(t *testing.T) {
	require.NoError(t, syncPromptAttachments(context.Background(), nil, nil))
}

// TestSyncPromptAttachments_有管理器无上下文时跳过 验证 nil 上下文不报错。
func TestSyncPromptAttachments_有管理器无上下文时跳过(t *testing.T) {
	require.NoError(t, syncPromptAttachments(context.Background(), prompts.NewManager(), nil))
}

// TestExtraString_取值语义 验证配置 Extra 读取。
func TestExtraString_取值语义(t *testing.T) {
	require.Equal(t, "", extraString(nil, "endpoint_profile"))
	require.Equal(t, "", extraString(map[string]any{"endpoint_profile": 1}, "endpoint_profile"))
	require.Equal(t, "bailian", extraString(map[string]any{"endpoint_profile": "bailian"}, "endpoint_profile"))
}

// TestSetPromptAttachmentManager 验证注入可空。
func TestSetPromptAttachmentManager(t *testing.T) {
	a := &ReActAgent{}
	a.SetPromptAttachmentManager(prompts.NewManager())
	require.NotNil(t, a.promptAttachments)
	a.SetPromptAttachmentManager(nil)
	require.Nil(t, a.promptAttachments)
}

// TestAttachmentWindowOptions_按provider挂投影 覆盖 nil 分支与百炼/Anthropic 两种投影。
func TestAttachmentWindowOptions_按provider挂投影(t *testing.T) {
	m := prompts.NewManager()

	require.Nil(t, attachmentWindowOptions(nil, nil, "s1"))
	require.Nil(t, attachmentWindowOptions(m, nil, "s1"))
	require.Nil(t, attachmentWindowOptions(m, &llm.Model{}, "s1"))

	bailian := &llm.Model{ClientConfig: &llmschema.ModelClientConfig{ClientProvider: "dashscope"}}
	opts := attachmentWindowOptions(m, bailian, "s1")
	require.Len(t, opts, 1)

	window := ceiface.NewContextWindow()
	window.ContextMessages = []llmschema.BaseMessage{
		llmschema.NewUserMessage("<system-reminder>x</system-reminder>", llmschema.WithMetadata(map[string]any{
			prompts.MetadataKeyAttachmentHistory: true,
			prompts.MetadataKeySessionID:         "s1",
		})),
	}
	projected := ceiface.WindowMutatorsFromOption(opts...)[0](window)
	require.IsType(t, &llmschema.SystemMessage{}, projected.ContextMessages[0], "dashscope 应换 System")

	anthropic := &llm.Model{ClientConfig: &llmschema.ModelClientConfig{ClientProvider: "anthropic"}}
	opts = attachmentWindowOptions(m, anthropic, "s1")
	projected = ceiface.WindowMutatorsFromOption(opts...)[0](window)
	require.IsType(t, &llmschema.UserMessage{}, projected.ContextMessages[0], "anthropic 保持 User")
}

// failHistory 故意失败的 HistoryContext，用于覆盖同步失败告警分支。
type failHistory struct{}

// SessionID 返回固定会话 id
func (f *failHistory) SessionID() string { return "s1" }

// GetMessages 故意失败
func (f *failHistory) GetMessages(int, bool) ([]llmschema.BaseMessage, error) {
	return nil, errFailHistory
}

// AddMessages 故意失败
func (f *failHistory) AddMessages(context.Context, []llmschema.BaseMessage, ...ceiface.Option) ([]llmschema.BaseMessage, error) {
	return nil, errFailHistory
}

// TestSyncPromptAttachments_同步失败只告警 验证失败不向调用方传播。
func TestSyncPromptAttachments_同步失败只告警(t *testing.T) {
	m := prompts.NewManager()
	_, err := m.AddSection("s1", "mode", "A", prompts.AttachmentRuntime, "test.rail")
	require.NoError(t, err)
	require.NoError(t, syncPromptAttachments(context.Background(), m, &failHistory{}))
}

// errFailHistory 测试用错误
var errFailHistory = errors.New("history unavailable")
