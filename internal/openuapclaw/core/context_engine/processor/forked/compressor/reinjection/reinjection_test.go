package reinjection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// TestStateReinjectorRegister_同名覆盖 测试注册与同名覆盖保持插入序
// Python: reinjector.py:37
func TestStateReinjectorRegister_同名覆盖(t *testing.T) {
	r := NewStateReinjector()
	r.Register("a", "A", func(*ReinjectContext) any { return "x" })
	r.Register("b", "B", func(*ReinjectContext) any { return "y" })
	r.Register("a", "A2", func(*ReinjectContext) any { return "z" })
	specs := r.IterBuilders()
	require.Len(t, specs, 2)
	assert.Equal(t, "a", specs[0].Name)
	assert.Equal(t, "A2", specs[0].Label)
}

// TestBuildMessages_多消息形态 测试 string 与 []BaseMessage 两种返回形态
// Python: reinjector.py:51
func TestBuildMessages_多消息形态(t *testing.T) {
	r := NewStateReinjector()
	r.Register("s", "S", func(*ReinjectContext) any { return "text-content" })
	r.Register("l", "L", func(*ReinjectContext) any {
		return []llm_schema.BaseMessage{llm_schema.NewUserMessage("m1"), llm_schema.NewUserMessage("m2")}
	})
	r.Register("empty", "E", func(*ReinjectContext) any { return "" })
	ctx := &ReinjectContext{StateMarker: "[STATE_REINJECTION]", Truncate: func(s string) string { return s }}
	msgs := r.BuildMessages(ctx, nil)
	// s → 1 条包装消息；l → 2 条原样；empty → 0 条
	require.Len(t, msgs, 3)
	assert.True(t, strings.HasPrefix(msgs[0].GetContent().String(), "[STATE_REINJECTION]\n[S]\ntext-content"))
	assert.Equal(t, "m1", msgs[1].GetContent().String())
	// only 过滤
	msgs = r.BuildMessages(ctx, []string{"l"})
	require.Len(t, msgs, 2)
}

// TestBuildSingleReinjectedStateMessage_合并与剥离 测试单条 recovered_context 消息
// Python: reinjector.py:66 + :105
func TestBuildSingleReinjectedStateMessage_合并与剥离(t *testing.T) {
	r := NewStateReinjector()
	// 返回带状态头的文本 → stripStateHeader 应剥掉
	r.Register("plain", "P", func(*ReinjectContext) any {
		return "[STATE_REINJECTION]\n[P]\nbody-text"
	})
	r.Register("list", "L", func(*ReinjectContext) any {
		return []llm_schema.BaseMessage{llm_schema.NewUserMessage("part-a"), llm_schema.NewUserMessage("part-b")}
	})
	r.Register("blank", "B", func(*ReinjectContext) any { return "" })
	ctx := &ReinjectContext{StateMarker: "[STATE_REINJECTION]", Truncate: func(s string) string { return s }}
	msg := BuildSingleReinjectedStateMessage(ctx, r.IterBuilders(), nil)
	require.NotNil(t, msg)
	content := msg.GetContent().String()
	assert.Contains(t, content, "<recovered_context>")
	assert.Contains(t, content, `<section name="plain">`+"\nbody-text\n</section>")
	assert.Contains(t, content, "part-a\n\npart-b")
	assert.NotContains(t, content, `<section name="blank">`)
	// only 过滤后单 section
	msg = BuildSingleReinjectedStateMessage(ctx, r.IterBuilders(), []string{"list"})
	assert.NotContains(t, msg.GetContent().String(), "body-text")
	// 全空 → nil
	r2 := NewStateReinjector()
	r2.Register("none", "N", func(*ReinjectContext) any { return "" })
	assert.Nil(t, BuildSingleReinjectedStateMessage(ctx, r2.IterBuilders(), nil))
}

// TestStripStateHeader 测试头剥离三态
// Python: reinjector.py:105
func TestStripStateHeader(t *testing.T) {
	assert.Equal(t, "body", stripStateHeader("[M]\n[L]\nbody", "[M]", "L"))
	assert.Equal(t, "rest", stripStateHeader("[M]\n[X]\nrest", "[M]", "L")) // 泛化前缀
	assert.Equal(t, "no-marker", stripStateHeader("no-marker", "[M]", "L"))
}

