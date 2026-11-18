package prompts

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

// strPtr 字符串指针助手。
func strPtr(s string) *string { return &s }

// metaJSON 把 meta map 序列化为 JSON 字符串指针（模拟 meta 列）。
func metaJSON(t *testing.T, meta map[string]any) *string {
	t.Helper()
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal meta: %v", err)
	}
	s := string(raw)
	return &s
}

// TestParseMeta_四类输入 对齐 parse_meta：nil/空串/坏 JSON/缺 template 键 → nil。
// Python: message_template.py L252-269
func TestParseMeta_四类输入(t *testing.T) {
	if ParseMeta(nil) != nil {
		t.Error("nil meta 应返回 nil")
	}
	empty := ""
	if ParseMeta(&empty) != nil {
		t.Error("空串应返回 nil")
	}
	bad := "not-json{"
	if ParseMeta(&bad) != nil {
		t.Error("坏 JSON 应返回 nil")
	}
	noTpl := "{}"
	if ParseMeta(&noTpl) != nil {
		t.Error("缺 template 键应返回 nil")
	}
	meta := ParseMeta(metaJSON(t, map[string]any{"template": "scheduler_task_start", "refs": map[string]any{"task": "t1"}}))
	if meta == nil || meta["template"] != "scheduler_task_start" {
		t.Errorf("合法 meta 解析失败: %v", meta)
	}
}

// TestBuildMeta_refs_params 对齐 build_meta：refs/params 缺省时不写入键。
// Python: message_template.py L271-289
func TestBuildMeta_refs_params(t *testing.T) {
	meta := BuildMeta("scheduler_task_start", map[string]string{"task": "t1"}, nil)
	if _, ok := meta["refs"]; !ok {
		t.Error("refs 应写入")
	}
	if _, ok := meta["params"]; ok {
		t.Error("params 为空时不应写入")
	}
	meta = BuildMeta("scheduler_rework", map[string]string{"task": "t1"}, map[string]string{"feedback": "x"})
	params, ok := meta["params"].(map[string]string)
	if !ok || params["feedback"] != "x" {
		t.Errorf("params 应写入并保持字符串: %v", meta["params"])
	}
}

// TestFallbackLine_带与不带task_id 对齐 fallback_line 两种形态。
// Python: message_template.py L291-303
func TestFallbackLine_带与不带task_id(t *testing.T) {
	withTask := FallbackLine(map[string]any{"template": "scheduler_task_start", "refs": map[string]any{"task": "t1"}})
	if !strings.Contains(withTask, "[scheduler_task_start] task_id=t1") {
		t.Errorf("带 task_id 的 fallback 形态不对: %q", withTask)
	}
	noTask := FallbackLine(map[string]any{"template": "x"})
	if noTask != "[x] — details unavailable." {
		t.Errorf("无 task_id 的 fallback 形态不对: %q", noTask)
	}
}

// TestCompletionSummary_截断与回落 对齐 completion_summary/completion_summary_truncated：
// summary 原样、超 200 rune 截断加…、无 summary 回落 note 首非空行。
// Python: message_template.py L128-163
func TestCompletionSummary_截断与回落(t *testing.T) {
	task := &database.TeamTaskBase{Summary: strPtr("done"), Note: strPtr("line1\nline2")}
	if got := CompletionSummary(task); got != "done" {
		t.Errorf("summary 优先: got %q", got)
	}
	task = &database.TeamTaskBase{Note: strPtr("first line\nsecond line")}
	if got := CompletionSummary(task); got != "first line" {
		t.Errorf("无 summary 回落 note 首行: got %q", got)
	}
	long := strings.Repeat("长", 201)
	task = &database.TeamTaskBase{Summary: strPtr(long)}
	got := CompletionSummary(task)
	if len([]rune(got)) != SummaryRenderLimit+1 || !strings.HasSuffix(got, "…") {
		t.Errorf("超限应截断加…: len=%d suffix=%v", len([]rune(got)), strings.HasSuffix(got, "…"))
	}
	if !CompletionSummaryTruncated(task) {
		t.Error("截断后 truncated 应为 true")
	}
	task = &database.TeamTaskBase{Note: strPtr("one\ntwo")}
	if !CompletionSummaryTruncated(task) {
		t.Error("回落 note 且不止一行时 truncated 应为 true")
	}
}

