// Package agent CLI 的运行时配置与后端装配。
//
// config.go 提供三层优先级的配置合并（CLI 参数 > 环境变量 >
// ~/.openjiuwen/settings.json > 默认值）与校验；factory.go（⏵ 下一批）
// 按 CLIConfig 装配 DeepAgent 后端。
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
