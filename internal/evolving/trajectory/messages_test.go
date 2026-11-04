package trajectory

import (
	"testing"

	"github.com/stretchr/testify/require"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
)

// TestTrimPromptToLastUser_跳过附件 验证附件不作为切片起点。
func TestTrimPromptToLastUser_跳过附件(t *testing.T) {
	attach := llmschema.NewUserMessage("<system-reminder>x</system-reminder>", llmschema.WithMetadata(map[string]any{
		hprompts.MetadataKeyAttachmentHistory: true,
	}))
	user := llmschema.NewUserMessage("真实输入")
	assistant := llmschema.NewDefaultMessage(llmschema.RoleTypeAssistant, "回答")

	got := TrimPromptToLastUser([]llmschema.BaseMessage{attach, user, assistant})
	require.Len(t, got, 2)
	require.Equal(t, "真实输入", got[0].GetContent().Text())
}

// TestTrimPromptToLastUser_无真实用户返回空 验证上游 `return []` 语义。
func TestTrimPromptToLastUser_无真实用户返回空(t *testing.T) {
	attach := llmschema.NewUserMessage("<system-reminder>x</system-reminder>", llmschema.WithMetadata(map[string]any{
		hprompts.MetadataKeyAttachmentHistory: true,
	}))
	require.Empty(t, TrimPromptToLastUser([]llmschema.BaseMessage{attach}))
	require.Empty(t, TrimPromptToLastUser(nil))
}

// TestTrimPromptToLastUser_保留真实用户之后的附件 验证附件随片段保留。
func TestTrimPromptToLastUser_保留真实用户之后的附件(t *testing.T) {
	user := llmschema.NewUserMessage("真实输入")
	attach := llmschema.NewUserMessage("<system-reminder>y</system-reminder>")
	got := TrimPromptToLastUser([]llmschema.BaseMessage{
		llmschema.NewUserMessage("上一轮"),
		user,
		attach,
	})
	require.Len(t, got, 2, "真实用户之后的附件必须保留")
	require.Equal(t, "真实输入", got[0].GetContent().Text())
}

// TestToMessagesLocal_裁剪前序轮次 验证本轮回显只含最后一条真实用户起的片段。
func TestToMessagesLocal_裁剪前序轮次(t *testing.T) {
	attach := llmschema.NewUserMessage("<system-reminder>x</system-reminder>", llmschema.WithMetadata(map[string]any{
		hprompts.MetadataKeyAttachmentHistory: true,
		hprompts.MetadataKeyMode:              hprompts.ModeSnapshot,
	}))
	traj := &Trajectory{Steps: []*TrajectoryStep{
		{
			Kind: StepKindLLM,
			Detail: &LLMCallDetail{
				Model:    "m1",
				Messages: []llmschema.BaseMessage{llmschema.NewUserMessage("上一轮"), attach, llmschema.NewUserMessage("真实输入")},
				Response: map[string]any{"role": "assistant", "content": "回答"},
			},
		},
	}}

	full := traj.ToMessages()
	require.Len(t, full, 4, "完整投影保留附件（可复现）")
	require.Contains(t, full[1]["content"], "system-reminder")

	local := traj.ToMessagesLocal()
	require.Len(t, local, 2)
	require.Equal(t, "真实输入", local[0]["content"])
}
