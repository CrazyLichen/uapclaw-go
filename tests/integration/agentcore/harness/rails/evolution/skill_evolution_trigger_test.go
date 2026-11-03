//go:build integration

package evolution_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	skilluse "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillEvolutionTriggerSuite 验证「真实跑一轮 Agent → AfterInvoke 触发演化 → 后台落盘」的触发链。
//
// 现有测试只在 rail 层手工调用 RunEvolution，从未验证 EvolutionRail.AfterInvoke →
// triggerEvolution → safeRunEvolution（异步）这一跳。本套件用真实 DeepAgent + MockLLM
// 跑一轮带技能读取失败的会话，再用 DrainPendingHostEvents(wait=true) 作为确定性屏障
// （不使用 sleep），断言磁盘落盘与后台事件。
type SkillEvolutionTriggerSuite struct {
	isuite.AgentSuite
	// skillsDir 每测试隔离的技能根目录
	skillsDir string
	// skillName 测试技能名
	skillName string
}

// triggerRecordsClient 返回固定演进记录 JSON 的模拟客户端（供演化 rail 使用，与 Agent 的 MockLLM 分离）。
type triggerRecordsClient struct {
	model_clients.BaseModelClient
	// response 固定响应
	response string
	// calls 调用次数
	calls int
}

// Invoke 返回预置记录 JSON。
func (c *triggerRecordsClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	c.calls++
	return llmschema.NewAssistantMessage(c.response), nil
}

