package checkpointing

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

// TestEvolveCheckpoint_基本字段 测试 EvolveCheckpoint 结构体字段赋值
func TestEvolveCheckpoint_基本字段(t *testing.T) {
	seed := 42
	ckpt := &EvolveCheckpoint{
		Version:        "v1",
		RunID:          "run_abc123",
		Step:           map[string]int{"epoch": 5, "batch": 3},
		Best:           map[string]any{"best_score": 0.85},
		Seed:           &seed,
		OperatorsState: map[string]map[string]any{"op_1": {"param": "val"}},
		UpdaterState:   map[string]any{"key": "value"},
		SearcherState:  map[string]any{"search_key": "search_val"},
		LastMetrics:    map[string]any{"current_epoch_score": 0.78},
	}

	if ckpt.Version != "v1" {
		t.Errorf("Version = %s, 期望 v1", ckpt.Version)
	}
	if ckpt.RunID != "run_abc123" {
		t.Errorf("RunID = %s, 期望 run_abc123", ckpt.RunID)
	}
	if ckpt.Step["epoch"] != 5 {
		t.Errorf("Step[epoch] = %d, 期望 5", ckpt.Step["epoch"])
	}
	if ckpt.Step["batch"] != 3 {
		t.Errorf("Step[batch] = %d, 期望 3", ckpt.Step["batch"])
	}
	if ckpt.Best["best_score"] != 0.85 {
		t.Errorf("Best[best_score] = %v, 期望 0.85", ckpt.Best["best_score"])
	}
	if ckpt.Seed == nil || *ckpt.Seed != 42 {
		t.Errorf("Seed = %v, 期望 42", ckpt.Seed)
	}
	if len(ckpt.OperatorsState) != 1 {
		t.Errorf("OperatorsState 长度 = %d, 期望 1", len(ckpt.OperatorsState))
	}
	if ckpt.OperatorsState["op_1"]["param"] != "val" {
		t.Errorf("OperatorsState[op_1][param] = %v, 期望 val", ckpt.OperatorsState["op_1"]["param"])
	}
	if ckpt.UpdaterState["key"] != "value" {
		t.Errorf("UpdaterState[key] = %v, 期望 value", ckpt.UpdaterState["key"])
	}
	if ckpt.SearcherState["search_key"] != "search_val" {
		t.Errorf("SearcherState[search_key] = %v, 期望 search_val", ckpt.SearcherState["search_key"])
	}
	if ckpt.LastMetrics["current_epoch_score"] != 0.78 {
		t.Errorf("LastMetrics[current_epoch_score] = %v, 期望 0.78", ckpt.LastMetrics["current_epoch_score"])
	}
}

// TestEvolveCheckpoint_Seed为nil 测试 Seed 字段为 nil 的情况
func TestEvolveCheckpoint_Seed为nil(t *testing.T) {
	ckpt := &EvolveCheckpoint{
		Version: "v1",
		RunID:   "run_test",
		Step:    map[string]int{"epoch": 0, "batch": 0},
		Best:    map[string]any{"best_score": 0.0},
		Seed:    nil,
	}
	if ckpt.Seed != nil {
		t.Errorf("Seed 应为 nil, 实际为 %v", ckpt.Seed)
	}
}

// TestEvolveCheckpoint_空StepAndBest 测试 Step 和 Best 为空 map
func TestEvolveCheckpoint_空StepAndBest(t *testing.T) {
	ckpt := &EvolveCheckpoint{
		Version: "v2",
		Step:    map[string]int{},
		Best:    map[string]any{},
	}
	if len(ckpt.Step) != 0 {
		t.Errorf("Step 应为空 map")
	}
	if len(ckpt.Best) != 0 {
		t.Errorf("Best 应为空 map")
	}
}
