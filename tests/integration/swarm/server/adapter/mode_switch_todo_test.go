//go:build integration

package adapter_test

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ModeSwitchTodoSuite 「规划模式 ↔ 性能模式」切换的全链路回归。
//
// 覆盖三类断言（全部对照代码写死期望值，不允许凭观测反推）：
//  1. 提示词：plan 指令节的注入/移除、todo 节的移除、无重复拼段
//  2. 工具注册：todo 工具随模式注册/注销、session_* 工具随模式卸载/注册、
//     模式切换工具常驻
//  3. 行为：规划模式硬调 todo_create 被拦截、性能模式 todo_create 执行失败
type ModeSwitchTodoSuite struct {
	isuite.BaseIntegrationSuite
	// env 每测试隔离的集成环境（数据目录 / MockLLM provider / env 备份）
	env *testhelpers.AdapterTestEnv
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// modeSwitchSessionID 本套件会话 ID（todo.json 落在 todo/<session_id>/ 下）
	modeSwitchSessionID = "sess_mode_switch"
	// modeSwitchTodoArgs 待办入参
	modeSwitchTodoArgs = `{"tasks":[{"id":"plan-task",` +
		`"content":"完成需求分析",` +
		`"activeForm":"正在完成需求分析",` +
		`"description":"梳理需求边界并输出结论"}]}`
	// todoSectionMarker todo 指令节特征文本（sections/todo.go todoSystemPromptCN）
	todoSectionMarker = "使用 todo 工具（todo_create"
	// planModeMarker plan 指令节特征文本（sections/agent_mode.go planModePromptCN）
	planModeMarker = "Plan 模式已激活"
	// planHiddenToolMarker plan 模式工具拦截文案（agent_mode.go:349）
	planHiddenToolMarker = "在 plan 模式下已隐藏"
)

// sessionToolNames multi-session 工具（updateToolsForMode 注册 / plan 模式卸载）
var sessionToolNames = []string{"session_cancel", "session_list", "session_new"}

// modeSwitchToolNames AgentModeRail 常驻注册的模式切换工具
var modeSwitchToolNames = []string{"enter_plan_mode", "exit_plan_mode", "switch_mode"}

// todoToolNames todo 工具全集
var todoToolNames = []string{"todo_create", "todo_get", "todo_list", "todo_modify"}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestModeSwitchTodoSuite 启动模式切换回归套件。
func TestModeSwitchTodoSuite(t *testing.T) {
	suite.Run(t, new(ModeSwitchTodoSuite))
}

// SetupTest 每个测试方法前构建隔离环境。
func (s *ModeSwitchTodoSuite) SetupTest() {
	s.env = testhelpers.NewAdapterTestEnv(s.T(), s.MockLLM, "mock_mode_switch_itest")
}

// TestPlan到性能模式_语义完整 主链路：plan 模式 todo_create 被拦截（完整
// plan 语义）→ 切性能模式 todo 节消失、工具注销、硬调失败、无重复拼段。
func (s *ModeSwitchTodoSuite) TestPlan到性能模式_语义完整() {
	s.env.WriteConfigYAML(s.T(), "")

	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	// ── 第 1 轮：规划模式，模型创建待办（工具已注册，可执行） ──
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("todo_create", modeSwitchTodoArgs),
		mockllm.CreateTextResponse("规划完成。"),
	)
	planEvents := s.runTurn(d, "req-plan-1", "agent.work.plan", "把需求拆成待办")
	// 规划模式语义（对齐 Python）：todo 工具在 hiddenInPlan——硬调被拒、
	// 不产生 todo.updated，任务栏数据来源为 todo.json 既有任务
	s.Require().False(hasEvent(planEvents, "todo.updated"),
		"规划模式 todo_create 应被拦截，实际事件: %v", eventTypes(planEvents))
	s.Require().Contains(toolResultFor(planEvents, "todo_create"),
		planHiddenToolMarker, "规划模式 todo_create 应被拒绝")

	inst := d.Instance()
	sb := inst.SystemPromptBuilder()
	s.Require().NotNil(sb)
	s.False(sb.HasSection("todo"), "规划模式 plan 分支应移除 todo 节")
	s.False(sb.HasSection("mode_instructions"), "plan 指引改走历史附件，不再注入系统提示词")
	s.NotNil(s.planAttachment(inst), "plan 分支应投递 mode_instructions 附件")
	planSystem := s.lastSystemPrompt()
	s.NotContains(planSystem, todoSectionMarker, "规划模式实发提示词不应含 todo 指令")
	s.Contains(s.windowText(), planModeMarker, "规划模式窗口（附件）应含 plan 指令")
	s.ElementsMatch(todoToolNames, registeredTodoTools(inst), "规划模式 todo 工具应已注册")

	// ── 第 2 轮：切到性能模式，模型再调 todo_create ──
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("todo_create", modeSwitchTodoArgs),
		mockllm.CreateTextResponse("执行完成。"),
	)
	agentEvents := s.runTurn(d, "req-agent-2", "agent.work.normal", "继续执行")

	inst = d.Instance()
	sb = inst.SystemPromptBuilder()
	s.Require().NotNil(sb)
	s.False(sb.HasSection("todo"), "性能模式不注入 todo 静态节")
	// S2：plan/fast 合并后 TaskPlanningRail 恒常驻，工具不再注销
	s.ElementsMatch(todoToolNames, registeredTodoTools(inst), "性能模式 todo 工具仍常驻")
	s.Nil(s.planAttachment(inst), "非 plan 模式应清除 mode_instructions 附件")

	agentSystem := s.lastSystemPrompt()
	s.NotContains(agentSystem, todoSectionMarker, "性能模式实发提示词不应含 todo 指令")
	s.NotContains(agentSystem, planModeMarker, "性能模式实发提示词不应含 plan 指令")
	s.Contains(toolResultFor(agentEvents, "todo_create"), "Successfully created",
		"性能模式 todo_create 应可正常执行，实际: %s", toolResultFor(agentEvents, "todo_create"))

	// 提示词不得重复拼段（双 rail 实例残留会导致整段重复与膨胀）
	heads := headings(agentSystem)
	s.Equal(len(heads), len(uniqueStrings(heads)), "系统提示词一级标题不应重复: %v", heads)
	// plan 指引改走附件后，系统提示词只差 workspace/todo 节，仅记录不设阈值
	s.T().Logf("[SIZE] 规划模式=%d 性能模式=%d", len(planSystem), len(agentSystem))
}

