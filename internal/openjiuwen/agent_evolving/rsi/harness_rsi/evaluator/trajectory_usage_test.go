package evaluator

import (
	"os"
	"path/filepath"
	"testing"
)

// toolStep 构造裸 Tool 步（不含 steps 包装）。
func toolStep(name, err any, callResult any, callArgs any) map[string]any {
	step := map[string]any{
		"kind": "tool",
		"detail": map[string]any{
			"tool_name":   name,
			"call_args":   callArgs,
			"call_result": callResult,
		},
	}
	if err != nil {
		step["error"] = err
	}
	return step
}

func TestCollectSuccessfulToolNames_只收成功步(t *testing.T) {
	traj := map[string]any{"steps": []any{
		toolStep("read_file", nil, map[string]any{"success": true}, nil),
		toolStep("bash", "boom", nil, nil),
		toolStep("write_file", nil, map[string]any{"success": false}, nil),
		toolStep("grep", nil, "error: bad", nil),
		toolStep("", nil, "ok", nil),
		"not-a-dict",
	}}
	names := map[string]struct{}{}
	CollectSuccessfulToolNames(traj, names)
	if len(names) != 1 {
		t.Fatalf("只应收集 read_file: %v", names)
	}
	if _, ok := names["read_file"]; !ok {
		t.Fatalf("read_file 缺失: %v", names)
	}
}

func TestCollectSuccessfulSkillNames_参数各键回退(t *testing.T) {
	// call_args JSON 字符串形态
	traj := map[string]any{"steps": []any{
		toolStep("skill_tool", nil, "ok", `{"skill_name":"alpha"}`),
		// arguments dict 形态
		map[string]any{"kind": "tool", "detail": map[string]any{
			"tool_name": "skill", "call_result": "ok",
			"arguments": map[string]any{"skill_name": "beta"},
		}},
		// 非 skill 工具应跳过
		toolStep("bash", nil, "ok", `{"skill_name":"gamma"}`),
	}}
	names := map[string]struct{}{}
	CollectSuccessfulSkillNames(traj, names)
	if len(names) != 2 {
		t.Fatalf("应收集 alpha/beta: %v", names)
	}
}

func TestCollectJSONLSuccessfulUsage_流式(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "trace.jsonl")
	content := "{\"steps\":[{\"kind\":\"tool\",\"detail\":{\"tool_name\":\"read_file\",\"call_result\":\"ok\"}}]}\n" +
		"{\"steps\":[{\"kind\":\"tool\",\"detail\":{\"tool_name\":\"skill\",\"call_result\":\"ok\",\"call_args\":\"{\\\"skill_name\\\":\\\"s1\\\"}\"}}]}\n" +
		"not-json\n\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatalf("写文件: %v", err)
	}
	tools := map[string]struct{}{}
	skills := map[string]struct{}{}
	CollectJSONLSuccessfulUsage(file, tools, skills)
	if _, ok := tools["read_file"]; !ok {
		t.Fatalf("read_file 应收集: %v", tools)
	}
	if _, ok := skills["s1"]; !ok {
		t.Fatalf("s1 应收集: %v", skills)
	}
	// 缺失文件吞错
	CollectJSONLSuccessfulUsage(filepath.Join(dir, "missing.jsonl"), tools, skills)
}

func TestCollectPreEditSuccessfulUsage_首编辑步号(t *testing.T) {
	traj := map[string]any{"steps": []any{
		toolStep("read_file", nil, "ok", nil),                 // 0
		toolStep("write_file", nil, "ok", nil),                // 1（编辑）
		toolStep("skill", nil, "ok", `{"skill_name":"late"}`), // 2
	}}
	tools := map[string]struct{}{}
	skills := map[string]struct{}{}
	first := CollectPreEditSuccessfulUsage(traj, tools, skills, false)
	if first == nil || *first != 1 {
		t.Fatalf("首编辑步号应为 1: %v", first)
	}
	// 编辑前用量：read_file 收、late 不收
	if _, ok := tools["read_file"]; !ok {
		t.Fatalf("编辑前 tool 应收集: %v", tools)
	}
	if _, ok := tools["skill"]; ok {
		t.Fatalf("编辑后 tool 不应收集: %v", tools)
	}
	if _, ok := skills["late"]; ok {
		t.Fatalf("编辑后 skill 不应收集: %v", skills)
	}
	// 无编辑 → nil
	none := CollectPreEditSuccessfulUsage(
		map[string]any{"steps": []any{toolStep("read_file", nil, "ok", nil)}},
		map[string]struct{}{}, map[string]struct{}{}, false)
	if none != nil {
		t.Fatalf("无编辑应 nil: %v", none)
	}
}

