package skilldev

import (
	"encoding/json"
	"strings"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSkillDevStage_Values(t *testing.T) {
	stages := map[string]SkillDevStage{
		"init":                 SkillDevStageInit,
		"plan":                 SkillDevStagePlan,
		"plan_confirm":         SkillDevStagePlanConfirm,
		"generate":             SkillDevStageGenerate,
		"validate":             SkillDevStageValidate,
		"test_design":          SkillDevStageTestDesign,
		"test_run":             SkillDevStageTestRun,
		"evaluate":             SkillDevStageEvaluate,
		"review":               SkillDevStageReview,
		"improve":              SkillDevStageImprove,
		"package":              SkillDevStagePackage,
		"desc_optimize_confirm": SkillDevStageDescOptimizeConfirm,
		"desc_optimize":        SkillDevStageDescOptimize,
		"completed":            SkillDevStageCompleted,
		"error":                SkillDevStageError,
	}
	for expected, stage := range stages {
		if string(stage) != expected {
			t.Errorf("期望 %q, 实际 %q", expected, string(stage))
		}
	}
	if len(stages) != 15 {
		t.Errorf("期望 15 个阶段枚举, 实际 %d", len(stages))
	}
}

func TestSkillDevTaskMode_Values(t *testing.T) {
	modes := map[string]SkillDevTaskMode{
		"create":              SkillDevTaskModeCreate,
		"create_with_resources": SkillDevTaskModeCreateWithResources,
		"modify":              SkillDevTaskModeModify,
	}
	for expected, mode := range modes {
		if string(mode) != expected {
			t.Errorf("期望 %q, 实际 %q", expected, string(mode))
		}
	}
	if len(modes) != 3 {
		t.Errorf("期望 3 个任务模式枚举, 实际 %d", len(modes))
	}
}

func TestSkillDevEventType_Values(t *testing.T) {
	types := map[string]SkillDevEventType{
		"skilldev.stage_changed":    SkillDevEventTypeStageChanged,
		"skilldev.progress":         SkillDevEventTypeProgress,
		"skilldev.error":            SkillDevEventTypeError,
		"skilldev.agent_thinking":   SkillDevEventTypeAgentThinking,
		"skilldev.test_progress":    SkillDevEventTypeTestProgress,
		"skilldev.confirm_request":  SkillDevEventTypeConfirmRequest,
		"skilldev.todos_update":     SkillDevEventTypeTodosUpdate,
		"skilldev.artifact_ready":   SkillDevEventTypeArtifactReady,
		"skilldev.eval_ready":       SkillDevEventTypeEvalReady,
		"skilldev.validate_result":  SkillDevEventTypeValidateResult,
		"skilldev.desc_opt_ready":   SkillDevEventTypeDescOptReady,
	}
	for expected, et := range types {
		if string(et) != expected {
			t.Errorf("期望 %q, 实际 %q", expected, string(et))
		}
	}
	if len(types) != 11 {
		t.Errorf("期望 11 个事件类型枚举, 实际 %d", len(types))
	}
}

func TestNewSkillDevState(t *testing.T) {
	state := NewSkillDevState("test_task")
	if state.TaskID != "test_task" {
		t.Errorf("期望 TaskID=%q, 实际 %q", "test_task", state.TaskID)
	}
	if state.Stage != SkillDevStageInit {
		t.Errorf("期望 Stage=init, 实际 %s", state.Stage)
	}
	if state.Mode != SkillDevTaskModeCreate {
		t.Errorf("期望 Mode=create, 实际 %s", state.Mode)
	}
	if state.Iteration != 0 {
		t.Errorf("期望 Iteration=0, 实际 %d", state.Iteration)
	}
	if state.CreatedAt == "" {
		t.Error("期望 CreatedAt 非空")
	}
	if state.UpdatedAt == "" {
		t.Error("期望 UpdatedAt 非空")
	}
	if state.ExistingSkillMD != nil {
		t.Error("期望 ExistingSkillMD 为 nil")
	}
}

func TestSkillDevState_Touch(t *testing.T) {
	state := NewSkillDevState("test")
	// 修改为可辨识的值以验证 Touch 更新了 UpdatedAt
	state.UpdatedAt = "2000-01-01T00:00:00Z"
	state.Touch()
	if state.UpdatedAt == "2000-01-01T00:00:00Z" {
		t.Error("期望 Touch 更新 UpdatedAt")
	}
}

func TestSkillDevState_ToCheckpointDict(t *testing.T) {
	state := NewSkillDevState("test_task")
	state.Stage = SkillDevStagePlan
	state.Mode = SkillDevTaskModeModify
	state.Iteration = 2
	skillMD := "existing content"
	state.ExistingSkillMD = &skillMD

	dict := state.ToCheckpointDict()
	if dict["task_id"] != "test_task" {
		t.Errorf("期望 task_id=test_task, 实际 %v", dict["task_id"])
	}
	if dict["stage"] != "plan" {
		t.Errorf("期望 stage=plan, 实际 %v", dict["stage"])
	}
	if dict["mode"] != "modify" {
		t.Errorf("期望 mode=modify, 实际 %v", dict["mode"])
	}
	if dict["iteration"] != 2 {
		t.Errorf("期望 iteration=2, 实际 %v", dict["iteration"])
	}
	if dict["existing_skill_md"] != &skillMD {
		t.Errorf("期望 existing_skill_md 有值, 实际 %v", dict["existing_skill_md"])
	}
}

func TestSkillDevState_ToCheckpointDict_全字段(t *testing.T) {
	state := NewSkillDevState("full_test")
	state.Stage = SkillDevStageEvaluate
	state.Mode = SkillDevTaskModeCreateWithResources
	state.Iteration = 3
	state.Input = map[string]any{"query": "test query"}
	state.ReferenceTexts = []string{"ref1", "ref2"}
	planConfirmedAt := "2025-01-01T00:00:00Z"
	state.PlanConfirmedAt = &planConfirmedAt
	state.Plan = map[string]any{"description": "test plan"}
	state.Evals = map[string]any{"evals": "data"}
	state.EvalResults = map[string]any{"benchmark": "result"}
	state.FeedbackHistory = []map[string]any{{"iteration": 0, "feedback": "fix"}}
	state.DescOptimizeResult = map[string]any{"best_description": "optimized"}
	zipPath := "/tmp/test.zip"
	state.ZipPath = &zipPath
	state.ZipSize = 1024
	errMsg := "some error"
	state.Error = &errMsg

	dict := state.ToCheckpointDict()

	// 验证所有字段都能序列化
	bytes, err := json.Marshal(dict)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if len(bytes) == 0 {
		t.Error("期望序列化结果非空")
	}
}

func TestFromCheckpointDict(t *testing.T) {
	skillMD := "existing content"
	planConfirmedAt := "2025-01-01T00:00:00Z"
	zipPath := "/tmp/test.zip"
	errMsg := "some error"

	data := map[string]any{
		"task_id":              "test_task",
		"stage":                "plan",
		"mode":                 "modify",
		"iteration":            float64(2),
		"input":                map[string]any{"query": "test"},
		"reference_texts":      []any{"ref1", "ref2"},
		"existing_skill_md":    skillMD,
		"plan":                 map[string]any{"description": "test"},
		"plan_confirmed_at":    planConfirmedAt,
		"evals":                map[string]any{"evals": "data"},
		"eval_results":         map[string]any{"benchmark": "result"},
		"feedback_history":     []any{map[string]any{"iteration": float64(0)}},
		"desc_optimize_result": nil,
		"zip_path":             zipPath,
		"zip_size":             float64(1024),
		"created_at":           "2025-01-01T00:00:00Z",
		"updated_at":           "2025-01-02T00:00:00Z",
		"error":                errMsg,
	}

	state := FromCheckpointDict(data)
	if state.TaskID != "test_task" {
		t.Errorf("期望 TaskID=test_task, 实际 %s", state.TaskID)
	}
	if state.Stage != SkillDevStagePlan {
		t.Errorf("期望 Stage=plan, 实际 %s", state.Stage)
	}
	if state.Mode != SkillDevTaskModeModify {
		t.Errorf("期望 Mode=modify, 实际 %s", state.Mode)
	}
	if state.Iteration != 2 {
		t.Errorf("期望 Iteration=2, 实际 %d", state.Iteration)
	}
	if state.ExistingSkillMD == nil || *state.ExistingSkillMD != skillMD {
		t.Errorf("期望 ExistingSkillMD=%s, 实际 %v", skillMD, state.ExistingSkillMD)
	}
	if state.PlanConfirmedAt == nil || *state.PlanConfirmedAt != planConfirmedAt {
		t.Errorf("期望 PlanConfirmedAt=%s, 实际 %v", planConfirmedAt, state.PlanConfirmedAt)
	}
	if state.ZipPath == nil || *state.ZipPath != zipPath {
		t.Errorf("期望 ZipPath=%s, 实际 %v", zipPath, state.ZipPath)
	}
	if state.ZipSize != 1024 {
		t.Errorf("期望 ZipSize=1024, 实际 %d", state.ZipSize)
	}
	if state.Error == nil || *state.Error != errMsg {
		t.Errorf("期望 Error=%s, 实际 %v", errMsg, state.Error)
	}
}

func TestFromCheckpointDict_默认值(t *testing.T) {
	data := map[string]any{
		"task_id": "minimal",
		"stage":   "init",
	}

	state := FromCheckpointDict(data)
	if state.TaskID != "minimal" {
		t.Errorf("期望 TaskID=minimal, 实际 %s", state.TaskID)
	}
	if state.Mode != SkillDevTaskModeCreate {
		t.Errorf("期望 Mode=create（默认）, 实际 %s", state.Mode)
	}
	if state.Iteration != 0 {
		t.Errorf("期望 Iteration=0（默认）, 实际 %d", state.Iteration)
	}
	if len(state.ReferenceTexts) != 0 {
		t.Errorf("期望 ReferenceTexts 为空切片, 实际 %v", state.ReferenceTexts)
	}
	if state.ExistingSkillMD != nil {
		t.Errorf("期望 ExistingSkillMD 为 nil, 实际 %v", state.ExistingSkillMD)
	}
}

func TestFromCheckpointDict_空值(t *testing.T) {
	data := map[string]any{
		"task_id":              "test",
		"stage":                "init",
		"existing_skill_md":    nil,
		"plan_confirmed_at":    nil,
		"zip_path":             nil,
		"error":                nil,
	}

	state := FromCheckpointDict(data)
	if state.ExistingSkillMD != nil {
		t.Errorf("期望 ExistingSkillMD 为 nil, 实际 %v", state.ExistingSkillMD)
	}
	if state.PlanConfirmedAt != nil {
		t.Errorf("期望 PlanConfirmedAt 为 nil, 实际 %v", state.PlanConfirmedAt)
	}
	if state.ZipPath != nil {
		t.Errorf("期望 ZipPath 为 nil, 实际 %v", state.ZipPath)
	}
	if state.Error != nil {
		t.Errorf("期望 Error 为 nil, 实际 %v", state.Error)
	}
}

func TestSkillDevState_ToStatusDict(t *testing.T) {
	state := NewSkillDevState("test_task")
	state.Stage = SkillDevStageEvaluate
	state.Mode = SkillDevTaskModeModify
	state.Iteration = 3
	state.Plan = map[string]any{"description": "test"}
	state.EvalResults = map[string]any{"benchmark": "result"}
	errMsg := "error"
	state.Error = &errMsg

	dict := state.ToStatusDict()
	if dict["task_id"] != "test_task" {
		t.Errorf("期望 task_id=test_task, 实际 %v", dict["task_id"])
	}
	if dict["stage"] != "evaluate" {
		t.Errorf("期望 stage=evaluate, 实际 %v", dict["stage"])
	}
	if dict["mode"] != "modify" {
		t.Errorf("期望 mode=modify, 实际 %v", dict["mode"])
	}
	if dict["iteration"] != 3 {
		t.Errorf("期望 iteration=3, 实际 %v", dict["iteration"])
	}
	// ToStatusDict 不包含 reference_texts, existing_skill_md 等字段
	if _, ok := dict["reference_texts"]; ok {
		t.Error("ToStatusDict 不应包含 reference_texts")
	}
}

func TestSuspensionPoints_完整配置(t *testing.T) {
	// 验证 3 个挂起点都存在
	expectedStages := []SkillDevStage{
		SkillDevStagePlanConfirm,
		SkillDevStageReview,
		SkillDevStageDescOptimizeConfirm,
	}
	for _, stage := range expectedStages {
		cfg, ok := SuspensionPoints[stage]
		if !ok {
			t.Errorf("期望存在挂起点 %s", stage)
			continue
		}
		if cfg.ConfirmType == "" {
			t.Errorf("挂起点 %s 的 ConfirmType 不应为空", stage)
		}
		if cfg.Title == "" {
			t.Errorf("挂起点 %s 的 Title 不应为空", stage)
		}
		if cfg.Message == "" {
			t.Errorf("挂起点 %s 的 Message 不应为空", stage)
		}
		if len(cfg.Actions) == 0 {
			t.Errorf("挂起点 %s 的 Actions 不应为空", stage)
		}
		if cfg.ExtractData == nil {
			t.Errorf("挂起点 %s 的 ExtractData 不应为 nil", stage)
		}
		if cfg.OnResume == nil {
			t.Errorf("挂起点 %s 的 OnResume 不应为 nil", stage)
		}
	}
	if len(SuspensionPoints) != 3 {
		t.Errorf("期望 3 个挂起点, 实际 %d", len(SuspensionPoints))
	}
}

func TestSuspensionPoints_PlanConfirm(t *testing.T) {
	cfg := SuspensionPoints[SkillDevStagePlanConfirm]
	state := NewSkillDevState("test")
	state.Plan = map[string]any{"description": "test plan"}

	// 测试 ExtractData
	data := cfg.ExtractData(state)
	if data["plan"] == nil {
		t.Error("期望 plan 非空")
	}

	// 测试 OnResume
	cfg.OnResume(state, map[string]any{"plan": map[string]any{"description": "modified"}})
	if state.PlanConfirmedAt == nil {
		t.Error("期望 PlanConfirmedAt 被设置")
	}

	// 测试 NextStage
	nextStage := cfg.GetNextStage(nil)
	if nextStage != SkillDevStageGenerate {
		t.Errorf("期望 NextStage=generate, 实际 %s", nextStage)
	}
}

func TestSuspensionPoints_Review(t *testing.T) {
	cfg := SuspensionPoints[SkillDevStageReview]
	state := NewSkillDevState("test")
	state.EvalResults = map[string]any{
		"benchmark": "bench_data",
		"report":    "report_data",
	}
	state.Iteration = 2

	// 测试 ExtractData
	data := cfg.ExtractData(state)
	if data["benchmark"] != "bench_data" {
		t.Errorf("期望 benchmark=bench_data, 实际 %v", data["benchmark"])
	}
	if data["report"] != "report_data" {
		t.Errorf("期望 report=report_data, 实际 %v", data["report"])
	}
	if data["iteration"] != 2 {
		t.Errorf("期望 iteration=2, 实际 %v", data["iteration"])
	}

	// 测试 OnResume
	cfg.OnResume(state, map[string]any{"feedback": "需要改进"})
	if len(state.FeedbackHistory) != 1 {
		t.Errorf("期望 FeedbackHistory 长度=1, 实际 %d", len(state.FeedbackHistory))
	}

	// 测试 NextStageFunc - improve
	nextStage := cfg.GetNextStage(map[string]any{"action": "improve"})
	if nextStage != SkillDevStageImprove {
		t.Errorf("期望 NextStage=improve, 实际 %s", nextStage)
	}

	// 测试 NextStageFunc - accept
	nextStage = cfg.GetNextStage(map[string]any{"action": "accept"})
	if nextStage != SkillDevStagePackage {
		t.Errorf("期望 NextStage=package, 实际 %s", nextStage)
	}

	// 测试 NextStageFunc - 默认
	nextStage = cfg.GetNextStage(map[string]any{})
	if nextStage != SkillDevStageImprove {
		t.Errorf("期望默认 NextStage=improve, 实际 %s", nextStage)
	}
}

func TestSuspensionPoints_DescOptimizeConfirm(t *testing.T) {
	cfg := SuspensionPoints[SkillDevStageDescOptimizeConfirm]
	state := NewSkillDevState("test")
	state.Plan = map[string]any{"description": "current desc"}

	// 测试 ExtractData
	data := cfg.ExtractData(state)
	if data["current_description"] != "current desc" {
		t.Errorf("期望 current_description=current desc, 实际 %v", data["current_description"])
	}

	// 测试 OnResume（空操作）
	cfg.OnResume(state, map[string]any{"action": "optimize"})

	// 测试 NextStageFunc - optimize
	nextStage := cfg.GetNextStage(map[string]any{"action": "optimize"})
	if nextStage != SkillDevStageDescOptimize {
		t.Errorf("期望 NextStage=desc_optimize, 实际 %s", nextStage)
	}

	// 测试 NextStageFunc - skip
	nextStage = cfg.GetNextStage(map[string]any{"action": "skip"})
	if nextStage != SkillDevStageCompleted {
		t.Errorf("期望 NextStage=completed, 实际 %s", nextStage)
	}

	// 测试 NextStageFunc - 默认
	nextStage = cfg.GetNextStage(map[string]any{})
	if nextStage != SkillDevStageCompleted {
		t.Errorf("期望默认 NextStage=completed, 实际 %s", nextStage)
	}
}

func TestSuspensionPoints_DescOptExtractData_无Plan(t *testing.T) {
	cfg := SuspensionPoints[SkillDevStageDescOptimizeConfirm]
	state := NewSkillDevState("test")
	state.Plan = nil

	data := cfg.ExtractData(state)
	if data["current_description"] != "" {
		t.Errorf("期望 current_description 为空, 实际 %v", data["current_description"])
	}
}

func TestSuspensionPoints_ReviewExtractData_无EvalResults(t *testing.T) {
	cfg := SuspensionPoints[SkillDevStageReview]
	state := NewSkillDevState("test")
	state.EvalResults = nil

	data := cfg.ExtractData(state)
	if data["benchmark"] != nil {
		t.Errorf("期望 benchmark 为 nil, 实际 %v", data["benchmark"])
	}
	if data["report"] != nil {
		t.Errorf("期望 report 为 nil, 实际 %v", data["report"])
	}
}

func TestComputeTodos_初始阶段(t *testing.T) {
	todos := ComputeTodos(SkillDevStageInit, nil)
	if len(todos) != 6 {
		t.Errorf("期望 6 个分组, 实际 %d", len(todos))
	}
	// INIT 在 plan 分组，应为 in_progress
	if todos[0]["status"] != "in_progress" {
		t.Errorf("期望 plan 分组状态=in_progress, 实际 %s", todos[0]["status"])
	}
	// 后续分组应为 pending
	for i := 1; i < len(todos); i++ {
		if todos[i]["status"] != "pending" {
			t.Errorf("期望分组 %d 状态=pending, 实际 %s", i, todos[i]["status"])
		}
	}
}

func TestComputeTodos_中间阶段(t *testing.T) {
	todos := ComputeTodos(SkillDevStageTestRun, nil)
	if len(todos) != 6 {
		t.Errorf("期望 6 个分组, 实际 %d", len(todos))
	}
	// 前面分组应为 completed
	if todos[0]["status"] != "completed" {
		t.Errorf("期望 plan 分组状态=completed, 实际 %s", todos[0]["status"])
	}
	if todos[1]["status"] != "completed" {
		t.Errorf("期望 generate 分组状态=completed, 实际 %s", todos[1]["status"])
	}
	// 当前分组应为 in_progress
	if todos[2]["status"] != "in_progress" {
		t.Errorf("期望 test 分组状态=in_progress, 实际 %s", todos[2]["status"])
	}
	// 后面分组应为 pending
	if todos[3]["status"] != "pending" {
		t.Errorf("期望 improve 分组状态=pending, 实际 %s", todos[3]["status"])
	}
}

func TestComputeTodos_完成(t *testing.T) {
	todos := ComputeTodos(SkillDevStageCompleted, nil)
	for _, todo := range todos {
		if todo["status"] != "completed" {
			t.Errorf("期望所有分组状态=completed, 实际 %s", todo["status"])
		}
	}
}

func TestComputeTodos_错误(t *testing.T) {
	todos := ComputeTodos(SkillDevStageError, nil)
	for _, todo := range todos {
		if todo["status"] != "cancelled" {
			t.Errorf("期望所有分组状态=cancelled, 实际 %s", todo["status"])
		}
	}
}

func TestComputeTodos_带Mode过滤(t *testing.T) {
	mode := SkillDevTaskModeCreate
	todos := ComputeTodos(SkillDevStageInit, &mode)
	// 当前所有分组 Modes 为 nil（所有模式适用），所以结果应与无过滤相同
	if len(todos) != 6 {
		t.Errorf("期望 6 个分组, 实际 %d", len(todos))
	}
}

func TestGenerateTaskID(t *testing.T) {
	id1 := GenerateTaskID()
	id2 := GenerateTaskID()

	if !strings.HasPrefix(id1, "sd_") {
		t.Errorf("期望 task_id 以 sd_ 开头, 实际 %s", id1)
	}
	if id1 == id2 {
		t.Error("期望两个 task_id 不同")
	}

	// 验证格式：sd_{timestamp}_{random}
	parts := strings.SplitN(id1, "_", 3)
	if len(parts) != 3 {
		t.Errorf("期望 task_id 格式 sd_timestamp_random, 实际 %s", id1)
	}
	if len(parts[2]) != 8 { // 4 bytes = 8 hex chars
		t.Errorf("期望 random 部分长度为 8, 实际 %d", len(parts[2]))
	}
}

func TestDetermineTaskMode(t *testing.T) {
	tests := []struct {
		name     string
		params   map[string]any
		expected SkillDevTaskMode
	}{
		{"空参数", map[string]any{}, SkillDevTaskModeCreate},
		{"existing_skill", map[string]any{"existing_skill": "content"}, SkillDevTaskModeModify},
		{"resources", map[string]any{"resources": []string{"file.txt"}}, SkillDevTaskModeCreateWithResources},
		{"existing_skill优先", map[string]any{"existing_skill": "content", "resources": "file"}, SkillDevTaskModeModify},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetermineTaskMode(tt.params)
			if result != tt.expected {
				t.Errorf("期望 %s, 实际 %s", tt.expected, result)
			}
		})
	}
}

