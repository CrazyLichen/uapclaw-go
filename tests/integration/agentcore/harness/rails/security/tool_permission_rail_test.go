//go:build integration

package security

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	interrupt "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	securityrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
	secpatterns "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolPermissionRailSuite 测试 PermissionInterruptRail 工具权限中断护栏深层集成。
//
// 对齐 Python: tests/system_tests/harness/rail/test_deep_agent_tool_permission_interrupt.py
// 聚焦 Rail 本身的构造、回调注册、优先级、权限层级验证，
// 以及 PermissionInterruptRail 与 BaseSecurityRail 的交互。
type ToolPermissionRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestToolPermissionRailSuite(t *testing.T) {
	suite.Run(t, new(ToolPermissionRailSuite))
}

// TestPermissionInterruptRail_Priority 测试 PermissionInterruptRail 默认优先级为 90。
// 对齐 Python: PermissionInterruptRail.priority = 90
func (s *ToolPermissionRailSuite) TestPermissionInterruptRail_Priority() {
	config := map[string]any{
		"enabled":  true,
		"defaults": map[string]any{"*": "allow"},
	}
	rail := s.newPermRail(config)
	s.Equal(90, rail.Priority(), "PermissionInterruptRail 默认优先级应为 90")
}

// TestNewPermissionInterruptRail_构造测试 测试 NewPermissionInterruptRail 构造函数。
// 对齐 Python: PermissionInterruptRail.__init__
func (s *ToolPermissionRailSuite) TestNewPermissionInterruptRail_构造测试() {
	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"read_file": "ask",
		},
	}

	// 使用完整参数构造
	rail := securityrail.NewPermissionInterruptRail(
		config,
		nil,                   // engine 从 config 自动创建
		[]string{"read_file"}, // toolNames（可选展示用）
		nil,                   // llmModel
		"",                    // modelName
		&secpatterns.ToolPermissionHost{
			ResolveWorkspaceDir: func() string { return "/workspace" },
		},
	)

	s.NotNil(rail, "构造结果不应为 nil")
	s.Equal(90, rail.Priority(), "默认优先级应为 90")

	// Engine 应自动创建
	s.NotNil(rail.Engine(), "Engine 应自动创建")
}

// TestPermissionInterruptRail_GetCallbacks_返回BeforeToolCall 测试 GetCallbacks 包含 before_tool_call 回调。
// 对齐 Python: PermissionInterruptRail.get_callbacks()
func (s *ToolPermissionRailSuite) TestPermissionInterruptRail_GetCallbacks_返回BeforeToolCall() {
	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"bash": "ask",
		},
	}
	rail := s.newPermRail(config)

	callbacks := rail.GetCallbacks()
	_, hasBeforeToolCall := callbacks[agentinterfaces.CallbackBeforeToolCall]
	s.True(hasBeforeToolCall, "GetCallbacks 应包含 CallbackBeforeToolCall 回调")
}

// TestPermissionInterruptRail_AutoConfirm选项_跳过确认 测试 AutoConfirm 配置使 ASK 决策自动放行。
// 对齐 Python: test_hitl_tool_permission_interrupt_read_file_ask 中的 auto_confirm 分支
func (s *ToolPermissionRailSuite) TestPermissionInterruptRail_AutoConfirm选项_跳过确认() {
	config := map[string]any{
		"enabled":  true,
		"defaults": map[string]any{"*": "allow"},
		"tools": map[string]any{
			"write_file": "ask",
		},
	}
	rail := s.newPermRail(config)

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", `{"path":"/tmp/test.txt","content":"hello"}`),
		mockllm.CreateTextResponse("写入完成"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	// 设置 AutoConfirm → ASK 决策自动放行
	sess := s.NewTestSession("tool-perm-auto-confirm")
	sess.PreRun(s.Ctx)
	sess.UpdateState(map[string]any{
		saschema.InterruptAutoConfirmKey: map[string]any{"write_file": true},
	})

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "写入文件"},
		agentinterfaces.WithSession(sess))
	sess.PostRun(s.Ctx)

	s.NoError(err, "AutoConfirm 下 ASK 应自动放行")
	s.NotNil(result)
}

// TestPermissionInterruptRail_自定义Config选项 测试静态配置中 defaults/tools 的层级策略。
// 对齐 Python: PermissionInterruptRail(config=...)
func (s *ToolPermissionRailSuite) TestPermissionInterruptRail_自定义Config选项() {
	config := map[string]any{
		"enabled":  true,
		"defaults": map[string]any{"*": "allow"},
		"tools": map[string]any{
			"read_file":  "allow",
			"write_file": "ask",
			"rm_file":    "deny",
		},
	}
	rail := s.newPermRail(config)

	// 验证 Engine 配置生效
	engine := rail.Engine()
	s.Require().NotNil(engine)

	// ALLOW 级别
	result := engine.CheckPermission(s.Ctx, "read_file", nil)
	s.Equal(secpatterns.PermissionLevelAllow, result.Permission,
		"read_file 应为 ALLOW")

	// ASK 级别
	result = engine.CheckPermission(s.Ctx, "write_file", nil)
	s.Equal(secpatterns.PermissionLevelAsk, result.Permission,
		"write_file 应为 ASK")

	// DENY 级别
	result = engine.CheckPermission(s.Ctx, "rm_file", nil)
	s.Equal(secpatterns.PermissionLevelDeny, result.Permission,
		"rm_file 应为 DENY")
}

