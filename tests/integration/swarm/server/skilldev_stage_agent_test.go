//go:build integration

package server_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev"
	_ "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev/stages"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillDevStageAgentSuite 覆盖 SkillDev 阶段 Agent 的真实装配：
// CreateStageAgent 按白名单构造 ReActAgent（模型 = MockLLM，文件工具 = 真 SysOperation），
// 再由模型发起工具调用真实读写工作区文件。
//
// 这是 CreateStageAgent/RegisterTools 的集成层守卫：单测只断言装配形状，
// 这里断言「模型 → 工具 → 磁盘」闭环可用。
type SkillDevStageAgentSuite struct {
	isuite.AgentSuite
	// workspace 测试工作区
	workspace string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSkillDevStageAgentSuite 运行阶段 Agent 集成测试。
func TestSkillDevStageAgentSuite(t *testing.T) {
	suite.Run(t, new(SkillDevStageAgentSuite))
}

// TestStageAgent_模型调用写盘 阶段 Agent 能真实调用 write_file 落盘，再 read_file 读回。
func (s *SkillDevStageAgentSuite) TestStageAgent_模型调用写盘() {
	ctx := s.Ctx
	skillDevCtx := s.newContext("task-agent-write")

	agent, err := skillDevCtx.CreateStageAgent("generate", "你是技能生成器", []string{"file_read", "file_write"}, 4)
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	am := agent.AbilityManager()
	s.Require().NotNil(am.Get("read_file"), "应注册 read_file")
	s.Require().NotNil(am.Get("write_file"), "应注册 write_file")

	skillPath := filepath.Join(s.workspace, "skill", "SKILL.md")
	writeArgs, err := json.Marshal(map[string]any{
		"file_path": skillPath,
		"content":   "---\nname: demo-skill\ndescription: 演示技能\n---\n# Demo\n",
	})
	s.Require().NoError(err)

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", string(writeArgs)),
		mockllm.CreateTextResponse("已写入 SKILL.md"),
	)

	output, err := agent.Invoke(ctx, map[string]any{"query": "生成 demo-skill 的 SKILL.md"})
	s.Require().NoError(err, "阶段 Agent Invoke 不应失败")
	s.Contains(fmt.Sprintf("%v", output), "已写入 SKILL.md")

	// 磁盘闭环：工具真的写到了工作区
	data, readErr := os.ReadFile(skillPath)
	s.Require().NoError(readErr, "阶段 Agent 应通过 write_file 真实落盘")
	s.Contains(string(data), "name: demo-skill")
	s.Contains(string(data), "演示技能")
}

// TestStageAgent_别名映射与默认迭代 白名单别名映射生效，未配置的系统操作下只允许纯对话。
func (s *SkillDevStageAgentSuite) TestStageAgent_别名映射与默认迭代() {
	ctx := s.Ctx

	// 无 SysOperation：纯对话阶段可创建，但文件工具应报错
	noSysOp := s.newContextWithoutSysOp("task-agent-nosysop")
	agent, err := noSysOp.CreateStageAgent("plan", "你是规划器", nil, 0)
	s.Require().NoError(err)
	s.Require().NotNil(agent)
	s.Nil(agent.AbilityManager().Get("read_file"), "无 SysOperation 时不应注册文件工具")

	err = noSysOp.RegisterTools(agent, []string{"file_read"})
	s.Require().Error(err, "无 SysOperation 时注册文件工具应失败")

	// 有 SysOperation：shell 别名映射到 bash
	withSysOp := s.newContext("task-agent-alias")
	agent2, err := withSysOp.CreateStageAgent("improve", "你是改进器", []string{"shell"}, 2)
	s.Require().NoError(err)
	s.Require().NotNil(agent2)
	s.NotNil(agent2.AbilityManager().Get("bash"), "shell 应映射为 bash")

	_ = ctx
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每用例独立工作区。
func (s *SkillDevStageAgentSuite) SetupTest() {
	s.workspace = s.T().TempDir()
	s.Require().NoError(os.MkdirAll(filepath.Join(s.workspace, "skill"), 0o755))
}

// newContext 构造带本地 SysOperation 的阶段上下文。
func (s *SkillDevStageAgentSuite) newContext(taskID string) *skilldev.SkillDevContext {
	s.T().Helper()

	deps := s.newDeps()
	deps.SysOpConfig = sysop.NewSysOperationCard(
		sysop.WithSysOpWorkConfig(&sysop.LocalWorkConfig{RestrictToSandbox: false}),
	)
	return skilldev.NewSkillDevContext(taskID, deps, skilldev.NewSkillDevState(taskID), s.workspace, make(chan skilldev.SkillDevEvent, 16))
}

// newContextWithoutSysOp 构造不带 SysOperation 的阶段上下文（纯对话阶段）。
func (s *SkillDevStageAgentSuite) newContextWithoutSysOp(taskID string) *skilldev.SkillDevContext {
	s.T().Helper()

	deps := s.newDeps()
	return skilldev.NewSkillDevContext(taskID, deps, skilldev.NewSkillDevState(taskID), s.workspace, make(chan skilldev.SkillDevEvent, 16))
}

// newDeps 构造指向 MockLLM 的阶段依赖。
func (s *SkillDevStageAgentSuite) newDeps() *skilldev.SkillDevDeps {
	s.T().Helper()

	return &skilldev.SkillDevDeps{
		ModelName: "mock-model",
		ModelClientConfig: map[string]any{
			"client_provider": s.ProviderName,
			"client_id":       s.ClientConfig.ClientID,
			"api_key":         "mock-api-key",
			"api_base":        "http://127.0.0.1:1",
			"timeout":         float64(5),
			"max_retries":     float64(1),
		},
	}
}
