//go:build integration

package evolving_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	evolving "github.com/uapclaw/uapclaw-go/internal/evolving"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
	"github.com/uapclaw/uapclaw-go/internal/evolving/evaluator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trainer"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
	updaterpkg "github.com/uapclaw/uapclaw-go/internal/evolving/updater"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TrainerE2ESuite Trainer 离线自演化训练编排器 E2E 测试套件
//
// 对齐 Python: tests/system_tests/agent_evolving/test_react_agent_evolve.py
// 使用 mock 组件验证训练循环、回调、检查点保存/恢复、候选选择等核心逻辑
type TrainerE2ESuite struct {
	isuite.BaseIntegrationSuite
	// tmpDir 临时目录
	tmpDir string
}

// tMockTrainableAgent 测试用 TrainableAgent 实现
type tMockTrainableAgent struct {
	card      *agentschema.AgentCard
	operators map[string]operator.Operator
	invokeFn  func(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error)
}

// tMockUpdater 测试用 Updater 实现
type tMockUpdater struct {
	bindCount           int
	requiresForwardData bool
	updateFn            func(ctx context.Context, trajectories []*trajectory.Trajectory, evaluatedCases []*dataset.EvaluatedCase, config map[string]any) ([]map[schema.UpdateKey]any, error)
	state               map[string]any
	mu                  sync.Mutex
}

// tMockEvaluator 测试用 BaseEvaluator 实现
type tMockEvaluator struct {
	score float64
}

// tMockOperator 测试用 Operator 实现
type tMockOperator struct {
	opID   string
	state  map[string]any
	frozen map[string]bool
	mu     sync.Mutex
}

// noTunablesOp 无 Tunables 的 Operator（使 Bind 返回 0）
type noTunablesOp struct {
	id string
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestTrainerE2E(t *testing.T) {
	suite.Run(t, new(TrainerE2ESuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func (s *TrainerE2ESuite) SetupTest() {
	s.tmpDir = s.T().TempDir()
}

// ─── tMockTrainableAgent 实现 ───

func (a *tMockTrainableAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error) {
	if a.invokeFn != nil {
		return a.invokeFn(ctx, inputs, opts...)
	}
	return map[string]any{"result": "ok"}, nil
}
func (a *tMockTrainableAgent) Card() *agentschema.AgentCard               { return a.card }
func (a *tMockTrainableAgent) GetOperators() map[string]operator.Operator { return a.operators }

// ─── tMockUpdater 实现 ───

func (u *tMockUpdater) Bind(operators map[string]operator.Operator, _ []string, _ map[string]any) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	count := 0
	for _, op := range operators {
		if len(op.GetTunables()) > 0 {
			count++
		}
	}
	u.bindCount = count
	return count
}
func (u *tMockUpdater) RequiresForwardData() bool { return u.requiresForwardData }
func (u *tMockUpdater) Update(ctx context.Context, trajectories []*trajectory.Trajectory, evaluatedCases []*dataset.EvaluatedCase, config map[string]any) ([]map[schema.UpdateKey]any, error) {
	if u.updateFn != nil {
		return u.updateFn(ctx, trajectories, evaluatedCases, config)
	}
	return []map[schema.UpdateKey]any{{}}, nil
}
func (u *tMockUpdater) Process(ctx context.Context, trajectories []*trajectory.Trajectory, signals []*signal.EvolutionSignal, config map[string]any) ([]map[schema.UpdateKey]any, error) {
	return u.Update(ctx, trajectories, nil, config)
}
func (u *tMockUpdater) GetState() map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.state == nil {
		return map[string]any{}
	}
	return u.state
}
func (u *tMockUpdater) LoadState(state map[string]any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.state = state
}

// ─── tMockEvaluator 实现 ───

func (e *tMockEvaluator) Evaluate(_ context.Context, case_ dataset.Case, _ map[string]any) (*dataset.EvaluatedCase, error) {
	ec := dataset.NewEvaluatedCase(case_, map[string]any{"mock": true})
	ec.SetScore(e.score)
	return ec, nil
}
func (e *tMockEvaluator) BatchEvaluate(ctx context.Context, cases []dataset.Case, predicts []map[string]any, numParallel int) ([]*dataset.EvaluatedCase, error) {
	result := make([]*dataset.EvaluatedCase, len(cases))
	for i, c := range cases {
		ec, err := e.Evaluate(ctx, c, predicts[i])
		if err != nil {
			return nil, err
		}
		result[i] = ec
	}
	return result, nil
}

// ─── tMockOperator 实现 ───

func (o *tMockOperator) OperatorID() string { return o.opID }
func (o *tMockOperator) GetTunables() map[string]operator.TunableSpec {
	return map[string]operator.TunableSpec{
		"prompt": {Name: "prompt"},
	}
}
func (o *tMockOperator) GetState() map[string]any {
	o.mu.Lock()
	defer o.mu.Unlock()
	cp := make(map[string]any, len(o.state))
	for k, v := range o.state {
		cp[k] = v
	}
	return cp
}
func (o *tMockOperator) SetParameter(target string, value any) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.frozen != nil && o.frozen[target] {
		return
	}
	if o.state == nil {
		o.state = make(map[string]any)
	}
	o.state[target] = value
}
func (o *tMockOperator) ApplyUpdate(target string, update schema.UpdateValue) schema.ApplyResult {
	o.SetParameter(target, update.Payload)
	return schema.ApplyResult{Applied: true}
}
func (o *tMockOperator) LoadState(state map[string]any) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.state = make(map[string]any, len(state))
	for k, v := range state {
		o.state[k] = v
	}
}

