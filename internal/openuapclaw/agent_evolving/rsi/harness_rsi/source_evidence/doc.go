// Package source_evidence 提供 RSI 评估材料身份计算（Python
// single_harness/source_evidence.py 的身份计算子集：_digest / read_mapping /
// _material_identity / _harness_identity）。
//
// 独立成包的原因：TeamEvaluator 指纹消费 _material_identity（Python
// team_evaluator.py:301 函数体内联 import），而 singleharness 包已依赖
// evaluator 包，evaluator 反向 import 会成编译环；Python 包系统无编译环
// 概念故可内联 import，Go 侧按规则 9.1 登记层级偏差，将该自包含闭包下沉
// 为兄弟包（singleharness 与 evaluator 均正向依赖本包）。
//
// 文件目录：
//
//	source_evidence/
//	├── doc.go              # 包文档
//	└── material_identity.go # EvidenceDigest/MaterialIdentity/HarnessIdentity
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/single_harness/source_evidence.py（L23-87 子集）。
package source_evidence
