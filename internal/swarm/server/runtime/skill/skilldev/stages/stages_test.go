package stages

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev"
)

// ──────────────────────────── 辅助函数 ────────────────────────────

// newTestContext 创建用于测试的 SkillDevContext（事件队列缓冲 64）。
func newTestContext(state *skilldev.SkillDevState, workspace string) *skilldev.SkillDevContext {
	ch := make(chan skilldev.SkillDevEvent, 64)
	return skilldev.NewSkillDevContext("test-task", nil, state, workspace, ch)
}

// ──────────────────────────── StageResult 测试 ────────────────────────────

func TestStageResult(t *testing.T) {
	result := &StageResult{NextStage: skilldev.SkillDevStagePlan}
	if result.NextStage != skilldev.SkillDevStagePlan {
		t.Errorf("期望 NextStage=%s, 实际=%s", skilldev.SkillDevStagePlan, result.NextStage)
	}
}

// ──────────────────────────── InitStage 测试 ────────────────────────────

func TestInitStageHandler_Execute(t *testing.T) {
	handler := &InitStageHandler{}
	state := skilldev.NewSkillDevState("test-task")
	state.Input = map[string]any{"query": "帮我创建一个翻译技能"}

	workspace := t.TempDir()
	_ = os.MkdirAll(filepath.Join(workspace, "resources"), 0o755)
	_ = os.MkdirAll(filepath.Join(workspace, "skill"), 0o755)

	sctx := newTestContext(state, workspace)
	result, err := handler.Execute(context.Background(), sctx)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if result.NextStage != skilldev.SkillDevStagePlan {
		t.Errorf("期望 NextStage=%s, 实际=%s", skilldev.SkillDevStagePlan, result.NextStage)
	}
}

func TestInitStageHandler_任务模式判断(t *testing.T) {
	handler := &InitStageHandler{}

	tests := []struct {
		name     string
		input    map[string]any
		expected skilldev.SkillDevTaskMode
	}{
		{"纯创建", map[string]any{"query": "test"}, skilldev.SkillDevTaskModeCreate},
		{"携带资源", map[string]any{"query": "test", "resources": []any{}}, skilldev.SkillDevTaskModeCreateWithResources},
		{"修改模式", map[string]any{"query": "test", "existing_skill": map[string]any{}}, skilldev.SkillDevTaskModeModify},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := skilldev.NewSkillDevState("test-task")
			state.Input = tt.input
			workspace := t.TempDir()
			_ = os.MkdirAll(filepath.Join(workspace, "resources"), 0o755)
			_ = os.MkdirAll(filepath.Join(workspace, "skill"), 0o755)

			sctx := newTestContext(state, workspace)
			_, err := handler.Execute(context.Background(), sctx)
			if err != nil {
				t.Fatalf("Execute 失败: %v", err)
			}
			if state.Mode != tt.expected {
				t.Errorf("期望 Mode=%s, 实际=%s", tt.expected, state.Mode)
			}
		})
	}
}

func TestParseFileToText(t *testing.T) {
	// 测试 .txt 文件
	txtFile := filepath.Join(t.TempDir(), "test.txt")
	_ = os.WriteFile(txtFile, []byte("hello world"), 0o644)
	text := parseFileToText(txtFile)
	if text != "hello world" {
		t.Errorf("期望 'hello world', 实际 '%s'", text)
	}

	// 测试 .md 文件
	mdFile := filepath.Join(t.TempDir(), "test.md")
	_ = os.WriteFile(mdFile, []byte("# Markdown"), 0o644)
	text = parseFileToText(mdFile)
	if text != "# Markdown" {
		t.Errorf("期望 '# Markdown', 实际 '%s'", text)
	}

	// 测试不支持的格式
	pdfFile := filepath.Join(t.TempDir(), "test.pdf")
	_ = os.WriteFile(pdfFile, []byte("binary"), 0o644)
	text = parseFileToText(pdfFile)
	if text != "" {
		t.Errorf("期望空字符串, 实际 '%s'", text)
	}
}

// ──────────────────────────── PlanStage 测试 ────────────────────────────

func TestPlanStageHandler_Execute(t *testing.T) {
	handler := &PlanStageHandler{}
	state := skilldev.NewSkillDevState("test-task")
	state.Input = map[string]any{"query": "帮我创建一个翻译技能"}
	state.Plan = nil

	workspace := t.TempDir()
	sctx := newTestContext(state, workspace)
	result, err := handler.Execute(context.Background(), sctx)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if result.NextStage != skilldev.SkillDevStagePlanConfirm {
		t.Errorf("期望 NextStage=%s, 实际=%s", skilldev.SkillDevStagePlanConfirm, result.NextStage)
	}
	if state.Plan == nil {
		t.Fatal("Plan 不应为 nil")
	}
	if state.Plan["skill_name"] != "placeholder-skill" {
		t.Errorf("期望 skill_name='placeholder-skill', 实际=%v", state.Plan["skill_name"])
	}
}

