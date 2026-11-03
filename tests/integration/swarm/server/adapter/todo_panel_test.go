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
	// todoPanelTaskActiveForm 待办任务进行中表述
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

// TestTodoCreate_任务栏收到非空todos 断言 todo_create 之后前端能拿到任务项。
func (s *TodoPanelSuite) TestTodoCreate_任务栏收到非空todos() {
	s.env.WriteConfigYAML(s.T(), "")

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("todo_create", todoPanelTaskArgs),
		mockllm.CreateTextResponse("待办已创建，开始执行。"),
	)

	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	todos := s.runAndCollectTodos(d, "req-todo-panel-1", "请把任务拆成待办：完成需求分析。")
	s.Require().NotEmpty(todos, "todo_create 之后必须收到非空 todos（空列表即前端「暂无任务」）")
	s.Require().Len(todos, 1, "todo_create 创建 1 个任务")

	item, ok := todos[0].(map[string]any)
	s.Require().True(ok, "todo 项应为 map[string]any，实际 %T", todos[0])
	s.Equal(todoPanelTaskID, item["id"])
	s.Equal(todoPanelTaskContent, item["content"])
	s.Equal(todoPanelTaskActiveForm, item["activeForm"], "进行中项展示 activeForm")
	s.Equal("in_progress", item["status"], "todo_create 首个任务应为 in_progress")

	// 反向佐证：工具真的落盘了（不是 mock 出来的假事件）
	s.assertTodoFilePersisted()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runAndCollectTodos 跑一轮流式对话，返回 todo.updated 事件里的 todos 列表。
func (s *TodoPanelSuite) runAndCollectTodos(d *adapter.DeepAdapter, requestID, query string) []any {
	s.T().Helper()

	params, err := json.Marshal(map[string]any{"mode": "agent.plan"})
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
	seen := false
	seenTypes := make([]string, 0, 16)
	for chunk := range chunkCh {
		if chunk == nil || chunk.Payload == nil {
			continue
		}
		eventType, _ := chunk.Payload["event_type"].(string)
		seenTypes = append(seenTypes, eventType)
		s.T().Logf("event=%s payload=%v", eventType, chunk.Payload)
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
	return todos
}

// assertTodoFilePersisted 断言待办已真实写入 todo/<session_id>/todo.json。
func (s *TodoPanelSuite) assertTodoFilePersisted() {
	s.T().Helper()

	filePath := filepath.Join(s.env.DataDir, "agent", "workspace", "todo", todoPanelSessionID, "todo.json")
	data, err := os.ReadFile(filePath)
	s.Require().NoError(err, "todo_create 应把待办写入 %s", filePath)

	var items []map[string]any
	s.Require().NoError(json.Unmarshal(data, &items), "todo.json 应为 JSON 数组")
	s.Require().Len(items, 1)
	s.Equal(todoPanelTaskContent, items[0]["content"], "落盘内容应与工具入参一致")
}
