// Package agent CLI 的运行时配置与后端装配。
//
// config.go 提供三层优先级的配置合并（CLI 参数 > 环境变量 >
// ~/.openjiuwen/settings.json > 默认值）与校验。
// factory.go 提供后端装配：MCP 配置加载（~/.openjiuwen/mcp.json）、
// 默认 skill 目录、CLI workspace 内容、AgentBackend 接口、LocalBackend
// 骨架与 CreateBackend 工厂。create_agent 的全量 rails 装配
// ⤵️ 15.6-15.9 内置 provider 落地后回填。
//
// 文件目录：
//
//	agent/
//	├── doc.go          # 包文档
//	├── config.go      # CLIConfig / LoadConfig / LoadSettingsJSON / SaveSettingsJSON
//	└── config_test.go
//
// 对应 Python 代码：openjiuwen/harness/cli/agent/config.py（227 行）
// （factory.py 774 行 ⏵ 回填）
package agent
