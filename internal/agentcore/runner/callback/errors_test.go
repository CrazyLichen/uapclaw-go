package callback

import (
	"errors"
	"fmt"
	"testing"
)

func TestNewAbortError_无Cause(t *testing.T) {
	ae := NewAbortError("test reason", nil)
	if ae.Reason != "test reason" {
		t.Errorf("Reason = %q, want %q", ae.Reason, "test reason")
	}
	if ae.Cause != nil {
		t.Errorf("Cause = %v, want nil", ae.Cause)
	}
	wantMsg := "回调执行中止: test reason"
	if ae.Error() != wantMsg {
		t.Errorf("Error() = %q, want %q", ae.Error(), wantMsg)
	}
}

func TestNewAbortError_有Cause(t *testing.T) {
	innerErr := fmt.Errorf("inner error")
	ae := NewAbortError("test reason", innerErr)
	if ae.Cause != innerErr {
		t.Errorf("Cause = %v, want %v", ae.Cause, innerErr)
	}
	wantMsg := "回调执行中止: test reason（由 inner error 引起）"
	if ae.Error() != wantMsg {
		t.Errorf("Error() = %q, want %q", ae.Error(), wantMsg)
	}
}

func TestAbortError_errorsAs(t *testing.T) {
	ae := NewAbortError("abort", nil)
	var target *AbortError
	if !errors.As(ae, &target) {
		t.Errorf("errors.As(AbortError, *AbortError) = false, want true")
	}
}

func TestAbortError_Unwrap_有Cause(t *testing.T) {
	innerErr := fmt.Errorf("inner")
	ae := NewAbortError("abort", innerErr)
	unwrapped := errors.Unwrap(ae)
	if unwrapped != innerErr {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, innerErr)
	}
}

func TestNewAbortErrorWithDetails(t *testing.T) {
	ae := NewAbortErrorWithDetails("reason", nil, map[string]int{"k": 1})
	if ae.Details == nil {
		t.Errorf("Details = nil, want non-nil")
	}
}