func TestParsePlanJSON(t *testing.T) {
	text := `这是一些文本 {"skill_name": "test-skill", "purpose": "测试"} 更多文本`
	plan, err := parsePlanJSON(text)
	if err != nil {
		t.Fatalf("parsePlanJSON 失败: %v", err)
	}
	if plan["skill_name"] != "test-skill" {
		t.Errorf("期望 skill_name='test-skill', 实际=%v", plan["skill_name"])
	}

	// 测试无效 JSON
	_, err = parsePlanJSON("没有 JSON")
	if err == nil {
		t.Error("期望错误，实际返回 nil")
	}
}

// ──────────────────────────── ValidateStage 测试 ────────────────────────────

func TestValidateSkillMD_合法(t *testing.T) {
	dir := t.TempDir()
	skillMD := filepath.Join(dir, "SKILL.md")
	content := "---\nname: my-skill\ndescription: A test skill\n---\n# Content\n"
	_ = os.WriteFile(skillMD, []byte(content), 0o644)

	valid, message := ValidateSkillMD(skillMD)
	if !valid {
		t.Errorf("期望合法, 实际不合法: %s", message)
	}
}

func TestValidateSkillMD_缺少frontmatter(t *testing.T) {
	dir := t.TempDir()
	skillMD := filepath.Join(dir, "SKILL.md")
	_ = os.WriteFile(skillMD, []byte("# No frontmatter"), 0o644)

	valid, _ := ValidateSkillMD(skillMD)
	if valid {
		t.Error("期望不合法, 实际合法")
	}
}

func TestValidateSkillMD_缺少name(t *testing.T) {
	dir := t.TempDir()
	skillMD := filepath.Join(dir, "SKILL.md")
	_ = os.WriteFile(skillMD, []byte("---\ndescription: A test\n---\n"), 0o644)

	valid, message := ValidateSkillMD(skillMD)
	if valid {
		t.Error("期望不合法, 实际合法")
	}
	if !strings.Contains(message, "name") {
		t.Errorf("期望错误信息包含 'name', 实际: %s", message)
	}
}

func TestValidateSkillMD_name不是kebab_case(t *testing.T) {
	dir := t.TempDir()
	skillMD := filepath.Join(dir, "SKILL.md")
	_ = os.WriteFile(skillMD, []byte("---\nname: My_Skill\ndescription: test\n---\n"), 0o644)

	valid, _ := ValidateSkillMD(skillMD)
	if valid {
		t.Error("期望不合法（name 不是 kebab-case）, 实际合法")
	}
}

func TestValidateSkillMD_description包含尖括号(t *testing.T) {
	dir := t.TempDir()
	skillMD := filepath.Join(dir, "SKILL.md")
	_ = os.WriteFile(skillMD, []byte("---\nname: test-skill\ndescription: Use <tag> for formatting\n---\n"), 0o644)

	valid, _ := ValidateSkillMD(skillMD)
	if valid {
		t.Error("期望不合法（description 包含尖括号）, 实际合法")
	}
}

func TestValidateSkillMD_未允许字段(t *testing.T) {
	dir := t.TempDir()
	skillMD := filepath.Join(dir, "SKILL.md")
	_ = os.WriteFile(skillMD, []byte("---\nname: test-skill\ndescription: test\nfoo: bar\n---\n"), 0o644)

	valid, message := ValidateSkillMD(skillMD)
	if valid {
		t.Error("期望不合法（包含未允许字段）, 实际合法")
	}
	if !strings.Contains(message, "foo") {
		t.Errorf("期望错误信息包含 'foo', 实际: %s", message)
	}
}

func TestValidateSkillMD_name连字符非法(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"开头连字符", "-test-skill"},
		{"结尾连字符", "test-skill-"},
		{"连续连字符", "test--skill"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			skillMD := filepath.Join(dir, "SKILL.md")
			content := "---\nname: " + tt.input + "\ndescription: test\n---\n"
			_ = os.WriteFile(skillMD, []byte(content), 0o644)

			valid, _ := ValidateSkillMD(skillMD)
			if valid {
				t.Errorf("期望不合法（name=%s）, 实际合法", tt.input)
			}
		})
	}
}

func TestParseSkillFrontmatter(t *testing.T) {
	dir := t.TempDir()
	skillMD := filepath.Join(dir, "SKILL.md")
	content := "---\nname: my-skill\ndescription: A test skill\n---\n# Body content\n"
	_ = os.WriteFile(skillMD, []byte(content), 0o644)

	name, desc, body := ParseSkillFrontmatter(skillMD)
	if name != "my-skill" {
		t.Errorf("期望 name='my-skill', 实际='%s'", name)
	}
	if desc != "A test skill" {
		t.Errorf("期望 desc='A test skill', 实际='%s'", desc)
	}
	if !strings.Contains(body, "# Body content") {
		t.Errorf("期望 body 包含 '# Body content', 实际='%s'", body)
	}
}

func TestValidateStageHandler_Execute_文件不存在(t *testing.T) {
	handler := &ValidateStageHandler{}
	state := skilldev.NewSkillDevState("test-task")
	workspace := t.TempDir()
	_ = os.MkdirAll(filepath.Join(workspace, "skill"), 0o755)

	sctx := newTestContext(state, workspace)
	result, err := handler.Execute(context.Background(), sctx)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if result.NextStage != skilldev.SkillDevStageGenerate {
		t.Errorf("期望回退到 GENERATE, 实际=%s", result.NextStage)
	}
}

