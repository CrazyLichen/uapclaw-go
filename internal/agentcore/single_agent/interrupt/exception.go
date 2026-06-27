package interrupt

import (
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────
// 从 sa/schema 包 re-export中断相关类型，保持 API 兼容。
// 类型定义已迁移至 sa/schema 包，本文件仅保留类型别名和函数委托。

// ToolInterruptException 工具中断异常。
// 类型定义已迁移至 sa/schema 包，此处为类型别名以保持 API 兼容。
type ToolInterruptException = saschema.ToolInterruptException

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
