//go:build integration

package evolving_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	skilluse "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	skillpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
	skillcallopt "github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/skill_call"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionConsumptionSuite 验证在线演进的「最后一跳」：
// 经验写回 SKILL.md 之后，下一个 Agent 能否重新加载、注入提示词并被 skill_tool 真正消费。
//
// 现有 online_evolution_e2e_test.go 只覆盖到「写回磁盘」；
// 本套件补齐「写回 → 重新扫描 → 提示词可见 → 工具真实读取到新经验」的闭环。
//
// 对齐 Python: tests/system_tests/agent_evolving/test_online_evolution_e2e.py
// 的持久化部分 + tests/unit_tests/harness/rails/evolution/test_skill_use_rail 的加载语义，
// 但按更严标准把两段连成一条链路（Python 无对应闭环用例）。
type EvolutionConsumptionSuite struct {
	isuite.AgentSuite
	// tmpDir 每测试隔离的技能根目录
	tmpDir string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestEvolutionConsumptionE2E 运行闭环消费端集成测试套件。
func TestEvolutionConsumptionE2E(t *testing.T) {
	suite.Run(t, new(EvolutionConsumptionSuite))
}

// TestEvolutionConsumption_经验写回后被重新加载并消费 完整闭环：
//
//	对话（工具失败 + 用户纠正）
//	→ SignalDetector 检测信号
//	→ SkillExperienceOptimizer（mock LLM）生成经验
//	→ EvolutionStore 写回 SKILL.md 的 evolution-index 块
//	→ 新的 SkillUseRail 重新扫描同一技能目录
//	→ SectionSkills 注入技能描述（frontmatter 未被破坏）
//	→ 模型调用 skill_tool，工具返回内容含新写入的经验原文
func (s *EvolutionConsumptionSuite) TestEvolutionConsumption_经验写回后被重新加载并消费() {
	ctx := s.Ctx
	const skillName = "data-processor"
	const skillDescription = "处理 CSV 与 JSON 数据文件"
	const evolvedText = "读取系统文件前先确认权限，必要时使用 sudo 提权"
	const evolvedSummary = "系统文件权限"

	skillDir := s.prepareSkill(skillName, skillDescription, "# Data Processor Skill\n\n处理 CSV 与 JSON 数据文件。\n")

	// ── 阶段 1：信号检测 ──
	messages := []map[string]any{
		{
			"role": "assistant",
			"tool_calls": []map[string]any{
				{"id": "tc_code", "name": "code", "arguments": `{"code": "open('/etc/hosts')"}`},
			},
		},
		{
			"role":         "tool",
			"tool_call_id": "tc_code",
			"name":         "code",
			"content":      "Error: permission denied reading /etc/hosts",
		},
		{"role": "user", "content": "不对，读取系统文件应该用 sudo"},
	}
	detector := signal.NewConversationSignalDetector(
		signal.WithExistingSkills(map[string]bool{skillName: true}),
	)
	sigList := detector.Detect(messages)
	s.Require().NotEmpty(sigList, "工具失败 + 用户纠正应检测到演进信号")

	sigValues := make([]signal.EvolutionSignal, len(sigList))
	for i, sig := range sigList {
		sigValues[i] = *sig
	}

	// ── 阶段 2：经验生成（mock LLM 返回固定记录）──
	recordsJSON, err := json.Marshal([]map[string]any{
		{
			"action":  "append",
			"target":  "body",
			"section": "Troubleshooting",
			"content": "### 系统文件权限\n- " + evolvedText,
		},
	})
	s.Require().NoError(err)

	model := s.newMockEvolutionModel(func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(string(recordsJSON)), nil
	})
	optimizer := skillcallopt.NewSkillExperienceOptimizer(model, "mock-model", "cn", generateRecordsLLMPolicy)

	evoCtx := &experience.EvolutionContext{
		SkillName:           skillName,
		Signals:             sigValues,
		SkillContent:        s.readSkillMD(skillDir),
		Messages:            messages,
		ExistingDescRecords: []checkpointing.EvolutionRecord{},
		ExistingBodyRecords: []checkpointing.EvolutionRecord{},
	}
	records, err := optimizer.GenerateRecords(ctx, evoCtx)
	s.Require().NoError(err, "GenerateRecords 不应返回错误")
	s.Require().Len(records, 1, "应生成 1 条经验记录")

	// ── 阶段 3：写回磁盘 ──
	store := checkpointing.NewEvolutionStore([]string{s.tmpDir})
	s.Require().NoError(store.AppendRecord(ctx, skillName, records[0]))

	// 磁盘断言：SKILL.md 同时保留原正文、演进索引块与经验摘要条目
	skillMD := s.readSkillMD(skillDir)
	s.Contains(skillMD, "<!-- evolution-index-start -->", "SKILL.md 应写入演进索引起始标记")
	s.Contains(skillMD, "<!-- evolution-index-end -->", "SKILL.md 应写入演进索引结束标记")
	s.Contains(skillMD, "# Data Processor Skill", "原有正文不应被覆盖")
	s.Contains(skillMD, evolvedSummary, "SKILL.md 索引应包含经验摘要")
	// 经验正文落在渐进披露明细文件，索引只放摘要与链接（对齐 Python 的 evolution/*.md 设计）
	detailPath := filepath.Join(skillDir, "evolution", "troubleshooting.md")
	s.FileExists(detailPath, "应生成 evolution/troubleshooting.md 明细文件")
	detail, err := os.ReadFile(detailPath)
	s.Require().NoError(err)
	s.Contains(string(detail), evolvedText, "明细文件应包含完整经验正文")

	// 演进记录可被独立读取（store 侧一致性）
	evoLog, err := store.LoadFullEvolutionLog(ctx, skillName)
	s.Require().NoError(err)
	s.Require().Len(evoLog.Entries, 1, "evolutions.json 应写入 1 条记录")

	// ── 阶段 4：新 Agent 重新加载（闭环消费端）──
	manager := skillpkg.NewSkillManager("")
	s.Require().NoError(manager.Register([]string{skillDir}, false), "写回后的技能目录应可重新注册")
	reloaded := manager.Get(skillName)
	s.Require().NotNil(reloaded, "应能按名称取回技能")
	s.Equal(skillDescription, reloaded.Description, "frontmatter 描述应保持可解析且未被演进破坏")

	skillRail := skilluse.NewSkillUseRail(
		[]string{s.tmpDir},
		skilluse.WithSkillMode(skilluse.SkillModeAll),
		skilluse.WithEvolutionStore(store),
	)
	observer := testhelpers.NewModelCallObserver()

	// 第 1 次工具调用读索引，第 2 次按索引链接读明细（渐进披露），最后收尾
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("skill_tool", fmt.Sprintf(`{"skill_name": %q}`, skillName)),
		mockllm.CreateToolCallResponse("skill_tool", fmt.Sprintf(
			`{"skill_name": %q, "relative_file_path": "evolution/troubleshooting.md"}`, skillName)),
		mockllm.CreateTextResponse("已按技能经验完成处理"),
	)

	restrictToWorkDir := false
	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Rails:             []agentinterfaces.AgentRail{skillRail, observer},
		MaxIterations:     5,
		RestrictToWorkDir: &restrictToWorkDir,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	output, err := agent.Invoke(ctx, map[string]any{"query": "用 data-processor 技能读取系统配置"})
	s.Require().NoError(err, "Invoke 不应返回错误")
	s.Contains(fmt.Sprintf("%v", output), "已按技能经验完成处理")

	// 断言 1：提示词注入了技能（描述来自 frontmatter，说明索引块未破坏解析）
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionSkills), "应注入 SectionSkills 节")
	section := spb.GetSection(hsections.SectionSkills)
	s.Require().NotNil(section)
	prompt := section.Render("cn")
	s.Contains(prompt, skillName, "提示词应包含技能名")
	s.Contains(prompt, skillDescription, "提示词应包含技能描述")

	// 断言 2：skill_tool 真实执行，索引与明细两层都读到演进产物
	s.Require().GreaterOrEqual(observer.CallCount(), 3, "应有三次模型调用（读索引 + 读明细 + 收尾）")
	s.Contains(s.messagesText(observer.GetMessages(1)), evolvedSummary,
		"第二次模型调用应含 SKILL.md 索引摘要（skill_tool 读到写回内容）")
	s.Contains(s.messagesText(observer.GetMessages(2)), evolvedText,
		"第三次模型调用应含明细文件中的经验原文（渐进披露链路打通）")
}

