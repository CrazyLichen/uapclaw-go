// Package goal 提供会话级持续目标（Goal）的领域模型、持久化、唯一写者与完成度评估。
//
// 本包是 Goal 能力的状态层：不调用模型、不注入提示词、不注册工具。
// 它回答两个问题：
//   - 现在该不该再排一轮 goal（GoalManager.EnsureActiveGoalWorkLocked）
//   - 这一轮的结论还能不能写回记录（GoalManager.BeginAttempt / ApplyAssessment 的代际校验）
//
// 状态独占：GoalRecord 的所有读写只经 GoalManager；GoalRecord 是内存形态，
// 跨边界（Session state、交互事件、服务面）一律走 ToDict/FromDict 的 map 形态。
//
// 文件目录：
//
//	goal/
//	├── doc.go        # 包文档
//	├── schema.go     # 3 枚举 + GoalAssessment/TokenUsage/GoalRecord/GoalStopConfig/GoalOperationError
//	├── store.go      # GoalSession 窄接口 + SessionGoalStore（Session state 唯一真源）
//	├── manager.go    # GoalStore/Coordinator 接口 + GoalManager（唯一写者）
//	└── evaluation.go # GoalEvaluator（agent_report/transcript/hybrid 三策略 + 硬上限）
//
// 对应 Python 代码：openjiuwen/harness/goal/
package goal
