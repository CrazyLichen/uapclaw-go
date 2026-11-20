package utils

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestProgressStep 对齐 Python TestProgressStep（4 用例）。
func TestProgressStep(t *testing.T) {
	// 子用例 1: test_progress_step_creation
	t.Run("创建默认值", func(t *testing.T) {
		step := NewProgressStep(ProgressStageInitializing, ProgressStatusRunning, "Starting build...")
		if step.Stage != ProgressStageInitializing {
			t.Errorf("Stage = %v, 期望 initializing", step.Stage)
		}
		if step.Status != ProgressStatusRunning {
			t.Errorf("Status = %v, 期望 running", step.Status)
		}
		if step.Message != "Starting build..." {
			t.Errorf("Message = %q, 期望 %q", step.Message, "Starting build...")
		}
		if step.Details == nil || len(step.Details) != 0 {
			t.Errorf("Details = %v, 期望空 map 非 nil", step.Details)
		}
		if step.Duration != nil {
			t.Errorf("Duration = %v, 期望 nil", *step.Duration)
		}
		if step.Error != nil {
			t.Errorf("Error = %v, 期望 nil", *step.Error)
		}
		if step.Timestamp.IsZero() {
			t.Error("Timestamp 期望非零值")
		}
	})

	// 子用例 2: test_progress_step_with_details
	t.Run("携带细节", func(t *testing.T) {
		step := NewProgressStep(ProgressStageClarifying, ProgressStatusSuccess, "Completed", WithStepDetails(map[string]any{"key": "value"}))
		if v, ok := step.Details["key"]; !ok || v != "value" {
			t.Errorf("Details[key] = %v, 期望 value", step.Details)
		}
	})

	// 子用例 3: test_progress_step_to_dict
	t.Run("ToDict", func(t *testing.T) {
		step := NewProgressStep(ProgressStageInitializing, ProgressStatusRunning, "Starting",
			WithStepDetails(map[string]any{"count": 1}), WithStepDuration(1.5))
		result := step.ToDict()
		if result["stage"] != "initializing" {
			t.Errorf("stage = %v, 期望 initializing", result["stage"])
		}
		if result["status"] != "running" {
			t.Errorf("status = %v, 期望 running", result["status"])
		}
		if result["message"] != "Starting" {
			t.Errorf("message = %v, 期望 Starting", result["message"])
		}
		if _, ok := result["timestamp"].(string); !ok {
			t.Errorf("timestamp 期望 string, got %T", result["timestamp"])
		}
		if result["duration"] != 1.5 {
			t.Errorf("duration = %v, 期望 1.5", result["duration"])
		}
		if ts, ok := result["timestamp"].(string); ok {
			if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
				t.Errorf("timestamp %q 非 RFC3339 可解析: %v", ts, err)
			}
		}
	})

	// 子用例 4: test_progress_step_with_error
	t.Run("携带错误", func(t *testing.T) {
		step := NewProgressStep(ProgressStageError, ProgressStatusFailed, "Failed", WithStepError("Test error"))
		if step.Error == nil || *step.Error != "Test error" {
			t.Error("Error 期望 Test error")
		}
		result := step.ToDict()
		if result["error"] != "Test error" {
			t.Errorf("to_dict error = %v, 期望 Test error", result["error"])
		}
	})
}

