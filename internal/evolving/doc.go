// Package evolving 提供技能进化（Skill Evolution）框架，支持通过训练和评估流程
// 自动优化 Agent 技能的提示词、测试用例和描述信息。
//
// 本包定义了进化流程中使用的全局常量、TrainableAgent 接口，
// 子包分别负责数据集加载、评估指标计算、训练执行和协议定义等具体功能。
//
// 文件目录：
//
//	evolving/
//	├── doc.go                # 包文档
//	├── constant.go           # 进化流程全局常量
//	├── trainable_agent.go    # TrainableAgent 接口（从 trainer 包迁移，解决循环依赖）
//	├── utils.go              # 辅助函数（GetContentStringFromTemplate）
//	└── update_execution.go   # 更新执行函数（ExecuteUpdates/ApplyUpdates/SummarizeApplyResults）
//
// 子包：
//
//	checkpointing/ # 检查点保存/恢复和技能演进数据文件系统 IO（9.78）
//	dataset/       # 数据集加载与用例管理
//	experience/    # 在线经验生命周期编排类型（9.79 前置定义）
//	evaluator/     # 评估器与评估指标
//	optimizer/     # 维度优化器基类 + LLM 弹性重试 + LLM 提示词优化器
//	schema/        # 进化协议与数据结构
//	signal/        # 信号检测
//	trainer/       # 训练执行器与进度管理
//	trajectory/    # 执行轨迹数据类型
//	updater/       # 更新器
//
// 对应 Python 代码：openjiuwen/core/evolving/
package evolving
