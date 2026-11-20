// Package singleharness 提供单 harness 迭代优化的入口编排（疗程管理者）。
//
// 本包是 RSI 17.4d 的落地：围绕一个专家智能体（Expert Harness）安排
// "基线体检 → 逐轮（诊断 → 手术 → 候选模考 → 晋级门）→ 发布冠军版本"的完整循环，
// 每轮进展写成 epoch 树事件对接 17.1 引擎事件契约。
// 候选评估经 17.4f 的 TeamEvaluator 打分桩注入（⤴️17.6 回填真身后删桩），
// 取消协作经 runtime_cancellation 桩接线（⤴️17.7 回填后删桩）。
//
// 文件目录：
//
//	singleharness/
//	├── doc.go                  # 包文档
//	├── iterative.go            # 主循环编排器 + 状态/指纹/持久化/发布（iterative.py 一比一）
//	├── continuous.go           # 连续改进模式（continuous.py）
//	├── promotion_policy.go     # 晋级判定：非回归门/EPSILON/包指纹/完整分数（promotion_policy.py）
//	├── candidate_feedback.go   # 候选反馈 cohort 构建与排序（candidate_feedback.py）
//	├── behavior_check.go       # 候选行为核查与反馈路由（behavior_check.py）
//	├── events_translate.go     # epoch 树事件翻译（events_translate.py）
//	├── source_evidence.go      # 评估来源证据与材料指纹（source_evidence.py）
//	├── runtime_evidence.go     # 能力不可判定用例收集（runtime_evidence.py）
//	└── evaluator_seam.go      # evaluator fake 注入缝（对齐 17.4f 桩签名）
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/single_harness/
package singleharness
