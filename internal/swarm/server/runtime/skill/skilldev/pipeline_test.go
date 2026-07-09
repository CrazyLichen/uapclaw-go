package skilldev

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeStageHandler 用于测试的模拟阶段处理器。
type fakeStageHandler struct {
	// nextStage 下一阶段
	nextStage SkillDevStage
	// err 执行错误
	err error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Execute 实现 StageHandler 接口。
func (h *fakeStageHandler) Execute(_ context.Context, _ *SkillDevContext) (*StageResult, error) {
	return &StageResult{NextStage: h.nextStage}, h.err
}

// TestPipeline_Run_InitToPlanConfirm 测试 Pipeline 从 INIT 执行到 PLAN_CONFIRM 挂起点。
func TestPipeline_Run_InitToPlanConfirm(t *testing.T) {
	// 使用 fake handler 替换 INIT 和 PLAN 阶段
	origInit := stageHandlers[SkillDevStageInit]
	origPlan := stageHandlers[SkillDevStagePlan]
	defer func() {
		stageHandlers[SkillDevStageInit] = origInit
		stageHandlers[SkillDevStagePlan] = origPlan
	}()

	stageHandlers[SkillDevStageInit] = &fakeStageHandler{nextStage: SkillDevStagePlan}
	stageHandlers[SkillDevStagePlan] = &fakeStageHandler{nextStage: SkillDevStagePlanConfirm}

	tmpDir := t.TempDir()
	deps := &SkillDevDeps{
		StateStore:        NewStateStore(tmpDir),
		WorkspaceProvider: NewWorkspaceProvider(tmpDir),
	}

	state := NewSkillDevState("test-task-1")
	pipeline := NewSkillDevPipeline("test-task-1", state, deps)

	events, err := pipeline.Run(context.Background())
	if err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}

	// 应在 PlanConfirm 挂起点暂停
	if state.Stage != SkillDevStagePlanConfirm {
		t.Errorf("期望阶段 plan_confirm, 实际: %s", state.Stage)
	}

	// 应有事件输出
	if len(events) == 0 {
		t.Error("期望有事件输出，实际为空")
	}

	// 验证事件类型：应有 STAGE_CHANGED、TODOS_UPDATE、CONFIRM_REQUEST
	hasStageChanged := false
	hasTodosUpdate := false
	hasConfirmRequest := false
	for _, evt := range events {
		switch evt.EventType {
		case SkillDevEventTypeStageChanged:
			hasStageChanged = true
		case SkillDevEventTypeTodosUpdate:
			hasTodosUpdate = true
		case SkillDevEventTypeConfirmRequest:
			hasConfirmRequest = true
		}
	}
	if !hasStageChanged {
		t.Error("缺少 STAGE_CHANGED 事件")
	}
	if !hasTodosUpdate {
		t.Error("缺少 TODOS_UPDATE 事件")
	}
	if !hasConfirmRequest {
		t.Error("缺少 CONFIRM_REQUEST 事件")
	}

	// 验证 state.json 已持久化
	stateFile := filepath.Join(tmpDir, "test-task-1", "state.json")
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		t.Error("state.json 未持久化")
	}
}

// TestPipeline_Resume_FromPlanConfirm 测试从 PlanConfirm 挂起点恢复。
func TestPipeline_Resume_FromPlanConfirm(t *testing.T) {
	// 替换 GENERATE、VALIDATE 和 TEST_DESIGN 阶段（Resume 后会跳到 GENERATE）
	origGenerate := stageHandlers[SkillDevStageGenerate]
	origValidate := stageHandlers[SkillDevStageValidate]
	origTestDesign := stageHandlers[SkillDevStageTestDesign]
	defer func() {
		stageHandlers[SkillDevStageGenerate] = origGenerate
		stageHandlers[SkillDevStageValidate] = origValidate
		stageHandlers[SkillDevStageTestDesign] = origTestDesign
	}()

	stageHandlers[SkillDevStageGenerate] = &fakeStageHandler{nextStage: SkillDevStageValidate}
	stageHandlers[SkillDevStageValidate] = &fakeStageHandler{nextStage: SkillDevStageTestDesign}
	// 让链在 TEST_DESIGN 后进入 REVIEW 挂起点以停止
	stageHandlers[SkillDevStageTestDesign] = &fakeStageHandler{nextStage: SkillDevStageReview}

	tmpDir := t.TempDir()
	deps := &SkillDevDeps{
		StateStore:        NewStateStore(tmpDir),
		WorkspaceProvider: NewWorkspaceProvider(tmpDir),
	}

	// 模拟已在 PlanConfirm 挂起的状态
	state := NewSkillDevState("test-task-2")
	state.Stage = SkillDevStagePlanConfirm
	state.Mode = SkillDevTaskModeCreate
	plan := map[string]any{
		"skill_name":   "test-skill",
		"display_name": "测试技能",
	}
	state.Plan = plan

	pipeline := NewSkillDevPipeline("test-task-2", state, deps)

	events, err := pipeline.Resume(context.Background(), map[string]any{
		"action": "confirm",
		"plan":   plan,
	})
	if err != nil {
		t.Fatalf("Resume 返回错误: %v", err)
	}

	// Resume 后：PlanConfirm → OnResume → nextStage=Generate
	// Generate → Validate → TestDesign → Review(挂起点)
	if state.Stage != SkillDevStageReview {
		t.Errorf("期望阶段 review, 实际: %s", state.Stage)
	}

	// 应有事件输出
	if len(events) == 0 {
		t.Error("期望有事件输出，实际为空")
	}

	// plan_confirmed_at 应被设置
	if state.PlanConfirmedAt == nil {
		t.Error("plan_confirmed_at 未被设置")
	}
}

