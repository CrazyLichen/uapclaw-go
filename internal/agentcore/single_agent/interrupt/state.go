package interrupt

import (
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 枚举 ────────────────────────────

// BaseInterruptionState 中断状态基类。
// TODO: 考虑移除 reexport，让调用者直接使用 saschema 包
type BaseInterruptionState = saschema.BaseInterruptionState

// ToolInterruptEntry 工具中断条目。
// TODO: 考虑移除 reexport，让调用者直接使用 saschema 包
type ToolInterruptEntry = saschema.ToolInterruptEntry

// ToolInterruptionState 工具中断状态（HITL 中断）。
// TODO: 考虑移除 reexport，让调用者直接使用 saschema 包
type ToolInterruptionState = saschema.ToolInterruptionState

// ──────────────────────────── 常量 ────────────────────────────

// 从 sa/schema 包 re-export 中断状态相关常量，保持 API 兼容。
// 常量定义已迁移至 sa/schema 包。
const (
	// InterruptionKey ReActAgent 中断状态键
	// TODO: 考虑移除 reexport，让调用者直接使用 saschema 包
	InterruptionKey = saschema.InterruptionKey
	// ResumeStartIterationKey 恢复时起始迭代键
	// TODO: 考虑移除 reexport，让调用者直接使用 saschema 包
	ResumeStartIterationKey = saschema.ResumeStartIterationKey
	// ResumeUserInputKey 恢复时用户输入的键
	// TODO: 考虑移除 reexport，让调用者直接使用 saschema 包
	ResumeUserInputKey = saschema.ResumeUserInputKey
	// InterruptAutoConfirmKey 自动确认配置键
	// TODO: 考虑移除 reexport，让调用者直接使用 saschema 包
	InterruptAutoConfirmKey = saschema.InterruptAutoConfirmKey
)
