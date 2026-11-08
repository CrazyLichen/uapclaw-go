// Package fileguard 提供文件路径防护判定（Pipeline B）。
//
// 本包对齐 Python openjiuwen/harness/security/permission_engine/fileguard/
// 四个模块，共 1,650 行：
//
//   - file_tool_specs.py（63）：工具 → (参数名, 读写动作) 的注册表
//   - path_extract.py（524）：把工具参数翻译成 [(path, action, source)]
//   - file_guard.py（870）：配置编译与路径判定
//   - sensitive_paths.py（193）：包内敏感路径底线注入
//
// # 在判定管线中的位置
//
//	Pipeline A（toolguard）：工具级与参数级规则
//	Pipeline B（本包）：路径防护——workspace 边界、敏感路径、读写轴
//	Pipeline C（netguard）：fetch 工具的 URL 白黑名单
//	→ strictest(A, B, C)
//
// # 双模式
//
// legacy 模式兼容旧的 external_directory 配置形态（无读写轴，只有统一
// level）；native 模式支持 file_guard.paths 的 prefix/glob 两种匹配与
// read/write/exec 三轴独立配置。模式由配置形态自动判定（见 file_guard.go
// 的 hasNativeFileGuard）。
//
// 文件目录：
//
//	fileguard/
//	├── doc.go                  # 包文档
//	├── file_tool_specs.go      # 工具参数与动作注册表
//	├── path_extract.go         # 参数 → [(path, action, source)]
//	├── file_guard.go           # 配置编译 + 路径判定
//	└── sensitive_paths.go      # 包内敏感路径底线注入
//
// 对应 Python 代码：openjiuwen/harness/security/permission_engine/fileguard/
package fileguard
