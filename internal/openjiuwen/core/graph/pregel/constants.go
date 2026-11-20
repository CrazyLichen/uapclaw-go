package pregel

// ──────────────────────────── 常量 ────────────────────────────

// 常量顺序与 Python constants.py 逐行一致，便于逐行比对。
const (
	// Start 图引擎的起始虚拟节点名，构图器自动创建。Python: START
	Start = "__start__"
	// End 图引擎的结束虚拟节点名，构图器自动创建，通道就绪判定时跳过。Python: END
	End = "__end__"
	// MaxRecursiveLimit 单次执行的最大步数默认值（递归上限）。Python: MAX_RECURSIVE_LIMIT
	MaxRecursiveLimit = 10000
	// TaskStatusInterrupt 顶层图中断的返回键，同时作为中断节点的失败状态。Python: TASK_STATUS_INTERRUPT
	TaskStatusInterrupt = "__interrupt__"
	// TaskStatusError 节点执行失败的失败标记，写入 PendingNode.Status。Python: TASK_STATUS_ERROR
	TaskStatusError = "__error__"
	// NsSeparator 命名空间分隔符。Python: NS_SEPARATOR
	//
	// 对齐留档：Python 侧零消费。禁止用于命名空间拼接——Python 就是字面量 ":"
	//（见 task.py:144），据此实现转义逻辑即偏离。
	NsSeparator = ":"
	// NsReplaceChar 命名空间替换字符。Python: NS_REPLACE_CHAR
	//
	// 对齐留档：Python 侧零消费，禁止据此实现任何转义逻辑。
	NsReplaceChar = "#"
	// NsKey config 中命名空间字段的字典键名。Python: NS
	//
	// 对齐留档：Go 结构体化后无消费者，仅作与 Python 键名的对照。
	NsKey = "ns"
	// ParentNsKey config 中父命名空间字段的字典键名。Python: PARENT_NS
	//
	// 对齐留档：Go 结构体化后无消费者，仅作与 Python 键名的对照。
	ParentNsKey = "parent_ns"
	// SessionIDKey config 中会话 ID 字段的字典键名。Python: SESSION_ID
	//
	// 对齐留档：Go 结构体化后无消费者，仅作与 Python 键名的对照。
	SessionIDKey = "session_id"
	// RecursionLimitKey config 中递归上限字段的字典键名。Python: RECURSION_LIMIT
	//
	// 对齐留档：Go 结构体化后无消费者，仅作与 Python 键名的对照。
	RecursionLimitKey = "recursion_limit"
)
