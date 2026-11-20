// Package netguard 提供 fetch 工具的网络防护判定（Pipeline C）。
//
// 本包对齐 Python openjiuwen/harness/security/permission_engine/netguard/
// 两个模块，共 315 行：
//
//   - net_guard.py（163）：NetGuardChecker 判定器
//   - net_urls.py（152）：包内 net_urls 黑名单注入
//
// # 在判定管线中的位置
//
//	Pipeline A（toolguard）：工具级与参数级规则
//	Pipeline B（fileguard）：路径防护
//	Pipeline C（本包）：fetch 工具的 URL 白黑名单
//	→ strictest(A, B, C)
//
// # 三条不可混同的语义
//
//  1. 只有 allow / deny，**没有 ask**。ask 不是 net_guard 的合法动作。
//  2. 只对 fetch 类工具生效（mcp_fetch_webpage / fetch_webpage /
//     web_fetch_webpage），其余工具直接返回 nil。
//  3. allow 命中返回 nil（不抬升 Pipeline A 的结论）；只有 deny 产生结果。
//
// # 内置黑名单不可放宽
//
// net_urls 的内置四项（云元数据地址、metadata 域名、localhost、127.0.0.1）
// 是 SSRF 底线。overlay 把它们改成 allow 时，合并逻辑会强制保留 deny。
//
// 文件目录：
//
//	netguard/
//	├── doc.go          # 包文档
//	├── net_guard.go    # NetGuardChecker 判定器
//	└── net_urls.go     # 包内 net_urls 注入
//
// 对应 Python 代码：openjiuwen/harness/security/permission_engine/netguard/
package netguard
