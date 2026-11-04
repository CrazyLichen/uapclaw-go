//go:build integration

package adapter_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TodoPanelSuite 验证「模型调用 todo 工具 → 前端任务栏收到 todo.updated」的整条链路。
//
// 链路：MockLLM 返回 todo_create 工具调用 → ReAct 执行工具并落盘 todo.json
// → JiuClawStreamEventRail.AfterToolCall 读回列表 → deep_adapter 的
// ParseStreamChunk 转前端事件 → ProcessMessageStreamImpl 输出 todo.updated。
//
// 回归的缺陷（2026-11-03）：Rail 的 formatTodosForFrontend 产出 []map[string]any，
// 而 ParseStreamChunk 只断言 payload["todos"].([]any)，断言失败后兜成空列表，
// 前端每次收到 todos=[] 的 todo.updated，任务栏永远显示「暂无任务」。
type TodoPanelSuite struct {
	isuite.BaseIntegrationSuite
	// env 每测试隔离的集成环境（数据目录 / MockLLM provider / env 备份）
	env *testhelpers.AdapterTestEnv
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// todoPanelSessionID 本用例的会话 ID（todo.json 落在 todo/<session_id>/ 下）
	todoPanelSessionID = "sess_todo_panel"
	// todoPanelTaskID 待办任务 ID
	todoPanelTaskID = "task-req-analysis"
	// todoPanelTaskContent 待办任务内容（断言任务栏文案）
	todoPanelTaskContent = "完成需求分析"
	// todoPanelTaskActiveForm 预置待办的进行中表述
	todoPanelTaskActiveForm = "正在完成需求分析"
	// todoPanelTaskArgs todo_create 的工具入参
	todoPanelTaskArgs = `{"tasks":[{"id":"task-req-analysis",` +
		`"content":"完成需求分析",` +
		`"activeForm":"正在完成需求分析",` +
		`"description":"梳理需求边界并输出结论"}]}`
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestTodoPanelSuite 启动 todo 任务栏链路套件。
func TestTodoPanelSuite(t *testing.T) {
	suite.Run(t, new(TodoPanelSuite))
}

// SetupTest 每个测试方法前构建隔离环境。
func (s *TodoPanelSuite) SetupTest() {
	s.env = testhelpers.NewAdapterTestEnv(s.T(), s.MockLLM, "mock_todo_panel_itest")
}

// TestTodoPanel_任务栏展示既有待办 断言性能模式下任务栏能收到 todo.json 中的既有任务。
//
// 2026-11-03 完整 plan 语义上线后的产品形态：规划模式 todo 工具被硬性屏蔽、
// 性能模式 TaskPlanningRail 注销——web 两个模式模型都无法再创建待办，任务栏
// 的数据来源是 todo.json 中的既有任务（由 enter_plan_mode 流程或其他入口写入）。
// 本用例预置 todo.json，模型硬调 todo_create（已注销，执行失败），断言
// StreamEventRail 仍读取文件并把非空任务以 []any 发给前端。
func (s *TodoPanelSuite) TestTodoPanel_任务栏展示既有待办() {
	s.env.WriteConfigYAML(s.T(), "")

	// 预置既有任务（enter_plan_mode 流程写入的形态）
	todoDir := filepath.Join(s.env.DataDir, "agent", "workspace", "todo", todoPanelSessionID)
	if err := os.MkdirAll(todoDir, 0o755); err != nil {
		s.Require().NoError(err)
	}
	seed, err := json.MarshalIndent([]map[string]any{{
		"id":         todoPanelTaskID,
		"content":    todoPanelTaskContent,
		"activeForm": todoPanelTaskActiveForm,
		"status":     "in_progress",
	}}, "", "  ")
	s.Require().NoError(err)
	s.Require().NoError(os.WriteFile(filepath.Join(todoDir, "todo.json"), seed, 0o644))

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("todo_create", todoPanelTaskArgs),
		mockllm.CreateTextResponse("继续执行。"),
	)

	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.normal", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	todos, toolResult := s.runAndCollectTodos(d, "req-todo-panel-1", "请把任务拆成待办：完成需求分析。")

	// 新语义：性能模式 todo 工具已注销，硬调执行失败（结果为空）
	s.Empty(toolResult, "性能模式 todo_create 已注销，工具结果应为空（执行失败）")

	// 任务栏仍收到 todo.json 中的非空任务
	s.Require().NotEmpty(todos, "todo.updated 必须携带非空 todos（空列表即前端「暂无任务」）")
	s.Require().Len(todos, 1)

	item, ok := todos[0].(map[string]any)
	s.Require().True(ok, "todo 项应为 map[string]any，实际 %T", todos[0])
	s.Equal(todoPanelTaskID, item["id"])
	s.Equal(todoPanelTaskContent, item["content"])
	s.Equal(todoPanelTaskActiveForm, item["activeForm"], "进行中项展示 activeForm")
	s.Equal("in_progress", item["status"])
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runAndCollectTodos 跑一轮流式对话，返回 todo.updated 事件里的 todos 列表
// 与 todo_create 的工具结果文本。
func (s *TodoPanelSuite) runAndCollectTodos(d *adapter.DeepAdapter, requestID, query string) ([]any, string) {
	s.T().Helper()

	params, err := json.Marshal(map[string]any{"mode": "agent.work.normal"})
	s.Require().NoError(err)

	sid := todoPanelSessionID
	req := &schema.AgentRequest{
		RequestID: requestID,
		ChannelID: "web",
		SessionID: &sid,
		Params:    params,
		IsStream:  true,
	}
	chunkCh, streamErr := d.ProcessMessageStreamImpl(s.Ctx, req, map[string]any{
		"query":           query,
		"conversation_id": todoPanelSessionID,
	})
	s.Require().NoError(streamErr, "ProcessMessageStreamImpl 不应返回启动错误")

	var todos []any
	toolResult := ""
	seen := false
	seenTypes := make([]string, 0, 16)
	for chunk := range chunkCh {
		if chunk == nil || chunk.Payload == nil {
			continue
		}
		eventType, _ := chunk.Payload["event_type"].(string)
		seenTypes = append(seenTypes, eventType)
		s.T().Logf("event=%s payload=%v", eventType, chunk.Payload)
		if eventType == "chat.tool_result" {
			if name, _ := chunk.Payload["tool_name"].(string); name == "todo_create" {
				toolResult, _ = chunk.Payload["result"].(string)
			}
			continue
		}
		if eventType != "todo.updated" {
			continue
		}
		seen = true
		raw, exists := chunk.Payload["todos"]
		s.Require().True(exists, "todo.updated 必须带 todos 键")
		list, ok := raw.([]any)
		s.Require().True(ok, "todos 应为 []any（前端 JSON 数组），实际 %T", raw)
		todos = list
	}
	s.Require().True(seen, "工具调用后必须发出 todo.updated 事件，实际事件序列: %v", seenTypes)
	return todos, toolResult
}
