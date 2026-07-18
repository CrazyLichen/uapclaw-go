package trainer

import (
	"context"
	"fmt"
	"math"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
	"github.com/uapclaw/uapclaw-go/internal/evolving/evaluator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	updaterpkg "github.com/uapclaw/uapclaw-go/internal/evolving/updater"
	"golang.org/x/sync/errgroup"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TrainableAgent 可训练 Agent 接口。
//
// Trainer 需要通过 Agent 获取 Operator 注册表和执行推理，
// 这是 BaseAgent 的最小扩展接口。
//
// 对应 Python: BaseAgent + get_operators() 方法
type TrainableAgent interface {
	// Invoke 非流式调用 Agent。
	// 对应 Python: BaseAgent.invoke(inputs, session)
	Invoke(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error)
	// Card 返回 Agent 身份卡片。
	// 对应 Python: BaseAgent.card 属性
	Card() *agentschema.AgentCard
	// GetOperators 获取 Operator 注册表。
	// 对应 Python: BaseAgent.get_operators()
	GetOperators() map[string]operator.Operator
}

// Trainer 离线自演化训练编排器。
//
// 编排 "evaluate → update → writeback" 自演化循环，
// 接受 Updater 和 BaseEvaluator，管理检查点保存/恢复和早停。
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
	// ⤵️ 待 9.77 Trajectory Extractor 回填：暂用 any 占位，填充后替换为 trajectory.Extractor
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
	// ⤵️ 待 9.78 EvolveCheckpoint 回填：由此字段创建 FileCheckpointStore(checkpointDir) 赋给 checkpointStore
	// 对应 Python: checkpoint_dir
	checkpointDir string
	// checkpointEveryNEpochs 每 N 个 epoch 保存一次检查点。
	// 对应 Python: checkpoint_every_n_epochs, 默认 1
	checkpointEveryNEpochs int
	// checkpointOnImprove 验证分数提升时是否保存检查点。
	// 对应 Python: checkpoint_on_improve, 默认 true
	checkpointOnImprove bool
	// checkpointStore 检查点存储。
	// ⤵️ 待 9.78 EvolveCheckpoint 回填：填充后替换为 evolving/checkpointing.FileStore
	checkpointStore any
	// resumeFrom 恢复检查点路径
	resumeFrom string
	// checkpointManager 检查点管理器。
	// ⤵️ 待 9.78 EvolveCheckpoint 回填：填充后替换为 evolving/checkpointing.Manager
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
//   - ctx: 上下文
//   - agent: 待优化的 Agent（必须实现 TrainableAgent 接口）
//   - trainCases: 训练用例加载器
//   - valCases: 验证用例加载器（若为 nil 则使用 trainCases）
//   - numIterations: 最大训练 epoch 数
//   - config: 传递给 updater.Update 的配置
//
// 返回优化后的 Agent 和 error。
//
// 对应 Python: Trainer.train(agent, train_cases, val_cases, num_iterations)
func (t *Trainer) Train(
	ctx context.Context,
	agent TrainableAgent,
	trainCases *dataset.CaseLoader,
	valCases *dataset.CaseLoader,
	numIterations int,
	config map[string]any,
) (TrainableAgent, error) {
	progress := NewProgressWithMaxEpoch(numIterations)
	if valCases == nil {
		valCases = trainCases
	}

	operators := GetOperatorRegistry(agent)
	if t.BindUpdater(operators, config) == 0 {
		logger.Error(logComponent).Msg("[Trainer] 无 Operator 匹配 Updater 目标，软退出不训练")
		return agent, nil
	}

	// ⤵️ 待 9.78 EvolveCheckpoint 回填：恢复检查点逻辑
	// 对齐 Python: self._resume_if_needed(agent, progress)
	_ = t.ResumeIfNeeded(ctx, agent)

	var curEpochEvaluated []*dataset.EvaluatedCase
	if t.UpdaterRequiresForward() {
		score, evaluated, err := t.Evaluate(ctx, agent, valCases)
		if err != nil {
			return agent, fmt.Errorf("Train 基线评估失败: %w", err)
		}
		progress.CurrentEpochScore = score
		progress.BestScore = math.Max(progress.BestScore, score)
		curEpochEvaluated = evaluated
	} else {
		// 黑盒优化器：跳过初始验证基线（使用内部评估）
		progress.CurrentEpochScore = 0.0
		curEpochEvaluated = nil
	}

	// 触发 OnTrainBegin 回调
	invokeCallback(t.callbacks.OnTrainBegin, agent, progress, curEpochEvaluated)

	if progress.BestScore >= t.earlyStopScore {
		invokeCallback(t.callbacks.OnTrainEnd, agent, progress, curEpochEvaluated)
		return agent, nil
	}

	for range progress.RunEpoch() {
		invokeCallback(t.callbacks.OnTrainEpochBegin, agent, progress, nil)

		var evaluated []*dataset.EvaluatedCase

		if t.UpdaterRequiresForward() {
			forwardScore, forwardEvaluated, _, _, err := t.Forward(ctx, agent, trainCases)
			if err != nil {
				logger.Warn(logComponent).
					Int("epoch", progress.CurrentEpoch).
					Err(err).
					Msg("Train Forward 失败")
			}
			progress.CurrentEpochScore = forwardScore
			evaluated = forwardEvaluated
		} else {
			// 黑盒优化器：跳过 forward，传递空数据（优化器内部生成）
			progress.CurrentEpochScore = 0.0
		}

		// 对齐 Python: updated = asyncio.run(self._updater.update(trajectories, evaluated, config=kwargs))
		// ⤵️ 待 9.77 Trajectory Extractor 回填：传递 trajectories
		updated, updateErr := t.updater.Update(ctx, nil, evaluated, config)
		if updateErr != nil {
			logger.Warn(logComponent).
				Int("epoch", progress.CurrentEpoch).
				Err(updateErr).
				Msg("Train Updater.Update 失败")
		}

		var valScore float64
		var valEvaluated []*dataset.EvaluatedCase

		if updateErr == nil {
			// 对齐 Python: isinstance(updated, list) — 候选方案A
			// Go 中 Updater.Update 返回 map[schema.UpdateKey]any，
			// 候选列表模式通过检查 value 是否为 []map[schema.UpdateKey]schema.UpdateValue 实现
			// ⤵️ 待 9.72 Optimizer 回填：候选列表多方案评估，当前仅处理单方案
			updates := normalizeUpdates(updated)
			ApplyUpdates(operators, updates)
			valScore, valEvaluated, _ = t.Evaluate(ctx, agent, valCases)
		}

		improved := valScore > progress.BestScore
		if improved {
			progress.BestScore = valScore
		}

		invokeCallback(t.callbacks.OnTrainEpochEnd, agent, progress, valEvaluated)

		// ⤵️ 待 9.78 EvolveCheckpoint 回填：检查点保存逻辑
		// 对齐 Python: self._save_checkpoint_if_needed(agent, progress, improved=improved)
		_ = t.SaveCheckpointIfNeeded(progress.CurrentEpoch, valScore, operators, improved)

		if progress.BestScore >= t.earlyStopScore {
			break
		}
	}

	invokeCallback(t.callbacks.OnTrainEnd, agent, progress, curEpochEvaluated)

	return agent, nil
}

// Forward 单次前向推理 + 评估 + 轨迹提取。
//
// 返回 (平均分数, 评估结果列表, 轨迹列表, Session列表, error)。
// 轨迹提取部分 ⤵️ 待 9.77 Trajectory Extractor 回填。
//
// 对应 Python: Trainer.forward(agent, cases) -> (score, evaluated, trajectories, sessions)
func (t *Trainer) Forward(
	ctx context.Context,
	agent TrainableAgent,
	cases *dataset.CaseLoader,
) (float64, []*dataset.EvaluatedCase, any, []*session.Session, error) {
	if cases == nil || cases.Len() == 0 {
		return 0, nil, nil, nil, nil
	}

	predicts, sessions, err := t.Predict(ctx, agent, cases)
	if err != nil {
		return 0, nil, nil, nil, fmt.Errorf("Forward Predict 失败: %w", err)
	}

	caseList := cases.Cases()
	evaluated, err := t.evaluator.BatchEvaluate(ctx, caseList, predicts, t.numParallel)
	if err != nil {
		return 0, nil, nil, nil, fmt.Errorf("Forward BatchEvaluate 失败: %w", err)
	}

	score := meanScore(evaluated)

	// ⤵️ 待 9.77 Trajectory Extractor 回填：从每个 Session 提取 Trajectory
	// 对齐 Python:
	//   trajectories = []
	//   for case, sess in zip(cases.get_cases(), sessions):
	//       trajectories.append(self._extractor.extract(sess, case_id=case.case_id))
	var trajectories any = nil
	_ = predicts

	logger.Info(logComponent).
		Float64("score", score).
		Int("evaluated_count", len(evaluated)).
		Msg("Forward 完成")

	return score, evaluated, trajectories, sessions, nil
}

// Evaluate 在用例集上运行推理和评估，返回平均分数和评估结果。
//
// 不提取轨迹（与 Forward 的区别）。
//
// 对应 Python: Trainer.evaluate(agent, cases) -> (score, evaluated)
func (t *Trainer) Evaluate(
	ctx context.Context,
	agent TrainableAgent,
	cases *dataset.CaseLoader,
) (float64, []*dataset.EvaluatedCase, error) {
	if cases == nil || cases.Len() == 0 {
		return 0, nil, nil
	}

	predicts, err := t.PredictOnly(ctx, agent, cases)
	if err != nil {
		return 0, nil, fmt.Errorf("Evaluate PredictOnly 失败: %w", err)
	}

	caseList := cases.Cases()
	evaluated, err := t.evaluator.BatchEvaluate(ctx, caseList, predicts, t.numParallel)
	if err != nil {
		return 0, nil, fmt.Errorf("Evaluate BatchEvaluate 失败: %w", err)
	}

	score := meanScore(evaluated)
	return score, evaluated, nil
}

// PredictOnly 仅运行推理，返回每个用例的模型输出（不含 Session）。
//
// 对应 Python: Trainer.predict_only(agent, cases) -> predicts
func (t *Trainer) PredictOnly(
	ctx context.Context,
	agent TrainableAgent,
	cases *dataset.CaseLoader,
) ([]map[string]any, error) {
	if cases == nil {
		return nil, nil
	}

	predicts, _, err := t.Predict(ctx, agent, cases)
	return predicts, err
}

// Predict 运行 Agent 推理（含 Session），并发度由 numParallel 控制。
//
// 返回 (模型输出列表, Session列表, error)。
//
// 对应 Python: Trainer.predict(agent, cases) -> (predicts, sessions)
func (t *Trainer) Predict(
	ctx context.Context,
	agent TrainableAgent,
	cases *dataset.CaseLoader,
) ([]map[string]any, []*session.Session, error) {
	if cases == nil {
		return nil, nil, nil
	}

	caseList := cases.Cases()
	if len(caseList) == 0 {
		return nil, nil, nil
	}

	predicts := make([]map[string]any, len(caseList))
	sessionsList := make([]*session.Session, len(caseList))

	// 对齐 Python: asyncio.Semaphore(min(self._num_parallel, len(case_list)))
	limit := t.numParallel
	if limit > len(caseList) {
		limit = len(caseList)
	}

	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(limit)

	for i, case_ := range caseList {
		i, case_ := i, case_
		g.Go(func() error {
			// 对齐 Python: session = create_agent_session()
			sess := session.CreateAgentSession(case_.CaseID, agent.Card(), nil)

			// 对齐 Python: res = await agent.invoke({**case.inputs, "conversation_id": case.case_id}, session=session)
			inputs := make(map[string]any, len(case_.Inputs)+1)
			for k, v := range case_.Inputs {
				inputs[k] = v
			}
			inputs["conversation_id"] = case_.CaseID

			res, err := agent.Invoke(gCtx, inputs, agentinterfaces.WithSession(sess))
			if err != nil {
				// 对齐 Python: res = dict(error=f"Get wrong result due to {str(e)}")
				predicts[i] = map[string]any{"error": fmt.Sprintf("Get wrong result due to %s", err.Error())}
				sessionsList[i] = sess
				return nil // 不中断其他 case
			}

			predicts[i] = res
			sessionsList[i] = sess
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, nil, fmt.Errorf("Predict 并发推理失败: %w", err)
	}

	return predicts, sessionsList, nil
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
// 候选评估与选择（方案 A）：
// - 在验证集上评估每个候选更新，选择最优
// - 使用 Operator.GetState/LoadState 快照回滚，避免复制 Agent
// - 最终恢复 operators 为最优状态（提交最优）
//
// 返回 (最优分数, 最优评估结果, error)。
//
// 对应 Python: Trainer._select_best_candidate_on_val(candidates, agent, val_cases)
func (t *Trainer) SelectBestCandidateOnVal(
	ctx context.Context,
	agent TrainableAgent,
	operators map[string]operator.Operator,
	candidates []map[schema.UpdateKey]schema.UpdateValue,
	valCases *dataset.CaseLoader,
) (float64, []*dataset.EvaluatedCase, error) {
	if len(candidates) == 0 {
		return t.Evaluate(ctx, agent, valCases)
	}

	baseState := SnapshotOperatorsState(operators)

	var bestScore float64 = math.Inf(-1)
	var bestEvaluated []*dataset.EvaluatedCase
	var bestState map[string]map[string]any

	for idx, candUpdates := range candidates {
		RestoreOperatorsState(operators, baseState)
		ApplyUpdates(operators, candUpdates)

		candScore, candEvaluated, err := t.Evaluate(ctx, agent, valCases)
		if err != nil {
			logger.Warn(logComponent).
				Int("candidate_idx", idx).
				Err(err).
				Msg("候选评估失败")
			continue
		}

		logger.Info(logComponent).
			Int("candidate_idx", idx).
			Float64("val_score", candScore).
			Msg("候选评估完成")

		if candScore > bestScore {
			bestScore = candScore
			bestEvaluated = candEvaluated
			bestState = SnapshotOperatorsState(operators)
		}
	}

	if bestState != nil {
		RestoreOperatorsState(operators, bestState)
		return bestScore, bestEvaluated, nil
	}

	// 没有成功评估的候选，恢复基线
	RestoreOperatorsState(operators, baseState)
	return t.Evaluate(ctx, agent, valCases)
}

// SnapshotOperatorsState 快照当前所有 Operator 的状态。
//
// 保存 Operator 注册表的状态副本，用于候选评估回滚/提交。
// 返回 map[operatorID]operatorState。
//
// 对应 Python: Trainer._snapshot_operators_state(operators) — 静态方法
func SnapshotOperatorsState(operators map[string]operator.Operator) map[string]map[string]any {
	out := make(map[string]map[string]any, len(operators))
	for opID, op := range operators {
		out[opID] = op.GetState()
	}
	return out
}

// RestoreOperatorsState 从快照恢复 Operator 注册表状态。
//
// 遍历快照中的每个 operator 状态，调用 LoadState 恢复。
// operatorID 在 operators 中不存在时跳过。
//
// 对应 Python: Trainer._restore_operators_state(operators, state) — 静态方法
func RestoreOperatorsState(operators map[string]operator.Operator, state map[string]map[string]any) {
	for opID, st := range state {
		op, ok := operators[opID]
		if !ok {
			continue
		}
		op.LoadState(st)
	}
}

// GetOperatorRegistry 从 Agent 获取 Operator 注册表。
//
// 调用 Agent 的 GetOperators() 方法获取其关联的 Operator 映射。
//
// 对应 Python: Trainer._get_operator_registry(agent) — 静态方法
func GetOperatorRegistry(agent TrainableAgent) map[string]operator.Operator {
	if agent == nil {
		return nil
	}
	return agent.GetOperators()
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
// ⤵️ 待 9.78 EvolveCheckpoint 回填
//
// 对应 Python: Trainer._resume_if_needed(agent)
func (t *Trainer) ResumeIfNeeded(_ context.Context, _ any) error {
	// ⤵️ 待 9.78 EvolveCheckpoint 回填：检查点恢复逻辑
	// 对齐 Python:
	//   if self._checkpoint_store is None or self._checkpoint_manager is None or not self._resume_from:
	//       return
	//   ckpt = self._checkpoint_store.load_checkpoint(self._resume_from)
	//   restored = self._checkpoint_manager.restore(agent=agent, checkpoint=ckpt)
	//   progress.start_epoch = int(restored.get("start_epoch", 0))
	//   progress.best_score = float(restored.get("best_score", 0.0))
	return nil
}

// SaveCheckpointIfNeeded 根据条件判断是否保存检查点。
//
// 当达到 checkpointEveryNEpochs 间隔或验证分数提升（checkpointOnImprove）时保存。
// ⤵️ 待 9.78 EvolveCheckpoint 回填
//
// 对应 Python: Trainer._save_checkpoint_if_needed(epoch, val_score, operators, improved)
func (t *Trainer) SaveCheckpointIfNeeded(_ int, _ float64, _ map[string]operator.Operator, _ bool) error {
	// ⤵️ 待 9.78 EvolveCheckpoint 回填：检查点保存逻辑
	// 对齐 Python:
	//   if self._checkpoint_store is None or self._checkpoint_manager is None:
	//       return
	//   if not self._checkpoint_manager.should_save(epoch=progress.current_epoch, improved=improved):
	//       return
	//   ckpt = self._checkpoint_manager.build_checkpoint(agent=agent, progress=progress, updater_state=self._updater.get_state())
	//   path = self._checkpoint_store.save_checkpoint(ckpt, filename="latest.json")
	return nil
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
// ⤵️ 待 9.77 Trajectory Extractor 回填：暂用 any 占位
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
// ⤵️ 待 9.78 EvolveCheckpoint 回填：延迟初始化策略，当前仅存储路径
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
// ⤵️ 待 9.78 EvolveCheckpoint 回填：暂用 any 占位
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

// normalizeUpdates 将 Updater.Update 返回的 map[schema.UpdateKey]any 转为 map[schema.UpdateKey]schema.UpdateValue。
//
// 对齐 Python: updated 是 Updates (Dict[Tuple[str,str], UpdateValue]) 类型
func normalizeUpdates(updated map[schema.UpdateKey]any) map[schema.UpdateKey]schema.UpdateValue {
	if updated == nil {
		return nil
	}
	result := make(map[schema.UpdateKey]schema.UpdateValue, len(updated))
	for key, value := range updated {
		if uv, ok := value.(schema.UpdateValue); ok {
			result[key] = uv
		} else {
			// 简单值包装为 UpdateValue
			result[key] = schema.NormalizeUpdateValue(value, key.Target())
		}
	}
	return result
}

// invokeCallback 安全调用回调函数。
// 回调字段类型为 any，支持多种函数签名。
func invokeCallback(cb any, args ...any) {
	if cb == nil {
		return
	}
	if fn, ok := cb.(func(...any)); ok {
		fn(args...)
	}
	// 其他函数签名类型可在此扩展
}
