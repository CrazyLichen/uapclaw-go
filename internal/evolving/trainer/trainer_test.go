package trainer

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeTrainableAgent 用于测试的模拟 Agent
type fakeTrainableAgent struct {
	card      *agentschema.AgentCard
	operators map[string]operator.Operator
	invokeFn  func(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error)
}

func (a *fakeTrainableAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error) {
	if a.invokeFn != nil {
		return a.invokeFn(ctx, inputs, opts...)
	}
	return map[string]any{"result": "ok"}, nil
}

func (a *fakeTrainableAgent) Card() *agentschema.AgentCard {
	return a.card
}

func (a *fakeTrainableAgent) GetOperators() map[string]operator.Operator {
	return a.operators
}

// fakeOperator 用于测试的模拟 Operator
type fakeOperator struct {
	id     string
	state  map[string]any
	params map[string]any
}

func (o *fakeOperator) OperatorID() string                           { return o.id }
func (o *fakeOperator) GetTunables() map[string]operator.TunableSpec { return nil }
func (o *fakeOperator) GetState() map[string]any                     { return o.state }
func (o *fakeOperator) SetParameter(target string, value any)        { o.params[target] = value }
func (o *fakeOperator) ApplyUpdate(target string, update schema.UpdateValue) schema.ApplyResult {
	o.params[target] = update.Payload
	return schema.ApplyResult{OperatorID: o.id, Target: target, Applied: true}
}
func (o *fakeOperator) LoadState(state map[string]any) { o.state = state }

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNewTrainer 测试 Trainer 构造函数默认值
func TestNewTrainer(t *testing.T) {
	trainer := NewTrainer()
	if trainer.numParallel != defaultNumParallel {
		t.Errorf("期望 numParallel=%d, 实际=%d", defaultNumParallel, trainer.numParallel)
	}
	if trainer.earlyStopScore != defaultEarlyStopScore {
		t.Errorf("期望 earlyStopScore=%f, 实际=%f", defaultEarlyStopScore, trainer.earlyStopScore)
	}
	if trainer.callbacks == nil {
		t.Error("期望 callbacks 非 nil")
	}
	if trainer.checkpointEveryNEpochs != defaultCheckpointEveryNEpochs {
		t.Errorf("期望 checkpointEveryNEpochs=%d, 实际=%d", defaultCheckpointEveryNEpochs, trainer.checkpointEveryNEpochs)
	}
	if trainer.checkpointOnImprove != defaultCheckpointOnImprove {
		t.Errorf("期望 checkpointOnImprove=%v, 实际=%v", defaultCheckpointOnImprove, trainer.checkpointOnImprove)
	}
}

// TestNewTrainer_使用选项 测试 Trainer 构造函数带选项
func TestNewTrainer_使用选项(t *testing.T) {
	cb := &Callbacks{}
	trainer := NewTrainer(
		WithNumParallel(5),
		WithEarlyStopScore(0.8),
		WithCallbacks(cb),
		WithResumeFrom("/tmp/ckpt"),
		WithCheckpointDir("/tmp/evolve"),
		WithCheckpointEveryNEpochs(3),
		WithCheckpointOnImprove(false),
	)
	if trainer.numParallel != 5 {
		t.Errorf("期望 numParallel=5, 实际=%d", trainer.numParallel)
	}
	if trainer.earlyStopScore != 0.8 {
		t.Errorf("期望 earlyStopScore=0.8, 实际=%f", trainer.earlyStopScore)
	}
	if trainer.callbacks != cb {
		t.Error("期望 callbacks 为传入实例")
	}
	if trainer.resumeFrom != "/tmp/ckpt" {
		t.Errorf("期望 resumeFrom=/tmp/ckpt, 实际=%s", trainer.resumeFrom)
	}
	if trainer.checkpointDir != "/tmp/evolve" {
		t.Errorf("期望 checkpointDir=/tmp/evolve, 实际=%s", trainer.checkpointDir)
	}
	if trainer.checkpointEveryNEpochs != 3 {
		t.Errorf("期望 checkpointEveryNEpochs=3, 实际=%d", trainer.checkpointEveryNEpochs)
	}
	if trainer.checkpointOnImprove != false {
		t.Error("期望 checkpointOnImprove=false")
	}
}