// TestBuildProgress 对齐 Python TestBuildProgress（3 用例）。
func TestBuildProgress(t *testing.T) {
	// 子用例 1: test_build_progress_creation
	t.Run("创建默认值", func(t *testing.T) {
		progress := NewBuildProgress("test_session", "llm_agent", ProgressStageInitializing, ProgressStatusPending, "Initializing...")
		if progress.SessionID != "test_session" {
			t.Errorf("SessionID = %v, 期望 test_session", progress.SessionID)
		}
		if progress.AgentType != "llm_agent" {
			t.Errorf("AgentType = %v, 期望 llm_agent", progress.AgentType)
		}
		if progress.CurrentStage != ProgressStageInitializing {
			t.Errorf("CurrentStage = %v, 期望 initializing", progress.CurrentStage)
		}
		if progress.CurrentStatus != ProgressStatusPending {
			t.Errorf("CurrentStatus = %v, 期望 pending", progress.CurrentStatus)
		}
		if progress.Steps == nil || len(progress.Steps) != 0 {
			t.Errorf("Steps = %v, 期望空切片非 nil", progress.Steps)
		}
		if progress.OverallProgress != 0.0 {
			t.Errorf("OverallProgress = %v, 期望 0.0", progress.OverallProgress)
		}
	})

	// 子用例 2: test_build_progress_to_dict
	t.Run("ToDict", func(t *testing.T) {
		progress := NewBuildProgress("test_session", "workflow", ProgressStageCompleted, ProgressStatusSuccess, "Done")
		result := progress.ToDict()
		if result["session_id"] != "test_session" {
			t.Errorf("session_id = %v, 期望 test_session", result["session_id"])
		}
		if result["agent_type"] != "workflow" {
			t.Errorf("agent_type = %v, 期望 workflow", result["agent_type"])
		}
		if result["current_stage"] != "completed" {
			t.Errorf("current_stage = %v, 期望 completed", result["current_stage"])
		}
		if result["current_status"] != "success" {
			t.Errorf("current_status = %v, 期望 success", result["current_status"])
		}
		if result["current_message"] != "Done" {
			t.Errorf("current_message = %v, 期望 Done", result["current_message"])
		}
		for _, key := range []string{"start_time", "last_update_time"} {
			if ts, ok := result[key].(string); ok {
				if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
					t.Errorf("%s %q 非 RFC3339 可解析: %v", key, ts, err)
				}
			} else {
				t.Errorf("%s 期望 string, got %T", key, result[key])
			}
		}
	})

	// 子用例 3: test_build_progress_with_steps
	t.Run("携带步骤", func(t *testing.T) {
		step1 := NewProgressStep(ProgressStageInitializing, ProgressStatusSuccess, "Init done")
		step2 := NewProgressStep(ProgressStageClarifying, ProgressStatusRunning, "Clarifying...")
		progress := NewBuildProgress("test", "llm_agent", ProgressStageClarifying, ProgressStatusRunning, "Processing",
			WithProgressSteps([]ProgressStep{step1, step2}))
		result := progress.ToDict()
		steps, ok := result["steps"].([]map[string]any)
		if !ok {
			t.Fatalf("steps 期望 []map[string]any, got %T", result["steps"])
		}
		if len(steps) != 2 {
			t.Errorf("steps len = %d, 期望 2", len(steps))
		}
	})
}

