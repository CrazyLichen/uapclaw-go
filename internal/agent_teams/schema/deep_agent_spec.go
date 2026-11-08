package schema

import (
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// ──────────────────────────── 类型别名 ────────────────────────────

// 本文件是 core 侧单角色规格类型的团队侧别名。
//
// 类型本体已全部下沉到 internal/agentcore/harness/schema/，对齐上游归属：
// openjiuwen/harness/schema/deep_agent_spec.py 是这些类型的真相来源
// （DeepAgentSpec :433、WorkspaceSpec :176、RailSpec :248、BuiltinToolSpec :296、
// SubAgentSpec、VisionModelSpec、AudioModelSpec、ProgressiveToolSpec、
// SysOperationSpec 同文件）。
//
// 保留别名让既有团队 import 路径继续工作，且与 core 是**同一类型**（非副本）。
// 别名同类型性由 deep_agent_spec_test.go 守护。

// WorkspaceSpec 工作空间规格。
// Python: WorkspaceSpec (deep_agent_spec.py:176)
type WorkspaceSpec = hschema.WorkspaceSpec

// VisionModelSpec 视觉模型规格。
// Python: VisionModelSpec (deep_agent_spec.py:146)
type VisionModelSpec = hschema.VisionModelSpec

// AudioModelSpec 音频模型规格。
// Python: AudioModelSpec (deep_agent_spec.py:158)
type AudioModelSpec = hschema.AudioModelSpec

// ProgressiveToolSpec 渐进式工具规格。
// Python: ProgressiveToolSpec
type ProgressiveToolSpec = hschema.ProgressiveToolSpec

// SysOperationSpec 系统操作规格。
// Python: SysOperationSpec
type SysOperationSpec = hschema.SysOperationSpec

// RailSpec 约束规则声明。
// Python: RailSpec (deep_agent_spec.py:248)
type RailSpec = hschema.RailSpec

// BuiltinToolSpec 内置工具声明。
// Python: BuiltinToolSpec (deep_agent_spec.py:296)
type BuiltinToolSpec = hschema.BuiltinToolSpec

// SubAgentSpec 子代理规格。
// Python: SubAgentSpec
type SubAgentSpec = hschema.SubAgentSpec

// DeepAgentSpec 单角色 DeepAgent 规格。
// Python: DeepAgentSpec (deep_agent_spec.py:433)
type DeepAgentSpec = hschema.DeepAgentSpec

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDeepAgentSpec 创建默认 DeepAgentSpec（转发 core 实现）。
func NewDeepAgentSpec() DeepAgentSpec { return hschema.NewDeepAgentSpec() }
