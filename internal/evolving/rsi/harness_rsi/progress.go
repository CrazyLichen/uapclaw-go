package harnessrsi

// ──────────────────────────── 结构体 ────────────────────────────

// OptimizationProgressEvent 优化编排器流式运行状态的单条进度事件（对齐 OptimizationProgressEvent）
type OptimizationProgressEvent struct {
	// Phase 当前阶段
	Phase string `json:"phase" yaml:"phase"`
	// Epoch 轮次（从 0 起）
	Epoch int `json:"epoch" yaml:"epoch"`
	// BatchIndex 批次下标
	BatchIndex int `json:"batch_index" yaml:"batch_index"`
	// Stage 阶段名
	Stage string `json:"stage" yaml:"stage"`
	// Score 分数（nil 表示未产生）
	Score *float64 `json:"score,omitempty" yaml:"score,omitempty"`
	// Improved 是否改进（nil 表示未知）
	Improved *bool `json:"improved,omitempty" yaml:"improved,omitempty"`
	// Message 消息
	Message string `json:"message" yaml:"message"`
	// Metrics 指标映射
	Metrics map[string]any `json:"metrics" yaml:"metrics"`
	// Artifacts 产物映射
	Artifacts map[string]any `json:"artifacts" yaml:"artifacts"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ProgressCallback 进度回调（对齐 ProgressCallback）
type ProgressCallback func(OptimizationProgressEvent)

// SeedOptimizationDecisionCallback 种子优化决策回调；Go 为同步回调 + error（偏差 D-5，
// Python 为 bool | Awaitable[bool] 联合类型）
type SeedOptimizationDecisionCallback func(map[string]any) (bool, error)