func TestCollectPreEditSuccessfulUsage_allowLaterEdit(t *testing.T) {
	traj := map[string]any{"steps": []any{
		toolStep("read_file", nil, "ok", nil),                  // 0
		toolStep("skill", nil, "ok", `{"skill_name":"early"}`), // 1
		toolStep("apply_patch", nil, "ok", nil),                // 2（编辑）
	}}
	skills := map[string]struct{}{}
	first := CollectPreEditSuccessfulUsage(traj, nil, skills, true)
	if first == nil || *first != 2 {
		t.Fatalf("首编辑步号应为 2: %v", first)
	}
	if _, ok := skills["early"]; !ok {
		t.Fatalf("allowLaterEdit 应收编辑前 skill: %v", skills)
	}
}

func TestCollectPreInvestigationSuccessfulSkillUsage(t *testing.T) {
	traj := map[string]any{"steps": []any{
		toolStep("skill", nil, "ok", `{"skill_name":"pre"}`),  // skill 先于取证
		toolStep("bash", nil, "ok", `{"command":"ls"}`),       // 取证开始 step 1
		toolStep("skill", nil, "ok", `{"skill_name":"post"}`), // 取证后不算
	}}
	skills := map[string]struct{}{}
	first := CollectPreInvestigationSuccessfulSkillUsage(traj, skills)
	if first == nil || *first != 1 {
		t.Fatalf("取证开始步号应为 1: %v", first)
	}
	if _, ok := skills["pre"]; !ok {
		t.Fatalf("取证前 skill 应收集: %v", skills)
	}
	if _, ok := skills["post"]; ok {
		t.Fatalf("取证后 skill 不应收集: %v", skills)
	}
}

func TestToolStepSucceeded_分支(t *testing.T) {
	step := toolStep("bash", nil, nil, nil)
	detail := step["detail"].(map[string]any)
	detail["call_result"] = nil
	if toolStepSucceeded(step) {
		t.Fatalf("call_result nil 应失败")
	}
	// 状态 failed
	detail["call_result"] = map[string]any{"status": "failed"}
	if toolStepSucceeded(step) {
		t.Fatalf("status failed 应失败")
	}
	// 字符串 [reliability] 前缀
	detail["call_result"] = "[reliability] steer"
	if toolStepSucceeded(step) {
		t.Fatalf("[reliability] 前缀应失败")
	}
	// 非 tool kind
	nonTool := map[string]any{"kind": "message", "detail": map[string]any{
		"tool_name": "bash", "call_result": "ok"}}
	if toolStepSucceeded(nonTool) {
		t.Fatalf("非 tool kind 应失败")
	}
}

func TestIsPersistentEditStep_命令模式(t *testing.T) {
	cases := []struct {
		command string
		isEdit  bool
	}{
		{"apply_patch x", true},
		{"git apply p.diff", true},
		{"sed -i s/a/b f", true},
		{"cp a b", true},
		{"echo hi > out.txt", true},
		{"echo hi > /dev/null", false},
		{"ls -la", false},
		{"echo hello", false},
	}
	for _, tc := range cases {
		step := toolStep("bash", nil, "ok", `{"command":"`+tc.command+`"}`)
		rawStep := step
		if got := isPersistentEditStep(rawStep); got != tc.isEdit {
			t.Fatalf("command %q 期望 %v got %v", tc.command, tc.isEdit, got)
		}
	}
}

func TestIsPersistentEditStep_工具名集合(t *testing.T) {
	for _, name := range []string{"apply_patch", "str_replace_editor", "write_file"} {
		step := toolStep(name, nil, "ok", nil)
		rawStep := step
		if !isPersistentEditStep(rawStep) {
			t.Fatalf("%s 应视为编辑", name)
		}
	}
}

