// Package security 提供 harness 权限安全基础设施，包括权限模型、分层策略评估、
// 模式匹配、Shell AST 解析、路径防护与网络防护的判定器装配、权限建议构建和权限引擎。
//
// # 三段判定管线
//
//	Pipeline A：tiered_policy —— 工具级 + 参数规则 + defaults
//	Pipeline B：fileguard     —— 路径防护（workspace 边界 / 敏感路径 / 三轴）
//	Pipeline C：netguard      —— fetch 工具的 URL 白黑名单（只有 allow/deny）
//	→ Strictest(A, B, C)
//
// # 核心组件
//
//   - PermissionEngine：权限引擎，装配并串联三段管线
//   - EvaluateTieredPolicy：分层策略评估（Pipeline A）
//   - ParseShellForPermission：tree-sitter bash 解析 + 保守扫描 fallback
//   - MatchWildcard：安全通配符匹配（防 shell 注入）
//   - FileGuardEvaluator / NetGuardEvaluator：Pipeline B/C 的接口面，
//     实现位于 fileguard / netguard 子包，经工厂注入本包
//   - BuildPermissionSuggestions："始终允许"建议构建
//
// # 为什么判定器是接口而不是具体类型
//
// security ↔ fileguard 双向依赖：fileguard 需要本包的 PermissionLevel 等符号，
// 本包需要 fileguard 的判定器。Go 禁止包级循环 import，因此本包只定义接口与
// 工厂变量，子包在 init 中注册实现。需要「本包能拿到判定器」的调用方
// （生产 wiring、本包的集成测试）必须 blank import 两个子包触发 init。
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
//	├── tiered_policy.go      # 分层策略评估引擎（Pipeline A）
//	├── patterns.go           # 通配符/路径匹配 + YAML 持久化
//	├── guard_factory.go      # FileGuardEvaluator/NetGuardEvaluator 接口与工厂
//	├── suggestions.go        # 权限建议构建
//	├── permission_engine.go  # PermissionEngine 权限引擎（A/B/C 三段）
//	├── factory.go            # BuildPermissionInterruptRail 工厂函数
//	├── fileguard/            # Pipeline B：路径防护判定器
//	├── netguard/             # Pipeline C：网络防护判定器
//	└── toolguard/            # 内置规则注入 + shell 命令脱壳
//
// 对应 Python 代码：openjiuwen/harness/security/
package security

import "github.com/uapclaw/uapclaw-go/internal/common/logger"

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识（安全/权限）。
const logComponent = logger.ComponentPermissions
