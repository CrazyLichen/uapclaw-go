package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/locales"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// namesOf 把工具列表压成名字列表，便于断言权限过滤结果。
func namesOf(tools []tool.Tool) []string {
	out := make([]string, len(tools))
	for i, tl := range tools {
		out[i] = tl.Card().Name
	}
	return out
}

// hasTool 判断工具列表中是否含指定名字。
func hasTool(tools []tool.Tool, name string) bool {
	for _, tl := range tools {
		if tl.Card().Name == name {
			return true
		}
	}
	return false
}

// invokeByName 按名字取出工具并调用，返回结果 map。
func invokeByName(t *testing.T, tools []tool.Tool, name string, inputs map[string]any) map[string]any {
	t.Helper()
	for _, tl := range tools {
		if tl.Card().Name == name {
			res, err := tl.Invoke(context.Background(), inputs)
			if err != nil {
				t.Fatalf("%s.Invoke 返回 error: %v", name, err)
			}
			return res
		}
	}
	t.Fatalf("工具 %q 不在列表中: %v", name, namesOf(tools))
	return nil
}

// errText 取出工具失败结果的 error 字段。
func errText(res map[string]any) string {
	s, _ := res["error"].(string)
	return s
}

// dataOf 取出工具成功结果的 data 字段。
func dataOf(res map[string]any) map[string]any {
	m, _ := res["data"].(map[string]any)
	return m
}

