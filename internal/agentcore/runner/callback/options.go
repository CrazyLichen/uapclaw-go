package callback

// ──────────────────────────── 结构体 ────────────────────────────

// callbackOptionConfig 回调注册选项内部配置。
type callbackOptionConfig struct {
	Priority     int
	Once         bool
	Namespace    string
	Tags         []string
	MaxRetries   int
	RetryDelay   float64
	Timeout      float64
	CallbackType string
}

// ──────────────────────────── 枚举 ────────────────────────────

// CallbackOption 回调注册选项（Functional Options 模式）。
type CallbackOption func(*callbackOptionConfig)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithPriority 设置优先级。
func WithPriority(p int) CallbackOption {
	return func(cfg *callbackOptionConfig) {
		cfg.Priority = p
	}
}

// WithOnce 设置一次性执行。
func WithOnce() CallbackOption {
	return func(cfg *callbackOptionConfig) {
		cfg.Once = true
	}
}

// WithNamespace 设置命名空间。
func WithNamespace(ns string) CallbackOption {
	return func(cfg *callbackOptionConfig) {
		cfg.Namespace = ns
	}
}

// WithTags 设置标签集合。
func WithTags(tags ...string) CallbackOption {
	return func(cfg *callbackOptionConfig) {
		cfg.Tags = tags
	}
}

// WithMaxRetries 设置最大重试次数。
func WithMaxRetries(n int) CallbackOption {
	return func(cfg *callbackOptionConfig) {
		cfg.MaxRetries = n
	}
}

// WithRetryDelay 设置重试间隔（秒）。
func WithRetryDelay(d float64) CallbackOption {
	return func(cfg *callbackOptionConfig) {
		cfg.RetryDelay = d
	}
}

// WithTimeout 设置执行超时（秒），0 表示不限。
func WithTimeout(t float64) CallbackOption {
	return func(cfg *callbackOptionConfig) {
		cfg.Timeout = t
	}
}

// WithCallbackType 设置语义类型标记。
func WithCallbackType(t string) CallbackOption {
	return func(cfg *callbackOptionConfig) {
		cfg.CallbackType = t
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// applyCallbackOptions 应用选项列表，返回配置。
func applyCallbackOptions(opts ...CallbackOption) callbackOptionConfig {
	cfg := callbackOptionConfig{
		Namespace: "default",
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
