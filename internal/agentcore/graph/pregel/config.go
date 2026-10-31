package pregel

// ──────────────────────────── 结构体 ────────────────────────────

// PregelConfig 用户传入的外部配置。
//
// 字段零值等价于 Python TypedDict(total=False) 的「键缺失/None」：空串与 0 即未设置。
// Python: PregelConfig
type PregelConfig struct {
	// SessionID 会话 ID，与 Ns 同时非空且存在 state saver 时才做断点恢复
	SessionID string
	// RecursionLimit 递归上限（最大步数），0 表示未设置
	RecursionLimit int
	// Ns 命名空间（执行上下文标识，顶层为工作流 ID，子图为子图路径）
	Ns string
}

// InnerPregelConfig PregelLoop 内部使用的配置，比外部配置多父命名空间。
//
// 任意节点执行时都可能被派生子图，ParentNs 非空表示当前处于嵌套子图（非顶层执行）。
// Python: InnerPregelConfig(PregelConfig)
type InnerPregelConfig struct {
	// PregelConfig 嵌入外部配置，对应 Python TypedDict 继承
	PregelConfig
	// ParentNs 父命名空间
	ParentNs string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateInnerConfig 将外部配置归一化为内部配置：递归上限为 0 时补 MaxRecursiveLimit。
//
// 入参按值传递，调用方配置不会被修改（对齐 Python create_inner_config 的 config.copy()）。
// 判定必须是「等于 0」而非「非正」：Python 用 falsy 判定，负数在 Python 为真值，
// 因此负数原样保留。
// Python: create_inner_config(config)
func CreateInnerConfig(cfg PregelConfig) InnerPregelConfig {
	inner := InnerPregelConfig{PregelConfig: cfg}
	if inner.RecursionLimit == 0 {
		inner.RecursionLimit = MaxRecursiveLimit
	}
	return inner
}

// DefaultPregelConfig 返回默认外部配置（递归上限为 MaxRecursiveLimit，其余为空）。
//
// 每次返回独立值，杜绝导出可变全局被任意调用点污染；对齐 Python
// DEFAULT_PREGEL_CONFIG，后者因 create_inner_config 先行 copy 而从不被改坏。
// Python: DEFAULT_PREGEL_CONFIG
func DefaultPregelConfig() PregelConfig {
	return PregelConfig{RecursionLimit: MaxRecursiveLimit}
}
