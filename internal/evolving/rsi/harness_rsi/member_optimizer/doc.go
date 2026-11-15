// Package memberoptimizer 提供 RSI 手术系统（member 优化器）的载入与模型配置件。
//
// 本包对应 Python openjiuwen/rsi/harness_rsi/member_optimizer/。17.4b1 按
// 前置件方案A落地：
//
//   - loader.go：evaluation_result_analyzer 实际使用的符号闭包
//     （EvalRef、load_eval_ref、resolve_candidate_roles 及其私有依赖
//     _normalize_harness_refs_path/_parse_harness_refs_file/
//     _parse_harness_refs_dir/_supplement_from_team_skill/
//     _resolve_harness_ref_path）；load_analysis_ref / resolve_team_issues /
//     AnalysisUnavailableError / BoundedEvidenceBundle / load_case_evidence
//     及其依赖 ⤴️ 17.4c 手术系统章节补齐。
//   - schema.go：schema.py 的 MemberRoleCandidate 切片（analyzer 经 loader
//     依赖的唯一 schema 类型）；schema.py 其余类型 ⤴️ 17.4c。
//   - model_config.go：model_config.py 全量对齐（load_model_config_ref +
//     without_inner_sdk_retries 等），无回填残留；BudgetedRsiModel 占位与
//     BuildDeepAgentFromModelConfig 桩 ⤴️ 17.4c（budget_model.py 落真身）。
//
// 包内其余文件（action_executor/action_planner/verification/optimizer/
// mechanism/role_attributor/hypothesis/budget_model/worktree_coordinator
// 等）⤴️ 17.4c。
//
// 文件目录：
//
//	member_optimizer/
//	├── doc.go           # 包文档
//	├── loader.go        # 评估引用载入与角色解析（17.4b1 切片 ⤴️ 17.4c 补齐）
//	├── schema.go        # MemberRoleCandidate（schema.py 切片 ⤴️ 17.4c 补齐）
//	└── model_config.go  # 诊断 agent 模型配置引用载入（全量对齐；桩 ⤴️ 17.4c）
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/member_optimizer/
package memberoptimizer
