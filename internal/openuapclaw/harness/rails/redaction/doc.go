// Package redaction 提供 AutoReviewer 证据共享脱敏与权限 UI 载荷裁剪。
//
// 本包是「脱敏能力」的公共归属地：审计、审批卡片渲染、证据摘要都需要把
// 敏感值替换成 [redacted]、把路径投影成工作区相对形式、把载荷裁剪到可控大小。
//
// # 为什么独立成包
//
// 这批函数原先住在 `swarm/agents/harness/common/rails/permissions`（9.90d-2 落地），
// 但 9.90e 的 interrupt 包也需要脱敏能力渲染审批卡片，而 permissions 包
// 已经 import interrupt（permission_interrupt_rail.go / auto_permission/runtime_bridge.go）。
// 若 interrupt 反过来 import permissions 就成环。
//
// 本包只依赖标准库与 github.com/dlclark/regexp2，不依赖任何项目内包，
// 因此 permissions 与 interrupt 都可以安全依赖它。
//
// 文件目录：
//
//	redaction/
//	└── redaction.go      # 脱敏/裁剪/路径投影全部实现
//
// 对应 Python 代码：
// jiuwenswarm/agents/harness/common/rails/permissions/reviewer_redaction.py（689 行）
//
// # 核心类型/接口索引
//
//   - RedactText / RedactSecretValues：文本脱敏，可限长
//   - RedactSecretValuesPreserveFormat：保格式脱敏（不动路径与空白）
//   - ReviewerPathLocation：把绝对路径投影为「根标识 + 相对路径」
//   - SanitizePermissionUIPayload：把任意载荷裁剪成适合 UI 展示的有界副本
package redaction
