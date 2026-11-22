// Package security 提供团队权限收窄能力。
//
// 对齐 Python: openjiuwen/agent_teams/security/（narrowing.py）。
// 收窄语义：override 只能收紧（restrict），不能放宽（loosen）——
// 对 override 中列出的每个工具，生效级别取 base 与 override 的 strictest()。
//
// 文件目录：
//
//	security/
//	├── doc.go           # 包文档
//	└── narrowing.go    # NarrowPermissions 收窄 + FormatBasePermissionsForDesc 描述格式化
//
// 对应 Python 代码：openjiuwen/agent_teams/security/narrowing.py
package security