// TestPlan模式_plan分支完整生效 断言 D1 置位后 AgentModeRail 的 plan 语义。
func (s *ModeSwitchTodoSuite) TestPlan模式_plan分支完整生效() {
	s.env.WriteConfigYAML(s.T(), "")

	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("todo_create", modeSwitchTodoArgs),
		mockllm.CreateTextResponse("计划已写好。"),
	)
	events := s.runTurn(d, "req-plan-semantics", "agent.work.plan", "先规划再执行")

	inst := d.Instance()
	sb := inst.SystemPromptBuilder()
	s.Require().NotNil(sb)
	s.False(sb.HasSection("mode_instructions"), "plan 指引走历史附件")
	s.NotNil(s.planAttachment(inst), "plan 分支应投递 mode_instructions 附件")
	s.Contains(s.windowText(), planModeMarker, "实发窗口应含 plan 指令")
	s.False(sb.HasSection("todo"), "plan 分支应移除 todo 节")

	// todo 工具已注册但对模型硬性屏蔽：硬调被 BeforeToolCall 拒绝
	s.NotEmpty(registeredTodoTools(inst), "todo 工具应已注册（plan 只是对模型隐藏）")
	s.True(containsToolResult(events, planHiddenToolMarker),
		"todo_create 应被拒绝且文案含「在 plan 模式下已隐藏」，事件: %v", eventTypes(events))

	for _, name := range sessionToolNames {
		s.False(hasAbility(inst, name), "plan 模式应卸载 %s", name)
	}
}

// TestAgent模式_多会话语义 断言切到性能模式后的工具注册与提示词状态。
func (s *ModeSwitchTodoSuite) TestAgent模式_多会话语义() {
	s.env.WriteConfigYAML(s.T(), "")

	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	// 第 1 轮 plan 铺垫（让切换真实发生）
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("好。"))
	_ = s.runTurn(d, "req-agent-sem-1", "agent.work.plan", "你好")

	// 第 2 轮切 agent
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("收到。"))
	_ = s.runTurn(d, "req-agent-sem-2", "agent.work.normal", "继续")

	inst := d.Instance()
	sb := inst.SystemPromptBuilder()
	s.Require().NotNil(sb)
	s.False(sb.HasSection("mode_instructions"), "非 plan 模式应移除 mode_instructions 节")
	s.False(sb.HasSection("todo"), "性能模式不应有 todo 节")
	system := s.lastSystemPrompt()
	s.NotContains(system, planModeMarker, "实发提示词不应含 plan 指令")
	s.NotContains(system, todoSectionMarker, "实发提示词不应含 todo 指令")

	for _, name := range sessionToolNames {
		s.True(hasAbility(inst, name), "性能模式应注册 %s（updateToolsForMode）", name)
	}
	for _, name := range modeSwitchToolNames {
		s.True(hasAbility(inst, name), "AgentModeRail 注册的 %s 应常驻在册", name)
	}
}

