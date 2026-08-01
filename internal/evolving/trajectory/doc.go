// Package trajectory 提供自演化系统的执行轨迹数据类型和提取接口。
//
// Trajectory 是 Agent 执行过程的完整记录，包含有序的 TrajectoryStep 列表。
// 每个 Step 通过 StepDetail 接口区分 LLM 调用（LLMCallDetail）和工具调用（ToolCallDetail）。
// Trajectory 是优化器 backward 阶段的单源真相（single source of truth）。
//
// TrajectoryExtractor 接口定义从 Session 提取 Trajectory 的协议，
// 具体实现在后续章节（Builder/Aggregator/Registry/Store 等）。
//
// 文件目录：
//
//	trajectory/
//	├── doc.go           # 包文档
//	├── types.go         # 核心类型定义 + ToMessages
//	├── extractor.go     # TrajectoryExtractor 接口定义
//	└── json_safe.go     # JSONSafe + MessageToDict + responseToText
//
// 对应 Python 代码：openjiuwen/agent_evolving/trajectory/types.py + extractor.py
package trajectory