// TestTaskLine_UpstreamBrief 对齐 _task_line / upstream_brief（含收尾提示与空集为空）。
// Python: message_template.py L165-201
func TestTaskLine_UpstreamBrief(t *testing.T) {
	task := &database.TeamTaskBase{TaskID: "t1", Title: "T", Assignee: strPtr("bob"), Summary: strPtr("ok")}
	if got := TaskLine(task, true); got != "t1 · T · bob · ok" {
		t.Errorf("TaskLine 形态不对: %q", got)
	}
	if got := UpstreamBrief(nil, "cn"); got != "" {
		t.Errorf("无上游应返回空串: %q", got)
	}
	got := UpstreamBrief([]*database.TeamTaskBase{task}, "cn")
	if !strings.HasPrefix(got, "- t1 · T · · bob · ok") || !strings.Contains(got, "view_task(action='note'") {
		t.Errorf("UpstreamBrief 应含 bullet 与收尾提示: %q", got)
	}
}

// TestStaleAgainst_四分支 对齐 stale_against：顶层/终态/不落后/落后。
// Python: tools/database/graph.py L32-52
func TestStaleAgainst_四分支(t *testing.T) {
	top := &database.TeamTaskBase{Status: fsm.TaskStatusPending}
	if StaleAgainst(top, ptrI64(3)) != nil {
		t.Error("顶层任务应返回 nil")
	}
	done := &database.TeamTaskBase{ParentTaskID: strPtr("p"), Status: fsm.TaskStatusCompleted}
	if StaleAgainst(done, ptrI64(3)) != nil {
		t.Error("终态应返回 nil")
	}
	current := &database.TeamTaskBase{ParentTaskID: strPtr("p"), Status: fsm.TaskStatusPending, ParentRevision: 3}
	if StaleAgainst(current, ptrI64(3)) != nil {
		t.Error("基于父当前 revision 应返回 nil")
	}
	stale := &database.TeamTaskBase{ParentTaskID: strPtr("p"), Status: fsm.TaskStatusPending, ParentRevision: 2}
	got := StaleAgainst(stale, ptrI64(3))
	if got == nil || got[0] != 3 || got[1] != 2 {
		t.Errorf("落后应返回 (3, 2): %v", got)
	}
}

func ptrI64(v int64) *int64 { return &v }

// TestSubstitute_单遍不重扫_缺字段 对齐 _substitute：替换值永不重扫（LLM 正文里的
// {{...}} 不注入）、未知字段渲染 <missing:ns.field>。
// Python: message_template.py L531-545
func TestSubstitute_单遍不重扫_缺字段(t *testing.T) {
	tpl := "hello {{task.title}} {{param.x}}"
	got := substitute(tpl, map[string]map[string]string{
		"task":  {"title": "{{evil.placeholder}}"},
		"param": {"x": "1"},
	})
	if got != "hello {{evil.placeholder}} 1" {
		t.Errorf("替换值不得重扫: %q", got)
	}
	got = substitute("{{task.nofield}}", map[string]map[string]string{"task": {}})
	if got != "<missing:task.nofield>" {
		t.Errorf("未知字段应渲染 missing 标记: %q", got)
	}
}

// fakeGetter 组装 ExpandGetters 的测试桩。
func fakeGetter(tasks map[string]*database.TeamTaskBase, members map[string]*database.TeamMember) ExpandGetters {
	return ExpandGetters{
		Language: "cn",
		TaskGetter: func(_ context.Context, id string) (*database.TeamTaskBase, error) {
			return tasks[id], nil
		},
		MemberGetter: func(_ context.Context, name string) (*database.TeamMember, error) {
			return members[name], nil
		},
	}
}

