// Package trajectory 提供自演化系统的执行轨迹数据类型。
//
// Trajectory 是 Agent 执行过程的完整记录，包含有序的 TrajectoryStep 列表。
// 每个 Step 通过 StepDetail 接口区分 LLM 调用（LLMCallDetail）和工具调用（ToolCallDetail）。
// Trajectory 是优化器 backward 阶段的单源真相（single source of truth）。
//
// 本包仅提供基础类型定义和序列化工具函数，Builder/Extractor/Aggregator/Registry/Store
// 等高级功能在后续章节实现。
//
// 文件目录：
//
//	trajectory/
//	├── doc.go           # 包文档
//	├── types.go         # 核心类型定义 + ToMessages
//	└── json_safe.go     # JSONSafe + MessageToDict + responseToText
//
// 对应 Python 代码：openjiuwen/agent_evolving/trajectory/types.py
package trajectory
