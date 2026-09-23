package rails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestNewResponsePromptRail 测试构造和默认优先级
func TestNewResponsePromptRail(t *testing.T) {
	rail := NewResponsePromptRail()
	assert.Equal(t, responsePromptRailPriority, rail.Priority())
	assert.Nil(t, rail.builder)
}

// TestResponsePromptRail_Init 测试 Init 设置 builder 引用
func TestResponsePromptRail_Init(t *testing.T) {
	builder := newMockBuilder()
	rail := NewResponsePromptRail()
	// 直接设置 builder（与 RuntimePromptRail 测试同模式：不构造完整 BaseAgent）
	rail.builder = builder
	assert.Equal(t, builder, rail.builder)
}

// TestResponsePromptRail_Uninit 测试 Uninit 清除 section 和释放 builder
func TestResponsePromptRail_Uninit(t *testing.T) {
	builder := newMockBuilder()
	builder.AddSection(saprompt.PromptSection{Name: "response", Content: map[string]string{"cn": "test"}, Priority: 60})
	rail := NewResponsePromptRail()
	rail.builder = builder

	err := rail.Uninit(nil)
	require.NoError(t, err)
	assert.Nil(t, rail.builder)
	assert.False(t, builder.HasSection("response"))
}

// TestResponsePromptRail_BeforeModelCall_CN 测试中文语言注入 response section
func TestResponsePromptRail_BeforeModelCall_CN(t *testing.T) {
	builder := newMockBuilder()
	builder.SetLanguage("cn")
	rail := NewResponsePromptRail()
	rail.builder = builder

	err := rail.BeforeModelCall(context.Background(), &agentinterfaces.AgentCallbackContext{})
	require.NoError(t, err)

	section := builder.GetSection("response")
	require.NotNil(t, section)
	assert.Equal(t, "response", section.Name)
	assert.Equal(t, 60, section.Priority)
	assert.Contains(t, section.Content, "cn")
	assert.Contains(t, section.Content["cn"], "消息说明")
	assert.Contains(t, section.Content["en"], "Message Format")
}

// TestResponsePromptRail_BeforeModelCall_EN 测试英文语言注入 response section
func TestResponsePromptRail_BeforeModelCall_EN(t *testing.T) {
	builder := newMockBuilder()
	builder.SetLanguage("en")
	rail := NewResponsePromptRail()
	rail.builder = builder

	err := rail.BeforeModelCall(context.Background(), &agentinterfaces.AgentCallbackContext{})
	require.NoError(t, err)

	section := builder.GetSection("response")
	require.NotNil(t, section)
	assert.Contains(t, section.Content["cn"], "消息说明")
	assert.Contains(t, section.Content["en"], "Message Format")
}

// TestResponsePromptRail_BeforeModelCall_NoBuilder 测试 builder 为 nil 时无操作
func TestResponsePromptRail_BeforeModelCall_NoBuilder(t *testing.T) {
	rail := NewResponsePromptRail()
	err := rail.BeforeModelCall(context.Background(), nil)
	require.NoError(t, err)
}

// TestResponsePromptRail_BeforeModelCall_EmptyLanguage 测试空语言 fallback 到 cn
func TestResponsePromptRail_BeforeModelCall_EmptyLanguage(t *testing.T) {
	builder := newMockBuilder()
	builder.SetLanguage("")
	rail := NewResponsePromptRail()
	rail.builder = builder

	err := rail.BeforeModelCall(context.Background(), &agentinterfaces.AgentCallbackContext{})
	require.NoError(t, err)

	section := builder.GetSection("response")
	require.NotNil(t, section)
	// 空语言 fallback 到 "cn"，应包含中文内容
	assert.Contains(t, section.Content["cn"], "消息说明")
}

// TestResponsePromptRail_GetCallbacks 测试回调映射包含 BeforeModelCall
func TestResponsePromptRail_GetCallbacks(t *testing.T) {
	rail := NewResponsePromptRail()
	callbacks := rail.GetCallbacks()
	_, ok := callbacks[agentinterfaces.CallbackBeforeModelCall]
	assert.True(t, ok, "GetCallbacks 应包含 CallbackBeforeModelCall")
}

// TestResponsePromptRail_GetCallbacks_Invocable 测试回调可执行
func TestResponsePromptRail_GetCallbacks_Invocable(t *testing.T) {
	builder := newMockBuilder()
	builder.SetLanguage("cn")
	rail := NewResponsePromptRail()
	rail.builder = builder

	callbacks := rail.GetCallbacks()
	fn, ok := callbacks[agentinterfaces.CallbackBeforeModelCall]
	require.True(t, ok)

	cbc := &agentinterfaces.AgentCallbackContext{}
	err := fn(context.Background(), cbc)
	require.NoError(t, err)

	section := builder.GetSection("response")
	require.NotNil(t, section)
}