func TestAllowedFrontmatterKeys(t *testing.T) {
	expectedKeys := []string{"name", "description", "license", "allowed-tools", "metadata", "compatibility"}
	for _, key := range expectedKeys {
		if !AllowedFrontmatterKeys[key] {
			t.Errorf("期望 %s 在 AllowedFrontmatterKeys 中", key)
		}
	}
	if len(AllowedFrontmatterKeys) != 6 {
		t.Errorf("期望 6 个 frontmatter 键, 实际 %d", len(AllowedFrontmatterKeys))
	}
}

func TestEvalCase_ToDict(t *testing.T) {
	ec := EvalCase{
		ID:             1,
		Prompt:         "test prompt",
		ExpectedOutput: "expected",
		Files:          []string{"file1.txt"},
		Expectations:   []string{"should pass"},
	}
	dict := ec.ToDict()
	if dict["id"] != 1 {
		t.Errorf("期望 id=1, 实际 %v", dict["id"])
	}
	if dict["prompt"] != "test prompt" {
		t.Errorf("期望 prompt=test prompt, 实际 %v", dict["prompt"])
	}
}

func TestEvalSet_ToDict(t *testing.T) {
	es := EvalSet{
		SkillName: "test_skill",
		Evals: []EvalCase{
			{ID: 1, Prompt: "p1"},
			{ID: 2, Prompt: "p2"},
		},
	}
	dict := es.ToDict()
	if dict["skill_name"] != "test_skill" {
		t.Errorf("期望 skill_name=test_skill, 实际 %v", dict["skill_name"])
	}
	evals, ok := dict["evals"].([]map[string]any)
	if !ok || len(evals) != 2 {
		t.Errorf("期望 evals 长度=2, 实际 %v", dict["evals"])
	}
}

