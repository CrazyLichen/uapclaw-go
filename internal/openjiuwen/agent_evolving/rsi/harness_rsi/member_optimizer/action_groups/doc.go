// Package actiongroups 提供 member 优化动作组的定义装载、可执行策略与波次调度。
//
// 本包对应 Python openjiuwen/rsi/harness_rsi/member_optimizer/action_groups/
// （__init__.py 导出面由父包与子包 doc 共同承载）。当前 Auto Harness 以
// ExpertHarness 包消费 member 能力：MemberOptimizer 只规划和执行
// ExpertHarness 装载器今日可读的包面本地文件变更（prompt/skill/tool/rail/
// runtime.yaml 白名单面）。
//
// 文件目录：
//
//	action_groups/
//	├── doc.go          # 包文档
//	├── definitions.go  # 动作定义装载（内置符号组展开 + YAML/JSON 文件）
//	├── policy.go       # 可执行动作策略（白名单 + 校验矩阵 + 提示词）
//	└── scheduling.go  # 波次拓扑排序与角色子波分组
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/member_optimizer/action_groups/
package actiongroups
