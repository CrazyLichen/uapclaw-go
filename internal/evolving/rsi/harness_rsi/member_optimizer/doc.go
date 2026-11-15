// Package memberoptimizer 提供 RSI 手术系统（member 优化器）的载入与模型配置件。
//
// 本包对应 Python openjiuwen/rsi/harness_rsi/member_optimizer/。17.4b1 按
// 前置件方案A落地：
//
//   - loader.go：evaluation_result_analyzer 实际使用的符号切片
//     （EvalRef/AnalysisRef/AnalysisUnavailableError/BoundedEvidenceBundle、
//     load_eval_ref/load_analysis_ref/resolve_team_issues/
//     resolve_candidate_roles 及其私有依赖）；load_case_evidence 等未用
//     符号 ⤴️ 17.4c 手术系统章节补齐。
//   - model_config.go：model_config.py 全量对齐（load_model_config_ref +
//     without_inner_sdk_retries 等），无回填残留。
//
// 包内其余文件（action_executor/action_planner/verification/optimizer/
// mechanism/role_attributor/hypothesis/budget_model/worktree_coordinator
// 等）⤴️ 17.4c。
//
// 文件目录：
//
//	member_optimizer/
//	├── doc.go           # 包文档
//	├── loader.go        # 评估引用/分析引用载入与角色解析（17.4b1 切片 ⤴️ 17.4c 补齐）
//	└── model_config.go  # 诊断 agent 模型配置引用载入（全量对齐）
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/member_optimizer/
package memberoptimizer
