// Package permissions 提供数字分身场景下的权限上下文与工具权限检查。
//
// 本包对齐 Python jiuwenswarm/agents/harness/common/rails/permissions/，
// 包含 OwnerScopesPermissionContext、权限上下文注入、owner_scopes 权限检查和持久化。
//
// 核心函数：
//   - SetupPermissionContext: 从 metadata 构建权限上下文并注入 context.Context
//   - CleanupPermissionContext: 清理权限上下文（Go 中为空操作）
//   - CheckAvatarPermission: 单工具 owner_scopes 权限检查
//   - PersistToOwnerScope: 将规则持久化到 config.yaml
//
// 文件目录：
//
//	permissions/
//	├── doc.go            # 包文档
//	└── owner_scopes.go   # OwnerScopesPermissionContext + 6 个核心权限函数
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/permissions/
package permissions
