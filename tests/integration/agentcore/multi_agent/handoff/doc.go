// Package handoff_test 提供 HandoffTeam E2E 集成测试。
//
// 覆盖 HandoffOrchestrator 路由约束/最大 handoff 次数/终止条件/完成和错误通道、
// BuildRouteGraph 显式路由/全连接图、HandoffSignal 提取、HandoffTool 调用、
// HandoffConfig 默认值、HandoffTeam 创建/添加 Agent/订阅发布等场景。
//
// 对齐 Python: openjiuwen/core/multi_agent/builtin_teams/handoff/ 单元测试
//
// 文件目录：
//
//	handoff/
//	├── doc.go             # 包文档
//	└── handoff_e2e_test.go  # HandoffTeam E2E 测试
package handoff_test
