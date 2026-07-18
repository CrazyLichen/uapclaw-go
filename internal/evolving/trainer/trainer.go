package trainer

import (
	"context"
	"errors"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
	"github.com/uapclaw/uapclaw-go/internal/evolving/evaluator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	updaterpkg "github.com/uapclaw/uapclaw-go/internal/evolving/updater"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Trainer 离线自演化训练编排器。
//
// 编排 "evaluate → update → writeback" 自演化循环，
// 接受 Updater 和 BaseEvaluator，管理检查点保存/恢复和早停。
//
// 当前为纯接口桩骨架：所有依赖类型暂用 any 占位，
// 核心方法（Train/Forward/Evaluate/Predict）返回 errors.New("not implemented")。
// 后续章节填充依赖 interface 后替换 any 并实现逻辑体。
//
// 对应 Python: openjiuwen/agent_evolving/trainer/trainer.py Trainer
type Trainer struct {
	// updater 更新生成器。
	// 对应 Python: evolving/updater/protocol.py Updater
	updater updaterpkg.Updater
	// evaluator 评估器。
	// 对应 Python: evolving/evaluator.BaseEvaluator
	evaluator evaluator.BaseEvaluator
	// extractor 轨迹提取器。
	// 依赖 9.77 Trajectory，暂用 any 占位，填充后替换为 evolving/trajectory.Extractor
	extractor any
	// callbacks 训练生命周期回调
	callbacks *Callbacks
	// numParallel 并发推理数。
	// 对应 Python: _num_parallel, 默认 TuneConstant.default_parallel_num=1
	numParallel int
	// earlyStopScore 早停分数阈值。
	// 对应 Python: _early_stop_score, 默认 TuneConstant.default_early_stop_score=1.0
	earlyStopScore float64
	// checkpointDir 检查点目录。非空启用检查点保存。
	// 9.78 填充时由此字段创建 FileCheckpointStore(checkpointDir) 赋给 checkpointStore。
	// 对应 Python: checkpoint_dir
	checkpointDir string
	// checkpointEveryNEpochs 每 N 个 epoch 保存一次检查点。
	// 对应 Python: checkpoint_every_n_epochs, 默认 1
	checkpointEveryNEpochs int
	// checkpointOnImprove 验证分数提升时是否保存检查点。
	// 对应 Python: checkpoint_on_improve, 默认 true
	checkpointOnImprove bool
	// checkpointStore 检查点存储。
	// 依赖 9.78 EvolveCheckpoint，暂用 any 占位，填充后替换为 evolving/checkpointing.FileStore
	checkpointStore any
	// resumeFrom 恢复检查点路径
	resumeFrom string
	// checkpointManager 检查点管理器。
	// 依赖 9.78，暂用 any 占位，填充后替换为 evolving/checkpointing.Manager
	checkpointManager any
}

// TrainerOption Trainer 构造选项函数。
type TrainerOption func(*Trainer)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// 默认并行数，对应 Python TuneConstant.default_parallel_num=1
	defaultNumParallel = 1
	// 默认早停分数，对应 Python TuneConstant.default_early_stop_score=1.0
	defaultEarlyStopScore = 1.0
	// 默认每 N epoch 保存检查点，对应 Python checkpoint_every_n_epochs=1
	defaultCheckpointEveryNEpochs = 1
	// 默认验证提升时保存检查点，对应 Python checkpoint_on_improve=True
	defaultCheckpointOnImprove = true
)

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent Trainer 包日志组件常量
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTrainer 创建 Trainer 实例。
//
// 对应 Python: Trainer.__init__(updater, evaluator, extractor, callbacks, ...)
func NewTrainer(opts ...TrainerOption) *Trainer {
	t := &Trainer{
		numParallel:            defaultNumParallel,
		earlyStopScore:         defaultEarlyStopScore,
		checkpointEveryNEpochs: defaultCheckpointEveryNEpochs,
		checkpointOnImprove:    defaultCheckpointOnImprove,
		callbacks:              NewCallbacks(),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Train 执行离线自演化训练：验证基线评估 → 多轮 "训练前向 → 更新 → 验证评估 → 检查点"。
//
// 参数:
//   - agent: 待优化的 Agent（必须实现 get_operators()）
//   - trainCases: 训练用例加载器（依赖 9.70b CaseLoader）
//   - valCases: 验证用例加载器（若为 nil 则使用 trainCases）
//   - numIterations: 最大训练 epoch 数
//   - config: 传递给 updater.update 的配置（依赖 9.70c）
//
// 返回优化后的 Agent 和 error。当前为桩实现。
//
// 对应 Python: Trainer.train(agent, train_cases, val_cases, num_iterations)
func (t *Trainer) Train(_ context.Context, _ any, _ any, _ any, _ int, _ any) (any, error) {
	// TODO: 依赖 9.70a Operator + 9.70b Dataset + 9.70c Updater + 9.71 Evaluator + 9.77 Trajectory + 9.78 Checkpoint 填充后实现
	return nil, errors.New("not implemented: Trainer.Train")
}

// Forward 单次前向推理 + 评估 + 轨迹提取。
//
// 返回 (平均分数, 评估结果列表, 轨迹列表, Session列表, error)。当前为桩实现。
//
// 对应 Python: Trainer.forward(agent, cases) -> (score, evaluated, trajectories, sessions)
func (t *Trainer) Forward(_ context.Context, _ any, _ any) (float64, any, any, any, error) {
	// TODO: 依赖 9.71 Evaluator + 9.77 Trajectory 填充后实现
	return 0, nil, nil, nil, errors.New("not implemented: Trainer.Forward")
}

// Evaluate 在用例集上运行推理和评估，返回平均分数和评估结果。
//
// 不提取轨迹（与 Forward 的区别）。当前为桩实现。
//
// 对应 Python: Trainer.evaluate(agent, cases) -> (score, evaluated)
func (t *Trainer) Evaluate(_ context.Context, _ any, _ any) (float64, any, error) {
	// TODO: 依赖 9.71 Evaluator 填充后实现
	return 0, nil, errors.New("not implemented: Trainer.Evaluate")
}

// PredictOnly 仅运行推理，返回每个用例的模型输出（不含 Session）。
//
// 当前为桩实现。
//
// 对应 Python: Trainer.predict_only(agent, cases) -> predicts
func (t *Trainer) PredictOnly(_ context.Context, _ any, _ any) (any, error) {
	// TODO: 依赖 Session + Agent.Invoke 填充后实现
	return nil, errors.New("not implemented: Trainer.PredictOnly")
}

// Predict 运行 Agent 推理（含 Session），并发度由 numParallel 控制。
//
// 返回 (模型输出列表, Session列表, error)。当前为桩实现。
//
// 对应 Python: Trainer.predict(agent, cases) -> (predicts, sessions)
func (t *Trainer) Predict(_ context.Context, _ any, _ any) (any, any, error) {
	// TODO: 依赖 Session + Agent.Invoke 填充后实现
	return nil, nil, errors.New("not implemented: Trainer.Predict")
}

// ApplyUpdates 将 Updater 生成的更新应用到 Operator 注册表。
//
// 对应 Python: Trainer.apply_updates(operators, updates) — 静态方法
// 遍历 updates 映射，对每个 Operator 调用 ApplyUpdate 应用结构化更新。
func ApplyUpdates(operators map[string]operator.Operator, updates map[schema.UpdateKey]schema.UpdateValue) []schema.ApplyResult {
	var results []schema.ApplyResult
	for key, update := range updates {
		op, ok := operators[key.OperatorID()]
		if !ok {
			results = append(results, schema.ApplyResultWithErrors(
				key.OperatorID(), key.Target(),
				update.Mode, update.Effect, update.Payload,
				update.ChangeType, update.Metadata,
				"operator not found: "+key.OperatorID(),
			))
			continue
		}
		result := op.ApplyUpdate(key.Target(), update)
		results = append(results, result)
	}
	return results
}

// SelectBestCandidateOnVal 在验证集上选择最优候选更新。
//
// 比较不同候选更新在验证集上的分数，返回最优更新的索引。
// 当前为桩实现。
//
// 对应 Python: Trainer._select_best_candidate_on_val(candidates, agent, val_cases)
func (t *Trainer) SelectBestCandidateOnVal(_ context.Context, _ any, _ any, _ any) (int, error) {
	// TODO: 依赖 9.70a Operator + 9.71 Evaluator 填充后实现
	return 0, errors.New("not implemented: Trainer.SelectBestCandidateOnVal")
}

// SnapshotOperatorsState 快照当前所有 Operator 的状态。
//
// 保存 Operator 注册表的深拷贝，用于更新后回滚。
// 当前为桩实现。
//
// 对应 Python: Trainer._snapshot_operators_state(operators) — 静态方法
func SnapshotOperatorsState(_ map[string]operator.Operator) any {
	// TODO: 依赖 9.70a Operator 填充后实现
	return nil
}

// RestoreOperatorsState 从快照恢复 Operator 注册表状态。
//
// 将之前快照的状态恢复到 Operator 注册表，用于更新失败时回滚。
// 当前为桩实现。
//
// 对应 Python: Trainer._restore_operators_state(operators, snapshot) — 静态方法
func RestoreOperatorsState(_ map[string]operator.Operator, _ any) error {
	// TODO: 依赖 9.70a Operator 填充后实现
	return errors.New("not implemented: RestoreOperatorsState")
}

// GetOperatorRegistry 从 Agent 获取 Operator 注册表。
//
// 调用 Agent 的 get_operators() 方法获取其关联的 Operator 映射。
// 当前为桩实现。
//
// 对应 Python: Trainer._get_operator_registry(agent) — 静态方法
func GetOperatorRegistry(_ any) (map[string]operator.Operator, error) {
	// TODO: 依赖 9.70a Operator 填充后实现
	return nil, errors.New("not implemented: GetOperatorRegistry")
}

// BindUpdater 将 Updater 绑定到 Agent 的 Operator 注册表。
//
// 在训练开始前调用，使 Updater 能访问和修改 Operator。
// 返回绑定的 Operator 数量；0 触发软退出。
//
// 对应 Python: Trainer._bind_updater(updater, operators)
func (t *Trainer) BindUpdater(operators map[string]operator.Operator, config map[string]any) int {
	if t.updater == nil {
		return 0
	}
	return t.updater.Bind(operators, nil, config)
}

// UpdaterRequiresForward 判断 Updater 是否需要前向推理结果。
//
// 某些 Updater（如基于梯度的）需要前向推理产生的轨迹数据，
// 而另一些（如基于规则的）则不需要。
// 当 Updater 为 nil 时默认返回 true（兼容旧行为）。
//
// 对应 Python: Trainer._updater_requires_forward(updater)
func (t *Trainer) UpdaterRequiresForward() bool {
	if t.updater == nil {
		return true
	}
	return t.updater.RequiresForwardData()
}

// ResumeIfNeeded 如果配置了恢复路径，从检查点恢复训练状态。
//
// 读取 resumeFrom 指定的检查点，恢复 epoch、Operator 状态等。
// 当前为桩实现。
//
// 对应 Python: Trainer._resume_if_needed(agent)
func (t *Trainer) ResumeIfNeeded(_ context.Context, _ any) error {
	// TODO: 依赖 9.78 EvolveCheckpoint 填充后实现
	return errors.New("not implemented: Trainer.ResumeIfNeeded")
}

// SaveCheckpointIfNeeded 根据条件判断是否保存检查点。
//
// 当达到 checkpointEveryNEpochs 间隔或验证分数提升（checkpointOnImprove）时保存。
// 当前为桩实现。
//
// 对应 Python: Trainer._save_checkpoint_if_needed(epoch, val_score, operators, improved)
func (t *Trainer) SaveCheckpointIfNeeded(_ int, _ float64, _ map[string]operator.Operator, _ bool) error {
	// TODO: 依赖 9.78 EvolveCheckpoint 填充后实现
	return errors.New("not implemented: Trainer.SaveCheckpointIfNeeded")
}

// SetCallbacks 设置训练生命周期回调。
//
// 对应 Python: Trainer.set_callbacks(callbacks)
func (t *Trainer) SetCallbacks(callbacks *Callbacks) {
	t.callbacks = callbacks
}

// WithUpdater 设置更新生成器。
func WithUpdater(u updaterpkg.Updater) TrainerOption {
	return func(t *Trainer) { t.updater = u }
}

// WithEvaluator 设置评估器。
func WithEvaluator(e evaluator.BaseEvaluator) TrainerOption {
	return func(t *Trainer) { t.evaluator = e }
}

// WithExtractor 设置轨迹提取器。
// 依赖 9.77 Trajectory，暂用 any 占位。
func WithExtractor(extractor any) TrainerOption {
	return func(t *Trainer) { t.extractor = extractor }
}

// WithCallbacks 设置训练生命周期回调。
func WithCallbacks(callbacks *Callbacks) TrainerOption {
	return func(t *Trainer) { t.callbacks = callbacks }
}

// WithNumParallel 设置并发推理数。
// 对应 Python: num_parallel, 范围 [1, 20]（TuneConstant.min/max_parallel_num）。
func WithNumParallel(n int) TrainerOption {
	return func(t *Trainer) { t.numParallel = n }
}

// WithEarlyStopScore 设置早停分数阈值。
// 对应 Python: early_stop_score, 范围 [0.0, 1.0]。
func WithEarlyStopScore(score float64) TrainerOption {
	return func(t *Trainer) { t.earlyStopScore = score }
}

// WithCheckpointDir 设置检查点目录（非空启用检查点保存）。
// 9.78 填充时由此字段创建 FileCheckpointStore(dir) 赋给 checkpointStore。
// ⤵️ 待具体章节回填：延迟初始化策略，当前仅存储路径
// 对应 Python: checkpoint_dir, None 表示禁用。
func WithCheckpointDir(dir string) TrainerOption {
	return func(t *Trainer) { t.checkpointDir = dir }
}

// WithCheckpointEveryNEpochs 设置每 N 个 epoch 保存一次检查点。
// 对应 Python: checkpoint_every_n_epochs, 默认 1。
func WithCheckpointEveryNEpochs(n int) TrainerOption {
	return func(t *Trainer) { t.checkpointEveryNEpochs = n }
}

// WithCheckpointOnImprove 设置验证分数提升时是否保存检查点。
// 对应 Python: checkpoint_on_improve, 默认 true。
func WithCheckpointOnImprove(b bool) TrainerOption {
	return func(t *Trainer) { t.checkpointOnImprove = b }
}

// WithResumeFrom 设置恢复检查点路径。
// 对应 Python: resume_from
func WithResumeFrom(path string) TrainerOption {
	return func(t *Trainer) { t.resumeFrom = path }
}

// WithCheckpointManager 设置检查点管理器。
// 依赖 9.78，暂用 any 占位。
func WithCheckpointManager(manager any) TrainerOption {
	return func(t *Trainer) { t.checkpointManager = manager }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// meanScore 计算评估用例的平均分数。
//
// 对应 Python: Trainer._mean_score(evaluated)
func meanScore(cases []*dataset.EvaluatedCase) float64 {
	if len(cases) == 0 {
		return 0
	}
	var total float64
	for _, c := range cases {
		total += c.GetScore()
	}
	return total / float64(len(cases))
}