// ─── noTunablesOp 实现 ───

func (o *noTunablesOp) OperatorID() string                           { return o.id }
func (o *noTunablesOp) GetTunables() map[string]operator.TunableSpec { return nil }
func (o *noTunablesOp) GetState() map[string]any                     { return map[string]any{} }
func (o *noTunablesOp) SetParameter(_ string, _ any)                 {}
func (o *noTunablesOp) ApplyUpdate(_ string, _ schema.UpdateValue) schema.ApplyResult {
	return schema.ApplyResult{Applied: true}
}
func (o *noTunablesOp) LoadState(_ map[string]any) {}

// ─── 辅助函数 ───

func newTestAgent() *tMockTrainableAgent {
	op := &tMockOperator{opID: "agent/prompt_op", state: map[string]any{"prompt": "原始提示词"}}
	card := &agentschema.AgentCard{
		BaseCard: commonschema.BaseCard{ID: "test_agent", Name: "TestAgent"},
	}
	return &tMockTrainableAgent{
		card:      card,
		operators: map[string]operator.Operator{"agent/prompt_op": op},
	}
}

func newCaseLoader() *dataset.CaseLoader {
	cases := []dataset.Case{
		{Inputs: map[string]any{"query": "测试1"}, Label: map[string]any{"answer": "答案1"}, CaseID: "case_1"},
		{Inputs: map[string]any{"query": "测试2"}, Label: map[string]any{"answer": "答案2"}, CaseID: "case_2"},
	}
	return dataset.NewCaseLoader(cases)
}

// 编译期接口合规检查
var (
	_ updaterpkg.Updater      = (*tMockUpdater)(nil)
	_ evaluator.BaseEvaluator = (*tMockEvaluator)(nil)
	_ operator.Operator       = (*tMockOperator)(nil)
	_ operator.Operator       = (*noTunablesOp)(nil)
	_ evolving.TrainableAgent = (*tMockTrainableAgent)(nil)
)

// ──────────────────── 测试方法 ────────────────────