// TestSnapshotOperatorsState 测试快照 Operator 状态
func TestSnapshotOperatorsState(t *testing.T) {
	ops := map[string]operator.Operator{
		"op1": &fakeOperator{id: "op1", state: map[string]any{"prompt": "hello"}},
		"op2": &fakeOperator{id: "op2", state: map[string]any{"prompt": "world"}},
	}

	snapshot := SnapshotOperatorsState(ops)
	if len(snapshot) != 2 {
		t.Errorf("期望快照包含 2 个 operator, 实际=%d", len(snapshot))
	}
	if snapshot["op1"]["prompt"] != "hello" {
		t.Errorf("期望 op1 state prompt=hello, 实际=%v", snapshot["op1"]["prompt"])
	}
	if snapshot["op2"]["prompt"] != "world" {
		t.Errorf("期望 op2 state prompt=world, 实际=%v", snapshot["op2"]["prompt"])
	}
}

// TestSnapshotOperatorsState_空注册表 测试空 Operator 注册表快照
func TestSnapshotOperatorsState_空注册表(t *testing.T) {
	snapshot := SnapshotOperatorsState(nil)
	if snapshot == nil {
		t.Error("期望返回空 map 而非 nil")
	}
	if len(snapshot) != 0 {
		t.Errorf("期望快照为空, 实际=%d", len(snapshot))
	}
}

// TestRestoreOperatorsState 测试恢复 Operator 状态
func TestRestoreOperatorsState(t *testing.T) {
	op1 := &fakeOperator{id: "op1", state: map[string]any{"prompt": "old"}}
	ops := map[string]operator.Operator{
		"op1": op1,
	}

	state := map[string]map[string]any{
		"op1": {"prompt": "new"},
	}

	RestoreOperatorsState(ops, state)
	if op1.state["prompt"] != "new" {
		t.Errorf("期望恢复后 prompt=new, 实际=%v", op1.state["prompt"])
	}
}

// TestRestoreOperatorsState_忽略不存在的Operator 测试恢复时跳过不存在的 Operator
func TestRestoreOperatorsState_忽略不存在的Operator(t *testing.T) {
	op1 := &fakeOperator{id: "op1", state: map[string]any{"prompt": "old"}}
	ops := map[string]operator.Operator{
		"op1": op1,
	}

	state := map[string]map[string]any{
		"op1": {"prompt": "new1"},
		"op2": {"prompt": "new2"}, // op2 不存在于 ops
	}

	RestoreOperatorsState(ops, state)
	if op1.state["prompt"] != "new1" {
		t.Errorf("期望恢复后 prompt=new1, 实际=%v", op1.state["prompt"])
	}
}

// TestGetOperatorRegistry 测试从 Agent 获取 Operator 注册表
func TestGetOperatorRegistry(t *testing.T) {
	ops := map[string]operator.Operator{
		"op1": &fakeOperator{id: "op1"},
	}
	agent := &fakeTrainableAgent{
		operators: ops,
	}

	result := GetOperatorRegistry(agent)
	if len(result) != 1 {
		t.Errorf("期望 1 个 operator, 实际=%d", len(result))
	}
	if _, ok := result["op1"]; !ok {
		t.Error("期望包含 op1")
	}
}

// TestGetOperatorRegistry_nilAgent 测试 nil Agent 返回空注册表
func TestGetOperatorRegistry_nilAgent(t *testing.T) {
	result := GetOperatorRegistry(nil)
	if result != nil {
		t.Errorf("期望 nil, 实际=%v", result)
	}
}

// TestPredict_nilCases 测试 Predict 传入 nil 用例
func TestPredict_nilCases(t *testing.T) {
	trainer := NewTrainer()
	ctx := context.Background()
	agent := &fakeTrainableAgent{}

	predicts, sessions, err := trainer.Predict(ctx, agent, nil)
	if err != nil {
		t.Errorf("期望 nil 错误, 实际=%v", err)
	}
	if predicts != nil {
		t.Errorf("期望 nil predicts, 实际=%v", predicts)
	}
	if sessions != nil {
		t.Errorf("期望 nil sessions, 实际=%v", sessions)
	}
}