// TestEvolutionConsumption_技能目录未被写回时提示词无经验 反向用例：
// 未执行演进时，同样的 Agent 装配不应在提示词/工具返回中出现演进经验文本，
// 避免上一用例的断言因缓存或全局状态假绿。
func (s *EvolutionConsumptionSuite) TestEvolutionConsumption_技能目录未被写回时提示词无经验() {
	ctx := s.Ctx
	const skillName = "plain-skill"
	const skillDescription = "未经历演进的普通技能"

	skillDir := s.prepareSkill(skillName, skillDescription, "# Plain Skill\n\n普通技能正文。\n")

	skillRail := skilluse.NewSkillUseRail([]string{s.tmpDir})
	observer := testhelpers.NewModelCallObserver()
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("skill_tool", fmt.Sprintf(`{"skill_name": %q}`, skillName)),
		mockllm.CreateTextResponse("完成"),
	)

	restrictToWorkDir := false
	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Rails:             []agentinterfaces.AgentRail{skillRail, observer},
		MaxIterations:     4,
		RestrictToWorkDir: &restrictToWorkDir,
	})
	s.Require().NoError(err)
	_, err = agent.Invoke(ctx, map[string]any{"query": "使用 plain-skill"})
	s.Require().NoError(err)

	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	section := spb.GetSection(hsections.SectionSkills)
	s.Require().NotNil(section)
	s.NotContains(section.Render("cn"), "evolution-index", "未演进时提示词不应含演进索引")

	s.NotContains(s.messagesText(observer.GetMessages(1)), "<!-- evolution-index-start -->",
		"未演进时 SKILL.md 不应含演进索引块")
	s.FileExists(filepath.Join(skillDir, "SKILL.md"))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每个用例独享技能根目录。