// TestProgressReporter 对齐 Python TestProgressReporter（8 用例）。
func TestProgressReporter(t *testing.T) {
	// 子用例 1: test_progress_reporter_creation
	t.Run("创建", func(t *testing.T) {
		reporter := NewProgressReporter("session_123", "llm_agent")
		if reporter.sessionID != "session_123" {
			t.Errorf("sessionID = %v, 期望 session_123", reporter.sessionID)
		}
		if reporter.agentType != "llm_agent" {
			t.Errorf("agentType = %v, 期望 llm_agent", reporter.agentType)
		}
		if reporter.progress.CurrentStage != ProgressStageInitializing {
			t.Errorf("CurrentStage = %v, 期望 initializing", reporter.progress.CurrentStage)
		}
		if reporter.progress.CurrentStatus != ProgressStatusPending {
			t.Errorf("CurrentStatus = %v, 期望 pending", reporter.progress.CurrentStatus)
		}
	})

	// 子用例 2: test_add_callback
	t.Run("添加回调", func(t *testing.T) {
		reporter := NewProgressReporter("session_123", "llm_agent")
		var called []string
		cancel := reporter.AddCallback(func(p *BuildProgress) {
			called = append(called, p.SessionID)
		})
		_ = cancel
		reporter.StartStage(ProgressStageClarifying, "Test message")
		if len(called) != 1 {
			t.Errorf("回调调用次数 = %d, 期望 1", len(called))
		}
		if len(called) > 0 && called[0] != "session_123" {
			t.Errorf("回调收到 sessionID = %q, 期望 session_123", called[0])
		}
	})

	// 子用例 3: test_remove_callback（cancel 语义改写）
	t.Run("取消回调", func(t *testing.T) {
		reporter := NewProgressReporter("session_123", "llm_agent")
		var callCount int
		cancel := reporter.AddCallback(func(p *BuildProgress) {
			callCount++
		})
		cancel()
		reporter.StartStage(ProgressStageClarifying, "Test message")
		if callCount != 0 {
			t.Errorf("cancel 后调用次数 = %d, 期望 0", callCount)
		}
	})

	// 子用例 4: test_start_stage
	t.Run("开始阶段", func(t *testing.T) {
		reporter := NewProgressReporter("session_123", "llm_agent")
		reporter.StartStage(ProgressStageClarifying, "Clarifying requirements...",
			WithStartStageDetails(map[string]any{"query_length": 100}),
			WithStartStageProgress(20.0))
		p := reporter.GetProgress()
		if p.CurrentStage != ProgressStageClarifying {
			t.Errorf("CurrentStage = %v, 期望 clarifying", p.CurrentStage)
		}
		if p.CurrentStatus != ProgressStatusRunning {
			t.Errorf("CurrentStatus = %v, 期望 running", p.CurrentStatus)
		}
		if p.CurrentMessage != "Clarifying requirements..." {
			t.Errorf("CurrentMessage = %v, 期望 Clarifying requirements...", p.CurrentMessage)
		}
		if p.OverallProgress != 20.0 {
			t.Errorf("OverallProgress = %v, 期望 20.0", p.OverallProgress)
		}
	})

	// 子用例 5: test_complete_stage
	t.Run("完成阶段", func(t *testing.T) {
		reporter := NewProgressReporter("session_123", "llm_agent")
		reporter.StartStage(ProgressStageClarifying, "Clarifying...")
		reporter.CompleteStage(WithCompleteStageMessage("Clarification complete"),
			WithCompleteStageDetails(map[string]any{"found": 5}))
		p := reporter.GetProgress()
		if p.CurrentStatus != ProgressStatusSuccess {
			t.Errorf("CurrentStatus = %v, 期望 success", p.CurrentStatus)
		}
	})

	// 子用例 6: test_fail_stage
	t.Run("失败阶段", func(t *testing.T) {
		reporter := NewProgressReporter("session_123", "llm_agent")
		reporter.StartStage(ProgressStageClarifying, "Clarifying...")
		reporter.FailStage("Test error", WithFailStageMessage("Error occurred"))
		p := reporter.GetProgress()
		if p.CurrentStatus != ProgressStatusFailed {
			t.Errorf("CurrentStatus = %v, 期望 failed", p.CurrentStatus)
		}
		if p.Error == nil || *p.Error != "Test error" {
			t.Error("progress.Error 期望 Test error")
		}
	})

	// 子用例 7: test_complete
	t.Run("整体完成", func(t *testing.T) {
		reporter := NewProgressReporter("session_123", "llm_agent")
		reporter.StartStage(ProgressStageCompleted, "Finishing...")
		reporter.Complete("Build completed successfully")
		p := reporter.GetProgress()
		if p.CurrentStage != ProgressStageCompleted {
			t.Errorf("CurrentStage = %v, 期望 completed", p.CurrentStage)
		}
		if p.CurrentStatus != ProgressStatusSuccess {
			t.Errorf("CurrentStatus = %v, 期望 success", p.CurrentStatus)
		}
		if p.OverallProgress != 100.0 {
			t.Errorf("OverallProgress = %v, 期望 100.0", p.OverallProgress)
		}
	})

	// 子用例 8: test_multiple_stages
	t.Run("多阶段推进", func(t *testing.T) {
		reporter := NewProgressReporter("session_123", "llm_agent")

		reporter.StartStage(ProgressStageInitializing, "Init...")
		if reporter.GetProgress().CurrentStage != ProgressStageInitializing {
			t.Error("第 1 段后 CurrentStage 期望 initializing")
		}

		reporter.CompleteStage(WithCompleteStageMessage("Init done"))
		reporter.StartStage(ProgressStageClarifying, "Clarifying...")
		if reporter.GetProgress().CurrentStage != ProgressStageClarifying {
			t.Error("第 2 段后 CurrentStage 期望 clarifying")
		}

		reporter.CompleteStage(WithCompleteStageMessage("Clarify done"))
		reporter.StartStage(ProgressStageGeneratingConfig, "Generating...")
		if reporter.GetProgress().CurrentStage != ProgressStageGeneratingConfig {
			t.Error("第 3 段后 CurrentStage 期望 generating_config")
		}
	})
}

// ──────────────────────────── system 回填（TestProgressIntegration） ────────────────────────────

// TestProgressIntegration_回调上报 对齐 Python system test_progress_reporter_with_callbacks。
func TestProgressIntegration_回调上报(t *testing.T) {
	reporter := NewProgressReporter("test_session", "llm_agent")
	var events int
	reporter.AddCallback(func(p *BuildProgress) { events++ })
	reporter.StartStage(ProgressStageInitializing, "Starting...")
	reporter.CompleteStage(WithCompleteStageMessage("Done"))
	if events < 1 {
		t.Errorf("回调事件数 = %d, 期望 >= 1", events)
	}
}

// TestProgressIntegration_序列化 对齐 Python system test_build_progress_serialization。
func TestProgressIntegration_序列化(t *testing.T) {
	progress := NewBuildProgress("test_session", "llm_agent", ProgressStageInitializing, ProgressStatusPending, "Initializing")
	progressDict := progress.ToDict()
	if _, ok := progressDict["session_id"]; !ok {
		t.Error("ToDict 缺 session_id 键")
	}
	if _, ok := progressDict["steps"]; !ok {
		t.Error("ToDict 缺 steps 键")
	}
	if progressDict["session_id"] != "test_session" {
		t.Errorf("session_id = %v, 期望 test_session", progressDict["session_id"])
	}
}

