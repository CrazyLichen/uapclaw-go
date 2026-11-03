package stages

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev"
)

// ──────────────────────────── 结构体 ────────────────────────────

// queueMockClient 按队列顺序返回预设输出（耗尽后返回最后一条）。
type queueMockClient struct {
	// mu 保护 replies
	mu sync.Mutex
	// replies 预设输出队列
	replies []string
	// idx 当前下标
	idx int
}

// Invoke 返回下一条预设输出。
func (m *queueMockClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	reply := ""
	if len(m.replies) > 0 {
		if m.idx < len(m.replies) {
			reply = m.replies[m.idx]
		} else {
			reply = m.replies[len(m.replies)-1]
		}
		m.idx++
	}
	return llmschema.NewAssistantMessage(reply), nil
}

// Stream 未实现。
func (m *queueMockClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

// GenerateImage 未实现。
func (m *queueMockClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// GenerateSpeech 未实现。
func (m *queueMockClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// GenerateVideo 未实现。
func (m *queueMockClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// TranscribeAudio 未实现。
func (m *queueMockClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// Release 无需释放。
func (m *queueMockClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

// SupportsKVCacheRelease 不支持。
func (m *queueMockClient) SupportsKVCacheRelease() bool { return false }

// ──────────────────────────── 导出函数 ────────────────────────────

// TestStageAgentHelpers 阶段 Agent 通用助手：输出提取与代码围栏剥离。
func TestStageAgentHelpers(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"无围栏", "hello", "hello"},
		{"围栏带语言", "```markdown\n# T\nbody\n```", "# T\nbody"},
		{"围栏无语言", "```\n# T\n```", "# T"},
		{"单行围栏", "```", "```"},
	}
	for _, c := range cases {
		if got := skilldev.StripCodeFences(c.in); got != c.want {
			t.Errorf("%s: StripCodeFences(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}

	state := skilldev.NewSkillDevState("task")
	sctx := newMockStageContext(t, state, t.TempDir(), "hello world")
	text, err := sctx.RunStageAgent(context.Background(), "helper", "你是助手", nil, 2, "打个招呼")
	if err != nil {
		t.Fatalf("RunStageAgent 失败: %v", err)
	}
	if !strings.Contains(text, "hello world") {
		t.Errorf("应返回模型输出，实际 %q", text)
	}
}

// TestRunStageAgent_模型调用失败 阶段 Agent 调用失败时返回错误。
func TestRunStageAgent_模型调用失败(t *testing.T) {
	state := skilldev.NewSkillDevState("task")
	sctx := newMockStageContext(t, state, t.TempDir(), "x")
	sctx.Deps.ModelClientConfig = nil

	if _, err := sctx.RunStageAgent(context.Background(), "helper", "p", nil, 2, "q"); err == nil {
		t.Fatal("期望模型配置错误，实际 nil")
	}
}

// TestPlanStage_差量分析 已有 SKILL.md 时计划应带 diff_analysis。
func TestPlanStage_差量分析(t *testing.T) {
	existing := "---\nname: old-skill\ndescription: 旧描述\n---\n# Old\n"
	state := skilldev.NewSkillDevState("task")
	state.Input = map[string]any{"query": "升级技能"}
	state.ExistingSkillMD = &existing

	sctx := newMockStageContext(t, state, t.TempDir(),
		`{"skill_name":"new-skill","description":"新描述","directory_structure":{"SKILL.md":"主"}}`)
	if _, err := (&PlanStageHandler{}).Execute(context.Background(), sctx); err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	diff, _ := state.Plan["diff_analysis"].(string)
	if !strings.Contains(diff, "old-skill") || !strings.Contains(diff, "new-skill") {
		t.Errorf("diff_analysis 应包含新旧技能名，实际 %q", diff)
	}
}

// TestPlanStage_模型输出非法 JSON 解析失败应报错。
func TestPlanStage_模型输出非法JSON(t *testing.T) {
	state := skilldev.NewSkillDevState("task")
	state.Input = map[string]any{"query": "x"}
	sctx := newMockStageContext(t, state, t.TempDir(), "这不是 JSON")

	if _, err := (&PlanStageHandler{}).Execute(context.Background(), sctx); err == nil {
		t.Fatal("期望 JSON 解析错误，实际 nil")
	}
}

// TestParseSkillFrontmatterFields 提取 name/description，无 frontmatter 返回空。
func TestParseSkillFrontmatterFields(t *testing.T) {
	name, desc := parseSkillFrontmatterFields("---\nname: a-b\ndescription: 描述\n---\nbody")
	if name != "a-b" || desc != "描述" {
		t.Errorf("解析结果异常: %q %q", name, desc)
	}
	name, desc = parseSkillFrontmatterFields("no frontmatter")
	if name != "" || desc != "" {
		t.Errorf("无 frontmatter 应返回空，实际 %q %q", name, desc)
	}
}

// TestGenerateStage_补齐frontmatter 模型漏写 frontmatter 时按 plan 补齐。
func TestGenerateStage_补齐frontmatter(t *testing.T) {
	state := skilldev.NewSkillDevState("task")
	state.Plan = map[string]any{
		"skill_name":          "demo-skill",
		"description":         "演示 <技能>",
		"directory_structure": map[string]any{"SKILL.md": "主指令"},
	}
	workspace := t.TempDir()
	sctx := newMockStageContext(t, state, workspace, "# 无 frontmatter 的正文\n")

	if _, err := (&GenerateStageHandler{}).Execute(context.Background(), sctx); err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(workspace, "skill", "SKILL.md"))
	if err != nil {
		t.Fatalf("读取 SKILL.md 失败: %v", err)
	}
	text := string(content)
	if !strings.HasPrefix(text, "---\nname: demo-skill") {
		t.Errorf("应补齐 frontmatter，实际 %q", text)
	}
	if strings.Contains(text, "<技能>") {
		t.Errorf("description 应剔除尖括号，实际 %q", text)
	}
}

// TestGenerateStage_模型空输出 模型无输出时报错，不落盘空文件。
func TestGenerateStage_模型空输出(t *testing.T) {
	state := skilldev.NewSkillDevState("task")
	state.Plan = map[string]any{"skill_name": "s", "directory_structure": map[string]any{"SKILL.md": "主"}}
	sctx := newMockStageContext(t, state, t.TempDir(), "   ")

	if _, err := (&GenerateStageHandler{}).Execute(context.Background(), sctx); err == nil {
		t.Fatal("期望空输出错误，实际 nil")
	}
}

// TestBuildFilePrompt 单文件 prompt 应包含文件路径、职责与已生成列表。
func TestBuildFilePrompt(t *testing.T) {
	prompt := buildFilePrompt(`{"skill_name":"s"}`, generationItem{FilePath: "scripts/a.py", Role: "辅助脚本"}, []string{"SKILL.md"})
	for _, want := range []string{"scripts/a.py", "辅助脚本", "SKILL.md", "不要 Markdown 代码围栏"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt 缺少 %q", want)
		}
	}

	prompt = buildFilePrompt("{}", generationItem{FilePath: "SKILL.md", Role: "主"}, nil)
	if !strings.Contains(prompt, "已生成文件：无") {
		t.Errorf("无已生成文件时应显示「无」，实际 %q", prompt)
	}
}

// TestEnsureFileFrontmatter 仅对 SKILL.md 补 frontmatter，已有则原样返回。
func TestEnsureFileFrontmatter(t *testing.T) {
	plan := map[string]any{"skill_name": "s", "description": "d"}

	if got := ensureFileFrontmatter(plan, "scripts/a.py", "code"); got != "code" {
		t.Errorf("非 SKILL.md 不应改动，实际 %q", got)
	}
	withFM := "---\nname: s\ndescription: d\n---\nbody"
	if got := ensureFileFrontmatter(plan, "SKILL.md", withFM); got != withFM {
		t.Errorf("已有 frontmatter 应原样返回，实际 %q", got)
	}
	got := ensureFileFrontmatter(plan, "SKILL.md", "body")
	if !strings.HasPrefix(got, "---\nname: s\ndescription: d\n---") {
		t.Errorf("应补齐 frontmatter，实际 %q", got)
	}
	// plan 缺字段时用默认值
	got = ensureFileFrontmatter(map[string]any{}, "SKILL.md", "body")
	if !strings.Contains(got, "generated-skill") {
		t.Errorf("缺 plan 字段应回退默认技能名，实际 %q", got)
	}
}

// TestImproveStage_保留frontmatter 模型只输出正文时沿用原 frontmatter。
func TestImproveStage_保留frontmatter(t *testing.T) {
	state := skilldev.NewSkillDevState("task")
	state.FeedbackHistory = []map[string]any{{"feedback": map[string]any{"comment": "改进"}}}
	workspace := t.TempDir()
	_ = os.MkdirAll(filepath.Join(workspace, "skill"), 0o755)
	_ = os.WriteFile(filepath.Join(workspace, "skill", "SKILL.md"),
		[]byte("---\nname: keep-skill\ndescription: 保留\n---\n# V1\n"), 0o644)

	sctx := newMockStageContext(t, state, workspace, "# V2 只有正文\n")
	if _, err := (&ImproveStageHandler{}).Execute(context.Background(), sctx); err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(workspace, "skill", "SKILL.md"))
	text := string(content)
	if !strings.Contains(text, "name: keep-skill") || !strings.Contains(text, "# V2 只有正文") {
		t.Errorf("应沿用原 frontmatter 并替换正文，实际 %q", text)
	}
}

// TestImproveStage_模型空输出 模型无输出时报错。
func TestImproveStage_模型空输出(t *testing.T) {
	state := skilldev.NewSkillDevState("task")
	state.FeedbackHistory = []map[string]any{{"feedback": map[string]any{"comment": "x"}}}
	workspace := t.TempDir()
	_ = os.MkdirAll(filepath.Join(workspace, "skill"), 0o755)

	sctx := newMockStageContext(t, state, workspace, "")
	if _, err := (&ImproveStageHandler{}).Execute(context.Background(), sctx); err == nil {
		t.Fatal("期望空输出错误，实际 nil")
	}
}

// TestExtractFrontmatterBlock frontmatter 提取与缺失分支。
func TestExtractFrontmatterBlock(t *testing.T) {
	if got := extractFrontmatterBlock("no fm"); got != "" {
		t.Errorf("无 frontmatter 应返回空，实际 %q", got)
	}
	if got := extractFrontmatterBlock("---\nname: a\n---\nbody"); !strings.Contains(got, "name: a") {
		t.Errorf("应提取 frontmatter，实际 %q", got)
	}
	if got := extractFrontmatterBlock("---\nname: a"); got != "" {
		t.Errorf("frontmatter 未闭合应返回空，实际 %q", got)
	}
}

// TestParseGradingResult 评分解析：合法 / 非法 / 计数自动推导。
func TestParseGradingResult(t *testing.T) {
	text := `{"expectations":[{"text":"a","passed":true,"evidence":"e1"},{"text":"b","passed":false,"evidence":"e2"}]}`
	grading, ok := parseGradingResult(text, []string{"a", "b"})
	if !ok {
		t.Fatal("合法 JSON 应解析成功")
	}
	if grading.PassedCount != 1 || grading.FailedCount != 1 || grading.PassRate != 0.5 {
		t.Errorf("计数推导异常: %+v", grading)
	}

	if _, ok := parseGradingResult("no json", []string{"a"}); ok {
		t.Error("非法 JSON 应返回 false")
	}
	if _, ok := parseGradingResult(`{"expectations":[]}`, []string{"a"}); ok {
		t.Error("空 expectations 应返回 false")
	}
}

// TestParseStringArray 观察列表解析。
func TestParseStringArray(t *testing.T) {
	if got := parseStringArray(`["a","b"]`); len(got) != 2 {
		t.Errorf("应解析 2 条，实际 %v", got)
	}
	if got := parseStringArray("no array"); got != nil {
		t.Errorf("非法输入应返回 nil，实际 %v", got)
	}
}

// TestEvaluate_analyzePatterns_有运行记录 有 benchmark 运行时调用 Analyst Agent。
func TestEvaluate_analyzePatterns_有运行记录(t *testing.T) {
	state := skilldev.NewSkillDevState("task")
	sctx := newMockStageContext(t, state, t.TempDir(), `["观察一","观察二"]`)

	benchmark := &skilldev.Benchmark{Runs: []skilldev.BenchmarkRun{{EvalID: 1, EvalName: "case-1", Configuration: "with_skill"}}}
	notes := (&EvaluateStageHandler{}).analyzePatterns(context.Background(), sctx, benchmark)
	if len(notes) != 2 || notes[0] != "观察一" {
		t.Errorf("应解析 Agent 观察列表，实际 %v", notes)
	}
}

// TestEvaluate_analyzePatterns_无运行记录 无运行时给出确定性提示，不调用模型。
func TestEvaluate_analyzePatterns_无运行记录(t *testing.T) {
	state := skilldev.NewSkillDevState("task")
	sctx := newMockStageContext(t, state, t.TempDir(), "不应被调用")

	notes := (&EvaluateStageHandler{}).analyzePatterns(context.Background(), sctx, &skilldev.Benchmark{})
	if len(notes) != 1 || !strings.Contains(notes[0], "没有可评测") {
		t.Errorf("无运行记录应返回确定性提示，实际 %v", notes)
	}
}

// TestDescOptimize_ParseTriggerQueries 触发查询解析与过滤空查询。
func TestDescOptimize_ParseTriggerQueries(t *testing.T) {
	queries := parseTriggerQueries(`text [{"query":"a","should_trigger":true},{"query":"  ","should_trigger":false}] tail`)
	if len(queries) != 1 || queries[0].Query != "a" || !queries[0].ShouldTrigger {
		t.Errorf("解析结果异常: %+v", queries)
	}
	if got := parseTriggerQueries("no json"); got != nil {
		t.Errorf("非法输入应返回 nil，实际 %v", got)
	}
}

// TestDescOptimize_ParseBoolArray 布尔数组解析。
func TestDescOptimize_ParseBoolArray(t *testing.T) {
	if got := parseBoolArray("[true,false]"); len(got) != 2 || !got[0] || got[1] {
		t.Errorf("解析结果异常: %v", got)
	}
	if got := parseBoolArray("x"); got != nil {
		t.Errorf("非法输入应返回 nil，实际 %v", got)
	}
}

// TestDescOptimize_ExtractNewDescription 标签提取（闭合/未闭合/缺失）。
func TestDescOptimize_ExtractNewDescription(t *testing.T) {
	if got := extractNewDescription("前<new_description>新描述</new_description>后"); got != "新描述" {
		t.Errorf("应提取标签内容，实际 %q", got)
	}
	if got := extractNewDescription("<new_description>未闭合"); got != "未闭合" {
		t.Errorf("未闭合应取剩余内容，实际 %q", got)
	}
	if got := extractNewDescription("无标签"); got != "" {
		t.Errorf("无标签应返回空，实际 %q", got)
	}
}

// TestDescOptimize_BuildFailureDetails 失败用例汇总。
func TestDescOptimize_BuildFailureDetails(t *testing.T) {
	details := buildFailureDetails([]map[string]any{
		{"query": "失败1", "pass": false},
		{"query": "通过", "pass": true},
	})
	if !strings.Contains(details, "失败1") || strings.Contains(details, "通过") {
		t.Errorf("应只汇总失败用例，实际 %q", details)
	}
	if got := buildFailureDetails(nil); !strings.Contains(got, "无") {
		t.Errorf("无失败用例应提示无，实际 %q", got)
	}
}

// TestDescOptimize_GenerateTriggerQueries Agent 输出非法时回退确定性查询集。
func TestDescOptimize_GenerateTriggerQueries(t *testing.T) {
	state := skilldev.NewSkillDevState("task")
	sctx := newMockStageContext(t, state, t.TempDir(), "不是 JSON")

	queries := (&DescOptimizeStageHandler{}).generateTriggerQueries(context.Background(), sctx, "demo-skill", "描述")
	if len(queries) != 2 {
		t.Fatalf("应回退 2 条确定性查询，实际 %d", len(queries))
	}
	if !strings.Contains(queries[0].Query, "demo-skill") || !queries[0].ShouldTrigger {
		t.Errorf("回退查询内容异常: %+v", queries[0])
	}
}

// TestDescOptimize_EvalDescription 判定结果长度不符时按「全部正确」兜底。
func TestDescOptimize_EvalDescription(t *testing.T) {
	state := skilldev.NewSkillDevState("task")
	queries := []skilldev.TriggerEvalQuery{
		{Query: "q1", ShouldTrigger: true},
		{Query: "q2", ShouldTrigger: false},
	}

	okCtx := newMockStageContext(t, state, t.TempDir(), "[true,false]")
	results := (&DescOptimizeStageHandler{}).evalDescription(context.Background(), okCtx, "desc", queries)
	if len(results) != 2 {
		t.Fatalf("应返回 2 条结果，实际 %d", len(results))
	}
	for _, r := range results {
		if pass, _ := r["pass"].(bool); !pass {
			t.Errorf("判定与期望一致时应 pass，实际 %+v", r)
		}
	}

	badCtx := newMockStageContext(t, state, t.TempDir(), "[true]")
	results = (&DescOptimizeStageHandler{}).evalDescription(context.Background(), badCtx, "desc", queries)
	for _, r := range results {
		if pass, _ := r["pass"].(bool); !pass {
			t.Errorf("长度不符时兜底应全部 pass，实际 %+v", r)
		}
	}

	empty := (&DescOptimizeStageHandler{}).evalDescription(context.Background(), okCtx, "desc", nil)
	if len(empty) != 0 {
		t.Errorf("空查询集应返回空结果，实际 %v", empty)
	}
}

// TestDescOptimize_ImproveDescription 标签截断与超长裁剪。
func TestDescOptimize_ImproveDescription(t *testing.T) {
	state := skilldev.NewSkillDevState("task")
	input := improveDescriptionInput{
		SkillName:    "demo",
		CurrentDesc:  "旧",
		TrainResults: []map[string]any{{"query": "失败1", "pass": false}},
	}

	long := strings.Repeat("长", skilldev.SkillDescMaxLen+10)
	sctx := newMockStageContext(t, state, t.TempDir(), "<new_description>"+long+"</new_description>")
	got := (&DescOptimizeStageHandler{}).improveDescription(context.Background(), sctx, input)
	if len([]rune(got)) != skilldev.SkillDescMaxLen {
		t.Errorf("超长描述应裁剪到 %d，实际 %d", skilldev.SkillDescMaxLen, len([]rune(got)))
	}

	noTag := newMockStageContext(t, state, t.TempDir(), "没有标签")
	if got := (&DescOptimizeStageHandler{}).improveDescription(context.Background(), noTag, input); got != input.CurrentDesc {
		t.Errorf("无标签应保留原描述，实际 %q", got)
	}
}

// TestDescOptimize_ImproveDescription_模型失败 模型失败时保留原描述。
func TestDescOptimize_ImproveDescription_模型失败(t *testing.T) {
	state := skilldev.NewSkillDevState("task")
	sctx := newMockStageContext(t, state, t.TempDir(), "x")
	sctx.Deps.ModelClientConfig = nil

	input := improveDescriptionInput{SkillName: "demo", CurrentDesc: "旧"}
	if got := (&DescOptimizeStageHandler{}).improveDescription(context.Background(), sctx, input); got != "旧" {
		t.Errorf("模型失败应保留原描述，实际 %q", got)
	}
}

// TestDescOptimize_无SKILLMD 跳过优化并直接完成。
func TestDescOptimize_无SKILLMD(t *testing.T) {
	state := skilldev.NewSkillDevState("task")
	sctx := newMockStageContext(t, state, t.TempDir(), "x")

	result, err := (&DescOptimizeStageHandler{}).Execute(context.Background(), sctx)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if result.NextStage != skilldev.SkillDevStageCompleted {
		t.Errorf("无 SKILL.md 应直接完成，实际 %s", result.NextStage)
	}
}

// newQueueStageContext 构造按队列返回输出的阶段上下文。
func newQueueStageContext(t *testing.T, state *skilldev.SkillDevState, workspace string, replies []string) *skilldev.SkillDevContext {
	t.Helper()

	provider := fmt.Sprintf("stage_queue_%d", time.Now().UnixNano())
	client := &queueMockClient{replies: replies}
	model_clients.GetClientRegistry().Register(provider, "llm",
		func(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
			return client, nil
		})
	t.Cleanup(func() { _ = model_clients.GetClientRegistry().Unregister(provider, "llm") })

	deps := &skilldev.SkillDevDeps{
		ModelName: "mock-model",
		ModelClientConfig: map[string]any{
			"client_provider": provider,
			"client_id":       provider + "_id",
			"api_key":         "k",
			"api_base":        "http://127.0.0.1:1",
		},
		SysOpConfig: sysop.NewSysOperationCard(
			sysop.WithSysOpWorkConfig(&sysop.LocalWorkConfig{RestrictToSandbox: false}),
		),
	}
	return skilldev.NewSkillDevContext("task", deps, state, workspace, make(chan skilldev.SkillDevEvent, 64))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureImportsUsed 保持 sysop 导入被使用（并发用例会构造 SysOperationCard）。
var _ = sysop.NewSysOperationCard

// ensureTimeUsed 保持 time 导入被使用。
var _ = time.Now

// TestDescOptimize_Execute完整循环 走完「生成查询 → 评测 → 改进 → 收敛」并写回描述。
func TestDescOptimize_Execute完整循环(t *testing.T) {
	workspace := t.TempDir()
	skillDir := filepath.Join(workspace, "skill")
	_ = os.MkdirAll(skillDir, 0o755)
	_ = os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: demo-skill\ndescription: 旧描述\n---\n# Demo\n"), 0o644)

	queries := `[{"query":"q1","should_trigger":true},{"query":"q2","should_trigger":true},
{"query":"q3","should_trigger":true},{"query":"q4","should_trigger":false},
{"query":"q5","should_trigger":false},{"query":"q6","should_trigger":false}]`
	fourFalse := `[false,false,false,false]`
	twoFalse := `[false,false]`
	improved := "<new_description>新描述：处理表格数据</new_description>"

	state := skilldev.NewSkillDevState("task")
	ctx := newQueueStageContext(t, state, workspace, []string{
		queries,   // desc_gen
		fourFalse, // 第 1 轮 train 评测
		twoFalse,  // 第 1 轮 test 评测
		improved,  // 第 1 轮描述改进
		fourFalse, // 第 2 轮 train
		twoFalse,  // 第 2 轮 test
		improved,  // 第 2 轮改进
	})

	result, err := (&DescOptimizeStageHandler{}).Execute(context.Background(), ctx)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if result.NextStage != skilldev.SkillDevStageCompleted {
		t.Errorf("应进入 COMPLETED，实际 %s", result.NextStage)
	}
	content, _ := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if !strings.Contains(string(content), "新描述：处理表格数据") {
		t.Errorf("description 应被写回，实际 %s", string(content))
	}
	if state.DescOptimizeResult == nil {
		t.Error("应写入 DescOptimizeResult")
	}
}

// TestGenerateStage_目录创建失败 skill 目录被同名文件占用时报错。
func TestGenerateStage_目录创建失败(t *testing.T) {
	state := skilldev.NewSkillDevState("task")
	state.Plan = map[string]any{"skill_name": "s", "directory_structure": map[string]any{"scripts/a.py": "脚本"}}
	workspace := t.TempDir()
	// skill 是文件而非目录 → 无法创建 skill/scripts
	_ = os.WriteFile(filepath.Join(workspace, "skill"), []byte("block"), 0o644)

	sctx := newMockStageContext(t, state, workspace, "content")
	if _, err := (&GenerateStageHandler{}).Execute(context.Background(), sctx); err == nil {
		t.Fatal("期望目录创建失败错误，实际 nil")
	}
}

// TestImproveStage_无原frontmatter 原文件无 frontmatter 时直接使用模型输出。
func TestImproveStage_无原Frontmatter(t *testing.T) {
	if got := ensureImprovedSkillMD("no fm", "# V2\n"); got != "# V2\n" {
		t.Errorf("无原 frontmatter 应直接返回模型输出，实际 %q", got)
	}
	if got := ensureImprovedSkillMD("---\nname: a\n---\n", "# V2\n"); !strings.Contains(got, "name: a") {
		t.Errorf("应沿用原 frontmatter，实际 %q", got)
	}
}

// TestEvaluateGradeOneRun_模型失败 grade Agent 失败时退回「全部未通过」兜底。
func TestEvaluateGradeOneRun_模型失败(t *testing.T) {
	state := skilldev.NewSkillDevState("task")
	sctx := newMockStageContext(t, state, t.TempDir(), "x")
	sctx.Deps.ModelClientConfig = nil

	grading := (&EvaluateStageHandler{}).gradeOneRun(context.Background(), sctx, t.TempDir(),
		[]any{"断言一"}, "case-1", "with_skill")
	if grading.PassedCount != 0 || grading.FailedCount != 1 {
		t.Errorf("失败兜底应全部未通过，实际 %+v", grading)
	}
	if len(grading.Expectations) != 1 || grading.Expectations[0].Evidence != "评分未完成" {
		t.Errorf("兜底证据文案异常: %+v", grading.Expectations)
	}
}

// TestDescOptimize_跑满最大迭代 评测始终不通过时跑满 MaxIterations 后退出。
func TestDescOptimize_跑满最大迭代(t *testing.T) {
	workspace := t.TempDir()
	skillDir := filepath.Join(workspace, "skill")
	_ = os.MkdirAll(skillDir, 0o755)
	_ = os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: demo-skill\ndescription: 旧描述\n---\n# Demo\n"), 0o644)

	queries := `[{"query":"q1","should_trigger":true},{"query":"q2","should_trigger":true},
{"query":"q3","should_trigger":true},{"query":"q4","should_trigger":false},
{"query":"q5","should_trigger":false},{"query":"q6","should_trigger":false}]`
	replies := []string{queries}
	for i := 0; i < MaxIterations; i++ {
		replies = append(replies, `[false,false,false,false]`, `[false,false]`,
			"<new_description>候选描述"+fmt.Sprint(i)+"</new_description>")
	}

	state := skilldev.NewSkillDevState("task")
	ctx := newQueueStageContext(t, state, workspace, replies)

	result, err := (&DescOptimizeStageHandler{}).Execute(context.Background(), ctx)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if result.NextStage != skilldev.SkillDevStageCompleted {
		t.Errorf("应进入 COMPLETED，实际 %s", result.NextStage)
	}
	if state.DescOptimizeResult == nil {
		t.Error("应写入 DescOptimizeResult")
	}
}
