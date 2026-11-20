package harness

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema"
	hp "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session"
)

// TestNativeHarness_Start后相位为IDLE 钉住 Python state.py:188 的契约：
// HarnessInternalState.phase 初值即 HarnessState.IDLE，且 NativeHarness.start()
// 不额外设 RUNNING（native_harness.py:348-388 / protocol_adapter.py:209）。
// 25.1f adapter 的 _open_session 在 agent.start() 之后也无需再转 IDLE。
func TestNativeHarness_Start后相位为IDLE(t *testing.T) {
	h, err := NewNativeHarness(schema.NewDeepAgentSpec(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := h.State(); got != hp.HarnessStateIdle {
		t.Fatalf("Start 前相位 = %s, want idle（Python state.py:188 初值）", got)
	}
	sess := session.CreateAgentSession("s-idle", nil, nil)
	if err := h.Start(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if got := h.State(); got != hp.HarnessStateIdle {
		t.Fatalf("Start 后相位 = %s, want idle（上游 start 不设 RUNNING）", got)
	}
	_ = h.Stop(context.Background())
}
