package rails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	tools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
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

// TestResolveChannel_四级回退 对齐 Python _resolve_channel（:135-161）：
// inputs → run_context.extra → ctx.extra → runtime_channel。
func TestResolveChannel_四级回退(t *testing.T) {
	rail := NewResponsePromptRail()
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)

	// 1) inputs.Extra["channel"]
	cbc.SetInputs(&agentinterfaces.InvokeInputs{Extra: map[string]any{"channel": "feishu"}})
	assert.Equal(t, "feishu", rail.resolveChannel(cbc))

	// 2) run_context.extra（inputs 缺 channel 时）
	cbc2 := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{
		Extra: map[string]any{"run_context": map[string]any{
			"extra": map[string]any{"jiuwenswarm_channel": "ws_client"},
		}},
	}, nil)
	assert.Equal(t, "ws_client", rail.resolveChannel(cbc2))

	// 3) ctx.extra
	cbc3 := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)
	cbc3.SetExtra("jiuwenswarm_channel", "tui")
	assert.Equal(t, "tui", rail.resolveChannel(cbc3))

	// 4) runtime_channel 兜底：无任何标记的上下文（SetChannel 注入）
	rail.SetChannel("process_cli")
	assert.Equal(t, "process_cli", rail.resolveChannel(agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)))
	// 全空 → 空串（Python None）
	rail2 := NewResponsePromptRail()
	assert.Equal(t, "", rail2.resolveChannel(agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)))
}

// TestResolveContextValue_与BrowserWorkflow 对齐 _resolve_context_value/:190-197。
func TestResolveContextValue_与BrowserWorkflow(t *testing.T) {
	rail := NewResponsePromptRail()
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{
		Extra: map[string]any{"request_id": "req-1", "conversation_id": "conv-9"},
	}, nil)
	cbc.SetExtra("session_id", "sess-7")
	assert.Equal(t, "req-1", rail.resolveContextValue(cbc, "request_id"))
	assert.Equal(t, "conv-9", rail.resolveContextValue(cbc, "conversation_id"))
	// conversation_id 与 session_id 双取（orString 语义）
	assert.Equal(t, "conv-9", orString(rail.resolveContextValue(cbc, "conversation_id"), rail.resolveContextValue(cbc, "session_id")))
	// 缺失 → 空
	assert.Equal(t, "", rail.resolveContextValue(cbc, "nonexistent"))

	// isBrowserWorkflow：extra 标记优先
	cbc.SetExtra(a2uiBrowserWorkflowContextKey, true)
	assert.True(t, rail.isBrowserWorkflow(cbc))
	// 无标记 → 判定函数缝 false
	assert.False(t, rail.isBrowserWorkflow(agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)))
}

// TestShouldSkipA2UI_多来源 对齐 _should_skip_a2ui（:115-133）四来源。
func TestShouldSkipA2UI_多来源(t *testing.T) {
	rail := NewResponsePromptRail()

	// InvokeInputs.Extra 标记
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{
		Extra: map[string]any{"jiuwenswarm_skip_a2ui": true},
	}, nil)
	assert.True(t, rail.shouldSkipA2UI(cbc))

	// ctx.extra 标记
	cbc2 := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)
	cbc2.SetExtra("jiuwenswarm_skip_a2ui", true)
	assert.True(t, rail.shouldSkipA2UI(cbc2))

	// run_context.extra 标记
	cbc3 := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{
		Extra: map[string]any{"run_context": map[string]any{
			"extra": map[string]any{"jiuwenswarm_skip_a2ui": true},
		}},
	}, nil)
	assert.True(t, rail.shouldSkipA2UI(cbc3))

	// 无标记 → false
	assert.False(t, rail.shouldSkipA2UI(agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)))
}

// TestIsSendFileEnabled_Xiaoyi与PolicyChannel 对齐 file_delivery_policy.py 全分支。
func TestIsSendFileEnabled_Xiaoyi与PolicyChannel(t *testing.T) {
	config := map[string]any{"channels": map[string]any{
		"web": map[string]any{"send_file_allowed": true},
	}}
	// xiaoyi 前缀 → 恒 false（渠道能力限制）
	assert.False(t, tools.IsSendFileEnabled(config, "xiaoyi_app"))
	// 显式 allowed
	assert.True(t, tools.IsSendFileEnabled(config, "web"))
	// video_tool → 策略渠道回退 web
	assert.True(t, tools.IsSendFileEnabled(config, "video_tool"))
	// 缺失且非 web → false
	assert.False(t, tools.IsSendFileEnabled(config, "unknown_channel"))
	// 显式 false
	config2 := map[string]any{"channels": map[string]any{
		"web": map[string]any{"send_file_allowed": false},
	}}
	assert.False(t, tools.IsSendFileEnabled(config2, "web"))
	// 空配置 + web → true（policy_channel == "web" 兜底）
	assert.True(t, tools.IsSendFileEnabled(map[string]any{}, "web"))
}
