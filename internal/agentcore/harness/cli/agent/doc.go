// Package agent CLI 的运行时配置与后端装配。
//
// config.go 提供三层优先级的配置合并（CLI 参数 > 环境变量 >
// ~/.openjiuwen/settings.json > 默认值）与校验。
// factory.go 提供后端装配：CreateAgent 全量 rails 装配（SysOperationRail /
// AskUserRail / ConfirmInterruptRail / SkillUseRail / ContextProcessorRail /
// ContextAssembleRail / MemoryRail / 子 Agent / MCP / 视听多模态）、
// MCP 配置加载、CLI workspace 身份覆写、AgentBackend 接口、LocalBackend
// 真身（Runner 直连 + runtime_extensions 热加载扫描）与 CreateBackend 工厂。
//
// 文件目录：
//
//	agent/
//	├── doc.go          # 包文档
//	├── config.go      # CLIConfig / LoadConfig / LoadSettingsJSON / SaveSettingsJSON
//	└── factory.go     # CreateAgent / BuildSubagents / LoadMcpConfigs / LocalBackend / CreateBackend
//	└── config_test.go
//
// 对应 Python 代码：openjiuwen/harness/cli/agent/config.py（227 行）
// 与 openjiuwen/harness/cli/agent/factory.py（774 行）
package agent