func TestNewEvalSetFromDict(t *testing.T) {
	data := map[string]any{
		"skill_name": "test_skill",
		"evals": []any{
			map[string]any{
				"id":             float64(1),
				"prompt":         "p1",
				"expected_output": "e1",
				"files":          []any{"f1"},
				"expectations":   []any{"exp1"},
			},
		},
	}
	es := NewEvalSetFromDict(data)
	if es.SkillName != "test_skill" {
		t.Errorf("期望 SkillName=test_skill, 实际 %s", es.SkillName)
	}
	if len(es.Evals) != 1 {
		t.Fatalf("期望 1 个 EvalCase, 实际 %d", len(es.Evals))
	}
	if es.Evals[0].ID != 1 {
		t.Errorf("期望 ID=1, 实际 %d", es.Evals[0].ID)
	}
	if es.Evals[0].Prompt != "p1" {
		t.Errorf("期望 Prompt=p1, 实际 %s", es.Evals[0].Prompt)
	}
}

func TestGradingResult_ToDict(t *testing.T) {
	gr := GradingResult{
		Expectations: []GradingExpectation{
			{Text: "test assertion", Passed: true, Evidence: "evidence"},
		},
		PassRate:    1.0,
		PassedCount: 1,
		FailedCount: 0,
	}
	dict := gr.ToDict()
	expectations, ok := dict["expectations"].([]map[string]any)
	if !ok || len(expectations) != 1 {
		t.Fatalf("期望 1 个 expectation, 实际 %v", dict["expectations"])
	}
	if expectations[0]["passed"] != true {
		t.Errorf("期望 passed=true, 实际 %v", expectations[0]["passed"])
	}
	summary, ok := dict["summary"].(map[string]any)
	if !ok {
		t.Fatalf("期望 summary 为 map")
	}
	if summary["total"] != 1 {
		t.Errorf("期望 total=1, 实际 %v", summary["total"])
	}
}

