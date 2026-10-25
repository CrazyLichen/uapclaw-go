package utils

import (
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