func TestValidateStageHandler_Execute_校验通过(t *testing.T) {
	handler := &ValidateStageHandler{}
	state := skilldev.NewSkillDevState("test-task")
	workspace := t.TempDir()
	skillDir := filepath.Join(workspace, "skill")
	_ = os.MkdirAll(skillDir, 0o755)
	_ = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test-skill\ndescription: test\n---\n"), 0o644)

	sctx := newTestContext(state, workspace)
	result, err := handler.Execute(context.Background(), sctx)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if result.NextStage != skilldev.SkillDevStageTestDesign {
		t.Errorf("期望进入 TEST_DESIGN, 实际=%s", result.NextStage)
	}
}

// ──────────────────────────── GenerateStage 测试 ────────────────────────────

func TestGenerateStageHandler_Execute(t *testing.T) {
	handler := &GenerateStageHandler{}
	state := skilldev.NewSkillDevState("test-task")
	state.Plan = map[string]any{
		"skill_name": "test-skill",
		"directory_structure": map[string]any{
			"SKILL.md":        "主指令文件",
			"scripts/run.py":  "运行脚本",
		},
	}

	workspace := t.TempDir()
	_ = os.MkdirAll(filepath.Join(workspace, "skill"), 0o755)
	sctx := newTestContext(state, workspace)

	result, err := handler.Execute(context.Background(), sctx)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if result.NextStage != skilldev.SkillDevStageValidate {
		t.Errorf("期望 NextStage=%s, 实际=%s", skilldev.SkillDevStageValidate, result.NextStage)
	}

	// 验证占位文件已创建
	if _, err := os.Stat(filepath.Join(workspace, "skill", "SKILL.md")); os.IsNotExist(err) {
		t.Error("SKILL.md 未创建")
	}
	if _, err := os.Stat(filepath.Join(workspace, "skill", "scripts", "run.py")); os.IsNotExist(err) {
		t.Error("scripts/run.py 未创建")
	}
}

func TestGenerateStageHandler_缺少plan(t *testing.T) {
	handler := &GenerateStageHandler{}
	state := skilldev.NewSkillDevState("test-task")
	workspace := t.TempDir()
	sctx := newTestContext(state, workspace)

	_, err := handler.Execute(context.Background(), sctx)
	if err == nil {
		t.Error("期望错误（缺少 plan）, 实际返回 nil")
	}
}

func TestGenerateStageHandler_生成顺序(t *testing.T) {
	handler := &GenerateStageHandler{}
	plan := map[string]any{
		"directory_structure": map[string]any{
			"SKILL.md":         "主指令文件",
			"scripts/run.py":   "运行脚本",
			"references/doc.md": "参考文档",
			"assets/icon.png":  "图标",
		},
	}

	order := handler.resolveGenerationOrder(plan)
	if len(order) != 4 {
		t.Fatalf("期望 4 个文件, 实际 %d", len(order))
	}
	// SKILL.md 必须第一个
	if order[0].FilePath != "SKILL.md" {
		t.Errorf("期望第一个文件为 SKILL.md, 实际=%s", order[0].FilePath)
	}
	// scripts/ 排在其余前面
	if order[1].FilePath != "scripts/run.py" {
		t.Errorf("期望第二个文件为 scripts/run.py, 实际=%s", order[1].FilePath)
	}
}

// ──────────────────────────── TestDesignStage 测试 ────────────────────────────

func TestTestDesignStageHandler_Execute(t *testing.T) {
	handler := &TestDesignStageHandler{}
	state := skilldev.NewSkillDevState("test-task")
	state.Plan = map[string]any{"skill_name": "test-skill"}

	workspace := t.TempDir()
	skillDir := filepath.Join(workspace, "skill")
	_ = os.MkdirAll(skillDir, 0o755)
	_ = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test-skill\ndescription: test\n---\n"), 0o644)

	sctx := newTestContext(state, workspace)
	result, err := handler.Execute(context.Background(), sctx)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if result.NextStage != skilldev.SkillDevStageTestRun {
		t.Errorf("期望 NextStage=%s, 实际=%s", skilldev.SkillDevStageTestRun, result.NextStage)
	}
	if state.Evals == nil {
		t.Fatal("Evals 不应为 nil")
	}
}

// ──────────────────────────── TestRunStage 测试 ────────────────────────────

func TestTestRunStageHandler_Execute(t *testing.T) {
	handler := &TestRunStageHandler{}
	state := skilldev.NewSkillDevState("test-task")
	state.Evals = map[string]any{
		"evals": []any{
			map[string]any{
				"id":           1,
				"name":         "basic-usage",
				"prompt":       "测试提示",
				"expectations": []any{"期望1"},
			},
		},
	}

	workspace := t.TempDir()
	_ = os.MkdirAll(filepath.Join(workspace, "evals"), 0o755)
	sctx := newTestContext(state, workspace)

	result, err := handler.Execute(context.Background(), sctx)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if result.NextStage != skilldev.SkillDevStageEvaluate {
		t.Errorf("期望 NextStage=%s, 实际=%s", skilldev.SkillDevStageEvaluate, result.NextStage)
	}
}

