package subagent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewVerificationRail_默认白名单 测试默认 12 个允许工具
func TestNewVerificationRail_默认白名单(t *testing.T) {
	r := NewVerificationRail()

	assert.Equal(t, 90, r.Priority(), "VerificationRail 优先级应为 90")
	assert.Equal(t, 12, len(r.allowedTools), "默认应有 12 个允许工具")
	assert.True(t, r.allowedTools["read_file"])
	assert.True(t, r.allowedTools["bash"])
	assert.True(t, r.allowedTools["grep"])
	assert.True(t, r.allowedTools["glob"])
	assert.True(t, r.allowedTools["list_files"])
	assert.True(t, r.allowedTools["web_search"])
	assert.True(t, r.allowedTools["web_fetch"])
	assert.True(t, r.allowedTools["todo_create"])
	assert.True(t, r.allowedTools["todo_list"])
	assert.True(t, r.allowedTools["todo_modify"])
	assert.True(t, r.allowedTools["skill_tool"])
	assert.True(t, r.allowedTools["tool_search"])
}

// TestNewVerificationRail_自定义白名单 测试 WithAllowedTools
func TestNewVerificationRail_自定义白名单(t *testing.T) {
	customTools := map[string]bool{
		"read_file": true,
		"bash":      true,
	}

	r := NewVerificationRail(WithAllowedTools(customTools))

	assert.Equal(t, 2, len(r.allowedTools), "自定义白名单应有 2 个工具")
}

// TestVerificationRail_Init_捕获Builder 测试 Init 捕获 promptBuilder
func TestVerificationRail_Init_捕获Builder(t *testing.T) {
	r := NewVerificationRail()
	agent := newFakeBaseAgentForTest()

	err := r.Init(agent)

	require.NoError(t, err)
	assert.NotNil(t, r.promptBuilder, "Init 应捕获 promptBuilder")
}

// TestVerificationRail_BeforeModelCall_注入提醒 测试 section 被注入
func TestVerificationRail_BeforeModelCall_注入提醒(t *testing.T) {
	r := NewVerificationRail()
	agent := newFakeBaseAgentForTest()

	err := r.Init(agent)
	require.NoError(t, err)

	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	err = r.BeforeModelCall(context.Background(), cbc)

	require.NoError(t, err)
	assert.True(t, agent.builder.HasSection(reminderSectionName), "应注入约束提醒 section")
}

// TestVerificationRail_BeforeModelCall_无Builder跳过 测试 promptBuilder 为 nil 时跳过
func TestVerificationRail_BeforeModelCall_无Builder跳过(t *testing.T) {
	r := NewVerificationRail()
	// 不调用 Init，promptBuilder 为 nil

	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	err := r.BeforeModelCall(context.Background(), cbc)

	require.NoError(t, err)
	// 不应 panic
}

// TestVerificationRail_BeforeToolCall_允许工具 测试白名单内工具通过
func TestVerificationRail_BeforeToolCall_允许工具(t *testing.T) {
	r := NewVerificationRail()

	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "read_file",
		ToolArgs: `{"file_path": "/tmp/test.txt"}`,
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := r.BeforeToolCall(context.Background(), cbc)

	require.NoError(t, err)
	assert.Nil(t, inputs.ToolResult, "白名单内工具不应被拦截")
}

// TestVerificationRail_BeforeToolCall_拦截工具 测试 write_file/edit_file 被拦截
func TestVerificationRail_BeforeToolCall_拦截工具(t *testing.T) {
	r := NewVerificationRail()

	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "write_file",
		ToolArgs: `{"file_path": "/tmp/test.txt", "content": "hello"}`,
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := r.BeforeToolCall(context.Background(), cbc)

	require.NoError(t, err)
	assert.NotNil(t, inputs.ToolResult, "write_file 应被拦截")
	resultMap, ok := inputs.ToolResult.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, resultMap["error"], "[VerificationAgent]")
	assert.True(t, cbc.Extra()["_skip_tool"].(bool), "应设置 _skip_tool 标记")
}

// TestVerificationRail_BeforeToolCall_MCP放行 测试 mcp__* 前缀放行
func TestVerificationRail_BeforeToolCall_MCP放行(t *testing.T) {
	r := NewVerificationRail()

	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "mcp__server__tool",
		ToolArgs: `{}`,
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := r.BeforeToolCall(context.Background(), cbc)

	require.NoError(t, err)
	assert.Nil(t, inputs.ToolResult, "MCP 工具应无条件放行")
}

// TestVerificationRail_BeforeToolCall_skip标记跳过 测试 _skip_tool=true 时跳过
func TestVerificationRail_BeforeToolCall_skip标记跳过(t *testing.T) {
	r := NewVerificationRail()

	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "write_file",
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	cbc.Extra()["_skip_tool"] = true

	err := r.BeforeToolCall(context.Background(), cbc)

	require.NoError(t, err)
	assert.Nil(t, inputs.ToolResult, "已有 _skip_tool 标记时应跳过检查")
}

// TestVerificationRail_BeforeToolCall_路径范围守卫 测试超出 workspace 的路径被拦截
func TestVerificationRail_BeforeToolCall_路径范围守卫(t *testing.T) {
	r := NewVerificationRail()
	ws := &workspace.Workspace{RootPath: "/home/user/project"}
	r.SetWorkspace(ws)

	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "read_file",
		ToolArgs: `{"file_path": "/etc/passwd"}`,
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := r.BeforeToolCall(context.Background(), cbc)

	require.NoError(t, err)
	assert.NotNil(t, inputs.ToolResult, "超出 workspace 的路径应被拦截")
}

// TestRejectTool_设置ToolMsg 测试 rejectTool 创建 ToolMessage
func TestRejectTool_设置ToolMsg(t *testing.T) {
	r := &VerificationRail{}

	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "write_file",
		ToolCall: &llmschema.ToolCall{ID: "call-123"},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	r.rejectTool(cbc, inputs, "test error")

	assert.Equal(t, "test error", inputs.ToolMsg.GetContent().Text())
	assert.Equal(t, "call-123", inputs.ToolMsg.ToolCallID)
	assert.True(t, cbc.Extra()["_skip_tool"].(bool))
}
