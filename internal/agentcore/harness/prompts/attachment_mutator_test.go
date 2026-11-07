package prompts

import (
	"testing"

	"github.com/stretchr/testify/require"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// TestUsesSystemAttachmentRole_矩阵 覆盖上游 4 个判定字段的组合。
// Python: _uses_system_attachment_role
func TestUsesSystemAttachmentRole_矩阵(t *testing.T) {
	cases := []struct {
		name                                string
		provider, legacy, endpoint, apiMode string
		want                                bool
	}{
		{"anthropic 保持 user", "anthropic", "", "", "", false},
		{"anthropic api_mode", "", "", "", "anthropic-messages", false},
		{"messages api_mode", "", "", "", "messages", false},
		{"百炼 endpoint", "", "", "bailian", "", true},
		{"dashscope provider", "dashscope", "", "", "", true},
		{"openai 兼容回落 legacy", "", "bailian", "openai", "", true},
		{"普通 openai", "openai", "", "", "", false},
		{"空全部", "", "", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, UsesSystemAttachmentRole(c.provider, c.legacy, c.endpoint, c.apiMode))
		})
	}
}

// TestBuildWindowMutator_只换附件消息 验证 role 替换、session 隔离与不改入参。
func TestBuildWindowMutator_只换附件消息(t *testing.T) {
	attach := llmschema.NewUserMessage("<system-reminder>x</system-reminder>", llmschema.WithMetadata(map[string]any{
		MetadataKeyAttachmentHistory: true,
		MetadataKeySessionID:         "s1",
	}))
	otherSession := llmschema.NewUserMessage("<system-reminder>y</system-reminder>", llmschema.WithMetadata(map[string]any{
		MetadataKeyAttachmentHistory: true,
		MetadataKeySessionID:         "s2",
	}))
	plain := llmschema.NewUserMessage("hi")
	window := ceiface.NewContextWindow()
	window.ContextMessages = []llmschema.BaseMessage{attach, otherSession, plain}

	out := BuildWindowMutator("s1", true)(window)
	require.IsType(t, &llmschema.SystemMessage{}, out.ContextMessages[0], "本会话附件换 System")
	require.IsType(t, &llmschema.UserMessage{}, out.ContextMessages[1], "他会话附件保持 User")
	require.IsType(t, &llmschema.UserMessage{}, out.ContextMessages[2], "普通消息保持 User")
	require.IsType(t, &llmschema.UserMessage{}, window.ContextMessages[0], "不得修改入参窗口")
}

// TestBuildWindowMutator_关闭时原样返回 验证 useSystemRole=false。
func TestBuildWindowMutator_关闭时原样返回(t *testing.T) {
	window := ceiface.NewContextWindow()
	window.ContextMessages = []llmschema.BaseMessage{llmschema.NewUserMessage("hi")}
	require.Same(t, window, BuildWindowMutator("s1", false)(window))
	require.Same(t, window, BuildWindowMutator("s1", true)(window), "无附件消息时返回原窗口引用")
}
