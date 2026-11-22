package adapter

import (
	"context"
	"testing"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
	harnessproviders "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers"
)

// checkpointFakeHarness checkpoint 半区单测的最小协议桩。
// 实现 HarnessProtocol 全接口（未涉方法返回零值），Card/ExportCheckpoint 可脚本化。
type checkpointFakeHarness struct {
	card       harnessprotocol.HarnessCard
	checkpoint *harnessprotocol.HarnessCheckpoint
	exportErr  error
}

// Card 返回脚本化能力卡。
func (f *checkpointFakeHarness) Card() harnessprotocol.HarnessCard { return f.card }

// State 恒返 idle（桩）。
func (f *checkpointFakeHarness) State() harnessprotocol.HarnessState {
	return harnessprotocol.HarnessStateIdle
}

// ProviderSessionID 恒 nil（桩）。
func (f *checkpointFakeHarness) ProviderSessionID() *string { return nil }

// EventBufferConfig 零值（桩）。
func (f *checkpointFakeHarness) EventBufferConfig() harnessprotocol.EventBufferConfig {
	return harnessprotocol.EventBufferConfig{}
}

// Start/Stop 恒 nil（桩）。
func (f *checkpointFakeHarness) Start(ctx context.Context, c harnessprotocol.HarnessContext) error {
	return nil
}

// Stop 恒 nil（桩）。
func (f *checkpointFakeHarness) Stop(ctx context.Context) error { return nil }

// Events 恒 nil（桩）。
func (f *checkpointFakeHarness) Events() harnessprotocol.HarnessEventCursor { return nil }

// TurnEvents 恒 nil（桩）。
func (f *checkpointFakeHarness) TurnEvents(turnID *string) harnessprotocol.HarnessEventCursor {
	return nil
}

// Send 零值（桩）。
func (f *checkpointFakeHarness) Send(ctx context.Context, content harnessprotocol.HarnessInput,
	mode harnessprotocol.DeliveryMode) (harnessprotocol.SendReceipt, error) {
	return harnessprotocol.SendReceipt{}, nil
}

// Abort 恒 nil（桩）。
func (f *checkpointFakeHarness) Abort(ctx context.Context, mode harnessprotocol.AbortMode) error {
	return nil
}

// Pause 恒 nil（桩）。
func (f *checkpointFakeHarness) Pause(ctx context.Context) error { return nil }

// Resume 恒 nil（桩）。
func (f *checkpointFakeHarness) Resume(ctx context.Context, query *harnessprotocol.HarnessInput) error {
	return nil
}

// ExportCheckpoint 返回脚本化 checkpoint/错误。
func (f *checkpointFakeHarness) ExportCheckpoint(ctx context.Context) (*harnessprotocol.HarnessCheckpoint, error) {
	return f.checkpoint, f.exportErr
}

// newCheckpointFakeHarness 构造带/不带 CHECKPOINT 能力的桩。
func newCheckpointFakeHarness(withCapability bool, checkpoint *harnessprotocol.HarnessCheckpoint,
	exportErr error) *checkpointFakeHarness {
	card, _ := harnessprotocol.NewHarnessCard("fakeext", "1.0")
	card.Capabilities[harnessprotocol.HarnessCapabilityCheckpoint] = withCapability
	return &checkpointFakeHarness{card: card, checkpoint: checkpoint, exportErr: exportErr}
}

// validCheckpoint 合法 checkpoint 样例（agent_id 符合约定）。
func validCheckpoint(provider, template, sessionID string) *harnessprotocol.HarnessCheckpoint {
	psid := "cbc-session-1"
	rev := "r1"
	return &harnessprotocol.HarnessCheckpoint{
		Provider:          provider,
		SchemaVersion:     "1",
		AgentID:           expectedExternalHarnessAgentID(provider, template),
		HostSessionID:     sessionID,
		CheckpointID:      "cp-1",
		Sequence:          3,
		Data:              harnessprotocol.JsonObject{"k": "v"},
		ProviderSessionID: &psid,
		Revision:          &rev,
	}
}

