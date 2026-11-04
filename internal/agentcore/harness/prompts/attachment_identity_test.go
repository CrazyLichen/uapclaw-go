package prompts

import (
	"testing"

	"github.com/stretchr/testify/require"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// TestIsPromptAttachmentUser_三种命中形态 覆盖 marker 与内容特征。
func TestIsPromptAttachmentUser_三种命中形态(t *testing.T) {
	byMarker := llmschema.NewUserMessage("普通内容", llmschema.WithMetadata(map[string]any{
		MetadataKeyAttachmentHistory: true,
	}))
	byContent := llmschema.NewUserMessage("<system-reminder>x</system-reminder>")
	plain := llmschema.NewUserMessage("真正的用户输入")

	require.True(t, IsPromptAttachmentUser(byMarker))
	require.True(t, IsPromptAttachmentUser(byContent))
	require.False(t, IsPromptAttachmentUser(plain))
	require.False(t, IsPromptAttachmentUser(nil))

	// 标记为非布尔或 false 时不命中（只认 true）
	require.False(t, IsPromptAttachmentUser(llmschema.NewUserMessage("普通", llmschema.WithMetadata(map[string]any{
		MetadataKeyAttachmentHistory: "true",
	}))))
}

// TestIsPromptAttachmentHistory_需要marker与mode 覆盖严格判定。
func TestIsPromptAttachmentHistory_需要marker与mode(t *testing.T) {
	ok := llmschema.NewUserMessage("<system-reminder>x</system-reminder>", llmschema.WithMetadata(map[string]any{
		MetadataKeyAttachmentHistory: true,
		MetadataKeyMode:              ModeSnapshot,
	}))
	noMode := llmschema.NewUserMessage("<system-reminder>x</system-reminder>", llmschema.WithMetadata(map[string]any{
		MetadataKeyAttachmentHistory: true,
	}))
	badMode := llmschema.NewUserMessage("<system-reminder>x</system-reminder>", llmschema.WithMetadata(map[string]any{
		MetadataKeyAttachmentHistory: true,
		MetadataKeyMode:              "other",
	}))
	delta := llmschema.NewUserMessage("<system-reminder>x</system-reminder>", llmschema.WithMetadata(map[string]any{
		MetadataKeyAttachmentHistory: true,
		MetadataKeyMode:              ModeDelta,
	}))
	sysOK := llmschema.NewSystemMessage("<system-reminder>x</system-reminder>", llmschema.WithMetadata(map[string]any{
		MetadataKeyAttachmentHistory: true,
		MetadataKeyMode:              ModeDelta,
	}))

	require.True(t, IsPromptAttachmentHistory(ok))
	require.True(t, IsPromptAttachmentHistory(delta))
	require.True(t, IsPromptAttachmentHistory(sysOK), "窗口投影后的 SystemMessage 也要识别")
	require.False(t, IsPromptAttachmentHistory(noMode))
	require.False(t, IsPromptAttachmentHistory(badMode))
	require.False(t, IsPromptAttachmentHistory(llmschema.NewSystemMessage("稳定系统提示")))
	require.False(t, IsPromptAttachmentHistory(nil))
}
