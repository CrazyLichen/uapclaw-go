package memberoptimizer

import (
	"os"
	"path/filepath"
	"testing"
)

// basePath 返回指向字符串的指针（测试辅助）。
func basePath(s string) *string { return &s }

// TestEvalRefFromDict_相对路径基于basePath解析 对照 Python EvalRef.from_dict。
// Python: loader.py:38-68
func TestEvalRefFromDict_相对路径基于basePath解析(t *testing.T) {
	base := "/tmp/evalbase"
	ref := EvalRefFromDict(map[string]any{
		"eval_id":             "e1",
		"team_name":           "team-a",
		"team_skill_ref_path": "skills/team.yaml",
		"harness_refs_path":   "refs",
		"eval_dir":            "runs/e1",
		"summary_path":        "summary.yaml",
		"cases":               []any{map[string]any{"case_id": "c1"}},
	}, basePath(base))

	if ref.EvalID != "e1" || ref.TeamName != "team-a" {
		t.Fatalf("标量字段不符: %+v", ref)
	}
	if ref.TeamSkillRefPath != "skills/team.yaml" || ref.HarnessRefsPath != "refs" {
		t.Fatalf("引用路径字段不符: %+v", ref)
	}
	if ref.EvalDir != filepath.Join(base, "runs/e1") {
		t.Fatalf("eval_dir 应拼接 basePath, got %s", ref.EvalDir)
	}
	if ref.CaseResultsDir != filepath.Join(base, "results") {
		t.Fatalf("case_results_dir 默认 results 且拼 basePath, got %s", ref.CaseResultsDir)
	}
	if ref.CaseTracesDir != filepath.Join(base, "traces") {
		t.Fatalf("case_traces_dir 默认 traces 且拼 basePath, got %s", ref.CaseTracesDir)
	}
	if ref.SummaryPath == nil || *ref.SummaryPath != filepath.Join(base, "summary.yaml") {
		t.Fatalf("summary_path 应拼接 basePath, got %v", ref.SummaryPath)
	}
	if len(ref.Cases) != 1 {
		t.Fatalf("cases 应保留, got %v", ref.Cases)
	}

	// 绝对路径不拼 basePath
	ref2 := EvalRefFromDict(map[string]any{"eval_dir": "/abs/dir"}, basePath(base))
	if ref2.EvalDir != "/abs/dir" {
		t.Fatalf("绝对 eval_dir 不应拼接, got %s", ref2.EvalDir)
	}

	// summary_path 缺省为 nil；空串视为缺失
	ref3 := EvalRefFromDict(map[string]any{"summary_path": ""}, nil)
	if ref3.SummaryPath != nil {
		t.Fatalf("空 summary_path 应为 nil, got %v", ref3.SummaryPath)
	}
	// basePath 为 nil 时相对路径保持原样
	ref4 := EvalRefFromDict(map[string]any{"eval_dir": "rel"}, nil)
	if ref4.EvalDir != "rel" {
		t.Fatalf("nil basePath 相对路径保持原样, got %s", ref4.EvalDir)
	}
}