// TestBuildFileReinjectedContent 测试文件回注选择/预算/去重
// Python: builders.py:93
func TestBuildFileReinjectedContent(t *testing.T) {
	dir := t.TempDir()
	readTarget := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(readTarget, []byte("file-content-body"), 0o644))

	// assistant 发起 read_file(notes.txt)，tool 返回 JSON 结果
	assistant := llm_schema.NewAssistantMessage("")
	assistant.ToolCalls = []*llm_schema.ToolCall{{ID: "c1", Name: "read_file", Arguments: `{"file_path":"` + readTarget + `"}`}}
	toolMsg := llm_schema.NewToolMessage("c1", `{"file_path":"`+readTarget+`","content":"file-content-body","line_count":1}`)

	// 保留消息中已含该 toolMsg → collect_read_file_paths 命中 → 排除
	ctxKept := &ReinjectContext{
		SourceMessages: []llm_schema.BaseMessage{assistant, toolMsg},
		MessagesToKeep: []llm_schema.BaseMessage{toolMsg},
		Config:         map[string]any{"reinject_read_file_max_files": 5},
		StateMarker:    "[M]",
		Truncate:       func(s string) string { return s },
	}
	assert.Equal(t, "", BuildFileReinjectedContent(ctxKept))

	// 未保留 → 回注
	ctxKept.MessagesToKeep = nil
	out := BuildFileReinjectedContent(ctxKept)
	text, ok := out.(string)
	require.True(t, ok)
	assert.Contains(t, text, "Recently read file: "+readTarget)
	assert.Contains(t, text, "file-content-body")

	// max_files=0 → 空
	ctxKept.Config = map[string]any{"reinject_read_file_max_files": 0}
	assert.Equal(t, "", BuildFileReinjectedContent(ctxKept))
}

// TestIsExcludedFromReinject 测试路径排除
// Python: builders.py:593
func TestIsExcludedFromReinject(t *testing.T) {
	assert.True(t, isExcludedFromReinject("/repo/.git/x.js"))
	assert.True(t, isExcludedFromReinject("/p/node_modules/a.js"))
	assert.True(t, isExcludedFromReinject("/p/__pycache__/a.py"))
	assert.False(t, isExcludedFromReinject("/p/src/a.go"))
}

// TestBuildTaskStatusAndTodos 测试任务状态与 todo 回注
// Python: builders.py:132/:348
func TestBuildTaskStatusAndTodos(t *testing.T) {
	identity := func(s string) string { return s }
	// 后台任务段
	ctx := &ReinjectContext{
		SessionState: map[string]any{
			"background_tasks": []any{
				map[string]any{"description": "scan", "task_id": "t1", "status": "running"},
				map[string]any{"description": "build", "task_id": "t2", "status": "completed"},
			},
		},
		Truncate: identity,
	}
	out := BuildTaskStatusReinjectedContent(ctx).(string)
	assert.Contains(t, out, `Background agent "scan" (t1) is still running`)
	assert.Contains(t, out, `Background agent "build" (t2) status=completed`)

	// 团队段：静态看板 + 恢复调用
	assistant := llm_schema.NewAssistantMessage("")
	assistant.ToolCalls = []*llm_schema.ToolCall{{ID: "m1", Name: "send_message", Arguments: `{"to":"alice","summary":"hello"}`}}
	toolMsg := llm_schema.NewToolMessage("m1", `{"ok":true}`)
	teamCtx := &ReinjectContext{
		SessionState: map[string]any{
			"team_task_status": map[string]any{
				"team_name": "alpha",
				"members":   []any{map[string]any{"member_name": "alice", "role": "dev", "status": "idle"}},
				"open_tasks": []any{map[string]any{
					"task_id": "7", "status": "open", "title": "do it", "assignee": "alice",
				}},
			},
		},
		SourceMessages: []llm_schema.BaseMessage{assistant, toolMsg},
		Truncate:       identity,
	}
	out = BuildTaskStatusReinjectedContent(teamCtx).(string)
	assert.Contains(t, out, "Team collaboration state:")
	assert.Contains(t, out, "- Team: alpha")
	assert.Contains(t, out, "Current members:")
	assert.Contains(t, out, "Open tasks:")
	assert.Contains(t, out, "Recovered collaboration history:")
	assert.Contains(t, out, "Sent message to alice: hello")
	// 空看板且无调用 → 整段丢弃
	emptyTeam := &ReinjectContext{
		SessionState: map[string]any{"team_task_status": map[string]any{"team_name": "beta"}},
		Truncate:     identity,
	}
	assert.Equal(t, "", BuildTaskStatusReinjectedContent(emptyTeam))
}

