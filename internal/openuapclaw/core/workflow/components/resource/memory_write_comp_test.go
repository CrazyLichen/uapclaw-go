package resource

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

func TestMemoryWrite_validateInputs_messages缺失报错(t *testing.T) {
	_, err := validateMemoryWriteInputs(map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "101150")
}

func TestMemoryWrite_validateInputs_非法messages形态报错(t *testing.T) {
	_, err := validateMemoryWriteInputs(map[string]any{"messages": "not-a-list"})
	require.Error(t, err)
	_, err = validateMemoryWriteInputs(map[string]any{"messages": []any{123}})
	require.Error(t, err)
}

func TestMemoryWrite_默认值三元组与窗口(t *testing.T) {
	cfg := newMemoryWriteConfigDefaults(nil)
	assert.Equal(t, "__default__", cfg.ScopeID)
	assert.Equal(t, "__default__", cfg.UserID)
	assert.Equal(t, "__default__", cfg.SessionID)
	assert.NotNil(t, cfg.AgentConfig)
	require.NotNil(t, cfg.GenMem)
	assert.True(t, *cfg.GenMem, "Python dataclass 默认 True")
	assert.Equal(t, 2, cfg.GenMemWithHistoryMsgNum)
}

func TestMemoryWrite_默认值_GenMem显式false保留(t *testing.T) {
	f := false
	cfg := newMemoryWriteConfigDefaults(&MemoryWriteCompConfig{GenMem: &f})
	require.NotNil(t, cfg.GenMem)
	assert.False(t, *cfg.GenMem)
}

func TestMemoryWrite_invoke_空messages返回101150(t *testing.T) {
	e, err := NewMemoryWriteExecutable(&MemoryWriteCompConfig{Memory: newTestLTM()})
	require.NoError(t, err)
	sess := newTestNodeSession(t)
	_, err = e.invokeEntry(context.Background(), map[string]any{"messages": []llmschema.BaseMessage{}}, sess, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "101150")
}

func TestMemoryWrite_invoke_真实链错误包装101151(t *testing.T) {
	// 真实链：真 LTM + 非法 scope_id 格式（"scope/id" 含分隔符，validateID 拒绝）
	// → AddMessages 返回错误 → 组件 wrap 101151
	e, err := NewMemoryWriteExecutable(&MemoryWriteCompConfig{Memory: newTestLTM(), ScopeID: "scope/id"})
	require.NoError(t, err)
	sess := newTestNodeSession(t)
	msgs := []llmschema.BaseMessage{llmschema.NewUserMessage("hello")}
	_, err = e.invokeEntry(context.Background(), map[string]any{"messages": msgs}, sess, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "101151")
}

func TestMemoryWrite_构造_memory缺失报错(t *testing.T) {
	_, err := NewMemoryWriteExecutable(&MemoryWriteCompConfig{})
	require.Error(t, err)
}

func TestMemoryWrite_validateInputs_宽松list形态通过(t *testing.T) {
	in, err := validateMemoryWriteInputs(map[string]any{
		"messages": []any{llmschema.NewUserMessage("a")},
	})
	require.NoError(t, err)
	require.Len(t, in.Messages, 1)
}
