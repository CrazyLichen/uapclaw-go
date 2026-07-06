// Package schema 提供 harness 模式的类型定义，包括 AgentMode、PromptMode、
// DeepAgentConfig、TaskPlan 及相关类型。
//
// 这些类型用于控制 Agent 的执行模式（普通/规划）、提示词注入模式（完整/精简/无）、
// 任务计划管理及循环事件处理，对应 Python 中 openjiuwen/harness/schema/ 目录下的枚举和配置定义。
//
// 文件目录：
//
//	schema/
//	├── doc.go           # 包文档
//	├── agent_mode.go    # AgentMode 枚举及 JSON 序列化
//	├── prompt_mode.go   # PromptMode 枚举及 JSON 序列化
//	├── config.go        # DeepAgentConfig、SubagentSpec 及辅助类型（VisionModelConfig、AudioModelConfig、SubAgentConfig、ModelSelectionEntry）
//	├── loop_event.go    # DeepLoopEventType 枚举及 DeepLoopEvent 结构体
//	├── state.go         # PlanModeState、DeepAgentState 会话状态
//	├── task.go          # TodoStatus 枚举、TodoItem、TaskPlan、ModelUsageRecord 任务计划类型
//	└── task_type.go     # DeepTaskType + SessionSpawnTaskType 任务类型常量
//
// 对应 Python 代码：openjiuwen/harness/schema/
package schema