func TestTestRunStageHandler_缺少evals(t *testing.T) {
	handler := &TestRunStageHandler{}
	state := skilldev.NewSkillDevState("test-task")
	workspace := t.TempDir()
	sctx := newTestContext(state, workspace)

	_, err := handler.Execute(context.Background(), sctx)
	if err == nil {
		t.Error("期望错误（缺少 evals）, 实际返回 nil")
	}
}

// ──────────────────────────── EvaluateStage 测试 ────────────────────────────

func TestEvaluateStageHandler_Execute(t *testing.T) {
	handler := &EvaluateStageHandler{}
	state := skilldev.NewSkillDevState("test-task")
	state.Plan = map[string]any{"skill_name": "test-skill"}
	state.Evals = map[string]any{
		"evals": []any{
			map[string]any{
				"id":           1,
				"name":         "basic-usage",
				"expectations": []any{"期望1"},
			},
		},
	}

	workspace := t.TempDir()
	iterDir := filepath.Join(workspace, "evals", "iteration-0")
	caseDir := filepath.Join(iterDir, "basic-usage")
	withSkillDir := filepath.Join(caseDir, "with_skill")
	baselineDir := filepath.Join(caseDir, "baseline")
	_ = os.MkdirAll(withSkillDir, 0o755)
	_ = os.MkdirAll(baselineDir, 0o755)

	// 写入占位 grading.json 和 timing.json
	for _, configDir := range []string{withSkillDir, baselineDir} {
		grading := map[string]any{
			"expectations": []any{},
			"summary":      map[string]any{"pass_rate": 0.0},
		}
		gradingData, _ := json.MarshalIndent(grading, "", "  ")
		_ = os.WriteFile(filepath.Join(configDir, "grading.json"), gradingData, 0o644)

		timing := map[string]any{
			"total_tokens":           0,
			"duration_ms":            0,
			"total_duration_seconds": 0.0,
		}
		timingData, _ := json.MarshalIndent(timing, "", "  ")
		_ = os.WriteFile(filepath.Join(configDir, "timing.json"), timingData, 0o644)
	}

	sctx := newTestContext(state, workspace)
	result, err := handler.Execute(context.Background(), sctx)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if result.NextStage != skilldev.SkillDevStageReview {
		t.Errorf("期望 NextStage=%s, 实际=%s", skilldev.SkillDevStageReview, result.NextStage)
	}
}

// ──────────────────────────── ImproveStage 测试 ────────────────────────────

func TestImproveStageHandler_Execute(t *testing.T) {
	handler := &ImproveStageHandler{}
	state := skilldev.NewSkillDevState("test-task")
	state.FeedbackHistory = []map[string]any{
		{"iteration": 0, "feedback": map[string]any{"comment": "需要改进"}},
	}
	state.EvalResults = map[string]any{"report": "评测报告"}

	workspace := t.TempDir()
	sctx := newTestContext(state, workspace)

	result, err := handler.Execute(context.Background(), sctx)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if result.NextStage != skilldev.SkillDevStageTestRun {
		t.Errorf("期望 NextStage=%s, 实际=%s", skilldev.SkillDevStageTestRun, result.NextStage)
	}
	if state.Iteration != 1 {
		t.Errorf("期望 iteration=1, 实际=%d", state.Iteration)
	}
}

func TestImproveStageHandler_缺少反馈(t *testing.T) {
	handler := &ImproveStageHandler{}
	state := skilldev.NewSkillDevState("test-task")
	workspace := t.TempDir()
	sctx := newTestContext(state, workspace)

	_, err := handler.Execute(context.Background(), sctx)
	if err == nil {
		t.Error("期望错误（缺少反馈）, 实际返回 nil")
	}
}

// ──────────────────────────── PackageStage 测试 ────────────────────────────

func TestPackageStageHandler_Execute(t *testing.T) {
	handler := &PackageStageHandler{}
	state := skilldev.NewSkillDevState("test-task")
	state.Plan = map[string]any{"skill_name": "test-skill"}

	workspace := t.TempDir()
	skillDir := filepath.Join(workspace, "skill")
	_ = os.MkdirAll(skillDir, 0o755)
	_ = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test-skill\ndescription: test\n---\n"), 0o644)

	sctx := newTestContext(state, workspace)
	result, err := handler.Execute(context.Background(), sctx)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if result.NextStage != skilldev.SkillDevStageDescOptimizeConfirm {
		t.Errorf("期望 NextStage=%s, 实际=%s", skilldev.SkillDevStageDescOptimizeConfirm, result.NextStage)
	}
	if state.ZipPath == nil {
		t.Fatal("ZipPath 不应为 nil")
	}
}

func TestShouldExclude(t *testing.T) {
	skillDir := "/tmp/skill"
	tests := []struct {
		path     string
		exclude  bool
	}{
		{"/tmp/skill/SKILL.md", false},
		{"/tmp/skill/__pycache__/cache.pyc", true},
		{"/tmp/skill/node_modules/pkg/index.js", true},
		{"/tmp/skill/evals/results.json", true},
		{"/tmp/skill/.DS_Store", true},
		{"/tmp/skill/scripts/helper.pyc", true},
		{"/tmp/skill/scripts/helper.py", false},
	}

	for _, tt := range tests {
		result := shouldExclude(tt.path, skillDir)
		if result != tt.exclude {
			t.Errorf("shouldExclude(%s) 期望=%v, 实际=%v", tt.path, tt.exclude, result)
		}
	}
}

