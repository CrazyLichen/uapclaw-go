package callback

import "testing"

func TestFilterAction_值验证(t *testing.T) {
	if FilterActionContinue != "continue" {
		t.Errorf("FilterActionContinue = %q, want %q", FilterActionContinue, "continue")
	}
	if FilterActionStop != "stop" {
		t.Errorf("FilterActionStop = %q, want %q", FilterActionStop, "stop")
	}
	if FilterActionSkip != "skip" {
		t.Errorf("FilterActionSkip = %q, want %q", FilterActionSkip, "skip")
	}
	if FilterActionModify != "modify" {
		t.Errorf("FilterActionModify = %q, want %q", FilterActionModify, "modify")
	}
}

func TestChainAction_值验证(t *testing.T) {
	if ChainActionContinue != "continue" {
		t.Errorf("ChainActionContinue = %q, want %q", ChainActionContinue, "continue")
	}
	if ChainActionBreak != "break" {
		t.Errorf("ChainActionBreak = %q, want %q", ChainActionBreak, "break")
	}
	if ChainActionRetry != "retry" {
		t.Errorf("ChainActionRetry = %q, want %q", ChainActionRetry, "retry")
	}
	if ChainActionRollback != "rollback" {
		t.Errorf("ChainActionRollback = %q, want %q", ChainActionRollback, "rollback")
	}
}

func TestHookType_值验证(t *testing.T) {
	if HookTypeBefore != "before" {
		t.Errorf("HookTypeBefore = %q, want %q", HookTypeBefore, "before")
	}
	if HookTypeAfter != "after" {
		t.Errorf("HookTypeAfter = %q, want %q", HookTypeAfter, "after")
	}
	if HookTypeError != "error" {
		t.Errorf("HookTypeError = %q, want %q", HookTypeError, "error")
	}
	if HookTypeCleanup != "cleanup" {
		t.Errorf("HookTypeCleanup = %q, want %q", HookTypeCleanup, "cleanup")
	}
}

// TestWorkflowEventType_常量值锁定 8.14 前置件核对结论：WORKFLOW_STARTED/
// FINISHED/CANCELLED/ERROR 四事件常量已存在且字面量与 Python WorkflowEvents
// 对齐（_framework:workflow_*）；锁定防拼写漂移（plan Task 0 Step 6）。
func TestWorkflowEventType_常量值锁定(t *testing.T) {
	if WorkflowStarted != "_framework:workflow_started" {
		t.Errorf("WorkflowStarted = %q, want %q", WorkflowStarted, "_framework:workflow_started")
	}
	if WorkflowFinished != "_framework:workflow_finished" {
		t.Errorf("WorkflowFinished = %q, want %q", WorkflowFinished, "_framework:workflow_finished")
	}
	if WorkflowError != "_framework:workflow_error" {
		t.Errorf("WorkflowError = %q, want %q", WorkflowError, "_framework:workflow_error")
	}
	if WorkflowCancelled != "_framework:workflow_cancelled" {
		t.Errorf("WorkflowCancelled = %q, want %q", WorkflowCancelled, "_framework:workflow_cancelled")
	}
}
