// Package multi_agent_test 提供 MultiAgent 模块集成测试。
//
// 覆盖 TeamRuntime 核心生命周期/消息路由/订阅管理、HandoffTeam 编排/信号提取/工具调用、
// HierarchicalTeam（msgbus/tools 两种变体）创建/Agent管理/层级配置等场景。
//
// 对齐 Python: openjiuwen/core/multi_agent/ 单元测试
//
// 文件目录：
//
//	multi_agent/
//	├── doc.go                       # 包文档
//	├── team_runtime/                # TeamRuntime 核心集成测试
//	│   ├── doc.go
//	│   └── team_runtime_e2e_test.go
//	├── handoff/                     # HandoffTeam E2E 集成测试
//	│   ├── doc.go
//	│   └── handoff_e2e_test.go
//	└── hierarchical/               # HierarchicalTeam E2E 集成测试
//	    ├── doc.go
//	    └── hierarchical_e2e_test.go
package multi_agent_test
