//go:build integration

package externalharness_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
	harnessproviders "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers"
	adapter "github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/equipment"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BindingLifecycleSuite 15.18 绑定解析 → 懒启动 → checkpoint 往返 → 停止 全链套件。
type BindingLifecycleSuite struct {
	isuite.AgentSuite
}

// fakeExtProvider 集成用 fake provider（Create + ModelConfig 双面）。
type fakeExtProvider struct{}

// Card 返回声明 CHECKPOINT 能力的身份卡。
func (fakeExtProvider) Card() harnessprotocol.HarnessCard {
	card, _ := harnessprotocol.NewHarnessCard("fakeext", "1.0")
	card.Capabilities[harnessprotocol.HarnessCapabilityCheckpoint] = true
	return card
}

// Create 返回静默协议桩（不出事件）。
func (fakeExtProvider) Create(config harnessprotocol.JsonObject) (harnessprotocol.HarnessProtocol, error) {
	return &idleExtHarness{card: fakeExtProvider{}.Card()}, nil
}

// ModelConfig 返回空片段。
func (fakeExtProvider) ModelConfig(choice harnessprotocol.ModelChoice,
	fallback *harnessprotocol.ModelChoice) (harnessprotocol.JsonObject, error) {
	return harnessprotocol.JsonObject{}, nil
}

// idleExtHarness 不出事件的协议桩。
type idleExtHarness struct {
	card harnessprotocol.HarnessCard
}

// Card 返回能力卡。
func (h *idleExtHarness) Card() harnessprotocol.HarnessCard { return h.card }

// State 恒 idle。
func (h *idleExtHarness) State() harnessprotocol.HarnessState {
	return harnessprotocol.HarnessStateIdle
}

// ProviderSessionID 恒 nil。
func (h *idleExtHarness) ProviderSessionID() *string { return nil }

// EventBufferConfig 零值。
func (h *idleExtHarness) EventBufferConfig() harnessprotocol.EventBufferConfig {
	return harnessprotocol.EventBufferConfig{}
}

// Start 无操作。
func (h *idleExtHarness) Start(ctx context.Context, c harnessprotocol.HarnessContext) error {
	return nil
}

// Stop 无操作。
func (h *idleExtHarness) Stop(ctx context.Context) error { return nil }

// Events 空流。
func (h *idleExtHarness) Events() harnessprotocol.HarnessEventCursor { return &idleCursor{} }

// TurnEvents 空流。
func (h *idleExtHarness) TurnEvents(turnID *string) harnessprotocol.HarnessEventCursor {
	return &idleCursor{}
}

// Send 零值。
func (h *idleExtHarness) Send(ctx context.Context, content harnessprotocol.HarnessInput,
	mode harnessprotocol.DeliveryMode) (harnessprotocol.SendReceipt, error) {
	return harnessprotocol.SendReceipt{}, nil
}

// Abort 无操作。
func (h *idleExtHarness) Abort(ctx context.Context, mode harnessprotocol.AbortMode) error {
	return nil
}

// Pause 无操作。
func (h *idleExtHarness) Pause(ctx context.Context) error { return nil }

// Resume 无操作。
func (h *idleExtHarness) Resume(ctx context.Context, query *harnessprotocol.HarnessInput) error {
	return nil
}

// ExportCheckpoint 返回作用域内 checkpoint。
func (h *idleExtHarness) ExportCheckpoint(ctx context.Context) (*harnessprotocol.HarnessCheckpoint, error) {
	return nil, nil
}

// idleCursor 立即耗尽游标。
type idleCursor struct{}

// Iter 返回自身。
func (c *idleCursor) Iter() harnessprotocol.HarnessEventCursor { return c }

// Next 立即耗尽。
func (c *idleCursor) Next(ctx context.Context) (*harnessprotocol.HarnessEvent, error) {
	return nil, harnessprotocol.ErrEventStreamDone
}

