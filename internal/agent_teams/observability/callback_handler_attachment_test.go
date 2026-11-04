package observability

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// TestAttachmentMarkOf_消息对象与map两种形态 验证附件标注判据。
func TestAttachmentMarkOf_消息对象与map两种形态(t *testing.T) {
	base := llmschema.NewSystemMessage("<system-reminder>x</system-reminder>", llmschema.WithMetadata(map[string]any{
		hprompts.MetadataKeyAttachmentHistory: true,
		hprompts.MetadataKeyMode:              hprompts.ModeSnapshot,
	}))
	kind, mode := attachmentMarkOf(base)
	assert.Equal(t, PromptAttachmentHistoryKind, kind)
	assert.Equal(t, hprompts.ModeSnapshot, mode)

	// 真 system prompt 不标注
	kind, mode = attachmentMarkOf(llmschema.NewSystemMessage("稳定系统提示"))
	assert.Empty(t, kind)
	assert.Empty(t, mode)

	// map 形态（llm_params 反序列化）
	kind, mode = attachmentMarkOf(map[string]any{
		"role":    "user",
		"content": "<system-reminder>x</system-reminder>",
		"metadata": map[string]any{
			hprompts.MetadataKeyAttachmentHistory: true,
			hprompts.MetadataKeyMode:              hprompts.ModeDelta,
		},
	})
	assert.Equal(t, PromptAttachmentHistoryKind, kind)
	assert.Equal(t, hprompts.ModeDelta, mode)

	// map 形态但 mode 非法
	kind, _ = attachmentMarkOf(map[string]any{
		"metadata": map[string]any{
			hprompts.MetadataKeyAttachmentHistory: true,
			hprompts.MetadataKeyMode:              "other",
		},
	})
	assert.Empty(t, kind)

	// 非消息类型 / nil / 缺 metadata
	assert.Empty(t, func() string { k, _ := attachmentMarkOf(nil); return k }())
	assert.Empty(t, func() string { k, _ := attachmentMarkOf("字符串"); return k }())
	assert.Empty(t, func() string { k, _ := attachmentMarkOf(map[string]any{"role": "user"}); return k }())
}

// TestOnLLMInvokeInput_附件条目带kind标注 验证 span 属性写入。
func TestOnLLMInvokeInput_附件条目带kind标注(t *testing.T) {
	handler, tp, recorder := newTestHandler()
	defer func() { _ = tp.Shutdown(context.Background()) }()

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	attach := llmschema.NewUserMessage("<system-reminder>x</system-reminder>", llmschema.WithMetadata(map[string]any{
		hprompts.MetadataKeyAttachmentHistory: true,
		hprompts.MetadataKeyMode:              hprompts.ModeSnapshot,
	}))
	data := &cb.LLMCallEventData{
		Event:     cb.LLMInvokeInput,
		ModelName: "qwen-max",
		Messages: []any{
			map[string]any{"role": "system", "content": "稳定系统提示"},
			attach,
		},
	}
	handler.OnLLMInvokeInput(ctx, data)

	state := spanState.PopLlmSpanState(true)
	require.NotNil(t, state)
	state.Span.End()

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	attrs := map[string]attribute.Value{}
	for _, kv := range spans[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value
	}
	assert.Equal(t, PromptAttachmentHistoryKind, attrs["gen_ai.prompt.1."+GenAIPromptAttachmentKind].AsString())
	assert.Equal(t, hprompts.ModeSnapshot, attrs["gen_ai.prompt.1."+GenAIPromptAttachmentMode].AsString())
	_, hasTrueSystemMark := attrs["gen_ai.prompt.0."+GenAIPromptAttachmentKind]
	assert.False(t, hasTrueSystemMark, "真 system prompt 不得带附件标注")
}
