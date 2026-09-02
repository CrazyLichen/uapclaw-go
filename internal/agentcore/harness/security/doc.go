// Package security 提供 harness 权限安全基础设施，包括权限模型、分层策略评估、
// 模式匹配、Shell AST 解析、外部目录检查、权限建议构建和权限引擎。
//
// 核心组件：
//   - PermissionEngine: 权限引擎，组合 TieredPolicy + ExternalDirectoryChecker 评估工具权限
//   - EvaluateTieredPolicy: 分层策略评估（优先级链：tool-deny > builtin-param > user-param >
//     approval-overrides > baseline > defaults > fallback-ASK）
//   - ParseShellForPermission: tree-sitter bash 解析 + 保守扫描 fallback
//   - MatchWildcard: 安全通配符匹配（防 shell 注入）
//   - ExternalDirectoryChecker: 工作空间外路径检测
//   - BuildPermissionSuggestions: "始终允许"建议构建
//
// 运行时代码仍以 map[string]any 承载 YAML/JSON 中的 permissions，
// 本包的结构体用于静态类型检查与文档。
//
// 文件目录：
//
//	security/
//	├── doc.go                # 包文档
//	├── models.go             # 权限模型类型定义（PermissionLevel/PermissionResult/ToolPermissionHost 等）
//	├── shell_ast.go          # tree-sitter bash 解析 + 保守扫描 fallback
//	├── tiered_policy.go      # 分层策略评估引擎
//	├── patterns.go           # 通配符/路径匹配 + YAML 持久化
//	├── checker.go            # ExternalDirectoryChecker 外部目录检查
//	├── suggestions.go        # 权限建议构建
//	└── permission_engine.go  # PermissionEngine 权限引擎
//
// 对应 Python 代码：openjiuwen/harness/security/
package security
