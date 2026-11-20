// Package improver_evolution 提供 Harness improver 演进的策略层与复盘层。
//
// 三块职责：policy（版本化改进策略，17.4d 前置件切片）、feedback_analysis（候选反馈
// 账本 v1 的确定性复盘分析，纯计算零 LLM）、meta_validation（候选 Improver 的配对
// 元校验，只消费已物化 checkpoint 结果，不运行 Improver、不发布版本）。
// 17.4e1 完成后本包与 Python improver_evolution/ 四文件全量对齐。
//
// 文件目录：
//
//	improver_evolution/
//	├── doc.go               # 包文档
//	├── policy.go            # FrozenDict / VersionedImproverPolicy / 策略读写与候选提议
//	├── feedback_analysis.go # 账本加载校验 + 复盘报告 + 稳定模式 + training_ledger_digest
//	├── meta_validation.go   # 阈值契约 + 配对合法性校验 + 宏平均与晋升判定
//	└── python_semantics.go  # 两对齐文件共用的 Python 语义适配（无 Python 对应物）
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/improver_evolution/（policy.py /
// feedback_analysis.py / meta_validation.py / __init__.py）
package improver_evolution
