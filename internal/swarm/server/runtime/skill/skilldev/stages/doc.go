// Package stages 提供 SkillDev Pipeline 各阶段的处理器实现。
//
// 每个阶段实现 StageHandler 接口，通过 Execute() 与 Pipeline 交互。
// Pipeline 读取 Execute() 返回的 StageResult 驱动状态跳转。
// 处理器不应持有跨请求的状态——所有状态均通过 SkillDevContext 传入。
//
// 阶段列表：
//
//	INIT        — 解析请求参数、准备工作区
//	PLAN        — Agent 生成开发计划
//	GENERATE    — Agent 按 plan 生成 skill 文件集
//	VALIDATE    — 校验 SKILL.md 格式合规性
//	TEST_DESIGN — Agent 设计测试用例
//	TEST_RUN    — 子 Agent 并行执行测试用例
//	EVALUATE    — Grader 评分 + Benchmark 聚合 + Analyst 分析
//	IMPROVE     — Agent 根据反馈改进 Skill
//	PACKAGE     — 打包 skill/ 为 .skill (zip)
//	DESC_OPTIMIZE — 优化 SKILL.md 的 description 以提高触发准确率
//
// 文件目录：
//
//	stages/
//	├── doc.go               # 包文档
//	├── base.go              # StageHandler 接口 + StageResult
//	├── init_stage.go        # INIT 阶段处理器
//	├── plan_stage.go        # PLAN 阶段处理器
//	├── generate_stage.go    # GENERATE 阶段处理器
//	├── validate_stage.go    # VALIDATE 阶段处理器
//	├── test_design_stage.go # TEST_DESIGN 阶段处理器
//	├── test_run_stage.go    # TEST_RUN 阶段处理器
//	├── evaluate_stage.go    # EVALUATE 阶段处理器
//	├── improve_stage.go     # IMPROVE 阶段处理器
//	├── package_stage.go     # PACKAGE 阶段处理器
//	└── desc_optimize_stage.go # DESC_OPTIMIZE 阶段处理器
//
// 对应 Python 代码：jiwenswarm/server/runtime/skill/skilldev/stages/
package stages
