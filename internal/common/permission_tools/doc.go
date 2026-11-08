// Package permission_tools 提供权限工具名归一化与别名冲突检测。
//
// 本包对齐 Python jiuwenswarm/common/permission_tools.py（105 行），是权限
// 自动审查层 classify_tool 的硬依赖——所有工具名在进入能力分类前都必须先
// 经本包归一化。
//
// # 为什么需要 conflict 检测
//
// 别名表同时承担两个角色：把前端友好的短名（exec_command）映射到配置里的
// 规范名（mcp_exec_command），以及从规范名反查全部别名。当某个名字既是
// 「规范名集合的成员」又是「别名表的键」时，两种角色冲突——继续归一化会
// 让规则匹配到错误的目标。此时必须 fail-closed：返回空的 canonical 名，
// 由调用方（classify_tool）降级为 unknown/high 风险，走人工审查。
//
// # 与 tool_security_rail 别名表的区别
//
// 本包的 permissionToolAliases 有 4 项，服务于「配置里的工具名归一化」，
// 带 conflict 检测。rails/security/tool_security_rail.go 的 TOOL_NAME_ALIASES
// 有 5 项（多 web_fetch_webpage），服务于「中断卡的展示名归一化」，无 conflict
// 检测。二者是 Python 侧的两份独立数据，不得合并。
//
// 文件目录：
//
//	permission_tools/
//	├── doc.go                    # 包文档
//	└── permission_tools.go       # 别名表 + Resolve/Normalize/Stricter
//
// 对应 Python 代码：jiuwenswarm/common/permission_tools.py
package permission_tools