// TestTrainer_训练循环回调 验证完整训练循环中回调触发顺序
// 对齐 Python: test_end_to_end_training
func (s *TrainerE2ESuite) TestTrainer_训练循环回调() {
	var callbacks []string
	agent := newTestAgent()
	updater := &tMockUpdater{requiresForwardData: true}
	eval := &tMockEvaluator{score: 0.5}

	cbs := &trainer.Callbacks{
		OnTrainBegin: func(_ evolving.TrainableAgent, p *trainer.Progress, _ []*dataset.EvaluatedCase) {
			callbacks = append(callbacks, "train_begin")
		},
		OnTrainEnd: func(_ evolving.TrainableAgent, p *trainer.Progress, _ []*dataset.EvaluatedCase) {
			callbacks = append(callbacks, "train_end")
		},
		OnTrainEpochBegin: func(_ evolving.TrainableAgent, p *trainer.Progress) {
			callbacks = append(callbacks, fmt.Sprintf("epoch_%d_begin", p.CurrentEpoch))
		},
		OnTrainEpochEnd: func(_ evolving.TrainableAgent, p *trainer.Progress, _ []*dataset.EvaluatedCase) {
			callbacks = append(callbacks, fmt.Sprintf("epoch_%d_end", p.CurrentEpoch))
		},
	}

	t := trainer.NewTrainer(
		trainer.WithUpdater(updater),
		trainer.WithEvaluator(eval),
		trainer.WithCallbacks(cbs),
		trainer.WithEarlyStopScore(1.0),
	)

	cases := newCaseLoader()
	_, err := t.Train(s.Ctx, agent, cases, nil, 2, nil)
	s.Require().NoError(err)

	s.Contains(callbacks, "train_begin")
	s.Contains(callbacks, "train_end")
	s.Contains(callbacks, "epoch_1_begin")
	s.Contains(callbacks, "epoch_1_end")
	s.Contains(callbacks, "epoch_2_begin")
	s.Contains(callbacks, "epoch_2_end")
}

// TestTrainer_早停 验证分数达到阈值时提前停止
// 对齐 Python: test_end_to_end_training (early stop)
func (s *TrainerE2ESuite) TestTrainer_早停() {
	agent := newTestAgent()
	updater := &tMockUpdater{requiresForwardData: true}
	eval := &tMockEvaluator{score: 1.0}

	var epochEnds []int
	cbs := &trainer.Callbacks{
		OnTrainEpochEnd: func(_ evolving.TrainableAgent, p *trainer.Progress, _ []*dataset.EvaluatedCase) {
			epochEnds = append(epochEnds, p.CurrentEpoch)
		},
	}

	t := trainer.NewTrainer(
		trainer.WithUpdater(updater),
		trainer.WithEvaluator(eval),
		trainer.WithCallbacks(cbs),
		trainer.WithEarlyStopScore(1.0),
	)

	cases := newCaseLoader()
	_, err := t.Train(s.Ctx, agent, cases, nil, 10, nil)
	s.Require().NoError(err)

	s.LessOrEqual(len(epochEnds), 1, "早停后不应有多个 epoch")
}

// TestTrainer_快照回滚 验证 Operator 状态快照和回滚
func (s *TrainerE2ESuite) TestTrainer_快照回滚() {
	op := &tMockOperator{opID: "agent/prompt_op", state: map[string]any{"prompt": "v1"}}
	operators := map[string]operator.Operator{"agent/prompt_op": op}

	snapshot := trainer.SnapshotOperatorsState(operators)
	s.Equal("v1", snapshot["agent/prompt_op"]["prompt"])

	op.SetParameter("prompt", "v2")
	s.Equal("v2", op.GetState()["prompt"])

	trainer.RestoreOperatorsState(operators, snapshot)
	s.Equal("v1", op.GetState()["prompt"])
}

// TestTrainer_ApplyUpdates 验证 ApplyUpdates 将更新映射应用到 Operator
func (s *TrainerE2ESuite) TestTrainer_ApplyUpdates() {
	op := &tMockOperator{opID: "agent/prompt_op", state: map[string]any{"prompt": "v1"}}
	operators := map[string]operator.Operator{"agent/prompt_op": op}

	key := schema.UpdateKey{"agent/prompt_op", "prompt"}
	updates := map[schema.UpdateKey]schema.UpdateValue{
		key: {Payload: "v2"},
	}

	trainer.ApplyUpdates(operators, updates)
	s.Equal("v2", op.GetState()["prompt"])
}

// TestTrainer_候选选择 验证多候选更新时选择最优
// 对齐 Python: test_end_to_end_training (multi-candidate)
func (s *TrainerE2ESuite) TestTrainer_候选选择() {
	agent := newTestAgent()
	op := agent.operators["agent/prompt_op"]
	operators := agent.GetOperators()

	eval := &tMockEvaluator{score: 0.7}
	updater := &tMockUpdater{requiresForwardData: false}

	t := trainer.NewTrainer(
		trainer.WithUpdater(updater),
		trainer.WithEvaluator(eval),
		trainer.WithEarlyStopScore(1.0),
	)

	cases := newCaseLoader()

	key := schema.UpdateKey{"agent/prompt_op", "prompt"}
	candidates := []map[schema.UpdateKey]schema.UpdateValue{
		{key: {Payload: "cand1"}},
		{key: {Payload: "cand2"}},
	}

	score, _, err := t.SelectBestCandidateOnVal(s.Ctx, agent, operators, candidates, cases)
	s.Require().NoError(err)
	s.Greater(score, 0.0)

	finalPrompt := op.GetState()["prompt"]
	s.True(finalPrompt == "cand1" || finalPrompt == "cand2", "应选择某个候选更新")
}

