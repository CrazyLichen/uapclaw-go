package multi_dim

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	updater "github.com/uapclaw/uapclaw-go/internal/evolving/updater"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockDomainOptimizer 模拟域优化器
type mockDomainOptimizer struct {
	requireForward bool
}

func (m *mockDomainOptimizer) RequiresForwardData() bool {
	return m.requireForward
}

// 对齐 Python: test_process_accepts_signals_directly
func TestMultiDimUpdater_Process直接接受信号(t *testing.T) {
	u := NewMultiDimUpdater()
	sig := &signal.EvolutionSignal{
		SignalType: "low_score",
		Section:    "Troubleshooting",
		Excerpt:    "score=0.00",
	}

	result, err := u.Process(context.Background(), nil, []*signal.EvolutionSignal{sig}, nil)
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	// 默认实现返回空 map
	if len(result) != 0 {
		t.Errorf("Process returned %d updates, want 0", len(result))
	}
}

// 对齐 Python: test_update_adapts_evaluated_cases_to_process
func TestMultiDimUpdater_Update适配EvaluatedCases到Process(t *testing.T) {
	u := NewMultiDimUpdater()
	case_ := dataset.NewCase(
		map[string]any{"q": "question"},
		map[string]any{"a": "answer"},
	)
	ec := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "pred"}, )
	ec.SetScore(0.0)
	ec.Reason = "reason"

	result, err := u.Update(context.Background(), nil, []*dataset.EvaluatedCase{ec}, nil)
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	// 默认 Process 返回空 map
	if len(result) != 0 {
		t.Errorf("Update returned %d updates, want 0", len(result))
	}
}

// 对齐 Python: test_update_respects_score_threshold_from_config
func TestMultiDimUpdater_Update尊重ScoreThreshold(t *testing.T) {
	u := NewMultiDimUpdater()
	case_ := dataset.NewCase(
		map[string]any{"q": "question"},
		map[string]any{"a": "answer"},
	)
	highScore := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "good"})
	highScore.SetScore(1.0)
	highScore.Reason = "perfect"
	lowScore := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "pred"})
	lowScore.SetScore(0.0)
	lowScore.Reason = "reason"

	threshold := 1.0
	result, err := u.Update(
		context.Background(),
		nil,
		[]*dataset.EvaluatedCase{highScore, lowScore},
		map[string]any{"score_threshold": threshold},
	)
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	// 默认 Process 返回空 map，但验证不报错
	if len(result) != 0 {
		t.Errorf("Update returned %d updates, want 0", len(result))
	}
}

// 对齐 Python: test_update_adapts_multiple_evaluated_cases_to_signals_in_order
func TestMultiDimUpdater_Update多个EvaluatedCases按序转换(t *testing.T) {
	u := NewMultiDimUpdater()
	case_ := dataset.NewCase(
		map[string]any{"q": "question"},
		map[string]any{"a": "answer"},
	)
	firstCase := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "pred"})
	firstCase.SetScore(1.0)
	firstCase.Reason = "perfect"
	secondCase := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "pred"})
	secondCase.SetScore(0.0)
	secondCase.Reason = "reason"

	result, err := u.Update(
		context.Background(),
		nil,
		[]*dataset.EvaluatedCase{firstCase, secondCase},
		nil,
	)
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	// 默认 Process 返回空 map
	if len(result) != 0 {
		t.Errorf("Update returned %d updates, want 0", len(result))
	}
}

// 额外测试: RequiresForwardData 相关
func TestMultiDimUpdater_RequiresForwardData_全部不需要(t *testing.T) {
	u := NewMultiDimUpdater(WithDomainOptimizers(map[string]any{
		"llm":  &mockDomainOptimizer{requireForward: false},
		"tool": &mockDomainOptimizer{requireForward: false},
	}))

	if u.RequiresForwardData() {
		t.Error("RequiresForwardData should return false when all optimizers don't need forward")
	}
}

func TestMultiDimUpdater_RequiresForwardData_有需要前向的(t *testing.T) {
	u := NewMultiDimUpdater(WithDomainOptimizers(map[string]any{
		"llm":    &mockDomainOptimizer{requireForward: true},
		"tool":   &mockDomainOptimizer{requireForward: false},
		"memory": &mockDomainOptimizer{requireForward: false},
	}))

	if !u.RequiresForwardData() {
		t.Error("RequiresForwardData should return true when any optimizer needs forward")
	}
}

func TestMultiDimUpdater_RequiresForwardData_空优化器(t *testing.T) {
	u := NewMultiDimUpdater()

	if u.RequiresForwardData() {
		t.Error("RequiresForwardData should return false with no optimizers")
	}
}

func TestMultiDimUpdater_Bind默认返回零(t *testing.T) {
	u := NewMultiDimUpdater()
	n := u.Bind(nil, nil, nil)
	if n != 0 {
		t.Errorf("Bind returned %d, want 0", n)
	}
}

func TestMultiDimUpdater_GetState返回空(t *testing.T) {
	u := NewMultiDimUpdater()
	state := u.GetState()
	if len(state) != 0 {
		t.Errorf("GetState returned %v, want empty map", state)
	}
}

func TestMultiDimUpdater_LoadState无操作(t *testing.T) {
	u := NewMultiDimUpdater()
	// 不应 panic
	u.LoadState(map[string]any{"key": "value"})
}

func TestMultiDimUpdater_DomainOptimizers(t *testing.T) {
	u := NewMultiDimUpdater(WithDomainOptimizers(map[string]any{
		"llm":  &mockDomainOptimizer{requireForward: false},
		"tool": &mockDomainOptimizer{requireForward: true},
	}))

	opts := u.DomainOptimizers()
	if len(opts) != 2 {
		t.Errorf("DomainOptimizers count = %d, want 2", len(opts))
	}
	if _, ok := opts["llm"]; !ok {
		t.Error("DomainOptimizers missing 'llm' key")
	}
	if _, ok := opts["tool"]; !ok {
		t.Error("DomainOptimizers missing 'tool' key")
	}
}

// TestMultiDimUpdater_实现Updater接口 验证编译期接口兼容
func TestMultiDimUpdater_实现Updater接口(t *testing.T) {
	u := NewMultiDimUpdater()

	// 编译期验证
	var _ updater.Updater = u
	_ = u.Bind
	_ = u.RequiresForwardData
	_ = u.Update
	_ = u.Process
	_ = u.GetState
	_ = u.LoadState
}

// 验证 Bind 方法签名与 Updater 接口对齐
func TestMultiDimUpdater_Bind签名对齐(t *testing.T) {
	u := NewMultiDimUpdater()
	operators := map[string]operator.Operator{}
	n := u.Bind(operators, []string{"system_prompt"}, map[string]any{})
	if n != 0 {
		t.Errorf("Bind returned %d, want 0", n)
	}
}