func TestRunTiming_ToDict(t *testing.T) {
	rt := RunTiming{TotalTokens: 100, DurationMs: 5000, TotalDurationSeconds: 5.0}
	dict := rt.ToDict()
	if dict["total_tokens"] != 100 {
		t.Errorf("期望 total_tokens=100, 实际 %v", dict["total_tokens"])
	}
}

func TestMetricStats_ToDict(t *testing.T) {
	ms := MetricStats{Mean: 0.85, Stddev: 0.05, Min: 0.7, Max: 1.0}
	dict := ms.ToDict()
	if dict["mean"] != 0.85 {
		t.Errorf("期望 mean=0.85, 实际 %v", dict["mean"])
	}
}

func TestBenchmarkRun_ToDict(t *testing.T) {
	br := BenchmarkRun{
		EvalID:        1,
		EvalName:      "test_eval",
		Configuration: "with_skill",
		RunNumber:     1,
		PassRate:      0.9,
		TimeSeconds:   5.0,
		Tokens:        100,
	}
	dict := br.ToDict()
	if dict["eval_id"] != 1 {
		t.Errorf("期望 eval_id=1, 实际 %v", dict["eval_id"])
	}
	result, ok := dict["result"].(map[string]any)
	if !ok {
		t.Fatalf("期望 result 为 map")
	}
	if result["pass_rate"] != 0.9 {
		t.Errorf("期望 pass_rate=0.9, 实际 %v", result["pass_rate"])
	}
}

