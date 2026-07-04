// Package security 提供 harness 权限安全模型类型，包括 PermissionLevel、PermissionResult、
// PermissionConfirmResponse、ApprovalOverrideEntry 和 PermissionsSection。
//
// 这些类型用于定义工具执行的权限级别（允许/确认/拒绝）、权限判定结果、
// 用户确认响应以及 agent YAML 中 permissions 配置段的结构。
// 运行时代码仍以 map[string]any 承载 YAML/JSON 中的 permissions，
// 本包的结构体用于静态类型检查与文档。
//
// 文件目录：
//
//	security/
//	├── doc.go     # 包文档
//	└── models.go  # 权限模型类型定义
//
// 对应 Python 代码：openjiuwen/harness/security/
package security