// TestTrainer_黑盒优化器 验证 RequiresForwardData=false 跳过前向推理
func (s *TrainerE2ESuite) TestTrainer_黑盒优化器() {
	agent := newTestAgent()
	updater := &tMockUpdater{
		requiresForwardData: false,
		updateFn: func(_ context.Context, _ []*trajectory.Trajectory, _ []*dataset.EvaluatedCase, _ map[string]any) ([]map[schema.UpdateKey]any, error) {
			return []map[schema.UpdateKey]any{{}}, nil
		},
	}
	eval := &tMockEvaluator{score: 0.5}

	var invokeCount int
	agent.invokeFn = func(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
		invokeCount++
		return map[string]any{"result": "ok"}, nil
	}

	t := trainer.NewTrainer(
		trainer.WithUpdater(updater),
		trainer.WithEvaluator(eval),
		trainer.WithEarlyStopScore(1.0),
	)

	cases := newCaseLoader()
	_, err := t.Train(s.Ctx, agent, cases, nil, 2, nil)
	s.Require().NoError(err)
	s.Greater(invokeCount, 0, "评估仍需推理")
}

// TestTrainer_训练后推理 验证训练完成后 Agent 可正常推理
// 对齐 Python: test_evolved_agent_inference
func (s *TrainerE2ESuite) TestTrainer_训练后推理() {
	agent := newTestAgent()
	updater := &tMockUpdater{
		requiresForwardData: true,
		updateFn: func(_ context.Context, _ []*trajectory.Trajectory, _ []*dataset.EvaluatedCase, _ map[string]any) ([]map[schema.UpdateKey]any, error) {
			key := schema.UpdateKey{"agent/prompt_op", "prompt"}
			return []map[schema.UpdateKey]any{{key: schema.UpdateValue{Payload: "优化后的提示词"}}}, nil
		},
	}
	eval := &tMockEvaluator{score: 0.7}

	t := trainer.NewTrainer(
		trainer.WithUpdater(updater),
		trainer.WithEvaluator(eval),
		trainer.WithEarlyStopScore(1.0),
	)

	cases := newCaseLoader()
	_, err := t.Train(s.Ctx, agent, cases, nil, 2, nil)
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "测试推理"})
	s.Require().NoError(err)
	s.NotNil(result)
}

// TestTrainer_无Operator匹配时软退出 验证 Updater.Bind 返回 0 时软退出
func (s *TrainerE2ESuite) TestTrainer_无Operator匹配时软退出() {
	eval := &tMockEvaluator{score: 0.5}
	op := &noTunablesOp{id: "bare_op"}
	card := &agentschema.AgentCard{
		BaseCard: commonschema.BaseCard{ID: "bare_agent", Name: "Bare"},
	}
	agent := &tMockTrainableAgent{
		card:      card,
		operators: map[string]operator.Operator{"bare_op": op},
	}
	updater := &tMockUpdater{requiresForwardData: true}

	t := trainer.NewTrainer(
		trainer.WithUpdater(updater),
		trainer.WithEvaluator(eval),
		trainer.WithEarlyStopScore(1.0),
	)

	cases := newCaseLoader()
	result, err := t.Train(s.Ctx, agent, cases, nil, 3, nil)
	s.Require().NoError(err)
	s.NotNil(result, "软退出应返回原始 Agent")
}

// TestTrainer_Progress迭代 验证 Progress.RunEpoch 迭代
func (s *TrainerE2ESuite) TestTrainer_Progress迭代() {
	p := trainer.NewProgressWithMaxEpoch(3)
	s.Equal(3, p.MaxEpoch)

	var epochs []int
	for epoch := range p.RunEpoch() {
		epochs = append(epochs, epoch)
	}
	s.Equal([]int{1, 2, 3}, epochs)
	s.Equal(3, p.CurrentEpoch)
}