func TestBenchmark_ToDict(t *testing.T) {
	b := Benchmark{
		SkillName:  "test_skill",
		Runs:       []BenchmarkRun{{EvalID: 1, EvalName: "e1", Configuration: "with_skill"}},
		RunSummary: map[string]any{"avg_pass_rate": 0.9},
		Notes:      []string{"note1"},
		Timestamp:  "2025-01-01T00:00:00Z",
	}
	dict := b.ToDict()
	metadata, ok := dict["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("期望 metadata 为 map")
	}
	if metadata["skill_name"] != "test_skill" {
		t.Errorf("期望 skill_name=test_skill, 实际 %v", metadata["skill_name"])
	}
}

func TestTriggerEvalQuery_ToDict(t *testing.T) {
	q := TriggerEvalQuery{Query: "test query", ShouldTrigger: true}
	dict := q.ToDict()
	if dict["query"] != "test query" {
		t.Errorf("期望 query=test query, 实际 %v", dict["query"])
	}
	if dict["should_trigger"] != true {
		t.Errorf("期望 should_trigger=true, 实际 %v", dict["should_trigger"])
	}
}

func TestDescOptimizeIteration_ToDict(t *testing.T) {
	// 无 test 数据
	d := DescOptimizeIteration{
		Iteration:   1,
		Description: "desc",
		TrainPassed: 8,
		TrainTotal:  10,
	}
	dict := d.ToDict()
	if _, ok := dict["test_passed"]; ok {
		t.Error("期望无 test_passed 字段")
	}

	// 有 test 数据
	testP := 7
	testT := 10
	d.TestPassed = &testP
	d.TestTotal = &testT
	dict = d.ToDict()
	if dict["test_passed"] != 7 {
		t.Errorf("期望 test_passed=7, 实际 %v", dict["test_passed"])
	}
}