// TestProgressIntegration_管理器 对齐 Python system test_progress_manager_integration。
func TestProgressIntegration_管理器(t *testing.T) {
	manager := NewProgressManager()
	reporter := manager.CreateReporter("session_001", "llm_agent")
	if reporter == nil {
		t.Fatal("CreateReporter 返回 nil")
	}
	if reporter.sessionID != "session_001" {
		t.Errorf("sessionID = %v, 期望 session_001", reporter.sessionID)
	}
	retrieved := manager.GetReporter("session_001")
	// Python: assert retrieved is reporter —— Go 等价：同指针
	if retrieved != reporter {
		t.Error("GetReporter 与 CreateReporter 期望同一实例（指针相等）")
	}
}

// TestProgressIntegration_完整工作流 对齐 Python system test_progress_reporter_complete_workflow。
func TestProgressIntegration_完整工作流(t *testing.T) {
	reporter := NewProgressReporter("test_session", "llm_agent")
	reporter.StartStage(ProgressStageInitializing, "Starting...")
	reporter.CompleteStage(WithCompleteStageMessage("Initialization done"))
	reporter.StartStage(ProgressStageClarifying, "Clarifying requirements...")
	reporter.CompleteStage(WithCompleteStageMessage("Clarification done"))
	reporter.Complete("Build completed")
	progress := reporter.GetProgress()
	if progress.CurrentStage != ProgressStageCompleted {
		t.Errorf("CurrentStage = %v, 期望 completed", progress.CurrentStage)
	}
	if progress.OverallProgress != 100.0 {
		t.Errorf("OverallProgress = %v, 期望 100.0", progress.OverallProgress)
	}
}

// ──────────────────────────── Go 增强用例（并发+隐性语义，Python 无对应） ────────────────────────────

// TestProgressReporter_并发阶段操作 验证方法级锁（-race 必跑）。
func TestProgressReporter_并发阶段操作(t *testing.T) {
	reporter := NewProgressReporter("race_session", "llm_agent")
	reporter.AddCallback(func(p *BuildProgress) {})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			stage := []ProgressStage{ProgressStageClarifying, ProgressStageGeneratingConfig, ProgressStageTransformingDSL}[n%3]
			reporter.StartStage(stage, "concurrent")
			reporter.UpdateStage(WithUpdateStageMessage("tick"))
			reporter.CompleteStage(WithCompleteStageMessage("done"))
		}(i)
	}
	wg.Wait()
	p := reporter.GetProgress()
	if len(p.Steps) != 10 {
		t.Errorf("Steps len = %d, 期望 10（10 goroutine 各 1 次 StartStage，阶段按 n%%3 轮转）", len(p.Steps))
	}
}

// TestProgressReporter_并发回调注册 验证回调切片锁安全。
func TestProgressReporter_并发回调注册(t *testing.T) {
	reporter := NewProgressReporter("race_cb", "llm_agent")
	var wg sync.WaitGroup
	cancels := make([]func(), 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			cancels[n] = reporter.AddCallback(func(p *BuildProgress) {})
		}(i)
	}
	wg.Wait()
	reporter.StartStage(ProgressStageClarifying, "after add")
	for _, cancel := range cancels {
		if cancel != nil {
			cancel()
		}
	}
	reporter.StartStage(ProgressStageGeneratingConfig, "after cancel")
}

// TestProgressReporter_取消幂等 cancel 二次调用安全。
func TestProgressReporter_取消幂等(t *testing.T) {
	reporter := NewProgressReporter("idem", "llm_agent")
	var callCount int
	cancel := reporter.AddCallback(func(p *BuildProgress) { callCount++ })
	cancel()
	cancel() // 二次调用不得 panic
	reporter.StartStage(ProgressStageClarifying, "msg")
	if callCount != 0 {
		t.Errorf("cancel 后调用次数 = %d, 期望 0", callCount)
	}
}

// TestProgressReporter_回调panic不中断 对齐 Python except Exception 继续遍历语义。
func TestProgressReporter_回调panic不中断(t *testing.T) {
	reporter := NewProgressReporter("panic", "llm_agent")
	var secondCalled bool
	reporter.AddCallback(func(p *BuildProgress) { panic("boom") })
	reporter.AddCallback(func(p *BuildProgress) { secondCalled = true })
	reporter.StartStage(ProgressStageClarifying, "msg")
	if !secondCalled {
		t.Error("首个回调 panic 后第二个回调仍应被调用")
	}
}

