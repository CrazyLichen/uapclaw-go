// Package dataset 提供自演化系统的训练/评估数据类型。
//
// 包含 Case（单个样本）、EvaluatedCase（评估后样本）和 CaseLoader（样本容器）。
// Case 定义输入、期望答案和可选工具列表；EvaluatedCase 在 Case 基础上
// 附加模型输出、综合评分、评分原因和各指标独立评分。
// CaseLoader 支持按比例拆分训练集/验证集和随机打乱。
//
// 文件目录：
//
//	dataset/
//	├── doc.go            # 包文档
//	├── case.go           # Case + EvaluatedCase 结构体
//	└── case_loader.go    # CaseLoader 样本容器
//
// 对应 Python 代码：openjiuwen/agent_evolving/dataset/
package dataset