// TestBuildTodoReinjectedContent 测试活跃 todo 渲染与状态计数
// Python: builders.py:348
func TestBuildTodoReinjectedContent(t *testing.T) {
	dir := t.TempDir()
	sessionID := "sess1"
	todoDir := filepath.Join(dir, sessionID)
	require.NoError(t, os.MkdirAll(todoDir, 0o755))
	todoJSON := `[{"id":"1","status":"pending","content":"task one"},{"id":"2","status":"completed","content":"done"},{"id":"3","status":"in_progress","content":"task three","activeForm":"doing"}]`
	require.NoError(t, os.WriteFile(filepath.Join(todoDir, "todo.json"), []byte(todoJSON), 0o644))

	ctx := &ReinjectContext{
		WorkspaceRoot: dir,
		Context:       &fakeReinjectContext{sessionID: sessionID, workspaceDir: dir},
		Truncate:      func(s string) string { return s },
	}
	out := BuildTodoReinjectedContent(ctx).(string)
	assert.Contains(t, out, "- [pending] 1: task one")
	assert.Contains(t, out, "- [in_progress] 3: task three (doing)")
	assert.Contains(t, out, "Status counts: completed: 1, in_progress: 1, pending: 1")
	// 无文件 → 空
	ctx.WorkspaceRoot = t.TempDir()
	assert.Equal(t, "", BuildTodoReinjectedContent(ctx))
}

// fakeReinjectContext 最小上下文桩
type fakeReinjectContext struct {
	sessionID    string
	contextID    string
	workspaceDir string
}

func (f *fakeReinjectContext) SessionID() string    { return f.sessionID }
func (f *fakeReinjectContext) ContextID() string    { return f.contextID }
func (f *fakeReinjectContext) WorkspaceDir() string { return f.workspaceDir }

// TestBuildPlanModeAndPlan 测试 plan 模式状态与 plan 文件回注
// Python: builders.py:29/:382
func TestBuildPlanModeAndPlan(t *testing.T) {
	dir := t.TempDir()
	plansDir := filepath.Join(dir, ".plans")
	require.NoError(t, os.MkdirAll(plansDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(plansDir, "my-plan.md"), []byte("# plan body"), 0o644))

	identity := func(s string) string { return s }
	state := map[string]any{"plan_mode": map[string]any{"mode": "plan", "plan_slug": "my-plan"}}
	ctx := &ReinjectContext{SessionState: state, WorkspaceRoot: dir, Truncate: identity}

	// plan 文件内容
	out := BuildPlanReinjectedContent(ctx).(string)
	assert.Contains(t, out, "Current plan file: ")
	assert.Contains(t, out, "# plan body")

	// plan 模式约束
	modeOut := BuildPlanModeReinjectedContent(ctx).(string)
	assert.Contains(t, modeOut, "Active mode: plan.")
	assert.Contains(t, modeOut, "Plan-mode constraints:")

	// normal 且无 slug → 空
	normal := map[string]any{"plan_mode": map[string]any{"mode": "normal"}}
	assert.Equal(t, "", BuildPlanModeReinjectedContent(&ReinjectContext{SessionState: normal, Truncate: identity}))
	// 嵌套 deepagent.plan_mode 提取
	nested := map[string]any{"deepagent": map[string]any{"plan_mode": map[string]any{"mode": "plan"}}}
	assert.Contains(t, BuildPlanModeReinjectedContent(&ReinjectContext{SessionState: nested, Truncate: identity}).(string), "Active mode: plan.")
}

// TestDefaultRegistry_SevenBuilders 测试默认注册表七件套
func TestDefaultRegistry_SevenBuilders(t *testing.T) {
	specs := DefaultStateReinjector().IterBuilders()
	names := make([]string, len(specs))
	for i, spec := range specs {
		names[i] = spec.Name
	}
	assert.Equal(t, []string{"plan", "skills", "files", "task_status", "tool_result_hint", "todos", "plan_mode"}, names)
}

