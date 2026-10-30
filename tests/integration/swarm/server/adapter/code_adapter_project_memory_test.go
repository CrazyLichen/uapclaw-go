//go:build integration

package adapter_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	commonrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
	swarmschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodeAdapterProjectMemorySuite 测试 CodeAdapter 装配链中的 ProjectMemoryRail 与 explore/plan 子代理装配。
//
// 对齐 Python: jiuwenswarm/tests/system_tests/test_project_memory.py
// （类 3 装配、类 4 explore 开关、类 6 模式切换真实化）
type CodeAdapterProjectMemorySuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCodeAdapterProjectMemorySuite 运行 ProjectMemoryRail 装配层集成套件。
func TestCodeAdapterProjectMemorySuite(t *testing.T) {
	suite.Run(t, new(CodeAdapterProjectMemorySuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newCodeAdapterWithProject 在指定 env 下创建 CodeAdapter，project_dir 指向真实目录。
func (s *CodeAdapterProjectMemorySuite) newCodeAdapterWithProject(env *adapterTestEnv, subMode string) (*adapter.CodeAdapter, string) {
	projDir := filepath.Join(env.DataDir, "proj")
	s.Require().NoError(os.MkdirAll(projDir, 0o755))

	c := adapter.NewCodeAdapter()
	s.Require().NoError(c.CreateInstance(s.Ctx, map[string]any{"project_dir": projDir}, "code", subMode))
	s.T().Cleanup(func() { _ = c.Cleanup() })
	return c, projDir
}

// subagentByName 从 DeepConfig().Subagents 中按 AgentCard.Name 取子代理配置。
func (s *CodeAdapterProjectMemorySuite) subagentByName(inst *harness.DeepAgent, name string) *hschema.SubAgentConfig {
	s.Require().NotNil(inst.DeepConfig())
	for _, spec := range inst.DeepConfig().Subagents {
		cfg, ok := spec.(*hschema.SubAgentConfig)
		if !ok || cfg == nil || cfg.AgentCard == nil {
			continue
		}
		if cfg.AgentCard.Name == name {
			return cfg
		}
	}
	return nil
}

// processOnce 发一轮完整请求（ProcessMessageImpl），断言响应成功。
//
// 先例：code_adapter_coding_memory_test.go::TestProcessMessage_记忆Section注入
func (s *CodeAdapterProjectMemorySuite) processOnce(c *adapter.CodeAdapter, tag string, params map[string]any) {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("ok"))
	raw, err := json.Marshal(params)
	s.Require().NoError(err)

	sessionID := "itest-pm-" + tag
	req := swarmschema.NewAgentRequest(
		"itest-req-"+tag, "itest-channel", swarmschema.ReqMethodChatSend, raw,
		swarmschema.WithAgentSessionID(sessionID),
	)
	inputs := map[string]any{
		"conversation_id": sessionID,
		"query":           "读取项目记忆",
		"channel":         "itest-channel",
		"language":        "zh",
	}
	resp, err := c.ProcessMessageImpl(s.Ctx, req, inputs)
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.True(resp.OK, "响应应成功: %+v", resp)
}

// TestCreateInstance_各code子模式均挂载 验证 code.normal / code.plan 均装配 ProjectMemoryRail，
// 且 workspace 取 project_dir、语言固定 en。
//
// 对齐 Python: TestProjectMemoryRailModeSwitching —— Python 原用例为 pass / assert True 空壳
// （作者注释"CodeAdapter 永远挂载"），此处把该意图落成真实断言。
func (s *CodeAdapterProjectMemorySuite) TestCreateInstance_各code子模式均挂载() {
	for _, subMode := range []string{"normal", "plan"} {
		subMode := subMode
		s.Run("子模式_"+subMode, func() {
			env := newAdapterTestEnv(s.T(), s.MockLLM, "mock_pm_mode_"+subMode)
			env.WriteConfigYAML(s.T(), "")

			c, projDir := s.newCodeAdapterWithProject(env, subMode)
			inst := c.Instance()
			s.Require().NotNil(inst)

			found := inst.FindRailsByType(reflect.TypeOf(&commonrails.ProjectMemoryRail{}))
			s.Require().Len(found, 1, "ProjectMemoryRail 应恰好装配 1 个")

			rail, ok := found[0].(*commonrails.ProjectMemoryRail)
			s.Require().True(ok, "rail 具体类型应为 *ProjectMemoryRail")
			s.Equal(projDir, rail.ResolveWorkspacePath(), "workspace 应取 project_dir")
			s.Equal("en", rail.GetLanguage(), "code 族运行时语言固定 en")
		})
	}
}

// TestCreateInstance_explore与plan默认迭代上限 验证未配置 max_iterations 时两者取 15。
//
// 对齐 Python: _build_configured_subagents 中
// max_iterations=parse_int(子配置.max_iterations, react_cfg.get("max_iterations", 15))
// 修复前现状：explore 硬编码 15、plan 硬编码 25（plan 断言将失败）
func (s *CodeAdapterProjectMemorySuite) TestCreateInstance_explore与plan默认迭代上限() {
	env := newAdapterTestEnv(s.T(), s.MockLLM, "mock_pm_iters_default")
	env.WriteConfigYAML(s.T(), "")

	c, _ := s.newCodeAdapterWithProject(env, "normal")
	inst := c.Instance()
	s.Require().NotNil(inst)

	explore := s.subagentByName(inst, "explore_agent")
	s.Require().NotNil(explore, "explore_agent 应恒挂载")
	s.Equal(15, explore.MaxIterations, "explore 默认迭代上限应为 15")

	plan := s.subagentByName(inst, "plan_agent")
	s.Require().NotNil(plan, "plan_agent 应恒挂载")
	s.Equal(15, plan.MaxIterations, "plan 默认迭代上限应为 15（Python 恒传 react.max_iterations）")
}
