package interrupt

import (
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// BaseInterruptionState 中断状态基类。
type BaseInterruptionState = saschema.BaseInterruptionState

// ToolInterruptEntry 工具中断条目。
type ToolInterruptEntry = saschema.ToolInterruptEntry

// ToolInterruptionState 工具中断状态（HITL 中断）。
type ToolInterruptionState = saschema.ToolInterruptionState

// WorkflowInterruptEntry 工作流中断条目。
type WorkflowInterruptEntry = saschema.WorkflowInterruptEntry

// InterruptionState 工作流中断状态。
type InterruptionState = saschema.InterruptionState

// ──────────────────────────── 常量 ────────────────────────────

// 从 sa/schema 包 re-export 中断状态相关常量，保持 API 兼容。
// 常量定义已迁移至 sa/schema 包。

const (
	// InterruptionKey ReActAgent 中断状态键
	InterruptionKey = saschema.InterruptionKey
	// ResumeUserInputKey 恢复时用户输入的键
	ResumeUserInputKey = saschema.ResumeUserInputKey
	// InterruptAutoConfirmKey 自动确认配置键
	InterruptAutoConfirmKey = saschema.InterruptAutoConfirmKey
	// ResumeStartIterationKey 恢复时起始迭代键
	ResumeStartIterationKey = saschema.ResumeStartIterationKey
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
