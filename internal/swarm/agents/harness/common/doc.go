// Package common 提供 Agent harness 层跨子领域共享的辅助能力。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/
//
// 本包目前承载工作区路径解析与显示脱敏：
//   - 把模型可见路径（含 workspace://current URI、相对路径、绝对路径）
//     解析为规范化绝对路径，并约束在当前工作区根内
//   - 对 UI / reviewer / 模型可见的工具消息做沙箱产物路径脱敏
//   - review_ui 模式把当前工作区根前缀相对化，末尾接
//     rails/permissions 的 RedactSecretValuesPreserveFormat 做密钥脱敏
//
// 依赖方向：common → common/rails/permissions（单向，不成环）。
// reviewer_redaction.go 原属 9.90d-2，因本包末尾依赖由 9.90d-1 提前落地。
//
// 文件目录：
//
//	common/
//	├── doc.go             # 包文档
//	└── workspace_paths.go # 工作区路径解析 + 显示脱敏（22 符号）
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/workspace_paths.py
package common
