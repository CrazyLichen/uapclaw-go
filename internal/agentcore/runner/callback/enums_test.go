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