func TestCalcStats(t *testing.T) {
	stats := CalcStats([]float64{0.8, 0.9, 1.0})
	if stats.Min != 0.8 {
		t.Errorf("期望 Min=0.8, 实际 %f", stats.Min)
	}
	if stats.Max != 1.0 {
		t.Errorf("期望 Max=1.0, 实际 %f", stats.Max)
	}
	if stats.Mean != 0.9 {
		t.Errorf("期望 Mean=0.9, 实际 %f", stats.Mean)
	}
}

func TestCalcStats_空值(t *testing.T) {
	stats := CalcStats([]float64{})
	if stats != (MetricStats{}) {
		t.Errorf("期望零值 MetricStats, 实际 %+v", stats)
	}
}

func TestCalcStats_单值(t *testing.T) {
	stats := CalcStats([]float64{0.5})
	if stats.Mean != 0.5 {
		t.Errorf("期望 Mean=0.5, 实际 %f", stats.Mean)
	}
	if stats.Stddev != 0 {
		t.Errorf("期望 Stddev=0, 实际 %f", stats.Stddev)
	}
	if stats.Min != 0.5 || stats.Max != 0.5 {
		t.Errorf("期望 Min=Max=0.5, 实际 Min=%f Max=%f", stats.Min, stats.Max)
	}
}

