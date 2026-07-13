package trainer

// ──────────────────────────── 结构体 ────────────────────────────

// Progress 训练进度追踪，记录 epoch、batch、分数等信息。
//
// 提供 RunEpoch 和 RunBatch 迭代方法，用于驱动 Trainer 的训练循环。
//
// 对应 Python: openjiuwen/agent_evolving/trainer/progress.py Progress
type Progress struct {
	// StartEpoch 起始 epoch（用于断点续训）
	StartEpoch int
	// CurrentEpoch 当前 epoch
	CurrentEpoch int
	// MaxEpoch 最大 epoch 数
	// 对应 Python: max_epoch, 默认 TuneConstant.default_iteration_num=3
	MaxEpoch int
	// CurrentBatchIter 当前 batch 迭代步
	CurrentBatchIter int
	// MaxBatchIter 最大 batch 迭代步数
	MaxBatchIter int
	// BestScore 历史最佳分数
	BestScore float64
	// BestBatchScore 当前 batch 最佳分数
	BestBatchScore float64
	// CurrentEpochScore 当前 epoch 分数
	CurrentEpochScore float64
}

// Callbacks 训练生命周期回调钩子。
//
// 子类可覆盖各方法，集成日志记录、早停判断、指标上报等功能。
// 当前为纯接口桩，各回调字段暂用 any 占位，待后续章节填充具体函数签名。
//
// 对应 Python: openjiuwen/agent_evolving/trainer/progress.py Callbacks
type Callbacks struct {
	// OnTrainBegin 训练开始回调（验证基线评估完成后）。
	// 签名: func(agent any, progress *Progress, evalInfo any)
	OnTrainBegin any
	// OnTrainEnd 训练结束回调。
	// 签名: func(agent any, progress *Progress, evalInfo any)
	OnTrainEnd any
	// OnTrainEpochBegin 单 epoch 训练开始回调。
	// 签名: func(agent any, progress *Progress)
	OnTrainEpochBegin any
	// OnTrainEpochEnd 单 epoch 训练结束回调（best_score 更新/参数写回后）。
	// 签名: func(agent any, progress *Progress, evalInfo any)
	OnTrainEpochEnd any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewProgress 创建默认 Progress 实例。
//
// 对应 Python: Progress() 默认构造（max_epoch=TuneConstant.default_iteration_num=3）
func NewProgress() *Progress {
	return &Progress{
		StartEpoch:   0,
		CurrentEpoch: 0,
		MaxEpoch:     3, // TuneConstant.default_iteration_num
		MaxBatchIter: 1,
	}
}

// NewProgressWithMaxEpoch 创建指定最大 epoch 的 Progress 实例。
func NewProgressWithMaxEpoch(maxEpoch int) *Progress {
	p := NewProgress()
	p.MaxEpoch = maxEpoch
	return p
}

// NewCallbacks 创建默认 Callbacks 实例（所有回调为 nil）。
func NewCallbacks() *Callbacks {
	return &Callbacks{}
}