// TestPredictOnly_nilCases 测试 PredictOnly 传入 nil 用例
func TestPredictOnly_nilCases(t *testing.T) {
	trainer := NewTrainer()
	ctx := context.Background()
	agent := &fakeTrainableAgent{}

	predicts, err := trainer.PredictOnly(ctx, agent, nil)
	if err != nil {
		t.Errorf("期望 nil 错误, 实际=%v", err)
	}
	if predicts != nil {
		t.Errorf("期望 nil predicts, 实际=%v", predicts)
	}
}

// TestEvaluate_nilCases 测试 Evaluate 传入 nil 用例
func TestEvaluate_nilCases(t *testing.T) {
	trainer := NewTrainer()
	ctx := context.Background()
	agent := &fakeTrainableAgent{}

	score, evaluated, err := trainer.Evaluate(ctx, agent, nil)
	if err != nil {
		t.Errorf("期望 nil 错误, 实际=%v", err)
	}
	if score != 0 {
		t.Errorf("期望 score=0, 实际=%f", score)
	}
	if evaluated != nil {
		t.Errorf("期望 nil evaluated, 实际=%v", evaluated)
	}
}

// TestForward_nilCases 测试 Forward 传入 nil 用例
func TestForward_nilCases(t *testing.T) {
	trainer := NewTrainer()
	ctx := context.Background()
	agent := &fakeTrainableAgent{}

	score, evaluated, trajectories, sessions, err := trainer.Forward(ctx, agent, nil)
	if err != nil {
		t.Errorf("期望 nil 错误, 实际=%v", err)
	}
	if score != 0 {
		t.Errorf("期望 score=0, 实际=%f", score)
	}
	if evaluated != nil {
		t.Errorf("期望 nil evaluated, 实际=%v", evaluated)
	}
	if trajectories != nil {
		t.Errorf("期望 nil trajectories, 实际=%v", trajectories)
	}
	if sessions != nil {
		t.Errorf("期望 nil sessions, 实际=%v", sessions)
	}
}

// TestMeanScore 测试平均分数计算
func TestMeanScore(t *testing.T) {
	cases := []*dataset.EvaluatedCase{
		dataset.NewEvaluatedCase(dataset.Case{}, nil),
		dataset.NewEvaluatedCase(dataset.Case{}, nil),
		dataset.NewEvaluatedCase(dataset.Case{}, nil),
	}
	cases[0].SetScore(0.5)
	cases[1].SetScore(1.0)
	cases[2].SetScore(0.0)

	score := meanScore(cases)
	if score != 0.5 {
		t.Errorf("期望平均分=0.5, 实际=%f", score)
	}
}

// TestMeanScore_空列表 测试空列表平均分数
func TestMeanScore_空列表(t *testing.T) {
	score := meanScore(nil)
	if score != 0 {
		t.Errorf("期望平均分=0, 实际=%f", score)
	}
}

// TestApplyUpdates 测试应用更新到 Operator
// 对齐 Python: Trainer.apply_updates 直接调用 set_parameter，无返回值
func TestApplyUpdates(t *testing.T) {
	op1 := &fakeOperator{id: "op1", params: make(map[string]any)}
	ops := map[string]operator.Operator{
		"op1": op1,
	}

	key := schema.UpdateKey{"op1", "system_prompt"}
	updates := map[schema.UpdateKey]schema.UpdateValue{
		key: {Payload: "new prompt", Mode: schema.UpdateModeReplace, Effect: schema.UpdateEffectState},
	}

	ApplyUpdates(ops, updates)
	if op1.params["system_prompt"] != "new prompt" {
		t.Errorf("期望 system_prompt=new prompt, 实际=%v", op1.params["system_prompt"])
	}
}

// TestApplyUpdates_operator不存在 测试应用更新时 Operator 不存在
// 对齐 Python: op is not found 时跳过，不报错
func TestApplyUpdates_operator不存在(t *testing.T) {
	ops := map[string]operator.Operator{}

	key := schema.UpdateKey{"op_missing", "system_prompt"}
	updates := map[schema.UpdateKey]schema.UpdateValue{
		key: {Payload: "test", Mode: schema.UpdateModeReplace, Effect: schema.UpdateEffectState},
	}

	// 不应 panic
	ApplyUpdates(ops, updates)
}