// Test模式反复切换_状态稳定 断言 plan↔agent 四轮往返后状态无漂移。
func (s *ModeSwitchTodoSuite) Test模式反复切换_状态稳定() {
	s.env.WriteConfigYAML(s.T(), "")

	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	steps := []string{"agent.work.plan", "agent.work.normal", "agent.work.plan", "agent.work.normal"}
	var agentLens []int
	for i, mode := range steps {
		s.MockLLM.SetResponses(mockllm.CreateTextResponse(fmt.Sprintf("第%d轮完成。", i+1)))
		_ = s.runTurn(d, fmt.Sprintf("req-stab-%d", i+1), mode, "继续")

		sb := d.Instance().SystemPromptBuilder()
		s.Require().NotNil(sb)
		s.False(sb.HasSection("todo"), "第%d轮(%s)不应有 todo 节", i+1, mode)

		sys := s.lastSystemPrompt()
		s.NotContains(sys, todoSectionMarker, "第%d轮实发提示词不应含 todo 指令", i+1)
		heads := headings(sys)
		s.Equal(len(heads), len(uniqueStrings(heads)), "第%d轮系统提示词标题不应重复: %v", i+1, heads)
		if mode == "agent.work.normal" {
			agentLens = append(agentLens, len(sys))
		}
	}

	// 两次 agent 轮提示词长度应几乎一致（无累计膨胀；容忍 workspace 节小幅波动）
	s.Len(agentLens, 2, "应收集到两轮 agent 提示词长度")
	s.LessOrEqual(absDiff(agentLens[0], agentLens[1]), 500,
		"两次性能模式轮的提示词长度应稳定: %v", agentLens)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// windowText 返回最近一次模型调用实收的全部消息文本（含附件）。
func (s *ModeSwitchTodoSuite) windowText() string {
	s.T().Helper()
	var b strings.Builder
	for _, msg := range s.lastCallMessages().Messages() {
		b.WriteString(msg.GetContent().Text())
		b.WriteString("\n")
	}
	return b.String()
}

// planAttachment 返回 plan 指引附件（无则 nil）。
func (s *ModeSwitchTodoSuite) planAttachment(inst *harness.DeepAgent) *hprompts.Attachment {
	s.T().Helper()
	if inst == nil || inst.PromptAttachments() == nil {
		return nil
	}
	items, err := inst.PromptAttachments().ListByFilter(hprompts.AttachmentFilter{
		SessionID: modeSwitchSessionID,
		Section:   "mode_instructions",
	})
	s.Require().NoError(err)
	if len(items) == 0 {
		return nil
	}
	return items[0]
}

// runTurn 跑一轮流式对话并收集事件 payload。
func (s *ModeSwitchTodoSuite) runTurn(d *adapter.DeepAdapter, requestID, mode, query string) []map[string]any {
	s.T().Helper()

	params, err := json.Marshal(map[string]any{"mode": mode})
	s.Require().NoError(err)

	sid := modeSwitchSessionID
	req := &schema.AgentRequest{
		RequestID: requestID,
		ChannelID: "web",
		SessionID: &sid,
		Params:    params,
		IsStream:  true,
	}
	chunkCh, streamErr := d.ProcessMessageStreamImpl(s.Ctx, req, map[string]any{
		"query":           query,
		"mode":            mode,
		"conversation_id": modeSwitchSessionID,
	})
	s.Require().NoError(streamErr, "ProcessMessageStreamImpl 不应返回启动错误")

	events := make([]map[string]any, 0, 32)
	for chunk := range chunkCh {
		if chunk == nil || chunk.Payload == nil {
			continue
		}
		events = append(events, chunk.Payload)
	}
	return events
}

// lastCallMessages 返回最近一次模型调用的入参（优先流式记录）。
// Web/适配器链路走 Stream，MockLLM 的 Invoke 记录因此为空，必须读 streamCalls。
func (s *ModeSwitchTodoSuite) lastCallMessages() model_clients.MessagesParam {
	s.T().Helper()
	if rec := s.MockLLM.LastStreamCall(); rec != nil {
		return rec.Messages
	}
	if rec := s.MockLLM.LastInvokeCall(); rec != nil {
		return rec.Messages
	}
	return model_clients.MessagesParam{}
}

// lastSystemPrompt 返回最近一次模型调用实收的系统提示词。
func (s *ModeSwitchTodoSuite) lastSystemPrompt() string {
	s.T().Helper()
	rec := s.lastCallMessages()
	if rec.IsEmpty() {
		return ""
	}
	for _, m := range rec.Messages() {
		if m.GetRole() == llmschema.RoleTypeSystem {
			return m.GetContent().Text()
		}
	}
	return ""
}

// registeredTodoTools 返回 AbilityManager 中已注册的 todo 工具名（升序）。
func registeredTodoTools(inst *harness.DeepAgent) []string {
	out := make([]string, 0, 4)
	if am := inst.AbilityManager(); am != nil {
		for _, ability := range am.List() {
			if name := ability.AbilityName(); strings.HasPrefix(name, "todo_") {
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// hasAbility 判断 AbilityManager 中是否存在指定名工具。
func hasAbility(inst *harness.DeepAgent, name string) bool {
	if am := inst.AbilityManager(); am != nil {
		for _, ability := range am.List() {
			if ability.AbilityName() == name {
				return true
			}
		}
	}
	return false
}

// hasEvent 判断事件序列中是否存在指定 event_type。
func hasEvent(events []map[string]any, eventType string) bool {
	for _, evt := range events {
		if evt["event_type"] == eventType {
			return true
		}
	}
	return false
}

// eventTypes 提取事件类型序列，用于失败时打印。
func eventTypes(events []map[string]any) []string {
	out := make([]string, 0, len(events))
	for _, evt := range events {
		if et, ok := evt["event_type"].(string); ok && et != "" {
			out = append(out, et)
		}
	}
	return out
}

// toolResultFor 返回指定工具的 tool_result 文本（未找到返回空串）。
func toolResultFor(events []map[string]any, toolName string) string {
	for _, evt := range events {
		if evt["event_type"] != "chat.tool_result" || evt["tool_name"] != toolName {
			continue
		}
		result, _ := evt["result"].(string)
		return result
	}
	return ""
}

// containsToolResult 判断事件序列中是否有 tool_result 文本包含 marker。
func containsToolResult(events []map[string]any, marker string) bool {
	for _, evt := range events {
		if evt["event_type"] != "chat.tool_result" {
			continue
		}
		if strings.Contains(fmt.Sprintf("%v", evt["result"]), marker) {
			return true
		}
	}
	return false
}

// headings 提取系统提示词中的一级标题，用于观察每轮是否有节重复拼入。
func headings(text string) []string {
	out := make([]string, 0, 32)
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			out = append(out, strings.TrimPrefix(trimmed, "## "))
		}
	}
	return out
}

// uniqueStrings 去重保序。
func uniqueStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// absDiff 两数之差（非负）。
func absDiff(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

// 编译期引用占位：saprompt 在断言具体 builder 类型时使用（保留扩展位）。
var _ = saprompt.PromptSection{}

// TestPlan模式_写入工具拦截 验证规划模式的运行时写入约束（段 3c/3d）：
// write_file 写非 plan 文件被拒、bash 的 git 写被拒、bash 普通命令放行
// （bash 写文件不受路径校验是 Python 同款设计边界，靠提示词约束）。
func (s *ModeSwitchTodoSuite) TestPlan模式_写入工具拦截() {
	s.env.WriteConfigYAML(s.T(), "")

	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	// 第 1 轮：模型试图写非 plan 文件 → 应被路径校验拒绝
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file",
			`{"file_path":"/tmp/evil-escape.txt","content":"越权写入"}`),
		mockllm.CreateTextResponse("好的。"),
	)
	events := s.runTurn(d, "req-plan-write-1", "agent.work.plan", "帮我写个文件")
	result := toolResultFor(events, "write_file")
	s.Contains(result, "仅能用于计划文件",
		"write_file 写非 plan 文件应被拒绝，实际工具结果: %q", result)

	// 第 2 轮：bash 执行 git 写 → 应被 git 写正则拒绝
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("bash",
			`{"command":"cd /tmp/repo && git add -A && git commit -m x"}`),
		mockllm.CreateTextResponse("好的。"),
	)
	events = s.runTurn(d, "req-plan-write-2", "agent.work.plan", "提交一下代码")
	result = toolResultFor(events, "bash")
	s.Contains(result, "禁止执行 git 写操作",
		"bash 的 git 写操作应被拒绝，实际工具结果: %q", result)

	// 第 3 轮：bash 普通只读命令 → 放行（设计边界：bash 写文件运行时不拦，对齐 Python）
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("bash", `{"command":"echo hello > /tmp/plan-mode-boundary.txt"}`),
		mockllm.CreateTextResponse("好的。"),
	)
	events = s.runTurn(d, "req-plan-write-3", "agent.work.plan", "输出点内容")
	result = toolResultFor(events, "bash")
	s.NotEmpty(result, "bash 普通命令应放行（对齐 Python 只拦 git 写），实际工具结果: %q", result)
	s.NotContains(result, "AgentModeRail", "bash 普通命令不应被 rail 拒绝")
}
