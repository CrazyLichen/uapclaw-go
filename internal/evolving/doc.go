// Package evolving 提供技能进化（Skill Evolution）框架，支持通过训练和评估流程
// 自动优化 Agent 技能的提示词、测试用例和描述信息。
//
// 本包定义了进化流程中使用的全局常量，子包分别负责数据集加载、评估指标计算、
// 训练执行和协议定义等具体功能。
//
// 文件目录：
//
//	evolving/
//	├── doc.go           # 包文档
//	└── constant.go      # 进化流程全局常量
//
// 子包：
//
//	dataset/    # 数据集加载与用例管理
//	evaluator/  # 评估器与评估指标
//	schema/     # 进化协议与数据结构
//	trainer/    # 训练执行器与进度管理
//
// 对应 Python 代码：openjiuwen/core/evolving/
package evolving