// TestExpandMessage_普通消息原样通过 对齐 expand_message 非 meta 分支。
func TestExpandMessage_普通消息原样通过(t *testing.T) {
	msg := &database.TeamMessageBase{Content: "plain text"}
	out, err := ExpandMessage(context.Background(), msg, fakeGetter(nil, nil))
	if err != nil || out.Body != "plain text" || out.IsTemplate {
		t.Errorf("普通消息应原样通过且非模板: %+v err=%v", out, err)
	}
}

// TestExpandMessage_模板按当前行渲染 对齐 F_63：refs 绑定任务行、投递时渲染。
func TestExpandMessage_模板按当前行渲染(t *testing.T) {
	tasks := map[string]*database.TeamTaskBase{
		"t1": {TaskID: "t1", Title: "任务一", Content: "正文", Status: fsm.TaskStatusInProgress},
	}
	msg := &database.TeamMessageBase{
		Meta: metaJSON(t, map[string]any{
			"template": "scheduler_task_start",
			"refs":     map[string]any{"task": "t1"},
		}),
	}
	getters := fakeGetter(tasks, nil)
	getters.Loader = func(name, language string) (*PromptTemplate, error) {
		return &PromptTemplate{content: "# {{task.task_id}}\n{{task.title}}\n{{task.content}}"}, nil
	}
	out, err := ExpandMessage(context.Background(), msg, getters)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if !out.IsTemplate {
		t.Error("模板消息 IsTemplate 应为 true")
	}
	if out.Body != "# t1\n任务一\n正文" {
		t.Errorf("应按当前任务行渲染: %q", out.Body)
	}
	if !out.HasShownRevision || out.ShownTaskID != "t1" || out.ShownRevision != 0 {
		t.Errorf("展示 content 时应记录 shown revision: %+v", out)
	}
}

// TestExpandMessage_缺行降级fallback 对齐 RefUnresolved → fallback_line。
func TestExpandMessage_缺行降级fallback(t *testing.T) {
	msg := &database.TeamMessageBase{
		Meta: metaJSON(t, map[string]any{"template": "scheduler_task_start", "refs": map[string]any{"task": "gone"}}),
	}
	getters := fakeGetter(nil, nil)
	getters.Loader = func(name, language string) (*PromptTemplate, error) {
		return &PromptTemplate{content: "{{task.title}}"}, nil
	}
	out, err := ExpandMessage(context.Background(), msg, getters)
	if err != nil {
		t.Fatalf("expand 不应报错: %v", err)
	}
	if !out.IsTemplate || !strings.Contains(out.Body, "details unavailable") {
		t.Errorf("缺行应降级 fallback: %+v", out)
	}
}

// TestTaskReviewers_新旧格式 对齐 task.reviewers()：对象格式与旧字符串数组自动升级。
// Python: tools/models.py L195-232
func TestTaskReviewers_新旧格式(t *testing.T) {
	newFmt := `[{"reviewer_id":"bob","type":"verifier"},{"reviewer_id":"eve","type":"inspector"}]`
	if got := TaskReviewers(&database.TeamTaskBase{Reviewer: &newFmt}); len(got) != 2 || got[0] != "bob" {
		t.Errorf("对象格式解析: %v", got)
	}
	legacy := `["alice"]`
	if got := TaskReviewers(&database.TeamTaskBase{Reviewer: &legacy}); len(got) != 1 || got[0] != "alice" {
		t.Errorf("旧格式应自动升级: %v", got)
	}
	if got := TaskReviewers(&database.TeamTaskBase{}); len(got) != 0 {
		t.Errorf("空 reviewer 应返回空列表: %v", got)
	}
}
