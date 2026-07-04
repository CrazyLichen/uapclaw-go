// Package harness_config 提供 harness_config.yaml 的加载、校验和组装功能，
// 用于 DeepAgent 的声明式配置。
//
// 本包对应 Python 中 openjiuwen/harness/harness_config/ 目录，实现从 YAML 文件
// 到 ResolvedHarnessConfig 的完整管线：schema 定义 → loader 解析/模板渲染 →
// builder 组装 → registry 发现/管理。
//
// 文件目录：
//
//	harness_config/
//	├── doc.go           # 包文档
//	├── schema.go        # YAML Schema 结构体定义与校验/序列化方法
//	├── loader.go        # YAML 解析、模板渲染、配置加载
//	├── builder.go       # 组装逻辑：内置注册表、工具/Rail/MCP 解析、YAML 生成
//	└── registry.go      # HarnessConfig 注册表与发现机制
//
// 对应 Python 代码：openjiuwen/harness/harness_config/
package harness_config