// Stream 未实现。
func (c *triggerRecordsClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSkillEvolutionTriggerSuite 运行触发链集成测试套件。
func TestSkillEvolutionTriggerSuite(t *testing.T) {
	suite.Run(t, new(SkillEvolutionTriggerSuite))
}

// TestAfterInvoke触发异步演化并落盘 一轮带失败工具调用的会话结束后，
// 异步演化任务应被发起、完成，并把经验落到 SKILL.md 与 evolutions.json。
func (s *SkillEvolutionTriggerSuite) TestAfterInvoke触发异步演化并落盘() {
	ctx := s.Ctx
	client, rail := s.newEvolutionRail(evolution.WithAutoSave(true))

	agent := s.newAgent(rail)
	// 第 1 次：读技能（建立「活跃技能」归属），第 2 次：执行失败命令（产生 execution_failure 信号）
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("skill_tool", fmt.Sprintf(`{"skill_name": %q}`, s.skillName)),
		mockllm.CreateToolCallResponse("bash", `{"command": "cat /nonexistent/data.csv"}`),
		mockllm.CreateTextResponse("已处理完成"),
	)

	start := time.Now()
	_, err := agent.Invoke(ctx, map[string]any{"query": "用 data-processor 处理数据"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 确定性屏障：等待后台演化任务结束（不使用 sleep）
	timeout := 30 * time.Second
	events := rail.DrainPendingHostEvents(true, &timeout)
	elapsed := time.Since(start)
	s.GreaterOrEqual(client.calls, 1, "触发链应真正调用演化 LLM")
	s.NotEmpty(events, "应产生演化进度事件")
	// 回归守卫：manager 路径的 BackgroundTask.Done 未关闭时，drain 会白等满 timeout
	s.Less(elapsed, 20*time.Second, "后台演化任务应真实完成，drain 不应超时等待")

	// 落盘断言
	evoLog, err := rail.EvolutionStore().LoadFullEvolutionLog(ctx, s.skillName)
	s.Require().NoError(err)
	s.Require().Len(evoLog.Entries, 1, "异步演化应写入 1 条记录")

	skillMD := s.readSkillMD()
	s.Contains(skillMD, "<!-- evolution-index-start -->", "SKILL.md 应写入演进索引")
	s.Contains(skillMD, "Evolution Experiences", "索引应包含演进经验标题")
	s.Contains(skillMD, "# Data Processor Skill", "原有正文应保留")
}

// TestAutoScan关闭不触发演化 autoScan=false 时应完全跳过演化，磁盘保持干净。
func (s *SkillEvolutionTriggerSuite) TestAutoScan关闭不触发演化() {
	ctx := s.Ctx
	client, rail := s.newEvolutionRail(
		evolution.WithAutoSave(true),
		evolution.WithAutoScan(false),
	)

	agent := s.newAgent(rail)
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("skill_tool",
			fmt.Sprintf(`{"skill_name": %q, "relative_file_path": "references/missing.md"}`, s.skillName)),
		mockllm.CreateTextResponse("已处理完成"),
	)

	_, err := agent.Invoke(ctx, map[string]any{"query": "用 data-processor 处理数据"})
	s.Require().NoError(err)

	timeout := 5 * time.Second
	events := rail.DrainPendingHostEvents(true, &timeout)
	s.Empty(events, "autoScan=false 时不应产生演化事件")
	s.Equal(0, client.calls, "autoScan=false 时不应调用演化 LLM")
	s.NotContains(s.readSkillMD(), "<!-- evolution-index-start -->", "autoScan=false 时不应写回 SKILL.md")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每个用例准备独立技能目录。
func (s *SkillEvolutionTriggerSuite) SetupTest() {
	s.skillsDir = s.T().TempDir()
	s.skillName = "data-processor"
	skillDir := filepath.Join(s.skillsDir, s.skillName)
	s.Require().NoError(os.MkdirAll(skillDir, 0o755))
	skillMD := "---\nname: data-processor\ndescription: 处理 CSV 与 JSON 数据文件\n---\n" +
		"# Data Processor Skill\n\n读取并处理数据文件。\n"
	s.Require().NoError(os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0o600))
}

// newEvolutionRail 构造演化 rail（独立 mock LLM，与 Agent 的 MockLLM 分离）。
func (s *SkillEvolutionTriggerSuite) newEvolutionRail(
	opts ...evolution.SkillEvolutionRailOption,
) (*triggerRecordsClient, *evolution.SkillEvolutionRail) {
	s.T().Helper()

	payload, err := json.Marshal([]map[string]any{
		{
			"action":  "append",
			"target":  "body",
			"section": "Troubleshooting",
			"content": "### 明细文件缺失\n- 读取 evolution 明细前先确认路径存在，缺失时回退到 SKILL.md 索引",
		},
	})
	s.Require().NoError(err)

	client := &triggerRecordsClient{response: string(payload)}
	provider := fmt.Sprintf("mock_skill_evo_trigger_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(provider, "llm",
		func(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
			return client, nil
		})

	model, err := llm.NewModel(
		&llmschema.ModelClientConfig{ClientProvider: provider, ClientID: provider + "_id"},
		&llmschema.ModelRequestConfig{ModelName: "mock-model"},
	)
	s.Require().NoError(err)

	rail := evolution.NewSkillEvolutionRail([]string{s.skillsDir}, model, "mock-model", "cn", opts...)
	return client, rail
}

// newAgent 构造挂了技能使用护栏与演化护栏的真实 DeepAgent。
func (s *SkillEvolutionTriggerSuite) newAgent(evoRail *evolution.SkillEvolutionRail) *harness.DeepAgent {
	s.T().Helper()

	skillRail := skilluse.NewSkillUseRail(
		[]string{s.skillsDir},
		skilluse.WithSkillMode(skilluse.SkillModeAll),
	)
	restrictToWorkDir := false
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:             []agentinterfaces.AgentRail{skillRail, evoRail},
		MaxIterations:     4,
		RestrictToWorkDir: &restrictToWorkDir,
	})
	s.Require().NoError(err)
	return agent
}

// readSkillMD 读取技能 SKILL.md。
func (s *SkillEvolutionTriggerSuite) readSkillMD() string {
	s.T().Helper()
	data, err := os.ReadFile(filepath.Join(s.skillsDir, s.skillName, "SKILL.md"))
	s.Require().NoError(err)
	return string(data)
}