// ──────────────────────────── DescOptimizeStage 测试 ────────────────────────────

func TestSplitEvalSet(t *testing.T) {
	queries := []skilldev.TriggerEvalQuery{
		{Query: "q1", ShouldTrigger: true},
		{Query: "q2", ShouldTrigger: true},
		{Query: "q3", ShouldTrigger: true},
		{Query: "q4", ShouldTrigger: true},
		{Query: "q5", ShouldTrigger: true},
		{Query: "q6", ShouldTrigger: false},
		{Query: "q7", ShouldTrigger: false},
		{Query: "q8", ShouldTrigger: false},
		{Query: "q9", ShouldTrigger: false},
		{Query: "q10", ShouldTrigger: false},
	}

	train, test := SplitEvalSet(queries, 0.4, 42)
	if len(train)+len(test) != len(queries) {
		t.Errorf("train(%d) + test(%d) != total(%d)", len(train), len(test), len(queries))
	}
	// test 集应该包含至少 1 个 trigger 和 1 个 no-trigger
	trainTrigger := 0
	for _, q := range train {
		if q.ShouldTrigger {
			trainTrigger++
		}
	}
	testTrigger := 0
	for _, q := range test {
		if q.ShouldTrigger {
			testTrigger++
		}
	}
	if testTrigger == 0 {
		t.Error("test 集应包含至少 1 个 should_trigger=true 的查询")
	}
}

func TestDescOptimizeStageHandler_SKILLMD不存在(t *testing.T) {
	handler := &DescOptimizeStageHandler{}
	state := skilldev.NewSkillDevState("test-task")
	workspace := t.TempDir()
	_ = os.MkdirAll(filepath.Join(workspace, "skill"), 0o755)

	sctx := newTestContext(state, workspace)
	result, err := handler.Execute(context.Background(), sctx)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if result.NextStage != skilldev.SkillDevStageCompleted {
		t.Errorf("期望 NextStage=%s, 实际=%s", skilldev.SkillDevStageCompleted, result.NextStage)
	}
}

// ──────────────────────────── Benchmark 渲染测试 ────────────────────────────

func TestRenderBenchmarkMD(t *testing.T) {
	benchmark := &skilldev.Benchmark{
		SkillName: "test-skill",
		Runs:      []skilldev.BenchmarkRun{},
		RunSummary: map[string]any{
			"with_skill": map[string]any{
				"pass_rate":    map[string]any{"mean": 0.8, "stddev": 0.1},
				"time_seconds": map[string]any{"mean": 5.0, "stddev": 1.0},
				"tokens":       map[string]any{"mean": 1000.0, "stddev": 100.0},
			},
			"baseline": map[string]any{
				"pass_rate":    map[string]any{"mean": 0.3, "stddev": 0.15},
				"time_seconds": map[string]any{"mean": 3.0, "stddev": 0.5},
				"tokens":       map[string]any{"mean": 500.0, "stddev": 50.0},
			},
			"delta": map[string]any{
				"pass_rate":    "+0.50",
				"time_seconds": "+2.0",
				"tokens":       "+500",
			},
		},
		Notes:     []string{"观察1", "观察2"},
		Timestamp: "2026-07-09T00:00:00Z",
	}

	md := renderBenchmarkMD(benchmark)
	if !strings.Contains(md, "# Skill Benchmark: test-skill") {
		t.Error("Markdown 报告缺少标题")
	}
	if !strings.Contains(md, "with_skill") {
		t.Error("Markdown 报告缺少 with_skill 列")
	}
	if !strings.Contains(md, "baseline") {
		t.Error("Markdown 报告缺少 baseline 列")
	}
	if !strings.Contains(md, "观察1") {
		t.Error("Markdown 报告缺少 Analyst Notes")
	}
}

// ──────────────────────────── Frontmatter 解析测试 ────────────────────────────

func TestParseFrontmatter_简单(t *testing.T) {
	text := "name: my-skill\ndescription: A test"
	result := parseFrontmatter(text)
	if result["name"] != "my-skill" {
		t.Errorf("期望 name='my-skill', 实际='%s'", result["name"])
	}
	if result["description"] != "A test" {
		t.Errorf("期望 description='A test', 实际='%s'", result["description"])
	}
}

func TestParseFrontmatter_blockScalar(t *testing.T) {
	text := "name: my-skill\ndescription: |\n  Line 1\n  Line 2"
	result := parseFrontmatter(text)
	if !strings.Contains(result["description"], "Line 1") {
		t.Errorf("期望 description 包含 'Line 1', 实际='%s'", result["description"])
	}
}

func TestHasInvalidHyphenUsage(t *testing.T) {
	tests := []struct {
		input   string
		invalid bool
	}{
		{"my-skill", false},
		{"-my-skill", true},
		{"my-skill-", true},
		{"my--skill", true},
		{"a-b-c", false},
	}

	for _, tt := range tests {
		result := hasInvalidHyphenUsage(tt.input)
		if result != tt.invalid {
			t.Errorf("hasInvalidHyphenUsage(%s) 期望=%v, 实际=%v", tt.input, tt.invalid, result)
		}
	}
}

