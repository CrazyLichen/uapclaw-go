package harnessrsi

import (
	"errors"
	"testing"
)

// TestRuntimeReliability_占位工厂 占位工厂返回哨兵错误且不 panic
func TestRuntimeReliability_占位工厂(t *testing.T) {
	rail, err := BuildSingleAgentReliabilityRail()
	if rail != nil {
		t.Fatalf("placeholder must return nil rail, got %v", rail)
	}
	if !errors.Is(err, ErrReliabilityNotImplemented) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}