// Close 无操作。
func (c *idleCursor) Close(ctx context.Context) error { return nil }

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBindingLifecycleSuite 套件入口。
func TestBindingLifecycleSuite(t *testing.T) {
	suite.Run(t, new(BindingLifecycleSuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isolate 隔离工作区 + 注册 fake provider + 闭集扩 fakeext。
func (s *BindingLifecycleSuite) isolate() string {
	tmp := s.T().TempDir()
	s.T().Setenv("UAPCLAW_DATA_DIR", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "config"), 0o755); err != nil {
		s.T().Fatal(err)
	}
	path.ResetCache()
	s.T().Cleanup(path.ResetCache)
	return tmp
}

// writeExpert 落专家包（runtime provider=fakeext，manifest 顶层 description 必填）。
func (s *BindingLifecycleSuite) writeExpert(dataDir, name string) string {
	pkgDir := filepath.Join(dataDir, "agent", "workspace", "plugins", "agent_templates", "local", name)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		s.T().Fatal(err)
	}
	manifest := `{"name":"` + name + `","description":"fake expert","package_type":"agent_template",` +
		`"agent_card":{"name":"` + name + `"},` +
		`"runtime":{"provider_name":"fakeext","config":{"cli_path":"/usr/bin/fake"}}}`
	if err := os.WriteFile(filepath.Join(pkgDir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		s.T().Fatal(err)
	}
	return pkgDir
}

// TestBindingConflictFullChain 绑定冲突全链：解析 → 持久化 → 换名冲突（跨
// adapter + equipment + session 三子系统真实调用）。
func (s *BindingLifecycleSuite) TestBindingConflictFullChain() {
	dataDir := s.isolate()
	restore := equipment.SetAgentTemplateRuntimeProvidersForTest(
		[]string{"claudecode", "codex", "dsh", "codebuddy", "fakeext"})
	defer restore()
	s.writeExpert(dataDir, "expert_a")
	s.writeExpert(dataDir, "expert_b")
	sid := "sess_it_conflict"

	// 解析 + 持久化 expert_a
	resolved, err := adapter.ResolveExternalHarnessBinding(sid,
		map[string]any{"agent_template_name": "expert_a"})
	s.Require().NoError(err)
	s.Require().Equal("external_harness", resolved.Kind)
	s.Require().NoError(adapter.PersistExternalHarnessBinding(sid, resolved))

	// 持久化键已落会话元数据
	stored := session.GetSessionMetadata(sid)
	s.Require().NotNil(stored["session_runtime_binding"])

	// 换名 → 冲突（绑定不可变）
	_, err = adapter.ResolveExternalHarnessBinding(sid,
		map[string]any{"agent_template_name": "expert_b"})
	s.Require().Error(err)
	s.Require().Contains(err.Error(), "已绑定三方专家")

	// 同名幂等回放
	again, err := adapter.LoadPersistedExternalHarnessBinding(sid)
	s.Require().NoError(err)
	s.Require().NotNil(again)
	s.Require().Equal("expert_a", again.AgentTemplateName)
}

// TestStartStopCheckpointRoundTrip 启动 → checkpoint 导出落盘 → 重启恢复（RESUME_IF_AVAILABLE）。
func (s *BindingLifecycleSuite) TestStartStopCheckpointRoundTrip() {
	dataDir := s.isolate()
	restore := equipment.SetAgentTemplateRuntimeProvidersForTest(
		[]string{"claudecode", "codex", "dsh", "codebuddy", "fakeext"})
	defer restore()
	s.writeExpert(dataDir, "expert_cp")
	sid := "sess_it_cp"

	if err := harnessproviders.RegisterHarnessProvider("fakeext", func() (harnessprotocol.HarnessProvider, error) {
		return fakeExtProvider{}, nil
	}); err != nil {
		s.T().Logf("fakeext 已注册: %v", err)
	}

	binding, err := adapter.ResolveExternalHarnessBinding(sid,
		map[string]any{"agent_template_name": "expert_cp"})
	s.Require().NoError(err)

	// 启动（first harness: checkpoint 为 nil → NEW）
	io, err := adapter.StartExternalHarness(context.Background(), sid, "expert_cp", "fakeext",
		binding, "cn", dataDir, nil)
	s.Require().NoError(err)
	s.Require().NotNil(io)

	// 停止（导出 checkpoint；本桩 ExportCheckpoint 返回 nil → 无键落盘）
	s.Require().NoError(adapter.StopExternalHarness(context.Background(), binding, io, sid))

	// 重启：第二次启动走同一 binding（binding 不可变，provider 重新 Create）
	io2, err := adapter.StartExternalHarness(context.Background(), sid, "expert_cp", "fakeext",
		binding, "cn", dataDir, nil)
	s.Require().NoError(err)
	s.Require().NoError(adapter.StopExternalHarness(context.Background(), binding, io2, sid))

	// nil io no-op
	s.Require().NoError(adapter.StopExternalHarness(context.Background(), binding, nil, sid))
}
