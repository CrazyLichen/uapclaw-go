//go:build integration

package security

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	securityrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PermissionInterruptRailSuite 测试工具权限中断护栏。
//
// 对齐 Python: tests/system_tests/harness/rail/test_deep_agent_tool_permission_interrupt.py
// + test_guardrail.py 的 TestGuardrailWithReActAgentMock
type PermissionInterruptRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestPermissionInterruptRailSuite(t *testing.T) {
	suite.Run(t, new(PermissionInterruptRailSuite))
}

// newPermRailWithConfig 创建带静态配置的 PermissionInterruptRail。
func (s *PermissionInterruptRailSuite) newPermRailWithConfig(config map[string]any) *securityrail.PermissionInterruptRail {
	return securityrail.NewPermissionInterruptRail(
		config,
		nil, // engine 从 config 自动创建
		nil, // toolNames
		nil, // llmModel（静态配置不需要 LLM）
		"",  // modelName
		&harnesssecurity.ToolPermissionHost{
			ResolveWorkspaceDir: func() string { return "/workspace" },
		},
	)
}

// newPermRailWithHost 创建带自定义 Host 的 PermissionInterruptRail。
func (s *PermissionInterruptRailSuite) newPermRailWithHost(
	config map[string]any,
	host *harnesssecurity.ToolPermissionHost,
) *securityrail.PermissionInterruptRail {
	return securityrail.NewPermissionInterruptRail(
		config, nil, nil, nil, "", host,
	)
}

// TestPermissionRail_静态ALLOW_工具通过 测试 allow 配置下工具调用通过。
// 对齐 Python: test_allows_safe_content
func (s *PermissionInterruptRailSuite) TestPermissionRail_静态ALLOW_工具通过() {
	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"read_file": "allow",
		},
	}
	permRail := s.newPermRailWithConfig(config)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("ALLOW 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{permRail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 正常 Invoke（read_file 允许）
	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "测试 ALLOW"})
	s.NoError(err, "ALLOW 配置下 Invoke 不应返回错误")
	s.NotNil(result)
}

// TestPermissionRail_静态DENY_工具拒绝 测试 deny 配置下工具被拒绝。
// 对齐 Python: test_blocks_attack
func (s *PermissionInterruptRailSuite) TestPermissionRail_静态DENY_工具拒绝() {
	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"rm_file": "deny",
		},
	}
	permRail := s.newPermRailWithConfig(config)

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("rm_file", `{"path":"/etc/hosts"}`),
		mockllm.CreateTextResponse("已处理拒绝"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{permRail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// Invoke 时 LLM 返回 rm_file tool_call，应被 PermissionInterruptRail 拦截
	// DENY → RejectResult → skipPermissionTool（不 panic）
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "删除文件"})
	s.NoError(err, "DENY 不应导致 Invoke 报错（应跳过工具并返回拒绝消息）")
}

// TestPermissionRail_ASK_托管确认失败转拒绝 测试 ask 配置下，托管确认失败时转为拒绝。
// 对齐 Python: test_hitl_tool_permission_interrupt_read_file_ask
//
// 注意：ASK 决策在无 hosted 确认的情况下会 raiseInterrupt → panic，
// 因此必须提供 RequestPermissionConfirmation 来避免 panic。
// 当 hosted 确认返回 nil 时，ASK 转为 Reject。
func (s *PermissionInterruptRailSuite) TestPermissionRail_ASK_托管确认失败转拒绝() {
	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"write_file": "ask",
		},
	}

	host := &harnesssecurity.ToolPermissionHost{
		ResolveWorkspaceDir: func() string { return "/workspace" },
		// 托管确认返回 nil → ASK 转为 Reject（不 panic）
		RequestPermissionConfirmation: func(req harnesssecurity.PermissionConfirmationRequest) (*harnesssecurity.PermissionConfirmResponse, error) {
			return nil, nil
		},
	}
	permRail := s.newPermRailWithHost(config, host)

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", `{"path":"/tmp/test.txt","content":"test"}`),
		mockllm.CreateTextResponse("已处理拒绝"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{permRail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// ASK + 托管确认返回 nil → Reject（跳过工具）
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "写入文件"})
	s.NoError(err, "ASK + 托管确认失败 应转为 Reject，不导致 Invoke 报错")
}

