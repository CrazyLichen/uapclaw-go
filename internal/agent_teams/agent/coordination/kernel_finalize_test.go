package coordination

import (
	"context"
	"testing"
)

// TestFinalizeRound_停流控制器与成员脑 验证顺序：sc.Stop → harness.Stop → ResetStreamQueue。
func TestFinalizeRound_停流控制器与成员脑(t *testing.T) {
	stream := &fakeStreamControllerAccessor{}
	ha := &fakeHarnessAccessor{}
	host := &fakeKernelHost{
		streamControllerAccessor: stream,
		harnessAccessor:          ha,
	}
	k := NewCoordinationKernel(host)
	k.FinalizeRound(context.Background())
	if !stream.stopCalled {
		t.Fatal("FinalizeRound 未调用 StreamController.Stop")
	}
	if !ha.stopCalled {
		t.Fatal("FinalizeRound 未调用 Harness.Stop")
	}
	if !stream.resetCalled {
		t.Fatal("FinalizeRound 未调用 ResetStreamQueue")
	}
}