// TestLoadEvalRef_读取与错误对照 对照 Python load_eval_ref。
// Python: loader.py:147-165
func TestLoadEvalRef_读取与错误对照(t *testing.T) {
	dir := t.TempDir()

	// ① 正常读取：相对 eval_dir 基于 yaml 所在目录解析
	yamlPath := filepath.Join(dir, "eval_ref.yaml")
	content := `eval_id: e9
eval_dir: runs/e9
cases:
  - case_id: c1
`
	if err := os.WriteFile(yamlPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	ref, err := LoadEvalRef(yamlPath)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if ref.EvalID != "e9" || ref.EvalDir != filepath.Join(dir, "runs/e9") {
		t.Fatalf("字段解析不符: %+v", ref)
	}

	// ② 文件不存在 → 错误文案对齐 Python（含 resolve 后路径）
	missing := filepath.Join(dir, "nope.yaml")
	_, err = LoadEvalRef(missing)
	if err == nil || err.Error() != "eval_ref not found: "+missing {
		t.Fatalf("缺失文件错误文案不符: %v", err)
	}

	// ③ 空文件 → 空 mapping 走默认值
	emptyPath := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(emptyPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	ref2, err := LoadEvalRef(emptyPath)
	if err != nil {
		t.Fatal(err)
	}
	if ref2.EvalDir != dir {
		t.Fatalf("空文件 eval_dir 默认 . 且基于父目录, got %s", ref2.EvalDir)
	}
}

// TestResolveCandidateRoles_三级解析顺序 对照 Python resolve_candidate_roles。
// Python: loader.py:254-283
func TestResolveCandidateRoles_三级解析顺序(t *testing.T) {
	dir := t.TempDir()

	// ① harness_refs.yaml 文件路径（roles 列表形状）
	refsFile := filepath.Join(dir, "harness_refs.yaml")
	if err := os.WriteFile(refsFile, []byte(`roles:
  - role: planner
    member_name: planner-m
    harness_ref_path: ./planner_ref.yaml
    description: 计划角色
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cands := ResolveCandidateRoles(refsFile, &EvalRef{EvalDir: dir}, "")
	if len(cands) != 1 {
		t.Fatalf("roles 列表应解析 1 个候选, got %v", cands)
	}
	if cands[0].Role != "planner" || cands[0].MemberName != "planner-m" || cands[0].Description != "计划角色" {
		t.Fatalf("候选字段不符: %+v", cands[0])
	}
	// 相对 harness_ref_path 基于 refs 文件父目录 resolve
	if cands[0].HarnessRefPath != filepath.Join(dir, "planner_ref.yaml") {
		t.Fatalf("harness_ref_path 应基于 refs 父目录, got %s", cands[0].HarnessRefPath)
	}

	// ② 目录形状：子目录名成为角色
	refsDir := filepath.Join(dir, "refsdir")
	if err := os.Mkdir(refsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"beta", "alpha"} {
		if err := os.Mkdir(filepath.Join(refsDir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cands2 := ResolveCandidateRoles(refsDir, &EvalRef{EvalDir: dir}, "")
	if len(cands2) != 2 {
		t.Fatalf("目录应解析 2 个候选, got %v", cands2)
	}
	// Python sorted(iterdir)：按目录名排序
	if cands2[0].Role != "alpha" || cands2[1].Role != "beta" {
		t.Fatalf("目录名应排序, got %s,%s", cands2[0].Role, cands2[1].Role)
	}
	if cands2[0].Metadata["source"] != "directory" {
		t.Fatalf("目录候选 source 应为 directory, got %v", cands2[0].Metadata)
	}

	// ③ harness_refs dict 形状（无 roles 键）
	dictFile := filepath.Join(dir, "refsdict.yaml")
	if err := os.WriteFile(dictFile, []byte(`harness_refs:
  writer: ./w.yaml
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cands3 := ResolveCandidateRoles(dictFile, &EvalRef{EvalDir: dir}, "")
	if len(cands3) != 1 || cands3[0].Role != "writer" || cands3[0].MemberName != "writer" {
		t.Fatalf("harness_refs dict 形状解析不符: %+v", cands3)
	}
	if cands3[0].Metadata["source"] != "harness_refs" {
		t.Fatalf("dict 候选 source 应为 harness_refs, got %v", cands3[0].Metadata)
	}

	// ④ 裸 mapping 形状（跳过 version/source 键）
	bareFile := filepath.Join(dir, "bare.yaml")
	if err := os.WriteFile(bareFile, []byte(`version: "1"
source: somewhere
coder: ./c.yaml
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cands4 := ResolveCandidateRoles(bareFile, &EvalRef{EvalDir: dir}, "")
	if len(cands4) != 1 || cands4[0].Role != "coder" {
		t.Fatalf("裸 mapping 应跳过 version/source: %+v", cands4)
	}

	// ⑤ 一级失败回落 eval_ref.harness_refs_path（二级）
	evalRefs := filepath.Join(dir, "evalrefs.yaml")
	if err := os.WriteFile(evalRefs, []byte(`roles:
  - role: leader
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cands5 := ResolveCandidateRoles("", &EvalRef{HarnessRefsPath: evalRefs, EvalDir: dir}, "")
	if len(cands5) != 1 || cands5[0].Role != "leader" {
		t.Fatalf("应回落 eval_ref.harness_refs_path: %+v", cands5)
	}

	// ⑥ 路径存在但解析为空候选 → team_skill 兜底（_supplement_from_team_skill）
	//    （Python：refs_path 缺失时直接返回空，不触发兜底）
	refsEmptyDir := filepath.Join(dir, "refs_empty")
	if err := os.Mkdir(refsEmptyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillFile := filepath.Join(dir, "team_skill.yaml")
	if err := os.WriteFile(skillFile, []byte(`leader:
  member_name: captain
predefined_members:
  - member_name: worker1
    harness_ref_path: ./w1.yaml
    description: 工人
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cands6 := ResolveCandidateRoles(refsEmptyDir, &EvalRef{EvalDir: dir}, skillFile)
	if len(cands6) != 2 {
		t.Fatalf("team_skill 兜底应解析 2 个候选, got %v", cands6)
	}
	if cands6[0].Role != "captain" || cands6[0].Description != "Team leader" || cands6[0].HarnessRefPath != "" {
		t.Fatalf("leader 候选不符: %+v", cands6[0])
	}
	if cands6[1].Role != "worker1" || cands6[1].HarnessRefPath != "./w1.yaml" {
		t.Fatalf("member 候选不符: %+v", cands6[1])
	}

	// ⑦ 一级有候选时不走 team_skill 兜底
	cands7 := ResolveCandidateRoles(refsFile, &EvalRef{EvalDir: dir}, skillFile)
	if len(cands7) != 1 || cands7[0].Role != "planner" {
		t.Fatalf("已有候选时不应兜底: %+v", cands7)
	}

	// ⑧ 两级失败且无 team_skill → 空列表
	if got := ResolveCandidateRoles("", &EvalRef{EvalDir: dir}, ""); len(got) != 0 {
		t.Fatalf("应返回空列表, got %v", got)
	}
}

// TestResolveHarnessRefPath_非字符串与空串 对照 Python _resolve_harness_ref_path。
// Python: loader.py:286-293
func TestResolveHarnessRefPath_非字符串与空串(t *testing.T) {
	if got := resolveHarnessRefPath(nil, "/tmp"); got != "" {
		t.Fatalf("nil ref 应返回空串, got %q", got)
	}
	if got := resolveHarnessRefPath("  ", "/tmp"); got != "" {
		t.Fatalf("空白 ref 应返回空串, got %q", got)
	}
	if got := resolveHarnessRefPath("/abs/x.yaml", "/tmp"); got != "/abs/x.yaml" {
		t.Fatalf("绝对路径应原样, got %q", got)
	}
}