// TestApplyUpdates_payload为nil 测试应用更新时 payload 为 nil
// 对齐 Python: value is not None 时才调用 set_parameter
func TestApplyUpdates_payload为nil(t *testing.T) {
	op1 := &fakeOperator{id: "op1", params: make(map[string]any)}
	ops := map[string]operator.Operator{
		"op1": op1,
	}

	key := schema.UpdateKey{"op1", "system_prompt"}
	updates := map[schema.UpdateKey]schema.UpdateValue{
		key: {Payload: nil, Mode: schema.UpdateModeReplace, Effect: schema.UpdateEffectState},
	}

	ApplyUpdates(ops, updates)
	if _, ok := op1.params["system_prompt"]; ok {
		t.Error("期望 payload 为 nil 时不调用 SetParameter")
	}
}

// TestNewProgress 测试 Progress 构造默认值
func TestNewProgress(t *testing.T) {
	p := NewProgress()
	if p.MaxEpoch != defaultNumIterations {
		t.Errorf("期望 MaxEpoch=%d, 实际=%d", defaultNumIterations, p.MaxEpoch)
	}
	if p.MaxBatchIter != 1 {
		t.Errorf("期望 MaxBatchIter=1, 实际=%d", p.MaxBatchIter)
	}
	if p.StartEpoch != 0 {
		t.Errorf("期望 StartEpoch=0, 实际=%d", p.StartEpoch)
	}
}

// TestNewProgressWithMaxEpoch 测试指定最大 epoch 的 Progress 构造
func TestNewProgressWithMaxEpoch(t *testing.T) {
	p := NewProgressWithMaxEpoch(10)
	if p.MaxEpoch != 10 {
		t.Errorf("期望 MaxEpoch=10, 实际=%d", p.MaxEpoch)
	}
}

// TestSetCallbacks 测试设置回调
func TestSetCallbacks(t *testing.T) {
	trainer := NewTrainer()
	cb := &Callbacks{}
	trainer.SetCallbacks(cb)
	if trainer.callbacks != cb {
		t.Error("期望 callbacks 为设置后的实例")
	}
}

// TestNewCallbacks 测试 Callbacks 默认构造
func TestNewCallbacks(t *testing.T) {
	cb := NewCallbacks()
	if cb == nil {
		t.Error("期望 NewCallbacks 返回非 nil")
	}
}

// TestProgress_RunEpoch 测试 RunEpoch 迭代
func TestProgress_RunEpoch(t *testing.T) {
	p := NewProgressWithMaxEpoch(5)
	p.StartEpoch = 0

	var epochs []int
	for epoch := range p.RunEpoch() {
		epochs = append(epochs, epoch)
	}

	// 期望迭代 1,2,3,4,5
	if len(epochs) != 5 {
		t.Errorf("期望 5 个 epoch, 实际 %d", len(epochs))
	}
	for i, want := range []int{1, 2, 3, 4, 5} {
		if epochs[i] != want {
			t.Errorf("epochs[%d]=%d, 期望 %d", i, epochs[i], want)
		}
	}
	// 自然结束后 CurrentEpoch 应为 MaxEpoch
	if p.CurrentEpoch != 5 {
		t.Errorf("期望 CurrentEpoch=5, 实际=%d", p.CurrentEpoch)
	}
}

// TestProgress_RunEpoch_从断点续训 测试 RunEpoch 从 StartEpoch+1 开始
func TestProgress_RunEpoch_从断点续训(t *testing.T) {
	p := NewProgressWithMaxEpoch(5)
	p.StartEpoch = 2 // 从 epoch 3 开始

	var epochs []int
	for epoch := range p.RunEpoch() {
		epochs = append(epochs, epoch)
	}

	// 期望迭代 3,4,5
	if len(epochs) != 3 {
		t.Errorf("期望 3 个 epoch, 实际 %d", len(epochs))
	}
	for i, want := range []int{3, 4, 5} {
		if epochs[i] != want {
			t.Errorf("epochs[%d]=%d, 期望 %d", i, epochs[i], want)
		}
	}
}

// TestProgress_RunEpoch_中断时CurrentEpoch保持 测试 break 中断时 CurrentEpoch 保持
func TestProgress_RunEpoch_中断时CurrentEpoch保持(t *testing.T) {
	p := NewProgressWithMaxEpoch(10)

	for epoch := range p.RunEpoch() {
		if epoch == 3 {
			break
		}
	}

	// break 时 CurrentEpoch 应为 3
	if p.CurrentEpoch != 3 {
		t.Errorf("期望 CurrentEpoch=3, 实际=%d", p.CurrentEpoch)
	}
}

