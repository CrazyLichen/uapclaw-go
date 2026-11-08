package adapter

import (
	"context"
	"errors"
	"testing"

	coreevolution "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// newApprovalAdapter 构造带真实 SkillEvolutionRail 的 adapter。
// rail 中不存在该 requestID 时 Approve/RejectRecord 均返回 nil，用于覆盖成功分支。
func newApprovalAdapter(t *testing.T, autoScan bool) *DeepAdapter {
	t.Helper()
	d := NewDeepAdapter()
	d.model = usableTestModel()
	d.defaultModelName = "test-model"
	d.skillEvolutionRail = coreevolution.NewSkillEvolutionRail(
		[]string{t.TempDir()},
		d.model,
		"test-model",
		"cn",
		coreevolution.WithAutoScan(autoScan),
		coreevolution.WithAutoSave(false),
	)
	return d
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestHandleEvolutionApproval_批准与拒绝 验证两条决策路径都能返回 true。
func TestHandleEvolutionApproval_批准与拒绝(t *testing.T) {
	ctx := context.Background()

	t.Run("接收走批准路径", func(t *testing.T) {
		d := newApprovalAdapter(t, false)
		answers := []ApprovalAnswer{{SelectedOptions: []string{"接收"}}}
		if !d.handleEvolutionApproval(ctx, "req-approve-unknown", answers) {
			t.Fatal("未知 requestID 的批准应返回 true（rail 内部幂等返回 nil）")
		}
	})

	t.Run("拒绝走 reject 路径", func(t *testing.T) {
		d := newApprovalAdapter(t, false)
		answers := []ApprovalAnswer{{SelectedOptions: []string{"拒绝"}}}
		if !d.handleEvolutionApproval(ctx, "req-reject-unknown", answers) {
			t.Fatal("未知 requestID 的拒绝应返回 true")
		}
	})

	t.Run("空答案按拒绝处理", func(t *testing.T) {
		d := newApprovalAdapter(t, false)
		if !d.handleEvolutionApproval(ctx, "req-empty", nil) {
			t.Fatal("空答案应走拒绝路径并返回 true")
		}
	})
}

// TestPushEventToFrontend_已注入推送 验证 approval / outcome / 其它三类事件的分派。
// 注入的推送函数记录调用，用来区分是否真的推了。
func TestPushEventToFrontend_已注入推送(t *testing.T) {
	ctx := context.Background()

	newWithPush := func(t *testing.T, sessionID string) (*DeepAdapter, *int) {
		t.Helper()
		d, _ := newCEAdapter(t, sessionID)
		calls := 0
		d.SetSendPushFunc(func(context.Context, map[string]any) error {
			calls++
			return nil
		})
		return d, &calls
	}

	t.Run("approval 事件触发推送", func(t *testing.T) {
		d, calls := newWithPush(t, "push-approval")
		evt := &stream.OutputSchema{Payload: map[string]any{"_evolution_meta": map[string]any{"event_kind": "approval"}}}
		d.pushEventToFrontend(ctx, evt, "s1", "c1", "r1")
		if *calls == 0 {
			t.Fatal("approval 事件应触发一次推送")
		}
	})

	t.Run("outcome 事件触发推送", func(t *testing.T) {
		d, calls := newWithPush(t, "push-outcome")
		evt := &stream.OutputSchema{Payload: map[string]any{"_evolution_meta": map[string]any{"event_kind": "outcome"}}}
		d.pushEventToFrontend(ctx, evt, "s1", "c1", "r1")
		if *calls == 0 {
			t.Fatal("outcome 事件应触发一次推送")
		}
	})

	t.Run("其它事件不推送", func(t *testing.T) {
		d, calls := newWithPush(t, "push-progress")
		evt := &stream.OutputSchema{Payload: map[string]any{"_evolution_meta": map[string]any{"event_kind": "progress"}}}
		d.pushEventToFrontend(ctx, evt, "s1", "c1", "r1")
		if *calls != 0 {
			t.Fatalf("progress 事件不应推送，实际调用 %d 次", *calls)
		}
	})

	t.Run("推送失败仅记录告警不 panic", func(t *testing.T) {
		d, _ := newCEAdapter(t, "push-fail")
		d.SetSendPushFunc(func(context.Context, map[string]any) error { return errors.New("push boom") })
		evt := &stream.OutputSchema{Payload: map[string]any{"_evolution_meta": map[string]any{"event_kind": "approval"}}}
		d.pushEventToFrontend(ctx, evt, "s1", "c1", "r1")
	})
}
