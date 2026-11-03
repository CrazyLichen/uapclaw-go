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
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ModeSwitchTodoSuite 观测「规划模式 → 性能模式」切换后 todo 相关内容的残留情况。
//
// 切换语义（deep_adapter_rails.go:1140 updateAgentModeRails）：
// 离开 plan 模式时注销 TaskPlanningRail，其 Uninit 会移除 todo 工具与
// SectionTodo 提示词节。但切模式不会清理会话上下文，历史里由
// TaskPlanningRail 注入的进度提醒与 todo 工具结果仍会留在发给模型的 messages 中。
//
// 本套件记录三类观测量，用于判断是否存在「提示词说能用 todo、实际没有工具」的不一致：
//  1. 系统提示词是否含 todo 指令节（section）
//  2. 会话上下文是否含 todo 进度提醒 / 任务记录（history）
//  3. 性能模式下 todo_create 是否仍可执行（tool 注册）
type ModeSwitchTodoSuite struct {
	isuite.BaseIntegrationSuite
	// env 每测试隔离的集成环境（数据目录 / MockLLM provider / env 备份）
	env *testhelpers.AdapterTestEnv
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// modeSwitchSessionID 本用例会话 ID（todo.json 落在 todo/<session_id>/ 下）
	modeSwitchSessionID = "sess_mode_switch"
	// modeSwitchTodoArgs 待办入参
	modeSwitchTodoArgs = `{"tasks":[{"id":"plan-task",` +
		`"content":"完成需求分析",` +
		`"activeForm":"正在完成需求分析",` +
		`"description":"梳理需求边界并输出结论"}]}`
	// todoSectionMarker 系统提示词中 todo 指令节的特征文本
	todoSectionMarker = "使用 todo 工具（todo_create"
	// progressReminderMarker 进度提醒消息的特征文本
	progressReminderMarker = "以下是当前任务规划中所有任务的内容和状态"
)

// watchedSections 观测用的关注节（接口没有 GetAllSections，只能按名单探）。
var watchedSections = []string{
	"todo", "mode_instructions", "session_tools", "task_tool", "tools",
	"runtime", "workspace", "context", "response", "identity", "skills",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestModeSwitchTodoSuite 启动模式切换观测套件。
func TestModeSwitchTodoSuite(t *testing.T) {
	suite.Run(t, new(ModeSwitchTodoSuite))
}

// SetupTest 每个测试方法前构建隔离环境。
func (s *ModeSwitchTodoSuite) SetupTest() {
	s.env = testhelpers.NewAdapterTestEnv(s.T(), s.MockLLM, "mock_mode_switch_itest")
}

// TestPlan到性能模式_todo残留观测 走完整链路：plan 模式建待办 → 切 agent 模式 → 观测三处残留。
func (s *ModeSwitchTodoSuite) TestPlan到性能模式_todo残留观测() {
	s.env.WriteConfigYAML(s.T(), "")

	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	// ── 第 1 轮：规划模式，模型创建待办 ──
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("todo_create", modeSwitchTodoArgs),
		mockllm.CreateTextResponse("规划完成。"),
	)
	planEvents := s.runTurn(d, "req-plan-1", "agent.plan", "把需求拆成待办")
	s.Require().True(hasEvent(planEvents, "todo.updated"),
		"规划模式下 todo_create 应产生 todo.updated，实际事件: %v", eventTypes(planEvents))
	s.observe("【规划模式】", d.Instance(), planEvents)

	// ── 第 2 轮：切到性能模式，再让模型调用 todo_create ──
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("todo_create", modeSwitchTodoArgs),
		mockllm.CreateTextResponse("执行完成。"),
	)
	agentEvents := s.runTurn(d, "req-agent-2", "agent", "继续执行")
	s.observe("【性能模式】", d.Instance(), agentEvents)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

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

// observe 打印四类观测量：提示词节注册、模型实收提示词、上下文残留、todo 工具注册与执行结果。
func (s *ModeSwitchTodoSuite) observe(label string, inst *harness.DeepAgent, events []map[string]any) {
	s.T().Helper()

	// 1. 提示词构建器当前注册了哪些节（直接看构建器，不看拼出来的字符串）
	if inst != nil {
		if sb := inst.SystemPromptBuilder(); sb != nil {
			present := make([]string, 0, len(watchedSections))
			for _, name := range watchedSections {
				if sb.HasSection(name) {
					present = append(present, name)
				}
			}
			s.T().Logf("%s 提示词节: hasTodo=%v 命中的关注节=%v",
				label, sb.HasSection("todo"), present)

			if sec := sb.GetSection("todo"); sec != nil {
				content := sec.Content[sb.Language()]
				s.T().Logf("%s todo 节语言=%s 内容长度=%d",
					label, sb.Language(), len(content))
			}
		}

		// 2. AbilityManager 里还注册着哪些 todo 工具
		todoTools := make([]string, 0, 4)
		if am := inst.AbilityManager(); am != nil {
			for _, ability := range am.List() {
				name := ability.AbilityName()
				if strings.HasPrefix(name, "todo_") {
					todoTools = append(todoTools, name)
				}
			}
		}
		sort.Strings(todoTools)
		s.T().Logf("%s 已注册 todo 工具: %v", label, todoTools)
	}

	rec := s.lastCallMessages()
	if rec.IsEmpty() {
		s.T().Logf("%s 未捕获到模型调用", label)
		return
	}

	var systemText string
	var userTexts []string
	for _, m := range rec.Messages() {
		text := strings.TrimSpace(m.GetContent().Text())
		if text == "" {
			continue
		}
		if m.GetRole() == llmschema.RoleTypeSystem {
			systemText = text
			continue
		}
		userTexts = append(userTexts, text)
	}

	s.T().Logf("%s 系统提示词长度=%d，含 todo 指令节=%v",
		label, len(systemText), strings.Contains(systemText, todoSectionMarker))
	s.T().Logf("%s 系统提示词一级标题: %v", label, headings(systemText))

	reminderHits := 0
	taskRecordHits := 0
	for _, text := range userTexts {
		if strings.Contains(text, progressReminderMarker) {
			reminderHits++
		}
		if strings.Contains(text, "完成需求分析") {
			taskRecordHits++
		}
	}
	s.T().Logf("%s 上下文中：进度提醒=%d 条，含任务记录的消息=%d 条，消息总数=%d",
		label, reminderHits, taskRecordHits, len(rec.Messages()))

	for _, evt := range events {
		if evt["event_type"] != "chat.tool_result" {
			continue
		}
		result, _ := evt["tool_result"].(map[string]any)
		if result == nil || result["tool_name"] != "todo_create" {
			continue
		}
		s.T().Logf("%s todo_create 工具结果: success=%v status=%v result=%v",
			label, result["success"], result["status"],
			truncateForLog(fmt.Sprintf("%v", result["result"]), 300))
	}

	if hasEvent(events, "todo.updated") {
		s.T().Logf("%s 收到 todo.updated（说明工具执行成功并刷新了任务栏）", label)
	} else {
		s.T().Logf("%s 未收到 todo.updated（工具可能未注册或执行失败），事件序列=%v",
			label, eventTypes(events))
	}
}

// lastCallMessages 返回最近一次模型调用的入参（优先流式记录，回退到 Invoke 记录）。
//
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

// truncateForLog 截断日志文本，避免整段提示词刷屏。
func truncateForLog(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}
