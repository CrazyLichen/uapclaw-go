// Package judger 提供 LLM 评委：判分契约、证据工作区、裁判运行时与三种判分器。
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/evaluator/judger/
//
// 文件目录：
//
//	judger/
//	├── doc.go                # 包文档
//	├── base.go               # 判分器接口/工厂/JudgeResult（17.4b1 前置切片，17.6 Task5 扩写）
//	├── scoring.go            # 评分契约展开/判决解析/分数重算（scoring.py）
//	├── judge_evidence.go     # 证据工作区快照与响应分页（judge_evidence.py）
//	├── judge_diagnostics.go  # 单次评估尝试的模型调用诊断（judge_diagnostics.py）
//	├── judge_search.go       # 冻结证据字面量搜索工具（judge_search.py）
//	├── evidence_guard.go     # 证据读取工具与消息分页守卫（evidence_guard.py）
//	├── direct_evidence.go    # 小体量评估的无损内联证据（direct_evidence.py）
//	├── judge_prompt.md       # 连续判分模式裁判提示词（Python 原文件逐字）
//	├── judge_prompt.legacy.md# 二元判分模式裁判提示词（Python 原文件逐字）
//	└── fs_helpers.go         # 包内文件系统小助手（Go 侧共用，非 Python 对应）
package judger