// TestProgressReporter_回调内读进度 锁内安全窗口验证。
func TestProgressReporter_回调内读进度(t *testing.T) {
	reporter := NewProgressReporter("cbread", "llm_agent")
	var stageSeen string
	var dictOK bool
	reporter.AddCallback(func(p *BuildProgress) {
		stageSeen = string(p.CurrentStage)
		_ = p.ToDict() // 回调内走 ToDict 快照合法
		dictOK = true
	})
	reporter.StartStage(ProgressStageClarifying, "msg")
	if stageSeen != "clarifying" {
		t.Errorf("回调内 CurrentStage = %q, 期望 clarifying", stageSeen)
	}
	if !dictOK {
		t.Error("回调内 ToDict 未执行")
	}
}

// TestProgressManager_并发注册 manager 锁验证。
func TestProgressManager_并发注册(t *testing.T) {
	manager := NewProgressManager()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sid := fmt.Sprintf("s_%d", n%5)
			manager.CreateReporter(sid, "llm_agent")
			manager.GetReporter(sid)
			manager.GetProgress(sid)
		}(i)
	}
	wg.Wait()
	if len(manager.reporters) != 5 {
		t.Errorf("reporters 数 = %d, 期望 5（10 goroutine 折叠 5 session）", len(manager.reporters))
	}
}

// TestProgressReporter_自动进度计算 calculateProgress 三分支。
func TestProgressReporter_自动进度计算(t *testing.T) {
	if got := calculateProgress("llm_agent", ProgressStageClarifying); got != 20.0 {
		t.Errorf("llm_agent/clarifying = %v, 期望 20.0", got)
	}
	if got := calculateProgress("workflow", ProgressStageGeneratingDL); got != 45.0 {
		t.Errorf("workflow/generating_dl = %v, 期望 45.0", got)
	}
	if got := calculateProgress("llm_agent", ProgressStageGeneratingWorkflowDesign); got != 0.0 {
		t.Errorf("llm_agent/未映射阶段 = %v, 期望 0.0", got)
	}
	if got := calculateProgress("workflow", ProgressStageClarifying); got != 0.0 {
		t.Errorf("workflow/未映射阶段（clarifying） = %v, 期望 0.0", got)
	}
	if got := calculateProgress("unknown_type", ProgressStageInitializing); got != 0.0 {
		t.Errorf("未知 agentType = %v, 期望 0.0", got)
	}
}

// TestProgressStep_ToDict_空指针形态 duration/error nil → ToDict 键为 nil。
func TestProgressStep_ToDict_空指针形态(t *testing.T) {
	step := NewProgressStep(ProgressStageInitializing, ProgressStatusPending, "m")
	result := step.ToDict()
	if result["duration"] != nil {
		t.Errorf("duration = %v, 期望 nil", result["duration"])
	}
	if result["error"] != nil {
		t.Errorf("error = %v, 期望 nil", result["error"])
	}
}

// TestProgressReporter_隐性自动收尾 spec §4b-1：StartStage 自动结束前序阶段。
// Python 语义（实测）：前序为 INITIALIZING（构造器初值或显式 INITIALIZING 步）不触发收尾；
// 前序为非 INITIALIZING 阶段才触发。三段式：INIT→INIT 不收尾、INIT→CLARIFY 不收尾、
// CLARIFY→GENERATING 触发收尾（末步 SUCCESS+duration 计算）。
func TestProgressReporter_隐性自动收尾(t *testing.T) {
	reporter := NewProgressReporter("autoend", "llm_agent")
	reporter.StartStage(ProgressStageInitializing, "Init...")   // 前序=构造器 INITIALIZING：不收尾
	reporter.StartStage(ProgressStageInitializing, "Reinit...") // 前序=INITIALIZING：不收尾
	reporter.StartStage(ProgressStageClarifying, "Clarify...")  // 前序=INITIALIZING：不收尾
	p1 := reporter.GetProgress()
	if p1.Steps[0].Status != ProgressStatusRunning || p1.Steps[1].Status != ProgressStatusRunning {
		t.Error("前序为 INITIALIZING 时不应触发自动收尾（Python L194 条件为假）")
	}

	reporter.StartStage(ProgressStageGeneratingConfig, "Gen...") // 前序=CLARIFYING：触发收尾
	p2 := reporter.GetProgress()
	if len(p2.Steps) != 4 {
		t.Fatalf("Steps len = %d, 期望 4", len(p2.Steps))
	}
	if p2.Steps[2].Status != ProgressStatusSuccess {
		t.Errorf("CLARIFYING 步骤 Status = %v, 期望 success（自动收尾触发）", p2.Steps[2].Status)
	}
	if p2.Steps[2].Duration == nil {
		t.Error("被收尾步骤 Duration 期望非 nil（收尾时计算）")
	}
	if p2.Steps[3].Status != ProgressStatusRunning {
		t.Errorf("新步骤 Status = %v, 期望 running", p2.Steps[3].Status)
	}
}