// TestPermissionInterruptRail_PermissionConfig选项_层级验证 测试 PermissionConfig 层级策略验证。
// 对齐 Python: PermissionEngine 中 defaults + tools 层级策略
func (s *ToolPermissionRailSuite) TestPermissionInterruptRail_PermissionConfig选项_层级验证() {
	config := map[string]any{
		"enabled":  true,
		"defaults": map[string]any{"*": "ask"},
		"tools": map[string]any{
			"read_file": "allow",
			"rm_file":   "deny",
		},
	}
	rail := s.newPermRail(config)
	engine := rail.Engine()
	s.Require().NotNil(engine)

	// 默认 *: ask → 未显式配置的工具应为 ASK
	result := engine.CheckPermission(s.Ctx, "unknown_tool", nil)
	s.Equal(secpatterns.PermissionLevelAsk, result.Permission,
		"默认 *: ask → 未知工具应为 ASK")

	// 显式配置覆盖默认
	result = engine.CheckPermission(s.Ctx, "read_file", nil)
	s.Equal(secpatterns.PermissionLevelAllow, result.Permission,
		"read_file 显式 allow 应覆盖默认 ask")

	result = engine.CheckPermission(s.Ctx, "rm_file", nil)
	s.Equal(secpatterns.PermissionLevelDeny, result.Permission,
		"rm_file 显式 deny 应覆盖默认 ask")
}

// TestPermissionInterruptRail_默认Deny模式 测试默认拒绝模式下的行为。
// 对齐 Python: permissions.defaults: "*": "deny"
func (s *ToolPermissionRailSuite) TestPermissionInterruptRail_默认Deny模式() {
	config := map[string]any{
		"enabled":  true,
		"defaults": map[string]any{"*": "deny"},
		"tools": map[string]any{
			"read_file": "allow",
		},
	}
	rail := s.newPermRail(config)
	engine := rail.Engine()
	s.Require().NotNil(engine)

	// 默认 deny → 未知工具被拒绝
	result := engine.CheckPermission(s.Ctx, "unknown_tool", nil)
	s.Equal(secpatterns.PermissionLevelDeny, result.Permission,
		"默认 deny → 未知工具应为 DENY")

	// 显式 allow 仍有效
	result = engine.CheckPermission(s.Ctx, "read_file", nil)
	s.Equal(secpatterns.PermissionLevelAllow, result.Permission,
		"read_file 显式 allow 应在默认 deny 下生效")
}

// TestPermissionInterruptRail_满足AgentRail接口 测试 PermissionInterruptRail 满足 AgentRail 接口。
// 对齐 Python: PermissionInterruptRail(AgentRail)
func (s *ToolPermissionRailSuite) TestPermissionInterruptRail_满足AgentRail接口() {
	// 编译时验证（与 internal 中 var _ 一致）
	var _ agentinterfaces.AgentRail = (*securityrail.PermissionInterruptRail)(nil)

	config := map[string]any{
		"enabled":  true,
		"defaults": map[string]any{"*": "allow"},
	}
	rail := s.newPermRail(config)

	// 运行时验证：赋值到 AgentRail 接口
	var railIface agentinterfaces.AgentRail = rail
	s.NotNil(railIface, "PermissionInterruptRail 应可赋值给 AgentRail 接口")
	s.Equal(90, railIface.Priority(), "通过接口访问优先级应为 90")
}

// TestPermissionInterruptRail_与BaseSecurityRail交互 测试 PermissionInterruptRail 与 BaseSecurityRail 在同一 Agent 中共存。
// 对齐 Python: 多 Rail 链式协作
func (s *ToolPermissionRailSuite) TestPermissionInterruptRail_与BaseSecurityRail交互() {
	permConfig := map[string]any{
		"enabled":  true,
		"defaults": map[string]any{"*": "allow"},
	}
	permRail := s.newPermRail(permConfig)

	// 同时创建一个 BaseSecurityRail 实例（始终返回 Allow）
	baseRail := securityrail.NewBaseSecurityRail(
		securityrail.WithSupportedEvents(agentinterfaces.CallbackBeforeToolCall),
		securityrail.WithSecurityCheckFn(func(_ context.Context, _ *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
			return &securityrail.SecurityAllow{}, nil
		}),
	)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("交互测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{permRail, baseRail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 两种 Rail 共存时 Invoke 正常完成
	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "测试交互"})
	s.NoError(err, "PermissionInterruptRail + BaseSecurityRail 共存应正常执行")
	s.NotNil(result)

	// 验证两种 Rail 都已注册
	permType := reflect.TypeOf(&securityrail.PermissionInterruptRail{})
	baseType := reflect.TypeOf(&securityrail.BaseSecurityRail{})
	s.NotEmpty(agent.FindRailsByType(permType), "应注册 PermissionInterruptRail")
	s.NotEmpty(agent.FindRailsByType(baseType), "应注册 BaseSecurityRail")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newPermRail 创建带基础配置的 PermissionInterruptRail。
func (s *ToolPermissionRailSuite) newPermRail(config map[string]any) *securityrail.PermissionInterruptRail {
	return securityrail.NewPermissionInterruptRail(
		config,
		nil, // engine 从 config 自动创建
		nil, // toolNames
		nil, // llmModel
		"",  // modelName
		&secpatterns.ToolPermissionHost{
			ResolveWorkspaceDir: func() string { return "/workspace" },
		},
	)
}

// 编译时验证所需依赖类型
var (
	_ agentinterfaces.AgentRail = (*securityrail.PermissionInterruptRail)(nil)
	_ agentinterfaces.AgentRail = (*securityrail.BaseSecurityRail)(nil)
	_ agentinterfaces.AgentRail = (*interrupt.BaseInterruptRail)(nil)
	_ agentinterfaces.AgentRail = (*rails.DeepAgentRail)(nil)
)
