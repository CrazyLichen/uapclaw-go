package evolving

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultExampleNum 每次迭代示例数
	// 对应 Python: TuneConstant.default_example_num = 1
	DefaultExampleNum = 1
	// DefaultIterationNum 默认训练迭代次数
	// 对应 Python: TuneConstant.default_iteration_num = 3
	DefaultIterationNum = 3
	// DefaultMaxSampledExampleNum 最大采样示例数
	// 对应 Python: TuneConstant.default_max_sampled_example_num = 10
	DefaultMaxSampledExampleNum = 10
	// DefaultParallelNum 默认并行数
	// 对应 Python: TuneConstant.default_parallel_num = 1
	DefaultParallelNum = 1
	// DefaultMaxNumSampleErrorCases 最大采样错误用例数
	// 对应 Python: TuneConstant.default_max_num_sample_error_cases = 10
	DefaultMaxNumSampleErrorCases = 10
	// DefaultEarlyStopScore 早停分数阈值
	// 对应 Python: TuneConstant.default_early_stop_score = 1.0
	DefaultEarlyStopScore = 1.0

	// MinIterationNum 最小迭代次数
	// 对应 Python: TuneConstant.min_iteration_num = 1
	MinIterationNum = 1
	// MaxIterationNum 最大迭代次数
	// 对应 Python: TuneConstant.max_iteration_num = 20
	MaxIterationNum = 20
	// MinParallelNum 最小并行数
	// 对应 Python: TuneConstant.min_parallel_num = 1
	MinParallelNum = 1
	// MaxParallelNum 最大并行数
	// 对应 Python: TuneConstant.max_parallel_num = 20
	MaxParallelNum = 20
	// MinExampleNum 最小示例数
	// 对应 Python: TuneConstant.min_example_num = 0
	MinExampleNum = 0
	// MaxExampleNum 最大示例数
	// 对应 Python: TuneConstant.max_example_num = 20
	MaxExampleNum = 20
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