// TestProgressManager_幂等创建 spec §4b-7。
func TestProgressManager_幂等创建(t *testing.T) {
	manager := NewProgressManager()
	r1 := manager.CreateReporter("dup", "llm_agent")
	r2 := manager.CreateReporter("dup", "workflow") // agentType 被忽略
	if r1 != r2 {
		t.Error("同 sessionID 两次 CreateReporter 期望同实例")
	}
	if r1.agentType != "llm_agent" {
		t.Errorf("既有实例 agentType = %v, 期望 llm_agent（首次值保留）", r1.agentType)
	}
}

// TestGetProgressManager_单例 两次调用同指针。
func TestGetProgressManager_单例(t *testing.T) {
	m1 := GetProgressManager()
	m2 := GetProgressManager()
	if m1 != m2 {
		t.Error("GetProgressManager 两次调用期望同指针")
	}
}

// TestProgressReporter_更新阶段语义 spec §4b-3：不建新步骤。
func TestProgressReporter_更新阶段语义(t *testing.T) {
	reporter := NewProgressReporter("upd", "llm_agent")
	reporter.StartStage(ProgressStageClarifying, "start",
		WithStartStageDetails(map[string]any{"a": 1}))
	reporter.UpdateStage(WithUpdateStageMessage("updated"),
		WithUpdateStageDetails(map[string]any{"b": 2}))
	p := reporter.GetProgress()
	if len(p.Steps) != 1 {
		t.Fatalf("Steps len = %d, 期望 1（update 不建新步骤）", len(p.Steps))
	}
	if p.CurrentMessage != "updated" {
		t.Errorf("CurrentMessage = %v, 期望 updated", p.CurrentMessage)
	}
	if v, ok := p.Steps[0].Details["b"]; !ok || v != 2 {
		t.Error("末步骤 details 应合并 b=2")
	}
	if v, ok := p.Steps[0].Details["a"]; !ok || v != 1 {
		t.Error("末步骤 details 应保留 a=1")
	}
}

// TestProgressReporter_警告阶段语义 spec §4b-4：不建新步骤+warning 写入细节。
func TestProgressReporter_警告阶段语义(t *testing.T) {
	reporter := NewProgressReporter("warn", "llm_agent")
	reporter.StartStage(ProgressStageGeneratingConfig, "gen")
	reporter.WarnStage("slow response")
	p := reporter.GetProgress()
	if len(p.Steps) != 1 {
		t.Fatalf("Steps len = %d, 期望 1（warn 不建新步骤）", len(p.Steps))
	}
	if p.Steps[0].Status != ProgressStatusWarning {
		t.Errorf("末步骤 Status = %v, 期望 warning", p.Steps[0].Status)
	}
	if p.Steps[0].Details["warning"] != "slow response" {
		t.Errorf("details[warning] = %v, 期望 slow response", p.Steps[0].Details["warning"])
	}
	if p.CurrentStatus != ProgressStatusWarning {
		t.Errorf("CurrentStatus = %v, 期望 warning", p.CurrentStatus)
	}
}

// TestProgressManager_移除上报器 RemoveReporter + GetProgress nil 分支。
func TestProgressManager_移除上报器(t *testing.T) {
	manager := NewProgressManager()
	manager.CreateReporter("rm", "llm_agent")
	if manager.GetProgress("rm") == nil {
		t.Fatal("移除前 GetProgress 期望非 nil")
	}
	manager.RemoveReporter("rm")
	if manager.GetReporter("rm") != nil {
		t.Error("移除后 GetReporter 期望 nil")
	}
	if manager.GetProgress("rm") != nil {
		t.Error("移除后 GetProgress 期望 nil")
	}
	// 不存在的 session（GetReporter nil 分支）
	manager.RemoveReporter("nonexistent") // no-op 不 panic
	if manager.GetProgress("nonexistent") != nil {
		t.Error("不存在 session 的 GetProgress 期望 nil")
	}
}

// TestProgressReporter_更新阶段进度选项 WithUpdateStageProgress 全形态。
func TestProgressReporter_更新阶段进度选项(t *testing.T) {
	reporter := NewProgressReporter("upd_p", "llm_agent")
	reporter.StartStage(ProgressStageClarifying, "start")
	reporter.UpdateStage(WithUpdateStageProgress(50.0))
	p := reporter.GetProgress()
	if p.OverallProgress != 50.0 {
		t.Errorf("OverallProgress = %v, 期望 50.0", p.OverallProgress)
	}
	// UpdateStage 无 details 选项分支（cfg.details == nil）
	reporter.UpdateStage(WithUpdateStageMessage("only msg"))
	if p2 := reporter.GetProgress(); p2.CurrentMessage != "only msg" {
		t.Errorf("CurrentMessage = %v, 期望 only msg", p2.CurrentMessage)
	}
}

