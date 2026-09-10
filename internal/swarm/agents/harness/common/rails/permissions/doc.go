// Package permissions 提供数字分身场景下的权限上下文、工具权限检查和权限配置 RPC。
//
// 本包对齐 Python jiuwenswarm/agents/harness/common/rails/permissions/，
// 包含 OwnerScopesPermissionContext、owner_scopes 权限检查、参数匹配、持久化
// 以及 10 个权限配置 RPC 方法（tools/rules/approval_overrides CRUD）。
//
// 文件目录：
//
//	permissions/
//	├── doc.go                     # 包文档
//	├── owner_scopes.go            # OwnerScopesPermissionContext + CheckAvatarPermission/ResolveOwnerScopeLevel/MatchArgs/PersistToOwnerScope
//	├── permissions_config_rpc.go  # 10 个权限配置 RPC 分发 + config CRUD 辅助函数
//	└── permissions_persist.go     # 权限配置落盘 helper（PersistPermissionAllowRule/PersistExternalDirectoryAllow/PersistCliTrustedDirectoryWithOverrides）
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/permissions/ + jiuwenswarm/common/config.py（CRUD 部分）
package permissions
