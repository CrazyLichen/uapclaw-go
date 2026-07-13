// Package skilldev 提供 SkillDev Pipeline 的核心数据模型和基础设施。
//
// SkillDev 是技能开发流水线，支持从自然语言描述自动生成、测试、优化和打包技能。
// 本包定义了流程阶段、运行时状态、挂起点配置、评测数据模型等核心数据结构，
// 以及状态持久化、工作区管理、阶段执行上下文等基础设施。
//
// 核心流程：
//
//	INIT → PLAN → PLAN_CONFIRM(挂起) → GENERATE → VALIDATE
//	  → TEST_DESIGN → TEST_RUN → EVALUATE → REVIEW(挂起)
//	  → IMPROVE → (回到 TEST_RUN 迭代)
//	  → PACKAGE → DESC_OPTIMIZE_CONFIRM(挂起) → DESC_OPTIMIZE → COMPLETED
//
// 文件目录：
//
//	skilldev/
//	├── doc.go            # 包文档
//	├── schema.go         # 核心数据模型：阶段枚举、状态、事件、挂起配置、评测模型
//	├── deps.go           # SkillDevDeps：最小外部依赖定义
//	├── store.go          # StateStore：任务状态持久化（本地文件实现）
//	├── workspace.go      # WorkspaceProvider：工作区目录管理
//	├── context.go        # SkillDevContext：阶段执行上下文（emit/agent 创建）
//	├── pipeline.go       # SkillDevPipeline：确定性状态机编排器（Run/Resume/checkpoint）
//	├── service.go        # SkillDevService：无状态服务入口（Handle 分发/7 个方法 handler）
//	└── stages/           # 阶段处理器实现
//
// 对应 Python 代码：jiwenswarm/server/runtime/skill/skilldev/
package skilldev