// TestProgressReporter_失败阶段细节 WithFailStageDetails 分支。
func TestProgressReporter_失败阶段细节(t *testing.T) {
	reporter := NewProgressReporter("fail_d", "llm_agent")
	reporter.StartStage(ProgressStageGeneratingConfig, "gen")
	reporter.FailStage("boom", WithFailStageDetails(map[string]any{"retry": false}))
	p := reporter.GetProgress()
	if v, ok := p.Steps[0].Details["retry"]; !ok || v != false {
		t.Error("末步骤 details 应含 retry=false")
	}
	if p.Steps[0].Error == nil || *p.Steps[0].Error != "boom" {
		t.Error("末步骤 error 期望 boom")
	}
}

// TestProgressReporter_警告阶段全选项 WithWarnStageMessage+WithWarnStageDetails 分支。
func TestProgressReporter_警告阶段全选项(t *testing.T) {
	reporter := NewProgressReporter("warn_full", "llm_agent")
	reporter.StartStage(ProgressStageTransformingDSL, "tf")
	reporter.WarnStage("timeout", WithWarnStageMessage("降级继续"),
		WithWarnStageDetails(map[string]any{"level": "soft"}))
	p := reporter.GetProgress()
	if p.CurrentMessage != "降级继续" {
		t.Errorf("CurrentMessage = %v, 期望 降级继续", p.CurrentMessage)
	}
	if p.Steps[0].Details["warning"] != "timeout" {
		t.Errorf("details[warning] = %v, 期望 timeout", p.Steps[0].Details["warning"])
	}
	if p.Steps[0].Details["level"] != "soft" {
		t.Error("details 应合并 level=soft")
	}
}

// TestProgressReporter_无步骤时防御 UpdateStage/WarnStage steps 空分支。
func TestProgressReporter_无步骤时防御(t *testing.T) {
	reporter := NewProgressReporter("nostep", "llm_agent")
	// 未 StartStage 直接 UpdateStage/WarnStage/CompleteStage：Python steps 为空走 if 守卫跳过
	reporter.UpdateStage(WithUpdateStageMessage("m"))
	reporter.WarnStage("w")
	reporter.CompleteStage(WithCompleteStageMessage("early"))
	p := reporter.GetProgress()
	if p.CurrentMessage != "m" {
		t.Errorf("CurrentMessage = %v, 期望 m", p.CurrentMessage)
	}
	if p.CurrentStatus != ProgressStatusWarning {
		t.Errorf("CurrentStatus = %v, 期望 warning", p.CurrentStatus)
	}
	if len(p.Steps) != 0 {
		t.Errorf("Steps len = %d, 期望 0（空步骤时各方法不建步骤）", len(p.Steps))
	}
}

// TestBuildProgress_ToDict_错误形态 BuildProgress.Error 非 nil 分支。
func TestBuildProgress_ToDict_错误形态(t *testing.T) {
	p := NewBuildProgress("s", "llm_agent", ProgressStageError, ProgressStatusFailed, "bad")
	errMsg := "session failed"
	p.Error = &errMsg
	dict := p.ToDict()
	if dict["error"] != "session failed" {
		t.Errorf("error = %v, 期望 session failed", dict["error"])
	}
}