// TestPipeline_Resume_NotSuspendedPoint 测试从非挂起点恢复应报错。
func TestPipeline_Resume_NotSuspendedPoint(t *testing.T) {
	tmpDir := t.TempDir()
	deps := &SkillDevDeps{
		StateStore:        NewStateStore(tmpDir),
		WorkspaceProvider: NewWorkspaceProvider(tmpDir),
	}

	state := NewSkillDevState("test-task-3")
	state.Stage = SkillDevStageGenerate // 非挂起点

	pipeline := NewSkillDevPipeline("test-task-3", state, deps)

	_, err := pipeline.Resume(context.Background(), map[string]any{})
	if err == nil {
		t.Error("期望从非挂起点恢复返回错误，实际返回 nil")
	}
}

// TestPipeline_Run_ErrorHandling 测试阶段执行错误处理。
func TestPipeline_Run_ErrorHandling(t *testing.T) {
	origInit := stageHandlers[SkillDevStageInit]
	defer func() {
		stageHandlers[SkillDevStageInit] = origInit
	}()

	stageHandlers[SkillDevStageInit] = &fakeStageHandler{
		nextStage: SkillDevStagePlan,
		err:       context.Canceled,
	}

	tmpDir := t.TempDir()
	deps := &SkillDevDeps{
		StateStore:        NewStateStore(tmpDir),
		WorkspaceProvider: NewWorkspaceProvider(tmpDir),
	}

	state := NewSkillDevState("test-task-4")
	pipeline := NewSkillDevPipeline("test-task-4", state, deps)

	events, _ := pipeline.Run(context.Background())

	// 应进入 ERROR 阶段
	if state.Stage != SkillDevStageError {
		t.Errorf("期望阶段 error, 实际: %s", state.Stage)
	}

	// error 字段应被设置
	if state.Error == nil {
		t.Error("error 字段未设置")
	}

	// 应有 ERROR 事件
	hasError := false
	for _, evt := range events {
		if evt.EventType == SkillDevEventTypeError {
			hasError = true
		}
	}
	if !hasError {
		t.Error("缺少 ERROR 事件")
	}
}

// TestPipeline_Run_UnknownHandler 测试未知阶段处理。
func TestPipeline_Run_UnknownHandler(t *testing.T) {
	tmpDir := t.TempDir()
	deps := &SkillDevDeps{
		StateStore:        NewStateStore(tmpDir),
		WorkspaceProvider: NewWorkspaceProvider(tmpDir),
	}

	state := NewSkillDevState("test-task-5")
	state.Stage = SkillDevStage("unknown_stage")

	pipeline := NewSkillDevPipeline("test-task-5", state, deps)

	_, err := pipeline.Run(context.Background())
	if err == nil {
		t.Error("期望未知阶段返回错误，实际返回 nil")
	}
}

// TestPipeline_emit 测试 emit 方法。
func TestPipeline_emit(t *testing.T) {
	pipeline := &SkillDevPipeline{
		TaskID: "test-emit",
		events: make([]SkillDevEvent, 0),
	}

	pipeline.emit(SkillDevEventTypeStageChanged, map[string]any{
		"stage": "init",
	})

	if len(pipeline.events) != 1 {
		t.Fatalf("期望 1 个事件，实际: %d", len(pipeline.events))
	}

	evt := pipeline.events[0]
	if evt.EventType != SkillDevEventTypeStageChanged {
		t.Errorf("期望事件类型 stage_changed, 实际: %s", evt.EventType)
	}
	if evt.TaskID != "test-emit" {
		t.Errorf("期望 task_id test-emit, 实际: %s", evt.TaskID)
	}
	if evt.Payload["task_id"] != "test-emit" {
		t.Error("payload 中缺少 task_id")
	}
	if evt.Payload["stage"] != "init" {
		t.Error("payload 中缺少 stage")
	}
}

// TestPipeline_checkpoint 测试 checkpoint 持久化。
func TestPipeline_checkpoint(t *testing.T) {
	tmpDir := t.TempDir()
	deps := &SkillDevDeps{
		StateStore:        NewStateStore(tmpDir),
		WorkspaceProvider: NewWorkspaceProvider(tmpDir),
	}

	state := NewSkillDevState("test-cp")
	state.Stage = SkillDevStagePlan
	pipeline := NewSkillDevPipeline("test-cp", state, deps)

	err := pipeline.checkpoint()
	if err != nil {
		t.Fatalf("checkpoint 返回错误: %v", err)
	}

	// 验证 state.json 存在
	stateFile := filepath.Join(tmpDir, "test-cp", "state.json")
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		t.Error("state.json 未持久化")
	}

	// 验证可重新加载
	loaded, err := deps.StateStore.LoadState("test-cp")
	if err != nil {
		t.Fatalf("LoadState 返回错误: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadState 返回 nil")
	}
	if loaded.Stage != SkillDevStagePlan {
		t.Errorf("期望阶段 plan, 实际: %s", loaded.Stage)
	}
}