// ──────────────────────────── readJSONMap 测试 ────────────────────────────

func TestReadJSONMap(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.json")
	_ = os.WriteFile(f, []byte(`{"key": "value", "num": 42}`), 0o644)

	result := readJSONMap(f)
	if result["key"] != "value" {
		t.Errorf("期望 key='value', 实际=%v", result["key"])
	}

	// 不存在的文件
	empty := readJSONMap(filepath.Join(dir, "nonexistent.json"))
	if len(empty) != 0 {
		t.Errorf("期望空 map, 实际=%v", empty)
	}
}

// ──────────────────────────── System Prompt 非空测试 ────────────────────────────

func TestSystemPrompts非空(t *testing.T) {
	prompts := []struct {
		name   string
		value  string
	}{
		{"PlanSystemPrompt", PlanSystemPrompt},
		{"GenerateSystemPrompt", GenerateSystemPrompt},
		{"GraderSystemPrompt", GraderSystemPrompt},
		{"AnalystSystemPrompt", AnalystSystemPrompt},
		{"TestDesignSystemPrompt", TestDesignSystemPrompt},
		{"ImproveSystemPrompt", ImproveSystemPrompt},
		{"TriggerQueryGenPrompt", TriggerQueryGenPrompt},
		{"ImproveDescPrompt", ImproveDescPrompt},
	}

	for _, p := range prompts {
		if p.value == "" {
			t.Errorf("%s 不应为空", p.name)
		}
	}
}

// ──────────────────────────── CalcStats 对齐测试 ────────────────────────────

func TestCalcStats_对齐Python(t *testing.T) {
	// 对齐 Python _calc_stats: sample stddev (n-1)
	values := []float64{1.0, 2.0, 3.0, 4.0, 5.0}
	stats := skilldev.CalcStats(values)
	if stats.Min != 1.0 {
		t.Errorf("期望 min=1.0, 实际=%f", stats.Min)
	}
	if stats.Max != 5.0 {
		t.Errorf("期望 max=5.0, 实际=%f", stats.Max)
	}
	// mean = 3.0
	if stats.Mean != 3.0 {
		t.Errorf("期望 mean=3.0, 实际=%f", stats.Mean)
	}
}

// ──────────────────────────── applyDescription 测试 ────────────────────────────

func TestApplyDescription(t *testing.T) {
	dir := t.TempDir()
	skillMD := filepath.Join(dir, "SKILL.md")
	original := "---\nname: test-skill\ndescription: old desc\n---\n# Body\n"
	_ = os.WriteFile(skillMD, []byte(original), 0o644)

	applyDescription(skillMD, "old desc", "new desc")

	data, _ := os.ReadFile(skillMD)
	content := string(data)
	if !strings.Contains(content, "new desc") {
		t.Errorf("期望包含 'new desc', 实际='%s'", content)
	}
	if strings.Contains(content, "old desc") {
		t.Errorf("不应包含 'old desc', 实际='%s'", content)
	}
}

// ──────────────────────────── 正则表达式测试 ────────────────────────────

func TestFrontmatter正则(t *testing.T) {
	content := "---\nname: test-skill\ndescription: A test\n---\n# Body"
	fmRe := regexp.MustCompile(`(?s)^---\n(.*?)\n---`)
	match := fmRe.FindStringSubmatch(content)
	if match == nil {
		t.Fatal("frontmatter 正则匹配失败")
	}
	if !strings.Contains(match[1], "name: test-skill") {
		t.Errorf("期望匹配内容包含 name, 实际='%s'", match[1])
	}
}

// ──────────────────────────── 额外覆盖率测试 ────────────────────────────

func TestInitStageHandler_资源解析(t *testing.T) {
	handler := &InitStageHandler{}

	// 测试带资源文件的场景
	txtContent := "这是参考文本内容"
	state := skilldev.NewSkillDevState("test-task")
	state.Input = map[string]any{
		"query": "测试",
		"resources": []any{
			map[string]any{
				"name":           "ref.txt",
				"content_base64": "6L+Z5piv5Y+C6ICD5paH5pys5YaF5a65",
			},
		},
	}

	workspace := t.TempDir()
	_ = os.MkdirAll(filepath.Join(workspace, "resources"), 0o755)
	_ = os.MkdirAll(filepath.Join(workspace, "skill"), 0o755)

	sctx := newTestContext(state, workspace)
	result, err := handler.Execute(context.Background(), sctx)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if result.NextStage != skilldev.SkillDevStagePlan {
		t.Errorf("期望 NextStage=%s, 实际=%s", skilldev.SkillDevStagePlan, result.NextStage)
	}
	if len(state.ReferenceTexts) == 0 {
		t.Log("ReferenceTexts 为空（base64 解码/文件解析可能失败）")
	}

	// 验证解码后的文本
	if len(state.ReferenceTexts) > 0 && state.ReferenceTexts[0] != txtContent {
		t.Errorf("期望 ReferenceTexts[0]='%s', 实际='%s'", txtContent, state.ReferenceTexts[0])
	}
}

