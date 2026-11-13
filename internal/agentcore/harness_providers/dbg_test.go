package harness_providers

import (
	"context"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"testing"
	"time"
)

func TestDbgLifecycleOrder(t *testing.T) {
	harness, adapter := newAdapterHarness(t, HarnessIOAdapterOptions{EmitTurnLifecycle: true})
	ctx := context.Background()
	if err := adapter.Start(ctx, testHarnessContext()); err != nil {
		t.Fatalf("start: %v", err)
	}
	_, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	waitForState(t, harness, harnessprotocol.HarnessStateIdle)
	time.Sleep(100 * time.Millisecond)
	_ = adapter.Stop(context.Background())

	adapter.mu.Lock()
	t.Logf("queueLen=%d", len(adapter.outputQueue))
	for i, item := range adapter.outputQueue {
		if schema, ok := toOutputSchema(item); ok {
			if m, ok2 := schema.Payload.(map[string]any); ok2 {
				t.Logf("queue[%d] %s kind=%v", i, schema.Type, m["kind"])
			} else {
				t.Logf("queue[%d] %s", i, schema.Type)
			}
		}
	}
	adapter.mu.Unlock()
}
