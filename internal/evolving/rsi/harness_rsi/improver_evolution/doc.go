// Package improver_evolution 提供 Harness improver 的策略层（版本化改进策略）。
//
// 本包是 17.4d 的前置件切片：Python improver_evolution/policy.py 全文件对齐。
// 17.4e 将补齐同目录的 feedback_analysis / meta_validation（见计划表 ⤴️ 登记）。
//
// 文件目录：
//
//	improver_evolution/
//	├── doc.go        # 包文档
//	└── policy.go    # FrozenDict / VersionedImproverPolicy / 策略读写与候选提议
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/improver_evolution/policy.py
package improver_evolution
