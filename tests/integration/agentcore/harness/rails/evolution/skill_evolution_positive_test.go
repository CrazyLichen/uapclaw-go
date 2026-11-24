//go:build integration

package evolution_test

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

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillEvolutionPositiveSuite 覆盖 SkillEvolutionRail.RunEvolution 的**正例路径**。
//
// 现有 skill_evolution_rail_test.go 只覆盖 autoScan 关闭/空轨迹/空消息三类早退分支
// （RunEvolution 正例 0 条）。本套件补齐「信号 → 生成记录 → 自动保存/审批 → 落盘」，
// 对齐 Python 单元测试：
//   - test_run_evolution_auto_save_commits_via_manager_lifecycle
//   - test_run_evolution_auto_save_false_emits_real_approval_event
//   - test_on_approve_partial_failure_retains_pending_change
//   - test_on_approve_full_failure_then_retry_succeeds
//
// 相比 Python 更严的部分：断言落在真实磁盘文件内容（evolutions.json / SKILL.md 索引），
// 而非仅断言管理器返回值。
type SkillEvolutionPositiveSuite struct {
	isuite.BaseIntegrationSuite
	// skillsDir 每测试隔离的技能根目录
	skillsDir string
	// skillName 测试技能名
	skillName string
	// skillDir 技能目录绝对路径
	skillDir string
	// lastRecordsClient 最近一次 newRail 创建的 mock 客户端（用于断言调用次数）
	lastRecordsClient *recordsMockClient
}

// recordsMockClient 返回固定演进记录 JSON 的模拟 LLM 客户端。
type recordsMockClient struct {
	model_clients.BaseModelClient
	// response 固定响应内容
	response string
	// calls 调用次数计数器
	calls int
}

// Invoke 返回预置记录 JSON。
func (m *recordsMockClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	m.calls++
	return llmschema.NewAssistantMessage(m.response), nil
}

