package agents

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

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
