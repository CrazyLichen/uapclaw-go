// Package trajectory 提供自演化系统的执行轨迹数据类型、提取、存储、聚合和注册。
//
// Trajectory 是 Agent 执行过程的完整记录，包含有序的 TrajectoryStep 列表。
// 每个 Step 通过 StepDetail 接口区分 LLM 调用（LLMCallDetail）和工具调用（ToolCallDetail）。
// Trajectory 是优化器 backward 阶段的单源真相（single source of truth）。
//
// 核心模块：
//   - TrajectoryBuilder：从零构建 Trajectory（record_step + cost 累积 + maxSteps 滑窗）
//   - TracerTrajectoryExtractor：从 Session.tracer() 的 AgentSpanManager 提取轨迹
//   - TrajectoryStore / InMemoryTrajectoryStore / FileTrajectoryStore：轨迹持久化
//   - TeamTrajectoryAggregator：聚合多成员轨迹为团队级视图
//   - TrajectorySink / TrajectorySource / InMemoryTrajectoryRegistry：运行时轨迹注册表
//
// 文件目录：
//
//	trajectory/
//	├── doc.go           # 包文档
//	├── types.go         # 核心类型定义（Trajectory, TrajectoryStep, StepDetail, CostInfo, StepKind）+ ToMessages
//	├── builder.go       # TrajectoryBuilder 构建 + 选项模式
//	├── extractor.go     # TrajectoryExtractor 接口 + TracerTrajectoryExtractor 实现
//	├── store.go         # TrajectoryStore 接口 + InMemoryTrajectoryStore + FileTrajectoryStore
//	├── aggregator.go    # TeamTrajectoryAggregator + FilterMemberTrajectory + 协作步骤过滤
//	├── registry.go      # TrajectorySink/Source 接口 + InMemoryTrajectoryRegistry + MemberTrajectorySnapshot
//	└── json_safe.go     # JSONSafe + MessageToDict + responseToText
//
// 对应 Python 代码：openjiuwen/agent_evolving/trajectory/
package trajectory