// TestBuildSkillReinjectedContent 测试技能轮选取与去重
// Python: builders.py:67
func TestBuildSkillReinjectedContent(t *testing.T) {
	readSkill := llm_schema.NewAssistantMessage("")
	readSkill.ToolCalls = []*llm_schema.ToolCall{{ID: "s1", Name: "read_file", Arguments: `{"file_path":"/skills/foo/SKILL.md"}`}}
	skillResult := llm_schema.NewToolMessage("s1", `{"content":"skill body","file_path":"/skills/foo/SKILL.md"}`)
	// 轮 1（read skill）；轮 2（其他）；用 SystemMessage 分隔构造轮边界不可行，
	// 直接用 GroupCompletedAPIRounds 的 API 轮语义：User 触发轮次
	round1 := []llm_schema.BaseMessage{llm_schema.NewUserMessage("go"), readSkill, skillResult}
	round2 := []llm_schema.BaseMessage{llm_schema.NewUserMessage("more"), llm_schema.NewUserMessage("plain")}
	source := append(append([]llm_schema.BaseMessage{}, round1...), round2...)

	ctx := &ReinjectContext{
		SourceMessages: source,
		Config:         map[string]any{"reinject_recent_skills": 3},
		StateMarker:    "[M]",
		Truncate:       func(s string) string { return s },
	}
	out := BuildSkillReinjectedContent(ctx)
	msgs, ok := out.([]llm_schema.BaseMessage)
	require.True(t, ok)
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0].GetContent().String(), "skill body")
	// max_skills=0 → 空
	ctx.Config = map[string]any{"reinject_recent_skills": 0}
	assert.Empty(t, BuildSkillReinjectedContent(ctx))
}

// TestParseReadFileResult 测试快照解析（dict 结果 / 纯文本 / 部分）
// Python: builders.py:562
func TestParseReadFileResult(t *testing.T) {
	tc := &llm_schema.ToolCall{ID: "1", Name: "read_file", Arguments: `{"file_path":"/a.txt","offset":10}`}
	snap := parseReadFileResult(tc, `{"file_path":"/a.txt","content":"body","line_count":5}`)
	require.NotNil(t, snap)
	assert.Equal(t, "/a.txt", snap.FilePath)
	assert.Equal(t, "body", snap.Content)
	require.NotNil(t, snap.LineCount)
	assert.Equal(t, 5, *snap.LineCount)
	assert.True(t, snap.Partial)

	// 纯文本结果（非 JSON）→ content=result_text，file_path 从 args 正则兜底
	tc2 := &llm_schema.ToolCall{ID: "2", Name: "read_file", Arguments: `{"file_path":"/b.txt"}`}
	snap2 := parseReadFileResult(tc2, "plain-body")
	require.NotNil(t, snap2)
	assert.Equal(t, "/b.txt", snap2.FilePath)
	assert.Equal(t, "plain-body", snap2.Content)
	assert.False(t, snap2.Partial)
}

// TestDescribeTeamToolAction 测试团队工具动作描述
// Python: builders.py:294
func TestDescribeTeamToolAction(t *testing.T) {
	assert.Contains(t, describeTeamToolAction("send_message", map[string]any{"to": []any{"a", "b"}, "summary": "hi"}), "Sent message to a, b: hi")
	assert.Equal(t, "Broadcast message to the team: s", describeTeamToolAction("send_message", map[string]any{"to": "*", "summary": "s"}))
	assert.Equal(t, "Viewed task #9", describeTeamToolAction("view_task", map[string]any{"action": "get", "task_id": "9"}))
	assert.Equal(t, "Listed claimable tasks", describeTeamToolAction("view_task", map[string]any{"action": "claimable"}))
	assert.Equal(t, "Claimed task #3", describeTeamToolAction("claim_task", map[string]any{"task_id": "3", "status": "claimed"}))
	assert.Equal(t, "Marked task #3 as completed", describeTeamToolAction("claim_task", map[string]any{"task_id": "3", "status": "completed"}))
	assert.Contains(t, describeTeamToolAction("member_complete_task", map[string]any{"task_id": "5", "note": "ok"}), "Completed own task #5: ok")
	assert.Equal(t, "Called other", describeTeamToolAction("other", map[string]any{}))
}