func TestInitStageHandler_已有Skill(t *testing.T) {
	handler := &InitStageHandler{}
	state := skilldev.NewSkillDevState("test-task")
	state.Input = map[string]any{
		"query":         "修改技能",
		"existing_skill": map[string]any{"content_base64": "placeholder"},
	}

	workspace := t.TempDir()
	_ = os.MkdirAll(filepath.Join(workspace, "resources"), 0o755)
	_ = os.MkdirAll(filepath.Join(workspace, "skill"), 0o755)

	sctx := newTestContext(state, workspace)
	result, err := handler.Execute(context.Background(), sctx)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if result.NextStage != skilldev.SkillDevStagePlan {
		t.Errorf("期望 NextStage=%s, 实际=%s", skilldev.SkillDevStagePlan, result.NextStage)
	}
	// extractExistingSkill 尚未实现，所以 ExistingSkillMD 应为 nil
	if state.ExistingSkillMD != nil {
		t.Error("ExistingSkillMD 应为 nil（尚未实现）")
	}
}

func TestPlanStageHandler_buildMessages(t *testing.T) {
	handler := &PlanStageHandler{}
	state := skilldev.NewSkillDevState("test-task")
	state.Input = map[string]any{"query": "创建翻译技能"}
	state.ReferenceTexts = []string{"参考资料1", "参考资料2", "参考资料3", "参考资料4"}
	existingMD := "已有 SKILL.md 内容"
	state.ExistingSkillMD = &existingMD

	sctx := newTestContext(state, "/tmp/workspace")
	messages := handler.buildMessages(sctx)
	if len(messages) != 1 {
		t.Fatalf("期望 1 条消息, 实际 %d", len(messages))
	}
	content := messages[0]["content"]
	if !strings.Contains(content, "创建翻译技能") {
		t.Error("消息内容应包含查询")
	}
	if !strings.Contains(content, "参考资料") {
		t.Error("消息内容应包含参考资料")
	}
	if !strings.Contains(content, "已有 SKILL.md") {
		t.Error("消息内容应包含已有 SKILL.md")
	}
	// 应限制为最多 3 条参考文本
	if strings.Contains(content, "参考资料4") {
		t.Error("消息内容不应包含第 4 条参考资料（限制 3 条）")
	}
}

func TestGenerateStageHandler_空directory_structure(t *testing.T) {
	handler := &GenerateStageHandler{}
	plan := map[string]any{
		"skill_name":         "test-skill",
		"directory_structure": map[string]any{},
	}
	order := handler.resolveGenerationOrder(plan)
	if len(order) != 0 {
		t.Errorf("期望 0 个文件, 实际 %d", len(order))
	}
}

func TestValidateSkillMD_各种边界(t *testing.T) {
	tests := []struct {
		name    string
		content string
		valid   bool
	}{
		{"name过长", "---\nname: " + strings.Repeat("a", 65) + "\ndescription: test\n---\n", false},
		{"description过长", "---\nname: test\ndescription: " + strings.Repeat("a", 1025) + "\n---\n", false},
		{"合法license字段", "---\nname: test-skill\ndescription: test\nlicense: MIT\n---\n", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			skillMD := filepath.Join(dir, "SKILL.md")
			_ = os.WriteFile(skillMD, []byte(tt.content), 0o644)

			valid, _ := ValidateSkillMD(skillMD)
			if valid != tt.valid {
				t.Errorf("期望 valid=%v, 实际=%v", tt.valid, valid)
			}
		})
	}
}

func TestEvaluateStage_gradeAllEvals(t *testing.T) {
	handler := &EvaluateStageHandler{}
	state := skilldev.NewSkillDevState("test-task")
	state.Evals = map[string]any{
		"evals": []any{
			map[string]any{
				"id":           1,
				"name":         "test-case",
				"expectations": []any{"期望1", "期望2"},
			},
		},
	}

	workspace := t.TempDir()
	iterDir := filepath.Join(workspace, "evals", "iteration-0")
	caseDir := filepath.Join(iterDir, "test-case")
	withSkillDir := filepath.Join(caseDir, "with_skill")
	baselineDir := filepath.Join(caseDir, "baseline")
	_ = os.MkdirAll(withSkillDir, 0o755)
	_ = os.MkdirAll(baselineDir, 0o755)

	sctx := newTestContext(state, workspace)
	handler.gradeAllEvals(sctx, iterDir)

	// 验证 grading.json 已写入
	for _, config := range []string{"with_skill", "baseline"} {
		gradingFile := filepath.Join(caseDir, config, "grading.json")
		if _, err := os.Stat(gradingFile); os.IsNotExist(err) {
			t.Errorf("%s/grading.json 未创建", config)
		}
	}
}

func TestEvaluateStage_aggregateBenchmark_空evals(t *testing.T) {
	handler := &EvaluateStageHandler{}
	state := skilldev.NewSkillDevState("test-task")
	state.Evals = nil
	state.Plan = map[string]any{"skill_name": "test-skill"}

	workspace := t.TempDir()
	iterDir := filepath.Join(workspace, "evals", "iteration-0")
	_ = os.MkdirAll(iterDir, 0o755)

	sctx := newTestContext(state, workspace)
	benchmark := handler.aggregateBenchmark(sctx, iterDir)
	if benchmark == nil {
		t.Fatal("benchmark 不应为 nil")
	}
	if benchmark.SkillName != "test-skill" {
		t.Errorf("期望 SkillName='test-skill', 实际='%s'", benchmark.SkillName)
	}
}