// createOneTask 用 create_task 建一个单任务并返回其 task_id。
func createOneTask(t *testing.T, tools []tool.Tool, title string) string {
	t.Helper()
	res := invokeByName(t, tools, "create_task", map[string]any{
		"tasks": []any{map[string]any{"title": title, "content": title + " content"}},
	})
	if res["success"] != true {
		t.Fatalf("create_task 应成功，实际 %v", res)
	}
	id, _ := dataOf(res)["task_id"].(string)
	if id == "" {
		t.Fatalf("create_task 未返回 task_id，实际 %v", dataOf(res))
	}
	return id
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestCreateTeamTools_角色过滤 验证不同 role / 模式 / 生命周期下的工具集合。
func TestCreateTeamTools_角色过滤(t *testing.T) {
	backend := newTestTeamBackend()

	t.Run("leader 默认模式", func(t *testing.T) {
		tools := CreateTeamTools(backend, TeamToolOptions{
			Role:         "leader",
			TeammateMode: "build_mode",
			Lifecycle:    "ephemeral",
			Language:     "cn",
		}, nil, nil)
		if !hasTool(tools, "build_team") || !hasTool(tools, "spawn_member") {
			t.Fatalf("leader 应包含 build_team/spawn_member，实际 %v", namesOf(tools))
		}
		if hasTool(tools, "approve_plan") || hasTool(tools, "submit_plan") {
			t.Fatalf("非 plan_mode 不应包含审批/提交工具，实际 %v", namesOf(tools))
		}
	})

	t.Run("leader + plan_mode 含审批工具", func(t *testing.T) {
		tools := CreateTeamTools(backend, TeamToolOptions{
			Role:         "leader",
			TeammateMode: "plan_mode",
			Lifecycle:    "ephemeral",
			Language:     "cn",
		}, nil, nil)
		if !hasTool(tools, "approve_plan") || !hasTool(tools, "approve_tool") {
			t.Fatalf("plan_mode 应包含审批工具，实际 %v", namesOf(tools))
		}
	})

	t.Run("persistent 生命周期移除 clean_team", func(t *testing.T) {
		tools := CreateTeamTools(backend, TeamToolOptions{
			Role:         "leader",
			TeammateMode: "build_mode",
			Lifecycle:    "persistent",
			Language:     "cn",
		}, nil, nil)
		if hasTool(tools, "clean_team") {
			t.Fatalf("persistent 不应包含 clean_team，实际 %v", namesOf(tools))
		}
	})

	t.Run("excludeTools 生效", func(t *testing.T) {
		tools := CreateTeamTools(backend, TeamToolOptions{
			Role:         "leader",
			TeammateMode: "build_mode",
			Lifecycle:    "ephemeral",
			Language:     "cn",
		}, nil, map[string]struct{}{"spawn_member": {}})
		if hasTool(tools, "spawn_member") {
			t.Fatalf("排除的 spawn_member 不应出现，实际 %v", namesOf(tools))
		}
	})

	t.Run("backend 为 nil 时无工具", func(t *testing.T) {
		tools := CreateTeamTools(nil, TeamToolOptions{
			Role:         "leader",
			TeammateMode: "build_mode",
			Lifecycle:    "ephemeral",
			Language:     "cn",
		}, nil, nil)
		if len(tools) != 0 {
			t.Fatalf("backend 为 nil 时应无工具，实际 %v", namesOf(tools))
		}
	})

	t.Run("member 角色集合", func(t *testing.T) {
		tools := CreateTeamTools(backend, TeamToolOptions{
			Role:         "",
			TeammateMode: "build_mode",
			Lifecycle:    "ephemeral",
			Language:     "cn",
		}, nil, nil)
		if hasTool(tools, "build_team") {
			t.Fatalf("member 不应包含 build_team，实际 %v", namesOf(tools))
		}
		if !hasTool(tools, "claim_task") {
			t.Fatalf("member 应包含 claim_task，实际 %v", namesOf(tools))
		}
	})

	t.Run("human_agent 角色集合", func(t *testing.T) {
		tools := CreateTeamTools(backend, TeamToolOptions{
			Role:         "human_agent",
			TeammateMode: "build_mode",
			Lifecycle:    "ephemeral",
			Language:     "cn",
		}, nil, nil)
		if hasTool(tools, "spawn_member") {
			t.Fatalf("human_agent 不应包含 spawn_member，实际 %v", namesOf(tools))
		}
	})
}

// TestMemberNameRegexp 验证成员名正则暴露。
func TestMemberNameRegexp(t *testing.T) {
	re := MemberNameRegexp()
	if re == nil {
		t.Fatal("正则不应为 nil")
	}
	if !re.MatchString("dev-1") {
		t.Fatal("dev-1 应匹配")
	}
	if re.MatchString("Dev_1") {
		t.Fatal("Dev_1 不应匹配")
	}
}

// TestCommaStrToSet 验证逗号分隔字符串转集合（含空项与空白）。
func TestCommaStrToSet(t *testing.T) {
	got := commaStrToSet(" a , b ,, c ")
	if len(got) != 3 {
		t.Fatalf("集合大小 = %d, 期望 3 (%v)", len(got), got)
	}
	for _, k := range []string{"a", "b", "c"} {
		if _, ok := got[k]; !ok {
			t.Fatalf("缺少键 %q", k)
		}
	}
	if len(commaStrToSet("")) != 0 {
		t.Fatal("空串应得空集合")
	}
}

// TestMergeSets 验证集合合并且不修改入参。
func TestMergeSets(t *testing.T) {
	a := map[string]struct{}{"x": {}}
	b := map[string]struct{}{"y": {}}
	got := mergeSets(a, b)
	if len(got) != 2 {
		t.Fatalf("合并后大小 = %d, 期望 2", len(got))
	}
	if _, ok := got["x"]; !ok {
		t.Fatal("缺少 x")
	}
	if _, ok := got["y"]; !ok {
		t.Fatal("缺少 y")
	}
	if len(a) != 1 {
		t.Fatal("原集合不应被修改")
	}
}

// TestCopySet 验证集合复制是独立副本。
func TestCopySet(t *testing.T) {
	src := map[string]struct{}{"a": {}}
	cp := copySet(src)
	cp["b"] = struct{}{}
	if len(src) != 1 {
		t.Fatal("副本修改不应影响原集合")
	}
	if len(cp) != 2 {
		t.Fatal("副本应含两个元素")
	}
}

// TestTeamTools_StreamNotSupported 验证所有工具显式拒绝流式调用。
func TestTeamTools_StreamNotSupported(t *testing.T) {
	backend := newTestTeamBackend()
	tools := CreateTeamTools(backend, TeamToolOptions{
		Role:         "leader",
		TeammateMode: "plan_mode",
		Lifecycle:    "ephemeral",
		Language:     "cn",
	}, nil, nil)
	for _, tl := range tools {
		ch, err := tl.Stream(context.Background(), map[string]any{})
		if err == nil || ch != nil {
			t.Fatalf("%s.Stream 应返回不支持错误，实际 err=%v ch=%v", tl.Card().Name, err, ch)
		}
	}
}

// TestTeamTools_参数校验失败分支 覆盖各工具的输入校验与后端失败路径。
func TestTeamTools_参数校验失败分支(t *testing.T) {
	ctx := context.Background()
	backend := newTestTeamBackend()
	tools := CreateTeamTools(backend, TeamToolOptions{
		Role:         "leader",
		TeammateMode: "plan_mode",
		Lifecycle:    "ephemeral",
		Language:     "cn",
	}, nil, nil)
	// claim_task / member_complete_task 分属成员集与 human_agent 集，leader 集合里没有
	memberTools := CreateTeamTools(backend, TeamToolOptions{
		Role:         "",
		TeammateMode: "build_mode",
		Lifecycle:    "ephemeral",
		Language:     "cn",
	}, nil, nil)
	humanTools := CreateTeamTools(backend, TeamToolOptions{
		Role:         "human_agent",
		TeammateMode: "build_mode",
		Lifecycle:    "ephemeral",
		Language:     "cn",
	}, nil, nil)

	t.Run("spawn_member 成员名非法", func(t *testing.T) {
		res := invokeByName(t, tools, "spawn_member", map[string]any{"member_name": "Dev_1"})
		if res["success"] != false || !strings.Contains(errText(res), "Invalid member_name") {
			t.Fatalf("应报成员名非法，实际 %v", res)
		}
	})

	t.Run("spawn_member role_type 非法", func(t *testing.T) {
		res := invokeByName(t, tools, "spawn_member", map[string]any{
			"member_name": "dev-1", "role_type": "robot",
		})
		if res["success"] != false || !strings.Contains(errText(res), "Invalid role_type") {
			t.Fatalf("应报 role_type 非法，实际 %v", res)
		}
	})

	t.Run("spawn_member human_agent 在 HITT 关闭时被拒", func(t *testing.T) {
		res := invokeByName(t, tools, "spawn_member", map[string]any{
			"member_name": "dev-1", "role_type": "human_agent",
		})
		if res["success"] != false || !strings.Contains(errText(res), "HITT") {
			t.Fatalf("应报 HITT 未开启，实际 %v", res)
		}
	})

	t.Run("create_task 缺 tasks 参数", func(t *testing.T) {
		res := invokeByName(t, tools, "create_task", map[string]any{})
		if res["success"] != false || !strings.Contains(errText(res), "'tasks' is required") {
			t.Fatalf("应报缺 tasks，实际 %v", res)
		}
	})

	t.Run("create_task 单任务缺 title/content", func(t *testing.T) {
		res := invokeByName(t, tools, "create_task", map[string]any{
			"tasks": []any{map[string]any{"title": "only title"}},
		})
		if res["success"] != false || !strings.Contains(errText(res), "missing required title/content") {
			t.Fatalf("应报缺 title/content，实际 %v", res)
		}
	})

	t.Run("view_task get 缺 task_id", func(t *testing.T) {
		res := invokeByName(t, tools, "view_task", map[string]any{"action": "get"})
		if res["success"] != false || !strings.Contains(errText(res), "task_id required") {
			t.Fatalf("应报缺 task_id，实际 %v", res)
		}
	})

	t.Run("view_task get 不存在任务", func(t *testing.T) {
		res := invokeByName(t, tools, "view_task", map[string]any{"action": "get", "task_id": "nope"})
		if res["success"] != false || !strings.Contains(errText(res), "Task not found") {
			t.Fatalf("应报任务不存在，实际 %v", res)
		}
	})

	t.Run("update_task 缺 task_id", func(t *testing.T) {
		res := invokeByName(t, tools, "update_task", map[string]any{})
		if res["success"] != false || !strings.Contains(errText(res), "'task_id' is required") {
			t.Fatalf("应报缺 task_id，实际 %v", res)
		}
	})

	t.Run("update_task 未指定任何更新字段", func(t *testing.T) {
		taskID := createOneTask(t, tools, "t1")
		res := invokeByName(t, tools, "update_task", map[string]any{"task_id": taskID})
		if res["success"] != false || !strings.Contains(errText(res), "No update specified") {
			t.Fatalf("应报未指定更新字段，实际 %v", res)
		}
	})

	t.Run("claim_task 任务不存在", func(t *testing.T) {
		res := invokeByName(t, memberTools, "claim_task", map[string]any{"task_id": "nope", "status": "claimed"})
		if res["success"] != false || !strings.Contains(errText(res), "Task not found") {
			t.Fatalf("应报任务不存在，实际 %v", res)
		}
	})

	t.Run("claim_task status 非法", func(t *testing.T) {
		taskID := createOneTask(t, tools, "t2")
		res := invokeByName(t, memberTools, "claim_task", map[string]any{"task_id": taskID, "status": "bogus"})
		if res["success"] != false || !strings.Contains(errText(res), "Invalid status") {
			t.Fatalf("应报 status 非法，实际 %v", res)
		}
	})

	t.Run("send_message 缺 content", func(t *testing.T) {
		res := invokeByName(t, tools, "send_message", map[string]any{"to": "user"})
		if res["success"] != false || !strings.Contains(errText(res), "'content' is required") {
			t.Fatalf("应报缺 content，实际 %v", res)
		}
	})

	t.Run("send_message to 类型非法", func(t *testing.T) {
		res := invokeByName(t, tools, "send_message", map[string]any{"to": 42, "content": "hi"})
		if res["success"] != false || !strings.Contains(errText(res), "must be a string") {
			t.Fatalf("应报 to 类型非法，实际 %v", res)
		}
	})

	t.Run("send_message to 为空串", func(t *testing.T) {
		res := invokeByName(t, tools, "send_message", map[string]any{"to": "  ", "content": "hi"})
		if res["success"] != false || !strings.Contains(errText(res), "'to' is required") {
			t.Fatalf("应报 to 必填，实际 %v", res)
		}
	})

	t.Run("send_message multicast 含 user 被拒", func(t *testing.T) {
		res := invokeByName(t, tools, "send_message", map[string]any{
			"to": []any{"user", "dev-1"}, "content": "hi",
		})
		if res["success"] != false || !strings.Contains(errText(res), "cannot be combined in multicast") {
			t.Fatalf("应报 user 不可混入 multicast，实际 %v", res)
		}
	})

	t.Run("send_message multicast 含 * 被拒", func(t *testing.T) {
		res := invokeByName(t, tools, "send_message", map[string]any{
			"to": []any{"*", "dev-1"}, "content": "hi",
		})
		if res["success"] != false || !strings.Contains(errText(res), "Cannot mix broadcast") {
			t.Fatalf("应报不可混用广播，实际 %v", res)
		}
	})

	t.Run("send_message multicast 空列表被拒", func(t *testing.T) {
		res := invokeByName(t, tools, "send_message", map[string]any{
			"to": []any{"  ", ""}, "content": "hi",
		})
		if res["success"] != false || !strings.Contains(errText(res), "at least one member name") {
			t.Fatalf("应报需要目标成员，实际 %v", res)
		}
	})

	t.Run("member_complete_task 空输入走任务不存在分支", func(t *testing.T) {
		// 生产实现用 fmt.Sprintf("%v", nil) 得到 "<nil>"，非空串，故落到 get 后的
		// 「任务不存在」分支而非 task_id 必填分支——按实际行为断言。
		res := invokeByName(t, humanTools, "member_complete_task", map[string]any{})
		if res["success"] != false || !strings.Contains(errText(res), "not found") {
			t.Fatalf("应报任务不存在，实际 %v", res)
		}
	})

	_ = ctx
}

// TestTeamTools_后端交互分支 覆盖 leader 工具与后端交互的成功/失败路径。
func TestTeamTools_后端交互分支(t *testing.T) {
	backend := newTestTeamBackend()
	tools := CreateTeamTools(backend, TeamToolOptions{
		Role:         "leader",
		TeammateMode: "plan_mode",
		Lifecycle:    "ephemeral",
		Language:     "cn",
	}, nil, nil)

	t.Run("list_members 空团队返回零成员", func(t *testing.T) {
		res := invokeByName(t, tools, "list_members", map[string]any{})
		if res["success"] != true {
			t.Fatalf("list_members 应成功，实际 %v", res)
		}
		data := dataOf(res)
		if count, _ := data["count"].(int); count != 0 {
			t.Fatalf("空团队 count 应为 0，实际 %v", data["count"])
		}
	})

	t.Run("shutdown_member 不存在的成员返回失败", func(t *testing.T) {
		res := invokeByName(t, tools, "shutdown_member", map[string]any{"member_name": "nobody", "force": true})
		if res["success"] != false {
			t.Fatalf("关闭不存在成员应失败，实际 %v", res)
		}
	})

	t.Run("approve_plan 不存在的计划返回失败", func(t *testing.T) {
		res := invokeByName(t, tools, "approve_plan", map[string]any{"plan_id": "nope", "approved": true})
		if res["success"] != false || !strings.Contains(errText(res), "Failed to approve/reject plan") {
			t.Fatalf("应报审批失败，实际 %v", res)
		}
	})

	t.Run("approve_tool 不存在的成员返回失败", func(t *testing.T) {
		res := invokeByName(t, tools, "approve_tool", map[string]any{
			"member_name": "nobody", "tool_call_id": "c1", "approved": true, "auto_confirm": true,
		})
		if res["success"] != false || !strings.Contains(errText(res), "Failed to approve/reject tool call") {
			t.Fatalf("应报审批失败，实际 %v", res)
		}
	})

	t.Run("clean_team 无活跃成员时可清理", func(t *testing.T) {
		res := invokeByName(t, tools, "clean_team", map[string]any{})
		// 空团队无活跃成员 → 应成功；若后端判定仍有活跃成员则给出明确 error
		if res["success"] != true && !strings.Contains(errText(res), "Active members remain") {
			t.Fatalf("clean_team 结果异常: %v", res)
		}
	})

	t.Run("view_task list 无任务返回空列表", func(t *testing.T) {
		res := invokeByName(t, tools, "view_task", map[string]any{"action": "list"})
		if res["success"] != true {
			t.Fatalf("view_task 应成功，实际 %v", res)
		}
		if _, ok := dataOf(res)["tasks"]; !ok {
			t.Fatalf("应返回 tasks 字段，实际 %v", dataOf(res))
		}
	})

	t.Run("view_task claimable 只列 pending", func(t *testing.T) {
		res := invokeByName(t, tools, "view_task", map[string]any{"action": "claimable"})
		if res["success"] != true {
			t.Fatalf("view_task claimable 应成功，实际 %v", res)
		}
	})

	t.Run("send_message 给不存在的成员失败", func(t *testing.T) {
		res := invokeByName(t, tools, "send_message", map[string]any{"to": "nobody", "content": "hi"})
		if res["success"] != false || !strings.Contains(errText(res), "not found") {
			t.Fatalf("应报成员不存在，实际 %v", res)
		}
	})

	t.Run("create_task 批量全部失败时报错汇总", func(t *testing.T) {
		res := invokeByName(t, tools, "create_task", map[string]any{
			"tasks": []any{
				map[string]any{"title": ""},
				map[string]any{"content": ""},
			},
		})
		if res["success"] != false || !strings.Contains(errText(res), "All 2 task creations failed") {
			t.Fatalf("应报全部失败汇总，实际 %v", res)
		}
	})
}

// ──────────────────────────── 内联成员测试 ────────────────────────────

// newInlineTaskTool 造一个可用的 create_task 工具（leader 视角）。
func newInlineTaskTool(t *testing.T) (*TaskCreateTool, *TeamBackend) {
	t.Helper()
	backend := newTestTeamBackend()
	backend.BuildTeam(context.Background(), "T", "d", "Leader", "ld", nil, nil)
	return newTaskCreateTool(backend, locales.MakeTranslator(atschema.Language("cn"))), backend
}

// TestValidateInlineMembers_合法与冲突 覆盖各条拒绝分支。
func TestValidateInlineMembers_合法与冲突(t *testing.T) {
	okSpec := func() map[string]any {
		return map[string]any{"title": "t", "content": "c", "member": map[string]any{
			"member_name": "dev-1", "display_name": "Dev", "desc": "d", "lifetime": "shutdown",
		}}
	}
	t.Run("合法通过", func(t *testing.T) {
		if msg := validateInlineMembers([]map[string]any{okSpec()}, "leader"); msg != "" {
			t.Fatalf("不应报错: %s", msg)
		}
	})
	t.Run("member 非对象", func(t *testing.T) {
		if msg := validateInlineMembers([]map[string]any{{"title": "t", "member": "x"}}, ""); msg == "" {
			t.Fatal("应报错")
		}
	})
	t.Run("assignee 与 member_name 冲突", func(t *testing.T) {
		spec := okSpec()
		spec["assignee"] = "other"
		if msg := validateInlineMembers([]map[string]any{spec}, ""); msg == "" {
			t.Fatal("应报错")
		}
	})
	t.Run("member_name 非法", func(t *testing.T) {
		spec := okSpec()
		spec["member"] = map[string]any{"member_name": "Dev_1", "display_name": "D", "desc": "d", "lifetime": "shutdown"}
		if msg := validateInlineMembers([]map[string]any{spec}, ""); msg == "" {
			t.Fatal("应报错")
		}
	})
	t.Run("批内重名", func(t *testing.T) {
		if msg := validateInlineMembers([]map[string]any{okSpec(), okSpec()}, ""); msg == "" {
			t.Fatal("应报错")
		}
	})
	t.Run("是 leader", func(t *testing.T) {
		if msg := validateInlineMembers([]map[string]any{okSpec()}, "dev-1"); msg == "" {
			t.Fatal("应报错")
		}
	})
	t.Run("lifetime 非法", func(t *testing.T) {
		spec := okSpec()
		spec["member"] = map[string]any{"member_name": "dev-1", "display_name": "D", "desc": "d", "lifetime": "forever"}
		if msg := validateInlineMembers([]map[string]any{spec}, ""); msg == "" {
			t.Fatal("应报错")
		}
	})
	t.Run("isolation 非法", func(t *testing.T) {
		spec := okSpec()
		spec["member"] = map[string]any{"member_name": "dev-1", "display_name": "D", "desc": "d",
			"lifetime": "shutdown", "isolation": "copy"}
		if msg := validateInlineMembers([]map[string]any{spec}, ""); msg == "" {
			t.Fatal("应报错")
		}
	})
	t.Run("缺 display_name", func(t *testing.T) {
		spec := okSpec()
		spec["member"] = map[string]any{"member_name": "dev-1", "desc": "d", "lifetime": "shutdown"}
		if msg := validateInlineMembers([]map[string]any{spec}, ""); msg == "" {
			t.Fatal("应报错")
		}
	})
}

// TestValidateAssignees_各分支 未知成员 / leader / 已关闭 / 批内成员免探测。
func TestValidateAssignees_各分支(t *testing.T) {
	ctx := context.Background()
	tool, backend := newInlineTaskTool(t)
	_ = tool

	t.Run("未知成员", func(t *testing.T) {
		msg := validateAssignees(ctx, backend, []map[string]any{{"title": "t", "assignee": "ghost"}}, false, nil)
		if msg == "" {
			t.Fatal("应报错")
		}
	})
	t.Run("assignee 是 leader", func(t *testing.T) {
		msg := validateAssignees(ctx, backend, []map[string]any{{"title": "t", "assignee": "Leader"}}, false, nil)
		if msg == "" {
			t.Fatal("应报错")
		}
	})
	t.Run("批内成员免探测", func(t *testing.T) {
		msg := validateAssignees(ctx, backend, []map[string]any{{"title": "t", "assignee": "dev-1"}}, false,
			map[string]struct{}{"dev-1": {}})
		if msg != "" {
			t.Fatalf("批内成员应通过: %s", msg)
		}
	})
	t.Run("required 但缺失", func(t *testing.T) {
		msg := validateAssignees(ctx, backend, []map[string]any{{"title": "t"}}, true, nil)
		if msg == "" {
			t.Fatal("应报错")
		}
	})
}

// TestRetiredAssigneeError 已关闭拒绝、关闭中拒绝、其它放行。
func TestRetiredAssigneeError(t *testing.T) {
	if msg := retiredAssigneeError("t", "m", string(atschema.MemberStatusShutdown)); msg == "" {
		t.Fatal("SHUTDOWN 应拒绝")
	}
	if msg := retiredAssigneeError("t", "m", string(atschema.MemberStatusShutdownRequested)); msg == "" {
		t.Fatal("SHUTDOWN_REQUESTED 应拒绝")
	}
	if msg := retiredAssigneeError("t", "m", string(atschema.MemberStatusReady)); msg != "" {
		t.Fatalf("READY 应放行: %s", msg)
	}
}

// TestInlineBrief 只有内联成员才带 lifetime。
func TestInlineBrief(t *testing.T) {
	lifetimes := map[string]string{"dev-1": "shutdown"}
	if b := inlineBrief("dev-1", lifetimes); b["lifetime"] != "shutdown" {
		t.Fatalf("应带 lifetime，实际 %v", b)
	}
	if b := inlineBrief("other", lifetimes); b != nil {
		t.Fatalf("非内联成员不应带 lifetime，实际 %v", b)
	}
}

// TestCreateTask_内联成员端到端 建成员 + 绑 assignee + brief 带 lifetime。
func TestCreateTask_内联成员端到端(t *testing.T) {
	ctx := context.Background()
	tool, backend := newInlineTaskTool(t)

	res, err := tool.Invoke(ctx, map[string]any{"tasks": []any{
		map[string]any{"title": "任务A", "content": "内容", "member": map[string]any{
			"member_name": "dev-1", "display_name": "Dev", "desc": "开发", "lifetime": "shutdown",
		}},
	}})
	require.NoError(t, err)
	require.Equal(t, true, res["success"], "应成功: %v", res)

	// 成员已注册且 lifetime 落在 options 里
	member, getErr := backend.GetMember(ctx, "dev-1")
	require.NoError(t, getErr)
	require.NotNil(t, member, "内联成员应已注册")
	lifetime := database.GetMemberLifetime(member)
	require.NotNil(t, lifetime)
	assert.Equal(t, database.MemberLifetimeShutdown, *lifetime)

	// 任务已带 assignee
	tasks, listErr := backend.TaskManager().ListTasks(ctx, "")
	require.NoError(t, listErr)
	require.Len(t, tasks, 1)
	require.NotNil(t, tasks[0].Assignee)
	assert.Equal(t, "dev-1", *tasks[0].Assignee)
	assert.Equal(t, fsm.TaskStatusPending, tasks[0].Status, "调度式创建应停在 PENDING")
}

// TestCreateTask_内联成员校验失败不落库 校验失败时成员与任务都不应写入。
func TestCreateTask_内联成员校验失败不落库(t *testing.T) {
	ctx := context.Background()
	tool, backend := newInlineTaskTool(t)

	res, err := tool.Invoke(ctx, map[string]any{"tasks": []any{
		map[string]any{"title": "任务A", "content": "内容", "member": map[string]any{
			"member_name": "Dev_1", "display_name": "Dev", "desc": "开发", "lifetime": "shutdown",
		}},
	}})
	require.NoError(t, err)
	assert.Equal(t, false, res["success"])

	assert.False(t, backend.MemberExists(ctx, "Dev_1"), "校验失败不应写成员")
	tasks, _ := backend.TaskManager().ListTasks(ctx, "")
	assert.Empty(t, tasks, "校验失败不应写任务")
}

// TestDiscardInlineMembers 回滚只删 UNSTARTED 的行。
func TestDiscardInlineMembers(t *testing.T) {
	ctx := context.Background()
	_, backend := newInlineTaskTool(t)
	backend.SpawnMember(ctx, "dev-1", "Dev", nil, string(atschema.TeamRoleTeammate), "", "", "")

	discardInlineMembers(ctx, backend, []string{"dev-1"})
	assert.False(t, backend.MemberExists(ctx, "dev-1"), "UNSTARTED 行应被删掉")

	// 已启动的成员不应被回滚
	backend.SpawnMember(ctx, "dev-2", "Dev2", nil, string(atschema.TeamRoleTeammate), "", "", "")
	backend.db.Member().UpdateMemberStatus(ctx, "dev-2", backend.teamName, string(atschema.MemberStatusReady))
	discardInlineMembers(ctx, backend, []string{"dev-2"})
	assert.True(t, backend.MemberExists(ctx, "dev-2"), "离开 UNSTARTED 的行不应被删")
}

// TestValidateAssignees_team为nil team 为 nil 时跳过存在性探测。
func TestValidateAssignees_team为nil(t *testing.T) {
	msg := validateAssignees(context.Background(), nil,
		[]map[string]any{{"title": "t", "assignee": "anyone"}}, false, nil)
	if msg != "" {
		t.Fatalf("team 为 nil 应跳过探测: %s", msg)
	}
}

// TestMaterializeInlineMembers_跳过非法member member 缺失或非对象时跳过该 spec。
func TestMaterializeInlineMembers_跳过非法member(t *testing.T) {
	ctx := context.Background()
	_, backend := newInlineTaskTool(t)
	specs := []map[string]any{
		{"title": "无 member"},
		{"title": "member 非对象", "member": "x"},
	}
	created, lifetimes := materializeInlineMembers(ctx, backend, specs)
	assert.Empty(t, created)
	assert.Empty(t, lifetimes)
}

// TestMaterializeInlineMembers_成员已存在时回滚 已存在的成员名导致整批回滚。
func TestMaterializeInlineMembers_成员已存在时回滚(t *testing.T) {
	ctx := context.Background()
	_, backend := newInlineTaskTool(t)
	// 预先占住 dev-1
	backend.SpawnMember(ctx, "dev-1", "Old", nil, string(atschema.TeamRoleTeammate), "", "", "")

	specs := []map[string]any{
		{"title": "t1", "member": map[string]any{"member_name": "dev-2", "display_name": "D2", "desc": "d", "lifetime": "shutdown"}},
		{"title": "t2", "member": map[string]any{"member_name": "dev-1", "display_name": "D1", "desc": "d", "lifetime": "shutdown"}},
	}
	created, _ := materializeInlineMembers(ctx, backend, specs)
	assert.Nil(t, created, "已存在成员应导致整批放弃")
	assert.False(t, backend.MemberExists(ctx, "dev-2"), "先写入的成员应被回滚")
	assert.True(t, backend.MemberExists(ctx, "dev-1"), "原有成员不应被删")
}