// TestLoadExternalHarnessCheckpoint 加载/校验全场景。
// Python: :389-437。
func TestLoadExternalHarnessCheckpoint(t *testing.T) {
	isolateWorkspace(t)

	// 无存储 → nil
	if got := LoadExternalHarnessCheckpoint("sess_cp", "tpl", "dsh"); got != nil {
		t.Fatalf("无存储应 nil, got %+v", got)
	}

	// 写入合法 payload（经导出链保证 round-trip）
	io := harnessproviders.NewHarnessIOAdapter(
		newCheckpointFakeHarness(true, validCheckpoint("dsh", "tpl", "sess_cp"), nil),
		harnessproviders.HarnessIOAdapterOptions{})
	binding := &ResolvedExternalHarnessBinding{Kind: externalHarnessKind,
		AgentTemplateName: "tpl", ProviderName: "dsh"}
	PersistExternalHarnessCheckpoint(context.Background(), binding, io, "sess_cp")

	got := LoadExternalHarnessCheckpoint("sess_cp", "tpl", "dsh")
	if got == nil || got.CheckpointID != "cp-1" || got.Sequence != 3 ||
		got.ProviderSessionID == nil || *got.ProviderSessionID != "cbc-session-1" {
		t.Fatalf("round-trip 不符: %+v", got)
	}

	// 模板不符 → nil
	if got := LoadExternalHarnessCheckpoint("sess_cp", "other", "dsh"); got != nil {
		t.Fatalf("模板不符应 nil, got %+v", got)
	}

	// agent_id 越界 → nil（直接落盘越界 payload；persist 链会被 scope 拦截不落盘）
	bad := validCheckpoint("dsh", "tpl", "sess_cp2")
	bad.AgentID = "external_harness:dsh:other"
	updateSessionMetadataFields("sess_cp2", map[string]any{
		sessionHarnessCheckpointKey: CheckpointMetadata(bad, "tpl", "dsh"),
	})
	if got := LoadExternalHarnessCheckpoint("sess_cp2", "tpl", "dsh"); got != nil {
		t.Fatalf("越界应 nil, got %+v", got)
	}
}

// TestPersistExternalHarnessCheckpoint 持久化门卫全场景。
// Python: :463-506。
func TestPersistExternalHarnessCheckpoint(t *testing.T) {
	isolateWorkspace(t)

	// 能力不支持 → 跳过（不写键）
	ioNoCap := harnessproviders.NewHarnessIOAdapter(
		newCheckpointFakeHarness(false, validCheckpoint("dsh", "tpl", "sess_pc"), nil),
		harnessproviders.HarnessIOAdapterOptions{})
	binding := &ResolvedExternalHarnessBinding{Kind: externalHarnessKind,
		AgentTemplateName: "tpl", ProviderName: "dsh"}
	PersistExternalHarnessCheckpoint(context.Background(), binding, ioNoCap, "sess_pc")
	if freshSessionMetadata("sess_pc")[sessionHarnessCheckpointKey] != nil {
		t.Fatalf("无能力不应写键")
	}

	// nil 绑定 → 跳过
	io := harnessproviders.NewHarnessIOAdapter(
		newCheckpointFakeHarness(true, validCheckpoint("dsh", "tpl", "sess_pc"), nil),
		harnessproviders.HarnessIOAdapterOptions{})
	PersistExternalHarnessCheckpoint(context.Background(), nil, io, "sess_pc")
	if freshSessionMetadata("sess_pc")[sessionHarnessCheckpointKey] != nil {
		t.Fatalf("无绑定不应写键")
	}

	// 导出错误 → Warn 跳过
	ioErr := harnessproviders.NewHarnessIOAdapter(
		newCheckpointFakeHarness(true, nil, context.Canceled), harnessproviders.HarnessIOAdapterOptions{})
	PersistExternalHarnessCheckpoint(context.Background(), binding, ioErr, "sess_pc")
	if freshSessionMetadata("sess_pc")[sessionHarnessCheckpointKey] != nil {
		t.Fatalf("导出失败不应写键")
	}

	// 成功写键 + scope 不符跳过
	PersistExternalHarnessCheckpoint(context.Background(), binding, io, "sess_pc")
	if freshSessionMetadata("sess_pc")[sessionHarnessCheckpointKey] == nil {
		t.Fatalf("成功应写键")
	}
	bad := validCheckpoint("dsh", "tpl", "sess_pc")
	bad.HostSessionID = "other-session"
	ioBad := harnessproviders.NewHarnessIOAdapter(
		newCheckpointFakeHarness(true, bad, nil), harnessproviders.HarnessIOAdapterOptions{})
	PersistExternalHarnessCheckpoint(context.Background(), binding, ioBad, "sess_pc")
	// 越界不覆盖原合法值
	if freshSessionMetadata("sess_pc")[sessionHarnessCheckpointKey] == nil {
		t.Fatalf("越界不应清空已有键")
	}
}