func (s *EvolutionConsumptionSuite) SetupTest() {
	s.tmpDir = s.T().TempDir()
}

// prepareSkill 在隔离目录下写入一个带 YAML frontmatter 的技能。
func (s *EvolutionConsumptionSuite) prepareSkill(name, description, body string) string {
	s.T().Helper()

	skillDir := filepath.Join(s.tmpDir, name)
	s.Require().NoError(os.MkdirAll(skillDir, 0o755))
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n%s", name, description, body)
	s.Require().NoError(os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o600))
	return skillDir
}

// readSkillMD 读取技能的 SKILL.md 文本。
func (s *EvolutionConsumptionSuite) readSkillMD(skillDir string) string {
	s.T().Helper()

	data, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	s.Require().NoError(err)
	return string(data)
}

// messagesText 拼接一次模型调用的全部文本消息，便于做内容级断言。
func (s *EvolutionConsumptionSuite) messagesText(messages []llmschema.BaseMessage) string {
	var builder strings.Builder
	for _, msg := range messages {
		content := msg.GetContent()
		if content.IsText() {
			builder.WriteString(content.Text())
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

// newMockEvolutionModel 构造指向 mock client 的 Model 实例。
func (s *EvolutionConsumptionSuite) newMockEvolutionModel(
	invokeFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error),
) *llm.Model {
	s.T().Helper()

	mockClient := &mockBaseModelClient{invokeFn: invokeFn}
	providerName := fmt.Sprintf("mock_evo_consumption_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(providerName, "llm", func(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
		return mockClient, nil
	})

	model, err := llm.NewModel(
		&llmschema.ModelClientConfig{ClientProvider: providerName, ClientID: providerName + "_id"},
		&llmschema.ModelRequestConfig{ModelName: "mock-model"},
	)
	s.Require().NoError(err, "创建 mock model 失败")
	return model
}
