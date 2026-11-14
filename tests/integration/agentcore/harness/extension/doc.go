//go:build integration

// Package extension_test 领域 15.15 templates & plugins 热加载端到端集成测试。
//
// 链路：真文件型 Plugin/AgentTemplate 包（fixtures，文件系统）→
// resources/extension_loader（装载归一）→ resources/extension_resolver
// （Parts 装配）→ harness/extension_binder（六类热绑+回滚）→
// harness.DeepAgent（六入口 + 台账）→ rails（SkillUseRail / SubagentRail /
// SystemPromptBuilder）→ 卸载逆序撤销。跨 5 个 internal 二级子系统。
//
// 文件目录：
//
//	extension/
//	├── doc.go                  # 包文档
//	├── extension_suite_test.go # 套件定义
//	└── extension_e2e_test.go  # 端到端用例
//
// 对应 Python 代码：openjiuwen/harness/{extension_binder,deep_agent}.py ·
// openjiuwen/harness/resources/{extension_loader,extension_resolver}.py
package extension_test