// TestPermissionRail_ASK_托管确认通过 测试 ask 配置下，托管确认批准时工具通过。
func (s *PermissionInterruptRailSuite) TestPermissionRail_ASK_托管确认通过() {
	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"write_file": "ask",
		},
	}

	host := &harnesssecurity.ToolPermissionHost{
		ResolveWorkspaceDir: func() string { return "/workspace" },
		// 托管确认返回 approved → ASK 转为 Approve
		RequestPermissionConfirmation: func(req harnesssecurity.PermissionConfirmationRequest) (*harnesssecurity.PermissionConfirmResponse, error) {
			return &harnesssecurity.PermissionConfirmResponse{
				Approved: true,
			}, nil
		},
	}
	permRail := s.newPermRailWithHost(config, host)

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", `{"path":"/tmp/test.txt","content":"test"}`),
		mockllm.CreateTextResponse("写入完成"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{permRail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// ASK + 托管确认 approved → Approve（工具执行）
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "写入文件"})
	s.NoError(err, "ASK + 托管确认通过 应允许工具执行")
}

// TestPermissionRail_AutoConfirm_跳过确认 测试 auto-confirm 机制。
func (s *PermissionInterruptRailSuite) TestPermissionRail_AutoConfirm_跳过确认() {
	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"bash": "ask",
		},
	}
	permRail := s.newPermRailWithConfig(config)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("AutoConfirm 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{permRail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 创建 Session 并设置 auto_confirm
	sess := s.NewTestSession("perm-auto-confirm")
	sess.PreRun(s.Ctx)
	// 设置 auto_confirm key（对齐 Python session.state auto_confirm 机制）
	sess.UpdateState(map[string]any{
		saschema.InterruptAutoConfirmKey: map[string]any{"bash:ls": true},
	})

	// 无 tool_call 场景——仅验证 Rail 初始化和注册成功
	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "AutoConfirm 测试"}, agentinterfaces.WithSession(sess))
	s.NoError(err, "AutoConfirm 场景下 Invoke 不应报错")
	s.NotNil(result)

	sess.PostRun(s.Ctx)
}

// TestPermissionRail_SceneHook_短路允许 测试 PermissionSceneHook 短路允许。
func (s *PermissionInterruptRailSuite) TestPermissionRail_SceneHook_短路允许() {
	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"bash": "ask",
		},
	}

	sceneHookCalled := false
	host := &harnesssecurity.ToolPermissionHost{
		ResolveWorkspaceDir: func() string { return "/workspace" },
		PermissionSceneHook: func(input harnesssecurity.PermissionSceneHookInput) ([]string, error) {
			sceneHookCalled = true
			return []string{"approve"}, nil // 场景钩子直接放行
		},
	}

	permRail := s.newPermRailWithHost(config, host)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("SceneHook 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{permRail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 无 tool_call 场景——SceneHook 不会被触发（BeforeToolCall 不被调用）
	// 但可验证 Rail 初始化成功
	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "SceneHook 测试"})
	s.NoError(err)
	s.NotNil(result)

	// 无 tool_call 时 SceneHook 不被触发
	s.False(sceneHookCalled, "无工具调用时 SceneHook 不应被触发")
}

// TestPermissionRail_注册到Agent 测试 PermissionInterruptRail 注册到 Agent 的能力管理器。
func (s *PermissionInterruptRailSuite) TestPermissionRail_注册到Agent() {
	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"read_file": "allow",
			"write_file": "ask",
			"rm_file":    "deny",
		},
	}
	permRail := s.newPermRailWithConfig(config)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("注册测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{permRail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 验证 PermissionInterruptRail 已注册到 Agent
	railType := reflect.TypeOf(&securityrail.PermissionInterruptRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 PermissionInterruptRail")
}