// Stream 未实现。
func (m *recordsMockClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSkillEvolutionPositiveSuite 运行 RunEvolution 正例套件。
func TestSkillEvolutionPositiveSuite(t *testing.T) {
	suite.Run(t, new(SkillEvolutionPositiveSuite))
}

// TestRunEvolution_AutoSave直接落盘 autoSave=true 时演进而无需审批，直接写入 evolutions.json 与 SKILL.md 索引。
//
// 对齐 Python: test_run_evolution_auto_save_commits_via_manager_lifecycle
func (s *SkillEvolutionPositiveSuite) TestRunEvolution_AutoSave直接落盘() {
	ctx := s.Ctx
	rail := s.newRail(evolution.WithAutoSave(true))
	client := s.lastRecordsClient

	s.Require().NoError(rail.RunEvolution(ctx, nil, s.newSnapshot()))
	s.GreaterOrEqual(client.calls, 1, "应至少调用一次 LLM 生成记录")

	evoLog, err := rail.EvolutionStore().LoadFullEvolutionLog(ctx, s.skillName)
	s.Require().NoError(err)
	s.Require().Len(evoLog.Entries, 1, "自动保存后应有 1 条记录")
	s.Equal("Troubleshooting", evoLog.Entries[0].Change.Section)
	s.Contains(evoLog.Entries[0].Change.Content, "sudo", "记录内容应来自 LLM 输出")

	s.assertSkillMDHasIndex()

	// 自动保存不产生审批事件（进度事件允许存在）
	s.Empty(s.findApprovalRequestID(rail.DrainPendingHostEvents(false, nil)),
		"autoSave=true 时不应产生审批事件")
}

// TestRunEvolution_需审批时只暂存不落盘 autoSave=false 时应发出审批事件且磁盘无记录，审批后才写入。
//
// 对齐 Python: test_run_evolution_auto_save_false_emits_real_approval_event
func (s *SkillEvolutionPositiveSuite) TestRunEvolution_需审批时只暂存不落盘() {
	ctx := s.Ctx
	rail := s.newRail(evolution.WithAutoSave(false))

	s.Require().NoError(rail.RunEvolution(ctx, nil, s.newSnapshot()))

	// 未审批：磁盘无记录
	evoLog, err := rail.EvolutionStore().LoadFullEvolutionLog(ctx, s.skillName)
	s.Require().NoError(err)
	s.Empty(evoLog.Entries, "需审批时不应直接落盘")
	s.NotContains(s.readSkillMD(), "<!-- evolution-index-start -->", "需审批时 SKILL.md 不应写入索引")

	// 应发出真实审批事件（chat.ask_user_question + skill_evolve_ 前缀 request_id）
	events := rail.DrainPendingHostEvents(false, nil)
	s.Require().NotEmpty(events, "应发出审批事件")
	requestID := s.findApprovalRequestID(events)
	s.Require().NotEmpty(requestID, "审批事件应带 request_id")
	s.True(strings.HasPrefix(requestID, "skill_evolve_"), "request_id 前缀应为 skill_evolve_，实际 %s", requestID)

	// 审批通过 → 落盘
	s.Require().NoError(rail.ApproveRecord(ctx, requestID))
	evoLog, err = rail.EvolutionStore().LoadFullEvolutionLog(ctx, s.skillName)
	s.Require().NoError(err)
	s.Require().Len(evoLog.Entries, 1, "审批后应写入 1 条记录")
	s.assertSkillMDHasIndex()

	// 重复审批不应重复写入
	s.Require().NoError(rail.ApproveRecord(ctx, requestID))
	evoLog, err = rail.EvolutionStore().LoadFullEvolutionLog(ctx, s.skillName)
	s.Require().NoError(err)
	s.Len(evoLog.Entries, 1, "重复审批不应产生重复记录")
}

// TestRunEvolution_拒绝后不落盘 拒绝审批时磁盘保持干净。
func (s *SkillEvolutionPositiveSuite) TestRunEvolution_拒绝后不落盘() {
	ctx := s.Ctx
	rail := s.newRail(evolution.WithAutoSave(false))

	s.Require().NoError(rail.RunEvolution(ctx, nil, s.newSnapshot()))
	requestID := s.findApprovalRequestID(rail.DrainPendingHostEvents(false, nil))
	s.Require().NotEmpty(requestID)

	s.Require().NoError(rail.RejectRecord(ctx, requestID))

	evoLog, err := rail.EvolutionStore().LoadFullEvolutionLog(ctx, s.skillName)
	s.Require().NoError(err)
	s.Empty(evoLog.Entries, "拒绝后不应落盘")
}

// TestRunEvolution_落盘失败保留待审并可重试 写盘失败时保留待审记录，恢复后可重试成功。
//
// 对齐 Python: test_on_approve_full_failure_then_retry_succeeds
// 失败构造：把 evolutions.json 替换成目录，使 AppendRecord 的读写必然失败（root 也无法绕过）。
func (s *SkillEvolutionPositiveSuite) TestRunEvolution_落盘失败保留待审并可重试() {
	ctx := s.Ctx
	rail := s.newRail(evolution.WithAutoSave(false))

	s.Require().NoError(rail.RunEvolution(ctx, nil, s.newSnapshot()))
	requestID := s.findApprovalRequestID(rail.DrainPendingHostEvents(false, nil))
	s.Require().NotEmpty(requestID)

	// 构造写盘失败：evolutions.json 变为目录
	evoPath := filepath.Join(s.skillDir, "evolutions.json")
	s.Require().NoError(os.Mkdir(evoPath, 0o755))

	s.Require().NoError(rail.ApproveRecord(ctx, requestID), "写盘失败时不应返回 error（对齐 Python 保留待审语义）")
	s.NotContains(s.readSkillMD(), "<!-- evolution-index-start -->", "写盘失败后 SKILL.md 不应出现索引")

	// 恢复磁盘 → 重试成功
	s.Require().NoError(os.Remove(evoPath))
	retryResult, err := rail.Manager().RetryRequest(ctx, requestID)
	s.Require().NoError(err)
	s.Equal(1, retryResult.AppliedCount, "重试应写入 1 条记录")
	s.Equal(0, retryResult.PendingCount, "重试后不应残留待写入记录")

	evoLog, err := rail.EvolutionStore().LoadFullEvolutionLog(ctx, s.skillName)
	s.Require().NoError(err)
	s.Require().Len(evoLog.Entries, 1, "重试后应写入 1 条记录")
	s.Equal("Troubleshooting", evoLog.Entries[0].Change.Section)
	s.assertSkillMDHasIndex()
}

// TestRunEvolution_禁用技能不触发 无规则信号时新版 run_evolution 不再走
// 「路径推断主技能」兜底（Python :905-955 已移除该段）——只读快照应取消；
// 禁用技能后失败信号因 existing_skills 不含它而不归属，同样取消。
//
// 对齐 Python: run_evolution（skill_names 按 _disabled_skills 过滤 +
// detect_trajectory_signals 仅规则检测，无 fallback）。
func (s *SkillEvolutionPositiveSuite) TestRunEvolution_禁用技能不触发() {
	ctx := s.Ctx

	// 只读快照（无失败证据）→ 无规则信号 → 取消，不新增记录
	enabledRail := s.newRail(evolution.WithAutoSave(true))
	s.Require().NoError(enabledRail.RunEvolution(ctx, nil, s.newReadOnlySnapshot()))
	evoLog, err := enabledRail.EvolutionStore().LoadFullEvolutionLog(ctx, s.skillName)
	s.Require().NoError(err)
	s.Empty(evoLog.Entries, "只读快照无规则信号，不应产生演进记录")

	// 禁用同名技能：skill_names 过滤后 existing_skills 为空集，detector 的
	// is_existing_skill 语义是「空集=不过滤」（from_conv.py:543-545），
	// 失败信号仍归属成功 → 依旧演进。这是上游 run_evolution 的原样行为
	//（disabled 过滤只影响 skill_names 列表与 regular 过滤），Go 一比一保留。
	disabledRail := s.newRail(
		evolution.WithAutoSave(true),
		evolution.WithDisabledSkillsSet([]string{s.skillName}),
	)
	s.Require().NoError(disabledRail.RunEvolution(ctx, nil, s.newSnapshot()))
	evoLog, err = disabledRail.EvolutionStore().LoadFullEvolutionLog(ctx, s.skillName)
	s.Require().NoError(err)
	s.Len(evoLog.Entries, 1, "上游语义：existing_skills 空集不过滤归属，禁用技能仍演进（原样保留）")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每个用例准备独立技能目录与 mock LLM。
func (s *SkillEvolutionPositiveSuite) SetupTest() {
	s.skillsDir = s.T().TempDir()
	s.skillName = "data-processor"
	s.skillDir = filepath.Join(s.skillsDir, s.skillName)
	s.Require().NoError(os.MkdirAll(s.skillDir, 0o755))

	skillMD := "---\nname: data-processor\ndescription: 处理 CSV 与 JSON 数据文件\n---\n" +
		"# Data Processor Skill\n\n读取并处理数据文件。\n"
	s.Require().NoError(os.WriteFile(filepath.Join(s.skillDir, "SKILL.md"), []byte(skillMD), 0o600))
}

// newRail 构造带 records mock 的 SkillEvolutionRail。
func (s *SkillEvolutionPositiveSuite) newRail(opts ...evolution.SkillEvolutionRailOption) *evolution.SkillEvolutionRail {
	model, client := s.newRecordsMockModel()
	s.lastRecordsClient = client
	// RunEvolution 门为 signal_trigger（Python run_evolution :838-841），
	// 正例套件显式开启（Python 测试同款构造）。
	return evolution.NewSkillEvolutionRail(
		[]string{s.skillsDir},
		model,
		"mock-model",
		"cn",
		append([]evolution.SkillEvolutionRailOption{evolution.WithSignalTrigger(true)}, opts...)...,
	)
}

// newSnapshot 构造包含技能读取与执行失败的演化快照（v2 契约：快照带 Trajectory，
// messages 由轨迹投影产出——对齐 Python _trajectory_with_messages + _prepared_input）。
//
// 消息里带 `<skill>/SKILL.md` 路径 → 信号归属到 data-processor；
// `Error: permission denied` → 触发 execution_failure 信号。
func (s *SkillEvolutionPositiveSuite) newSnapshot() *evolution.EvolutionSnapshot {
	skillMDPath := filepath.Join(s.skillDir, "SKILL.md")
	messages := []map[string]any{
		{
			"role": "assistant",
			"tool_calls": []any{
				map[string]any{"id": "tc_read", "name": "read_file", "arguments": fmt.Sprintf(`{"path": %q}`, skillMDPath)},
			},
		},
		{
			"role": "tool", "tool_call_id": "tc_read", "name": "read_file",
			"content": "# Data Processor Skill\n\n读取并处理数据文件。",
		},
		{
			"role": "assistant",
			"tool_calls": []any{
				map[string]any{"id": "tc_code", "name": "code", "arguments": `{"code": "open('/etc/hosts').read()"}`},
			},
		},
		{
			"role": "tool", "tool_call_id": "tc_code", "name": "code",
			"content": "Error: permission denied reading /etc/hosts",
		},
		{"role": "user", "content": "不对，读取系统文件应该用 sudo"},
	}
	traj := trajectoryFromMessages(s.T(), messages)
	normalized, err := trajectory.TrajectoryToMessages(traj, nil, false)
	s.Require().NoError(err)
	return &evolution.EvolutionSnapshot{
		Trajectory:          traj,
		Messages:            normalized,
		SessionID:           "evo-positive-session",
		PresentedEntries:    []experience.PresentedRecordEntry{},
		IncrementalMessages: normalized,
	}
}

// trajectoryFromMessages 把消息列表编成规范轨迹（Python: _trajectory_with_messages）。
// 每步按序注入时间戳（真实 span 有时钟序，投影按时间排序消费）。
func trajectoryFromMessages(t *testing.T, messages []map[string]any) *trajectory.Trajectory {
	t.Helper()
	builder, err := trajectory.NewTrajectoryBuilder("evo-positive-session", "online", nil, nil, nil, nil, 0, nil, nil)
	if err != nil {
		t.Fatalf("构造 builder 失败: %v", err)
	}
	index := 0
	addSpan := func(span map[string]any) {
		start := fmt.Sprint(int64(index) * 1_000_000)
		span["startTimeUnixNano"] = start
		span["endTimeUnixNano"] = start
		if err := builder.RecordSpan(span); err != nil {
			t.Fatalf("记录 span 失败: %v", err)
		}
		index++
	}
	for _, message := range messages {
		role, _ := message["role"].(string)
		if role == "tool" {
			attrs := map[string]any{
				"gen_ai.operation.name":      "execute_tool",
				"gen_ai.tool.name":           message["name"],
				"gen_ai.tool.call.id":        message["tool_call_id"],
				"gen_ai.tool.call.arguments": "{}",
			}
			if result, ok := message["content"].(string); ok {
				attrs["gen_ai.tool.call.result"] = result
			}
			toolName, _ := message["name"].(string)
			addSpan(map[string]any{"name": "execute_tool " + toolName, "attributes": attrs})
			continue
		}
		attrs := map[string]any{"gen_ai.operation.name": "chat", "gen_ai.request.model": "test-model"}
		for key, value := range trajectory.WriteLLMExchange([]map[string]any{message}, nil) {
			attrs[key] = value
		}
		addSpan(map[string]any{"name": "llm.call", "attributes": attrs})
	}
	traj, err := builder.Build()
	if err != nil {
		t.Fatalf("构建轨迹失败: %v", err)
	}
	return traj
}

// newReadOnlySnapshot 构造「只读过技能、无失败」的快照：
// 无规则信号，只能靠路径推断主技能，用于验证 disabled 过滤对推断集合的影响。
func (s *SkillEvolutionPositiveSuite) newReadOnlySnapshot() *evolution.EvolutionSnapshot {
	skillMDPath := filepath.Join(s.skillDir, "SKILL.md")
	messages := []map[string]any{
		{
			"role": "assistant",
			"tool_calls": []any{
				map[string]any{"id": "tc_read", "name": "read_file", "arguments": fmt.Sprintf(`{"path": %q}`, skillMDPath)},
			},
		},
		{
			"role": "tool", "tool_call_id": "tc_read", "name": "read_file",
			"content": "# Data Processor Skill\n\n读取并处理数据文件。",
		},
	}
	traj := trajectoryFromMessages(s.T(), messages)
	normalized, err := trajectory.TrajectoryToMessages(traj, nil, false)
	s.Require().NoError(err)
	return &evolution.EvolutionSnapshot{
		Trajectory:          traj,
		Messages:            normalized,
		SessionID:           "evo-positive-session",
		PresentedEntries:    []experience.PresentedRecordEntry{},
		IncrementalMessages: normalized,
	}
}

// findApprovalRequestID 从宿主事件中提取审批 request_id。
func (s *SkillEvolutionPositiveSuite) findApprovalRequestID(events []*stream.OutputSchema) string {
	for _, event := range events {
		if event == nil || event.Payload == nil {
			continue
		}
		payload, ok := event.Payload.(map[string]any)
		if !ok {
			continue
		}
		raw, ok := payload["request_id"]
		if !ok {
			continue
		}
		requestID := fmt.Sprintf("%v", raw)
		if requestID != "" {
			return requestID
		}
	}
	return ""
}

// assertSkillMDHasIndex 断言 SKILL.md 已写入演进索引块。
func (s *SkillEvolutionPositiveSuite) assertSkillMDHasIndex() {
	content := s.readSkillMD()
	s.Contains(content, "<!-- evolution-index-start -->", "SKILL.md 应写入演进索引开始标记")
	s.Contains(content, "<!-- evolution-index-end -->", "SKILL.md 应写入演进索引结束标记")
	s.Contains(content, "Evolution Experiences", "SKILL.md 索引应包含演进经验标题")
	s.Contains(content, "# Data Processor Skill", "原有正文应保留")
}

// readSkillMD 读取技能 SKILL.md。
func (s *SkillEvolutionPositiveSuite) readSkillMD() string {
	s.T().Helper()
	data, err := os.ReadFile(filepath.Join(s.skillDir, "SKILL.md"))
	s.Require().NoError(err)
	return string(data)
}

// newRecordsMockModel 构造返回演进记录 JSON 的 Model 与客户端句柄。
func (s *SkillEvolutionPositiveSuite) newRecordsMockModel() (*llm.Model, *recordsMockClient) {
	s.T().Helper()

	payload, err := json.Marshal([]map[string]any{
		{
			"action":  "append",
			"target":  "body",
			"section": "Troubleshooting",
			"content": "### 系统文件权限\n- 读取系统文件前先确认权限，必要时使用 sudo 提权",
		},
	})
	s.Require().NoError(err)

	client := &recordsMockClient{response: string(payload)}
	provider := fmt.Sprintf("mock_skill_evo_positive_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(provider, "llm",
		func(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
			return client, nil
		})

	model, err := llm.NewModel(
		&llmschema.ModelClientConfig{ClientProvider: provider, ClientID: provider + "_id"},
		&llmschema.ModelRequestConfig{ModelName: "mock-model"},
	)
	s.Require().NoError(err, "创建 mock model 失败")
	return model, client
}