// TestProgressUTC_时间戳语义 8 处时间戳恒 UTC（对齐 Python datetime.now(timezone.utc)）。
// 2026-10-26 审查差异 1 修复验证：ToDict 输出 +00:00 偏移（对齐 isoformat 形态），
// 与本地时区无关（测试机本地时区非 UTC 时仍须通过）。
func TestProgressUTC_时间戳语义(t *testing.T) {
	// ProgressStep 构造器：Timestamp 恒 UTC
	step := NewProgressStep(ProgressStageInitializing, ProgressStatusRunning, "m")
	if got := step.Timestamp.Location(); got != time.UTC {
		t.Errorf("ProgressStep.Timestamp.Location() = %v, 期望 time.UTC", got)
	}
	// ToDict：RFC3339Nano 输出以 Z/+00:00 偏移结尾（UTC 标志）
	tsStr, ok := step.ToDict()["timestamp"].(string)
	if !ok {
		t.Fatalf("timestamp 期望 string, got %T", step.ToDict()["timestamp"])
	}
	if !strings.HasSuffix(tsStr, "Z") && !strings.HasSuffix(tsStr, "+00:00") {
		t.Errorf("timestamp = %q, 期望 UTC 偏移（Z 或 +00:00 结尾）", tsStr)
	}

	// BuildProgress 构造器：StartTime/LastUpdateTime 恒 UTC
	p := NewBuildProgress("s", "llm_agent", ProgressStageInitializing, ProgressStatusPending, "m")
	if got := p.StartTime.Location(); got != time.UTC {
		t.Errorf("BuildProgress.StartTime.Location() = %v, 期望 time.UTC", got)
	}
	if got := p.LastUpdateTime.Location(); got != time.UTC {
		t.Errorf("BuildProgress.LastUpdateTime.Location() = %v, 期望 time.UTC", got)
	}

	// 方法更新点：StartStage/UpdateStage/WarnStage/CompleteStage 后 LastUpdateTime 恒 UTC
	reporter := NewProgressReporter("utc", "llm_agent")
	reporter.StartStage(ProgressStageClarifying, "start")
	reporter.UpdateStage(WithUpdateStageMessage("update"))
	reporter.WarnStage("warn")
	reporter.Complete()
	prog := reporter.GetProgress()
	if got := prog.LastUpdateTime.Location(); got != time.UTC {
		t.Errorf("方法更新后 LastUpdateTime.Location() = %v, 期望 time.UTC", got)
	}
	if len(prog.Steps) == 0 || prog.Steps[0].Timestamp.Location() != time.UTC {
		t.Error("reporter 内建步骤 Timestamp 期望 time.UTC")
	}

	// BuildProgress.ToDict：start_time/last_update_time UTC 偏移
	pd := prog.ToDict()
	for _, key := range []string{"start_time", "last_update_time"} {
		s, ok := pd[key].(string)
		if !ok {
			t.Fatalf("ToDict[%q] 期望 string, got %T", key, pd[key])
		}
		if !strings.HasSuffix(s, "Z") && !strings.HasSuffix(s, "+00:00") {
			t.Errorf("ToDict[%q] = %q, 期望 UTC 偏移结尾", key, s)
		}
	}
}

// TestProgressReporter_空串消息不覆盖 审查修复 M-04：Python if message:
// 真值判断（空串与 None 同为 falsy 跳过赋值）——Go 四处补 != "" 对齐。
func TestProgressReporter_空串消息不覆盖(t *testing.T) {
	reporter := NewProgressReporter("session-m04", string(AgentTypeLLMAgent))
	reporter.StartStage(ProgressStageClarifying, "原始消息")

	// UpdateStage 空串消息：CurrentMessage 保持
	reporter.UpdateStage(WithUpdateStageMessage(""))
	if reporter.GetProgress().CurrentMessage != "原始消息" {
		t.Errorf("UpdateStage 空串消息应跳过赋值, got %q", reporter.GetProgress().CurrentMessage)
	}

	// CompleteStage 空串消息：step.Message 保持
	reporter.CompleteStage(WithCompleteStageMessage(""))
	p := reporter.GetProgress()
	if len(p.Steps) == 0 || p.Steps[len(p.Steps)-1].Message != "原始消息" {
		t.Errorf("CompleteStage 空串消息应保持 step.Message, got %+v", p.Steps)
	}

	// 非空消息正常覆盖
	reporter.UpdateStage(WithUpdateStageMessage("新消息"))
	if reporter.GetProgress().CurrentMessage != "新消息" {
		t.Errorf("非空消息应正常赋值, got %q", reporter.GetProgress().CurrentMessage)
	}
}

// TestProgressReporter_FailStage空串error保持 审查修复 M-04：
// fail_stage(error="") 后 step.error 保持 None（Python L342 if error:）。
func TestProgressReporter_FailStage空串error保持(t *testing.T) {
	reporter := NewProgressReporter("session-m04b", string(AgentTypeLLMAgent))
	reporter.StartStage(ProgressStageClarifying, "msg")
	reporter.FailStage("boom") // 先失败一次写入 error

	// 重建 reporter 测空串场景（FailStage 终态后不建新步骤）
	r2 := NewProgressReporter("session-m04c", string(AgentTypeLLMAgent))
	r2.StartStage(ProgressStageClarifying, "msg2")
	r2.FailStage("") // 空串 error
	p := r2.GetProgress()
	if len(p.Steps) == 0 {
		t.Fatal("应有步骤")
	}
	if p.Steps[len(p.Steps)-1].Error != nil {
		t.Errorf("FailStage 空串 error 后 step.Error 应保持 nil, got %v", *p.Steps[len(p.Steps)-1].Error)
	}
}