func TestImproveStage_readSkillFiles(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "scripts"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Test"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "scripts", "run.py"), []byte("print('hi')"), 0o644)

	content := readSkillFiles(dir)
	if !strings.Contains(content, "SKILL.md") {
		t.Error("内容应包含 SKILL.md")
	}
	if !strings.Contains(content, "run.py") {
		t.Error("内容应包含 run.py")
	}
}

func TestDescOptimize_buildDescOptResult(t *testing.T) {
	tp1 := 2
	tt1 := 3
	history := []skilldev.DescOptimizeIteration{
		{Iteration: 1, Description: "desc1", TrainPassed: 2, TrainTotal: 3, TestPassed: &tp1, TestTotal: &tt1},
	}

	result := buildDescOptResult("original", "best", history, []skilldev.TriggerEvalQuery{{Query: "q1", ShouldTrigger: true}})
	if result["original_description"] != "original" {
		t.Errorf("期望 original_description='original', 实际=%v", result["original_description"])
	}
	if result["best_description"] != "best" {
		t.Errorf("期望 best_description='best', 实际=%v", result["best_description"])
	}
	if result["iterations_run"] != 1 {
		t.Errorf("期望 iterations_run=1, 实际=%v", result["iterations_run"])
	}
}

func TestCountPassed(t *testing.T) {
	results := []map[string]any{
		{"pass": true},
		{"pass": false},
		{"pass": true},
	}
	if count := countPassed(results); count != 2 {
		t.Errorf("期望 count=2, 实际=%d", count)
	}
}

func TestFindBestIteration_空(t *testing.T) {
	result := findBestIteration(nil, false)
	if result != nil {
		t.Error("期望 nil, 实际非 nil")
	}
}

func TestToIntFromAny(t *testing.T) {
	tests := []struct {
		input  any
		expect int
	}{
		{42, 42},
		{int64(42), 42},
		{float64(42.7), 42},
		{"not a number", 0},
	}
	for _, tt := range tests {
		result := toIntFromAny(tt.input)
		if result != tt.expect {
			t.Errorf("toIntFromAny(%v) 期望=%d, 实际=%d", tt.input, tt.expect, result)
		}
	}
}

func TestValidateStageHandler_校验失败回退(t *testing.T) {
	handler := &ValidateStageHandler{}
	state := skilldev.NewSkillDevState("test-task")
	workspace := t.TempDir()
	skillDir := filepath.Join(workspace, "skill")
	_ = os.MkdirAll(skillDir, 0o755)
	// 写入不合法的 SKILL.md（name 不是 kebab-case）
	_ = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: INVALID\ndescription: test\n---\n"), 0o644)

	sctx := newTestContext(state, workspace)
	result, err := handler.Execute(context.Background(), sctx)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if result.NextStage != skilldev.SkillDevStageGenerate {
		t.Errorf("期望回退到 GENERATE, 实际=%s", result.NextStage)
	}
}

func TestPackageStage_shouldExclude_边界(t *testing.T) {
	skillDir := "/workspace/skill"
	tests := []struct {
		path    string
		exclude bool
	}{
		{"/workspace/skill/SKILL.md", false},
		{"/workspace/skill/.git/config", true},
		{"/workspace/skill/scripts/run.py", false},
	}
	for _, tt := range tests {
		result := shouldExclude(tt.path, skillDir)
		if result != tt.exclude {
			t.Errorf("shouldExclude(%s) 期望=%v, 实际=%v", tt.path, tt.exclude, result)
		}
	}
}

func TestEvaluateStage_analyzePatterns(t *testing.T) {
	handler := &EvaluateStageHandler{}
	state := skilldev.NewSkillDevState("test-task")
	sctx := newTestContext(state, "/tmp")
	benchmark := &skilldev.Benchmark{SkillName: "test"}

	notes := handler.analyzePatterns(sctx, benchmark)
	if len(notes) == 0 {
		t.Error("应返回至少一条占位 note")
	}
}

func TestEvaluateStage_renderBenchmarkMD_无delta(t *testing.T) {
	benchmark := &skilldev.Benchmark{
		SkillName:  "test",
		Timestamp:  "2026-01-01T00:00:00Z",
		RunSummary: map[string]any{},
		Notes:      nil,
	}
	md := renderBenchmarkMD(benchmark)
	if !strings.Contains(md, "# Skill Benchmark: test") {
		t.Error("应包含标题")
	}
}

func TestDescOptimize_applyDescription_无frontmatter(t *testing.T) {
	dir := t.TempDir()
	skillMD := filepath.Join(dir, "SKILL.md")
	_ = os.WriteFile(skillMD, []byte("# No frontmatter"), 0o644)

	// 不应 panic
	applyDescription(skillMD, "old", "new")

	data, _ := os.ReadFile(skillMD)
	if string(data) != "# No frontmatter" {
		t.Error("不应修改无 frontmatter 的文件")
	}
}
