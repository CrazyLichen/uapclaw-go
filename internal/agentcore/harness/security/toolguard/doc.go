// Package toolguard 提供工具策略的规则加载与 shell 命令脱壳。
//
// 本包对齐 Python openjiuwen/harness/security/permission_engine/toolguard/ 下的
// builtin_rules.py（151 行）与 command_canonicalize.py（134 行）。判定主体
// （tool_policy.py / shell_ast.py / pattern_matchers.py）在父包 security 内，
// 已存在且已对齐；本包只补两个此前缺失的辅助模块。
//
// # 为什么内置规则要走 ingest 而不是判定期加载
//
// Python 的 evaluate_tiered_policy docstring 明写：「内置规则必须已内联进
// rules（layer: builtin）；判定路径不再 load YAML」。把加载放在判定期会导致：
//   - 每次判定都读文件（性能）；
//   - 判定结果依赖文件系统状态（不可复现）；
//   - 配置与规则来源分散，无法从一份 config 快照复现判定。
//
// 文件目录：
//
//	toolguard/
//	├── doc.go                      # 包文档
//	├── builtin_rules.go            # 内置规则注入（3 函数）
//	└── command_canonicalize.go     # launcher 脱壳（3 正则 + 4 函数）
//
// 对应 Python 代码：
//   - openjiuwen/harness/security/permission_engine/toolguard/builtin_rules.py
//   - openjiuwen/harness/security/permission_engine/toolguard/command_canonicalize.py
package toolguard