// TestProgress_RunBatch 测试 RunBatch 迭代
func TestProgress_RunBatch(t *testing.T) {
	p := NewProgress()
	p.MaxBatchIter = 4
	p.BestBatchScore = 0.5 // 应被重置为 0

	var batches []int
	for batch := range p.RunBatch() {
		batches = append(batches, batch)
	}

	// 期望迭代 0,1,2,3
	if len(batches) != 4 {
		t.Errorf("期望 4 个 batch, 实际 %d", len(batches))
	}
	for i, want := range []int{0, 1, 2, 3} {
		if batches[i] != want {
			t.Errorf("batches[%d]=%d, 期望 %d", i, batches[i], want)
		}
	}
	// BestBatchScore 应被重置为 0
	if p.BestBatchScore != 0 {
		t.Errorf("期望 BestBatchScore=0, 实际=%f", p.BestBatchScore)
	}
}

// TestProgress_RunBatch_中断时CurrentBatchIter保持 测试 break 中断时 CurrentBatchIter 保持
func TestProgress_RunBatch_中断时CurrentBatchIter保持(t *testing.T) {
	p := NewProgress()
	p.MaxBatchIter = 10

	for batch := range p.RunBatch() {
		if batch == 2 {
			break
		}
	}

	if p.CurrentBatchIter != 2 {
		t.Errorf("期望 CurrentBatchIter=2, 实际=%d", p.CurrentBatchIter)
	}
}

// TestNormalizeUpdates 测试 normalizeUpdates 转换
func TestNormalizeUpdates(t *testing.T) {
	key := schema.UpdateKey{"op1", "system_prompt"}

	// 测试 UpdateValue 类型值
	uv := schema.UpdateValue{Payload: "test", Mode: schema.UpdateModeReplace, Effect: schema.UpdateEffectState}
	updated := map[schema.UpdateKey]any{key: uv}

	result := normalizeUpdates(updated)
	if len(result) != 1 {
		t.Errorf("期望 1 个结果, 实际=%d", len(result))
	}
	if result[key].Payload != "test" {
		t.Errorf("期望 Payload=test, 实际=%v", result[key].Payload)
	}
}

// TestNormalizeUpdates_nil 测试 normalizeUpdates 传入 nil
func TestNormalizeUpdates_nil(t *testing.T) {
	result := normalizeUpdates(nil)
	if result != nil {
		t.Errorf("期望 nil, 实际=%v", result)
	}
}

// TestNormalizeUpdates_简单值 测试 normalizeUpdates 包装简单值
func TestNormalizeUpdates_简单值(t *testing.T) {
	key := schema.UpdateKey{"op1", "system_prompt"}
	updated := map[schema.UpdateKey]any{key: "simple string"}

	result := normalizeUpdates(updated)
	if len(result) != 1 {
		t.Errorf("期望 1 个结果, 实际=%d", len(result))
	}
	if result[key].Payload != "simple string" {
		t.Errorf("期望 Payload=simple string, 实际=%v", result[key].Payload)
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestUpdaterRequiresForward_无Updater 测试无 Updater 时 UpdaterRequiresForward 返回 true
func TestUpdaterRequiresForward_无Updater(t *testing.T) {
	trainer := NewTrainer()
	if !trainer.UpdaterRequiresForward() {
		t.Error("无 updater 时期望返回 true")
	}
}

// TestSaveCheckpointIfNeeded_无Manager 测试无 CheckpointManager 时不执行保存
func TestSaveCheckpointIfNeeded_无Manager(t *testing.T) {
	trainer := NewTrainer()
	err := trainer.SaveCheckpointIfNeeded(0, 0.5, nil, false)
	if err != nil {
		t.Errorf("期望 nil 错误, 实际=%v", err)
	}
}

// TestResumeIfNeeded_无ResumeFrom 测试无 resumeFrom 时不执行恢复
func TestResumeIfNeeded_无ResumeFrom(t *testing.T) {
	trainer := NewTrainer()
	err := trainer.ResumeIfNeeded(context.Background(), nil)
	if err != nil {
		t.Errorf("期望 nil 错误, 实际=%v", err)
	}
}
