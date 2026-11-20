// Package resource 提供 auto_harness 的出厂资源文件（go:embed）。
//
// 对应 Python openjiuwen/rsi/harness_rsi/auto_harness/resources/ 目录：
//   - config.yaml  —— Auto Harness 出厂配置模板（bootstrap 起始配置的复制源）
//   - ci_gate.yaml —— CI 门禁规则（schema.py _default_immutable_files 列为不可改文件，
//     ci_gate_runner 在未显式配置 config_path 时加载）
//
// 文件内容与 Python 侧逐字一致（jiuwenclaw 等宿主路径保留原样，
// 对齐既有品牌映射惯例：jiuwenswarm-user→uapclawswarm-user 之外不改）。
//
// 文件目录：
//
//	resource/
//	├── doc.go        # 包文档
//	├── embed.go     # go:embed 导出（DefaultConfigTemplate / CIGateYAML）
//	├── config.yaml  # 出厂配置模板（逐字复制 Python resources/config.yaml）
//	└── ci_gate.yaml # CI 门禁规则（逐字复制 Python resources/ci_gate.yaml）
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/auto_harness/resources/
package resource
