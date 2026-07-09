package skilldev

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewStateStore(t *testing.T) {
	store := NewStateStore("/tmp/test_skilldev")
	if store.baseDir != "/tmp/test_skilldev" {
		t.Errorf("期望 baseDir=/tmp/test_skilldev, 实际 %s", store.baseDir)
	}
}

func TestStateStore_SaveAndLoad(t *testing.T) {
	baseDir := t.TempDir()
	store := NewStateStore(baseDir)

	state := NewSkillDevState("test_task_1")
	state.Stage = SkillDevStagePlan
	state.Mode = SkillDevTaskModeModify
	state.Iteration = 2
	skillMD := "existing content"
	state.ExistingSkillMD = &skillMD

	// 保存
	if err := store.SaveState("test_task_1", state); err != nil {
		t.Fatalf("SaveState 失败: %v", err)
	}

	// 验证文件存在
	stateFile := filepath.Join(baseDir, "test_task_1", "state.json")
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		t.Fatal("state.json 未创建")
	}

	// 加载
	loaded, err := store.LoadState("test_task_1")
	if err != nil {
		t.Fatalf("LoadState 失败: %v", err)
	}
	if loaded == nil {
		t.Fatal("期望非 nil 状态")
	}
	if loaded.TaskID != "test_task_1" {
		t.Errorf("期望 TaskID=test_task_1, 实际 %s", loaded.TaskID)
	}
	if loaded.Stage != SkillDevStagePlan {
		t.Errorf("期望 Stage=plan, 实际 %s", loaded.Stage)
	}
	if loaded.Mode != SkillDevTaskModeModify {
		t.Errorf("期望 Mode=modify, 实际 %s", loaded.Mode)
	}
	if loaded.Iteration != 2 {
		t.Errorf("期望 Iteration=2, 实际 %d", loaded.Iteration)
	}
	if loaded.ExistingSkillMD == nil || *loaded.ExistingSkillMD != skillMD {
		t.Errorf("期望 ExistingSkillMD=%s, 实际 %v", skillMD, loaded.ExistingSkillMD)
	}
}

func TestStateStore_SaveState_更新时间戳(t *testing.T) {
	baseDir := t.TempDir()
	store := NewStateStore(baseDir)

	state := NewSkillDevState("test_task")
	// 设置一个旧时间戳以验证 SaveState 更新它
	state.UpdatedAt = "2000-01-01T00:00:00Z"

	if err := store.SaveState("test_task", state); err != nil {
		t.Fatalf("SaveState 失败: %v", err)
	}

	// SaveState 内部调用 Touch，UpdatedAt 应已更新
	if state.UpdatedAt == "2000-01-01T00:00:00Z" {
		t.Error("期望 SaveState 更新 UpdatedAt")
	}
}

func TestStateStore_LoadState_不存在(t *testing.T) {
	baseDir := t.TempDir()
	store := NewStateStore(baseDir)

	loaded, err := store.LoadState("nonexistent")
	if err != nil {
		t.Fatalf("LoadState 失败: %v", err)
	}
	if loaded != nil {
		t.Error("期望 nil 状态")
	}
}

func TestStateStore_ListTasks(t *testing.T) {
	baseDir := t.TempDir()
	store := NewStateStore(baseDir)

	// 空目录
	tasks, err := store.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks 失败: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("期望 0 个任务, 实际 %d", len(tasks))
	}

	// 创建几个任务
	for i := 0; i < 3; i++ {
		state := NewSkillDevState("task_" + string(rune('0'+i)))
		if err := store.SaveState("task_"+string(rune('0'+i)), state); err != nil {
			t.Fatalf("SaveState 失败: %v", err)
		}
	}

	tasks, err = store.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks 失败: %v", err)
	}
	if len(tasks) != 3 {
		t.Errorf("期望 3 个任务, 实际 %d", len(tasks))
	}
}

func TestStateStore_ListTasks_目录不存在(t *testing.T) {
	store := NewStateStore("/nonexistent/path")

	tasks, err := store.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks 失败: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("期望 0 个任务, 实际 %d", len(tasks))
	}
}

func TestStateStore_完整状态序列化(t *testing.T) {
	baseDir := t.TempDir()
	store := NewStateStore(baseDir)

	state := NewSkillDevState("full_test")
	state.Stage = SkillDevStageEvaluate
	state.Mode = SkillDevTaskModeCreateWithResources
	state.Iteration = 3
	state.Input = map[string]any{"query": "test query", "resources": []any{"file.txt"}}
	state.ReferenceTexts = []string{"ref1", "ref2"}
	skillMD := "existing SKILL.md"
	state.ExistingSkillMD = &skillMD
	planConfirmedAt := "2025-01-01T00:00:00Z"
	state.PlanConfirmedAt = &planConfirmedAt
	state.Plan = map[string]any{"description": "test plan", "steps": []any{"step1", "step2"}}
	state.Evals = map[string]any{"evals": "data"}
	state.EvalResults = map[string]any{"benchmark": "result", "pass_rate": 0.9}
	state.FeedbackHistory = []map[string]any{{"iteration": 0, "feedback": "fix bugs"}}
	state.DescOptimizeResult = map[string]any{"best_description": "optimized"}
	zipPath := "/tmp/test.zip"
	state.ZipPath = &zipPath
	state.ZipSize = 2048
	errMsg := "some error"
	state.Error = &errMsg

	// 保存
	if err := store.SaveState("full_test", state); err != nil {
		t.Fatalf("SaveState 失败: %v", err)
	}

	// 验证 JSON 格式
	stateFile := filepath.Join(baseDir, "full_test", "state.json")
	data, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("读取状态文件失败: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("解析 JSON 失败: %v", err)
	}
	if m["task_id"] != "full_test" {
		t.Errorf("期望 task_id=full_test, 实际 %v", m["task_id"])
	}

	// 加载并验证
	loaded, err := store.LoadState("full_test")
	if err != nil {
		t.Fatalf("LoadState 失败: %v", err)
	}
	if loaded.TaskID != "full_test" {
		t.Errorf("期望 TaskID=full_test, 实际 %s", loaded.TaskID)
	}
	if loaded.Stage != SkillDevStageEvaluate {
		t.Errorf("期望 Stage=evaluate, 实际 %s", loaded.Stage)
	}
	if loaded.ZipSize != 2048 {
		t.Errorf("期望 ZipSize=2048, 实际 %d", loaded.ZipSize)
	}
}

func TestStateStore_空切片序列化(t *testing.T) {
	baseDir := t.TempDir()
	store := NewStateStore(baseDir)

	state := NewSkillDevState("empty_test")
	// 确保空切片正确序列化
	if err := store.SaveState("empty_test", state); err != nil {
		t.Fatalf("SaveState 失败: %v", err)
	}

	loaded, err := store.LoadState("empty_test")
	if err != nil {
		t.Fatalf("LoadState 失败: %v", err)
	}
	if len(loaded.ReferenceTexts) != 0 {
		t.Errorf("期望空 ReferenceTexts, 实际 %v", loaded.ReferenceTexts)
	}
	if len(loaded.FeedbackHistory) != 0 {
		t.Errorf("期望空 FeedbackHistory, 实际 %v", loaded.FeedbackHistory)
	}
}