func TestCommandFromArgs_各键回退(t *testing.T) {
	if got := commandFromArgs(map[string]any{
		"call_args": map[string]any{"cmd": "ls"}}); got != "ls" {
		t.Fatalf("cmd 键: %q", got)
	}
	if got := commandFromArgs(map[string]any{
		"tool_input": `{"code":"print(1)"}`}); got != "print(1)" {
		t.Fatalf("tool_input JSON 解码: %q", got)
	}
	if got := commandFromArgs(map[string]any{
		"input": "raw command"}); got != "raw command" {
		t.Fatalf("input 字符串直通: %q", got)
	}
	if got := commandFromArgs(map[string]any{}); got != "" {
		t.Fatalf("无参数应空: %q", got)
	}
}

func TestCanonicalToolName(t *testing.T) {
	if got := canonicalToolName("Skill-Tool"); got != "skill" {
		t.Fatalf("规范名: %q", got)
	}
	if got := canonicalToolName("  BASH "); got != "bash" {
		t.Fatalf("空白与小写: %q", got)
	}
}

func TestOrderedSteps_messages形态(t *testing.T) {
	traj := map[string]any{"messages": []any{
		map[string]any{"tool_calls": []any{
			map[string]any{"name": "bash", "input": "ls", "output": "ok", "error": ""},
		}},
	}}
	steps := orderedSteps(traj)
	if len(steps) != 1 {
		t.Fatalf("应合成 1 步: %d", len(steps))
	}
	if steps[0]["kind"] != "tool" {
		t.Fatalf("合成步 kind=tool: %v", steps[0])
	}
	detail := steps[0]["detail"].(map[string]any)
	if detail["tool_name"] != "bash" {
		t.Fatalf("tool_name 透传: %v", detail)
	}
	if detail["error"] != nil {
		t.Fatalf("空 error 应转 nil: %v", detail["error"])
	}
}

func TestOrderedSteps_嵌套递归(t *testing.T) {
	traj := map[string]any{
		"nested": map[string]any{"steps": []any{
			toolStep("read_file", nil, "ok", nil),
		}},
	}
	steps := orderedSteps(traj)
	if len(steps) != 1 {
		t.Fatalf("嵌套应递归: %d", len(steps))
	}
}

func TestCollectJSONLPreEditSuccessfulUsage(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "member.jsonl")
	lines := "{\"steps\":[{\"kind\":\"tool\",\"detail\":{\"tool_name\":\"read_file\",\"call_result\":\"ok\"}}]}\n" +
		"{\"steps\":[{\"kind\":\"tool\",\"detail\":{\"tool_name\":\"write_file\",\"call_result\":\"ok\"}}]}\n"
	if err := os.WriteFile(file, []byte(lines), 0o644); err != nil {
		t.Fatalf("写文件: %v", err)
	}
	tools := map[string]struct{}{}
	first := CollectJSONLPreEditSuccessfulUsage(file, tools, nil, false)
	if first == nil || *first != 1 {
		t.Fatalf("跨文件首编辑步号应 1: %v", first)
	}
	if _, ok := tools["read_file"]; !ok {
		t.Fatalf("编辑前 tool 应收集: %v", tools)
	}
	// 缺失文件 → nil
	if got := CollectJSONLPreEditSuccessfulUsage(
		filepath.Join(dir, "missing.jsonl"), tools, nil, false); got != nil {
		t.Fatalf("缺失文件应 nil: %v", got)
	}
}

func TestCollectJSONLPreInvestigationSuccessfulSkillUsage(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "trace.jsonl")
	line := "{\"steps\":[{\"kind\":\"tool\",\"detail\":{\"tool_name\":\"skill\",\"call_result\":\"ok\",\"call_args\":\"{\\\"skill_name\\\":\\\"s\\\"}\"}}]}\n"
	if err := os.WriteFile(file, []byte(line), 0o644); err != nil {
		t.Fatalf("写文件: %v", err)
	}
	skills := map[string]struct{}{}
	first := CollectJSONLPreInvestigationSuccessfulSkillUsage(file, skills)
	if first != nil {
		t.Fatalf("无取证调用应 nil: %v", first)
	}
	if _, ok := skills["s"]; !ok {
		t.Fatalf("skill 应收集: %v", skills)
	}
}