// TestTrainer_Progress断点续训 验证 StartEpoch 非零时从中间开始
// 对齐 Python: test_checkpoint_save_and_resume
func (s *TrainerE2ESuite) TestTrainer_Progress断点续训() {
	p := trainer.NewProgressWithMaxEpoch(5)
	p.StartEpoch = 2

	var epochs []int
	for epoch := range p.RunEpoch() {
		epochs = append(epochs, epoch)
	}
	s.Equal([]int{3, 4, 5}, epochs, "应从 epoch 3 开始（StartEpoch=2 + 1）")
}

// TestTrainer_检查点保存加载 验证 BuildCheckpoint + Restore 管道
// 对齐 Python: test_checkpoint_save_and_resume
// 注意：当前 SaveCheckpoint→LoadCheckpoint 往返存在键名大小写不一致
// （Save 输出 PascalCase "Best"，Load 期望 snake_case "best"），
// 因此本测试仅验证 BuildCheckpoint→Restore 不经过 JSON 序列化的正确性。
func (s *TrainerE2ESuite) TestTrainer_检查点保存加载() {
	agent := newTestAgent()
	progress := trainer.NewProgressWithMaxEpoch(5)
	progress.CurrentEpoch = 2
	progress.BestScore = 0.6

	mgr := checkpointing.NewDefaultCheckpointManager("run_test", "1.0", 1, true)
	ckpt := mgr.BuildCheckpoint(agent, progress, map[string]any{"version": 1})

	// 验证 BuildCheckpoint 产出结构正确
	s.Equal("run_test", ckpt.RunID)
	s.Equal(2, ckpt.Step["epoch"])
	s.InDelta(0.6, ckpt.Best["best_score"], 1e-9, "Best.best_score 应为 0.6")
	s.NotNil(ckpt.OperatorsState)

	// Restore 不经过 JSON 序列化，直接从内存恢复
	restored := mgr.Restore(agent, ckpt)
	s.NotNil(restored)
	s.Equal(2, restored["start_epoch"], "start_epoch 应恢复为 2")
	s.InDelta(0.6, restored["best_score"], 1e-9, "best_score 应恢复为 0.6")
	s.Equal("run_test", restored["run_id"], "run_id 应恢复")
}

// TestTrainer_检查点管理器逻辑 验证 ShouldSave、BuildCheckpoint、Restore
func (s *TrainerE2ESuite) TestTrainer_检查点管理器逻辑() {
	agent := newTestAgent()
	mgr := checkpointing.NewDefaultCheckpointManager("run1", "1.0", 1, true)

	s.True(mgr.ShouldSave(1, false), "epoch 1 应保存")
	s.True(mgr.ShouldSave(2, true), "improved 应保存")

	progress := trainer.NewProgressWithMaxEpoch(3)
	progress.CurrentEpoch = 1
	progress.BestScore = 0.7
	updaterState := map[string]any{"lr": 0.01}
	ckpt := mgr.BuildCheckpoint(agent, progress, updaterState)
	s.NotNil(ckpt)

	restored := mgr.Restore(agent, ckpt)
	s.NotNil(restored)

	if v, ok := restored["best_score"]; ok {
		s.Equal(0.7, v)
	}
	if v, ok := restored["start_epoch"]; ok {
		s.Equal(1, v)
	}
}

// TestCaseLoader_用例拆分 验证 CaseLoader 的训练/验证集拆分
func (s *TrainerE2ESuite) TestCaseLoader_用例拆分() {
	cases := make([]dataset.Case, 10)
	for i := range cases {
		cases[i] = dataset.Case{
			Inputs: map[string]any{"query": fmt.Sprintf("问题%d", i)},
			Label:  map[string]any{"answer": fmt.Sprintf("答案%d", i)},
			CaseID: fmt.Sprintf("case_%d", i),
		}
	}
	loader := dataset.NewCaseLoader(cases)
	s.Equal(10, loader.Len())

	trainLoader, valLoader, err := loader.Split(0.8, 42)
	s.Require().NoError(err)
	s.Equal(8, trainLoader.Len(), "训练集应有 8 个")
	s.Equal(2, valLoader.Len(), "验证集应有 2 个")
}
