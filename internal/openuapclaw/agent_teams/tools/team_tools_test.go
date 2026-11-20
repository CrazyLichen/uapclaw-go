package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/fsm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/locales"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestToolSuccess 构造成功的工具返回
func TestToolSuccess(t *testing.T) {
	data := map[string]any{"key": "value", "count": 42}
	result, err := toolSuccess(data)
	if err != nil {
		t.Fatalf("toolSuccess 不应返回 error: %v", err)
	}
	if result["success"] != true {
		t.Errorf("success 应为 true，实际 %v", result["success"])
	}
	if result["data"] == nil {
		t.Fatal("data 不应为 nil")
	}
	dataMap := result["data"].(map[string]any)
	if dataMap["key"] != "value" {
		t.Errorf("data.key 应为 value，实际 %v", dataMap["key"])
	}
	if dataMap["count"] != 42 {
		t.Errorf("data.count 应为 42，实际 %v", dataMap["count"])
	}
}

// TestToolSuccess_空数据 构造成功的工具返回（空 map）
func TestToolSuccess_空数据(t *testing.T) {
	result, err := toolSuccess(map[string]any{})
	if err != nil {
		t.Fatalf("toolSuccess 不应返回 error: %v", err)
	}
	if result["success"] != true {
		t.Errorf("success 应为 true，实际 %v", result["success"])
	}
	dataMap, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatal("data 类型应为 map[string]any")
	}
	if len(dataMap) != 0 {
		t.Errorf("空 data map 长度应为 0，实际 %d", len(dataMap))
	}
}

// TestToolError 构造业务失败的工具返回
func TestToolError(t *testing.T) {
	msg := "something went wrong"
	result, err := toolError(msg)
	if err != nil {
		t.Fatalf("toolError 不应返回 error: %v", err)
	}
	if result["success"] != false {
		t.Errorf("success 应为 false，实际 %v", result["success"])
	}
	if result["error"] != msg {
		t.Errorf("error 应为 %q，实际 %v", msg, result["error"])
	}
}

// TestToolError_空消息 构造业务失败的工具返回（空消息）
func TestToolError_空消息(t *testing.T) {
	result, err := toolError("")
	if err != nil {
		t.Fatalf("toolError 不应返回 error: %v", err)
	}
	if result["success"] != false {
		t.Errorf("success 应为 false，实际 %v", result["success"])
	}
	if result["error"] != "" {
		t.Errorf("error 应为空字符串，实际 %v", result["error"])
	}
}