func TestStageGroups_完整覆盖(t *testing.T) {
	if len(stageGroups) != 6 {
		t.Errorf("期望 6 个分组, 实际 %d", len(stageGroups))
	}
	// 验证每个分组的 ID 和 Label 非空
	for _, g := range stageGroups {
		if g.ID == "" {
			t.Error("分组 ID 不应为空")
		}
		if g.Label == "" {
			t.Error("分组 Label 不应为空")
		}
		if len(g.Stages) == 0 {
			t.Errorf("分组 %s 的 Stages 不应为空", g.ID)
		}
	}
}

func TestSkillDevEvent(t *testing.T) {
	event := SkillDevEvent{
		EventType: SkillDevEventTypeProgress,
		Payload:   map[string]any{"text": "progress message"},
		TaskID:    "test_task",
	}
	if event.EventType != SkillDevEventTypeProgress {
		t.Errorf("期望 EventType=skilldev.progress, 实际 %s", event.EventType)
	}
}

func TestSuspensionConfig_GetNextStage_固定值(t *testing.T) {
	cfg := SuspensionConfig{
		NextStage: SkillDevStageGenerate,
	}
	next := cfg.GetNextStage(nil)
	if next != SkillDevStageGenerate {
		t.Errorf("期望 NextStage=generate, 实际 %s", next)
	}
}

func TestSuspensionConfig_GetNextStage_动态函数(t *testing.T) {
	cfg := SuspensionConfig{
		NextStageFunc: func(data map[string]any) SkillDevStage {
			return SkillDevStageImprove
		},
	}
	next := cfg.GetNextStage(nil)
	if next != SkillDevStageImprove {
		t.Errorf("期望 NextStage=improve, 实际 %s", next)
	}
}

func TestSuspensionConfig_GetNextStage_函数优先(t *testing.T) {
	cfg := SuspensionConfig{
		NextStage:     SkillDevStageGenerate,
		NextStageFunc: func(data map[string]any) SkillDevStage { return SkillDevStageImprove },
	}
	next := cfg.GetNextStage(nil)
	if next != SkillDevStageImprove {
		t.Errorf("期望 NextStageFunc 优先, 实际 %s", next)
	}
}
