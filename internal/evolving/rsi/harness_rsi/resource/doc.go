// Package resource 内嵌单 harness 自我改进的默认配置模板。
//
// orchestrating.default.yaml 是 AutoCoordinatingHarnessConfig 的缺省模板：
// config.loader 在用户配置文件缺失时将其写出引导（bootstrap）。
// 模板内品牌键按 jiuwenswarm→uapclawswarm 映射，其余逐字复制上游。
//
// 文件目录：
//
//	resource/
//	├── doc.go                       # 包文档
//	├── embed.go                     # DefaultConfigTemplate（go:embed 导出）
//	└── orchestrating.default.yaml  # 默认配置模板
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/resource/orchestrating.default.yaml
package resource