// TestSpecLabel 返回任务规格的标签
func TestSpecLabel(t *testing.T) {
	tests := []struct {
		name     string
		spec     map[string]any
		expected string
	}{
		{
			name:     "有 task_id 时返回 task_id",
			spec:     map[string]any{"task_id": "T-001", "title": "hello"},
			expected: "T-001",
		},
		{
			name:     "task_id 为空时返回 title",
			spec:     map[string]any{"task_id": "", "title": "hello"},
			expected: "hello",
		},
		{
			name:     "无 task_id 时返回 title",
			spec:     map[string]any{"title": "build feature"},
			expected: "build feature",
		},
		{
			name:     "task_id 和 title 都为空时返回 unnamed",
			spec:     map[string]any{"task_id": "", "title": ""},
			expected: "<unnamed>",
		},
		{
			name:     "task_id 和 title 都不存在时返回 unnamed",
			spec:     map[string]any{},
			expected: "<unnamed>",
		},
		{
			name:     "task_id 非字符串时回退到 title",
			spec:     map[string]any{"task_id": 123, "title": "fallback"},
			expected: "fallback",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := specLabel(tt.spec)
			if got != tt.expected {
				t.Errorf("specLabel() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestTaskBrief 返回任务的简要信息 map
func TestTaskBrief(t *testing.T) {
	t.Run("正常任务", func(t *testing.T) {
		assignee := "alice"
		task := &database.TeamTaskBase{
			TaskID:   "T-001",
			Title:    "build feature",
			Status:   "pending",
			Assignee: &assignee,
			TeamName: "team-a",
		}
		brief := taskBrief(task)
		if brief["task_id"] != "T-001" {
			t.Errorf("task_id 应为 T-001，实际 %v", brief["task_id"])
		}
		if brief["title"] != "build feature" {
			t.Errorf("title 应为 build feature，实际 %v", brief["title"])
		}
		if brief["status"] != "pending" {
			t.Errorf("status 应为 pending，实际 %v", brief["status"])
		}
		if brief["assignee"] != "alice" {
			t.Errorf("assignee 应为 alice，实际 %v", brief["assignee"])
		}
		if brief["team_name"] != "team-a" {
			t.Errorf("team_name 应为 team-a，实际 %v", brief["team_name"])
		}
	})

	t.Run("assignee 为 nil 时显示 unassigned", func(t *testing.T) {
		task := &database.TeamTaskBase{
			TaskID:   "T-002",
			Title:    "review code",
			Status:   "claimed",
			Assignee: nil,
			TeamName: "team-b",
		}
		brief := taskBrief(task)
		if brief["assignee"] != "<unassigned>" {
			t.Errorf("assignee 应为 <unassigned>，实际 %v", brief["assignee"])
		}
	})

	t.Run("assignee 为空字符串时显示 unassigned", func(t *testing.T) {
		emptyAssignee := ""
		task := &database.TeamTaskBase{
			TaskID:   "T-003",
			Title:    "test",
			Status:   "pending",
			Assignee: &emptyAssignee,
			TeamName: "team-c",
		}
		brief := taskBrief(task)
		if brief["assignee"] != "<unassigned>" {
			t.Errorf("assignee 应为 <unassigned>，实际 %v", brief["assignee"])
		}
	})

	t.Run("nil 任务返回 nil", func(t *testing.T) {
		brief := taskBrief(nil)
		if brief != nil {
			t.Errorf("nil 任务应返回 nil，实际 %v", brief)
		}
	})
}

// TestExtractStringSlice 从 map 中提取字符串切片
func TestExtractStringSlice(t *testing.T) {
	t.Run("不存在 key 返回 nil", func(t *testing.T) {
		m := map[string]any{"other": "value"}
		result := extractStringSlice(m, "missing")
		if result != nil {
			t.Errorf("不存在的 key 应返回 nil，实际 %v", result)
		}
	})

	t.Run("值为 nil 返回 nil", func(t *testing.T) {
		m := map[string]any{"items": nil}
		result := extractStringSlice(m, "items")
		if result != nil {
			t.Errorf("nil 值应返回 nil，实际 %v", result)
		}
	})

	t.Run("[]string 类型直接返回", func(t *testing.T) {
		m := map[string]any{"items": []string{"a", "b", "c"}}
		result := extractStringSlice(m, "items")
		if len(result) != 3 {
			t.Fatalf("长度应为 3，实际 %d", len(result))
		}
		if result[0] != "a" || result[1] != "b" || result[2] != "c" {
			t.Errorf("内容应为 [a,b,c]，实际 %v", result)
		}
	})

	t.Run("[]any 类型提取字符串", func(t *testing.T) {
		m := map[string]any{"items": []any{"x", "y", "z"}}
		result := extractStringSlice(m, "items")
		if len(result) != 3 {
			t.Fatalf("长度应为 3，实际 %d", len(result))
		}
		if result[0] != "x" || result[1] != "y" || result[2] != "z" {
			t.Errorf("内容应为 [x,y,z]，实际 %v", result)
		}
	})

	t.Run("[]any 类型跳过非字符串元素", func(t *testing.T) {
		m := map[string]any{"items": []any{"a", 123, "b"}}
		result := extractStringSlice(m, "items")
		if len(result) != 2 {
			t.Fatalf("长度应为 2（跳过 123），实际 %d", len(result))
		}
		if result[0] != "a" || result[1] != "b" {
			t.Errorf("内容应为 [a,b]，实际 %v", result)
		}
	})

	t.Run("JSON 序列化反序列化路径", func(t *testing.T) {
		// 传入一个可以被 JSON 序列化为字符串数组的值
		m := map[string]any{"items": []any{1.0, 2.0, 3.0}}
		result := extractStringSlice(m, "items")
		// []any{1.0, 2.0, 3.0} JSON 序列化后为 [1,2,3]，反序列化到 []string 会失败
		if result != nil {
			t.Logf("JSON 路径: %v", result)
		}
	})
}

// setupReassignToolFixture 建 UpdateTaskTool/ClaimTaskTool 测试夹具。
func setupReassignToolFixture(t *testing.T) (*TeamBackend, *database.InMemoryTeamDatabase, context.Context) {
	t.Helper()
	db := database.NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "", "autonomous", false)
	db.CreateMember(ctx, "leader1", "alpha", "Leader", "{}", "ready", "leader", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "agent1", "alpha", "Agent1", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "agent2", "alpha", "Agent2", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	db.Initialize(ctx)
	backend := NewTeamBackend("alpha", "leader1", true, db, nil, WithLeaderMemberName("leader1"))
	return backend, db, ctx
}

// TestUpdateTaskTool_ReassignKeepsStatus 原子改派：状态原位保留，不发 RELEASED。
// Python: tool_task.py L1810-1822。
func TestUpdateTaskTool_ReassignKeepsStatus(t *testing.T) {
	backend, db, ctx := setupReassignToolFixture(t)
	owner := "agent1"
	if ok, _ := db.Task().CreateTask(ctx, &database.TeamTaskBase{TaskID: "t1", TeamName: "alpha",
		Title: "T", Content: "C", Status: fsm.TaskStatusInProgress, Assignee: &owner}); !ok {
		t.Fatal("夹具建任务失败")
	}
	tr := locales.MakeTranslator("cn")
	tool := newUpdateTaskTool(backend, tr)
	out, err := tool.Invoke(ctx, map[string]any{"task_id": "t1", "assignee": "agent2"})
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := out["success"].(bool); !ok {
		t.Fatalf("改派应成功: %v", out)
	}
	got, _ := db.Task().GetTask(ctx, "t1")
	if got.Status != fsm.TaskStatusInProgress {
		t.Errorf("状态应原位保留 in_progress: got %q", got.Status)
	}
	if got.Assignee == nil || *got.Assignee != "agent2" {
		t.Errorf("新 owner 应为 agent2: got %v", got.Assignee)
	}
}

// TestUpdateTaskTool_ReassignBusyRejected one-active 前置：新 owner 持在办票时
// 改派被拒且任务原 owner 不变。Python: tool_task.py L1800-1807。
func TestUpdateTaskTool_ReassignBusyRejected(t *testing.T) {
	backend, db, ctx := setupReassignToolFixture(t)
	owner := "agent1"
	busy := "agent2"
	if ok, _ := db.Task().CreateTask(ctx, &database.TeamTaskBase{TaskID: "t1", TeamName: "alpha",
		Title: "T", Content: "C", Status: fsm.TaskStatusInProgress, Assignee: &owner}); !ok {
		t.Fatal("夹具建任务失败")
	}
	if ok, _ := db.Task().CreateTask(ctx, &database.TeamTaskBase{TaskID: "t2", TeamName: "alpha",
		Title: "T2", Content: "C", Status: fsm.TaskStatusInProgress, Assignee: &busy}); !ok {
		t.Fatal("夹具建在办票失败")
	}
	tool := newUpdateTaskTool(backend, locales.MakeTranslator("cn"))
	out, err := tool.Invoke(ctx, map[string]any{"task_id": "t1", "assignee": "agent2"})
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := out["success"].(bool); ok {
		t.Fatal("改派给在办票成员应被拒")
	}
	if msg, _ := out["error"].(string); !strings.Contains(msg, "already has an active task #t2") {
		t.Errorf("错误文案应含 one-active 原因: %q", msg)
	}
	got, _ := db.Task().GetTask(ctx, "t1")
	if got.Assignee == nil || *got.Assignee != "agent1" {
		t.Errorf("被拒后原 owner 应保持不变: got %v", got.Assignee)
	}
}

// TestClaimTaskTool_BusyRejected claim 工具边界的 one-active 前置。
// Python: tool_task.py L2049-2058。
func TestClaimTaskTool_BusyRejected(t *testing.T) {
	backend, db, ctx := setupReassignToolFixture(t)
	// backend 以 leader1 身份构建；claim 工具按 member_name=agent1 探测需要
	// teammate 视角，另建一个 backend。
	teammate := NewTeamBackend("alpha", "agent1", false, db, nil)
	busy := "agent1"
	if ok, _ := db.Task().CreateTask(ctx, &database.TeamTaskBase{TaskID: "t-held", TeamName: "alpha",
		Title: "Held", Content: "C", Status: fsm.TaskStatusInProgress, Assignee: &busy}); !ok {
		t.Fatal("夹具建在办票失败")
	}
	if ok, _ := db.Task().CreateTask(ctx, &database.TeamTaskBase{TaskID: "t-open", TeamName: "alpha",
		Title: "Open", Content: "C", Status: fsm.TaskStatusPending}); !ok {
		t.Fatal("夹具建可认领票失败")
	}
	tool := newClaimTaskTool(teammate.TaskManager(), locales.MakeTranslator("cn"))
	out, err := tool.Invoke(ctx, map[string]any{"task_id": "t-open", "status": "claimed"})
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := out["success"].(bool); ok {
		t.Fatal("已持在办票时再认领应被拒")
	}
	if msg, _ := out["error"].(string); !strings.Contains(msg, "You already have an active task #t-held") {
		t.Errorf("错误文案应含 one-active 原因: %q", msg)
	}
	_ = backend
}
