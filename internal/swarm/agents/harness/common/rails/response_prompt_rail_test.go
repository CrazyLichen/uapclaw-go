package rails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
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

// TestResponsePromptRail_BeforeModelCall_CN 对齐 Python 新版：input/output 双节。
func TestResponsePromptRail_BeforeModelCall_CN(t *testing.T) {
	builder := newMockBuilder()
	builder.SetLanguage("cn")
	rail := NewResponsePromptRail()
	rail.builder = builder

	err := rail.BeforeModelCall(context.Background(), &agentinterfaces.AgentCallbackContext{})
	require.NoError(t, err)

	inputSection := builder.GetSection("input")
	require.NotNil(t, inputSection)
	assert.Equal(t, 60, inputSection.Priority)
	assert.Contains(t, inputSection.Content["cn"], "输入说明")
	assert.Contains(t, inputSection.Content["cn"], "系统消息类型说明")

	outputSection := builder.GetSection("output")
	require.NotNil(t, outputSection)
	assert.Equal(t, 65, outputSection.Priority)
	assert.Contains(t, outputSection.Content["cn"], "输出规则")
	// 无渠道配置缝 → sendFileEnabled fallback true
	assert.Contains(t, outputSection.Content["cn"], "send_file_to_user")
	// response 旧节被移除
	assert.False(t, builder.HasSection("response"))
}

// TestResponsePromptRail_BeforeModelCall_EN 英文分支。
func TestResponsePromptRail_BeforeModelCall_EN(t *testing.T) {
	builder := newMockBuilder()
	builder.SetLanguage("en")
	rail := NewResponsePromptRail()
	rail.builder = builder

	err := rail.BeforeModelCall(context.Background(), &agentinterfaces.AgentCallbackContext{})
	require.NoError(t, err)
	assert.NotNil(t, builder.GetSection("input"))
	assert.Contains(t, builder.GetSection("input").Content["en"], "Input Instructions")
	assert.Contains(t, builder.GetSection("output").Content["en"], "Output Rules")
}

// TestResponsePromptRail_SetChannel 对齐 set_channel + resolveChannel 回退。
func TestResponsePromptRail_SetChannel(t *testing.T) {
	rail := NewResponsePromptRail()
	rail.SetChannel("  web  ")
	builder := newMockBuilder()
	rail.builder = builder
	err := rail.BeforeModelCall(context.Background(), &agentinterfaces.AgentCallbackContext{})
	require.NoError(t, err)
	// channel=web 且配置缝 nil → fallback true（send_file 规则保留）
	assert.Contains(t, builder.GetSection("output").Content["cn"], "send_file_to_user")
}

// TestResponsePromptRail_SendFileDisabled 对齐 send_file_allowed=false 分支。
func TestResponsePromptRail_SendFileDisabled(t *testing.T) {
	builder := newMockBuilder()
	builder.SetLanguage("cn")
	rail := NewResponsePromptRail()
	rail.SetSendFileConfigProvider(func() map[string]any {
		return map[string]any{"channels": map[string]any{"web": map[string]any{"send_file_allowed": false}}}
	})
	rail.SetChannel("web")
	rail.builder = builder
	require.NoError(t, rail.BeforeModelCall(context.Background(), &agentinterfaces.AgentCallbackContext{}))
	assert.Contains(t, builder.GetSection("output").Content["cn"], "当前渠道不支持文件投递")
}

// TestResponsePromptRail_BeforeInvoke_Extra写入 对齐 before_invoke 三标记。
func TestResponsePromptRail_BeforeInvoke_Extra写入(t *testing.T) {
	rail := NewResponsePromptRail()
	rail.SetChannel("web")
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)
	require.NoError(t, rail.BeforeInvoke(context.Background(), cbc))
	if v, ok := cbc.GetExtra("jiuwenswarm_channel"); !ok || v != "web" {
		t.Fatalf("channel extra 应写入")
	}
	// 浏览器工作流判定缝返回 false
	if v, ok := cbc.GetExtra("a2ui_browser_workflow"); !ok || v != false {
		t.Fatalf("browser workflow extra 应写入 false")
	}
}

// TestResponsePromptRail_Uninit清除全部节 对齐 uninit 四节清除。
func TestResponsePromptRail_Uninit清除全部节(t *testing.T) {
	builder := newMockBuilder()
	builder.AddSection(saprompt.PromptSection{Name: "input"})
	builder.AddSection(saprompt.PromptSection{Name: "output"})
	builder.AddSection(saprompt.PromptSection{Name: "a2ui"})
	rail := NewResponsePromptRail()
	rail.builder = builder
	require.NoError(t, rail.Uninit(nil))
	assert.False(t, builder.HasSection("input"))
	assert.False(t, builder.HasSection("output"))
	assert.False(t, builder.HasSection("a2ui"))
	assert.Nil(t, rail.builder)
}

func TestResponsePromptRail_GetCallbacks_Invocable(t *testing.T) {
	builder := newMockBuilder()
	builder.SetLanguage("cn")
	rail := NewResponsePromptRail()
	rail.builder = builder

	callbacks := rail.GetCallbacks()
	fn, ok := callbacks[agentinterfaces.CallbackBeforeModelCall]
	require.True(t, ok)

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)
	err := fn(context.Background(), cbc)
	require.NoError(t, err)

	// 新版注入 input/output 双节（response 旧节废弃清除）
	require.NotNil(t, builder.GetSection("input"))
	require.NotNil(t, builder.GetSection("output"))
}
