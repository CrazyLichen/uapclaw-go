package experience

import (
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// PendingChange 等待审批的暂存演进记录快照。
//
// 类型别名，指向 checkpointing.PendingChange。
// Go 不允许 checkpointing ↔ experience 循环引用，
// 因此 PendingChange 的实际定义在 checkpointing 包中，
// experience 包通过类型别名提供等效访问。
//
// 对应 Python: openjiuwen/agent_evolving/experience/types.py PendingChange
type PendingChange = checkpointing.PendingChange
