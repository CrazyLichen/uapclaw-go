// Package memberoptimizer 提供 RSI 手术系统（member 优化器）的完整实现。
//
// 本包对应 Python openjiuwen/rsi/harness_rsi/member_optimizer/，按 17.4c1/c2/c3
// 三子节推进：
//
//   - 17.4c1（已落地）：schema 全量契约 / lever 杠杆分类 / loader 补齐 /
//     role_attributor + mechanism_attributor 两步 LLM 归因 / member_selector
//     纯算术选择 / hypothesis 不可变假设契约 / budget_model + model_config
//     桩回填（BudgetedRsiModel 真身 + BuildDeepAgentFromModelConfig 装配真
//     DeepAgent）/ agents 子包（factory/output/profiles + 5 提示词 go:embed）。
//   - 17.4c2（⤴️）：action_planner / action_groups / action_executor /
//     worktree_coordinator。
//   - 17.4c3（⤴️）：verification / package_registry / plugin_manifest /
//     path_layout / execution_contract / optimizer 门面+发布 / gopapy AST 扫描。
//
// 跨包桥接：Go 子包不可反向引用父包（agents/factory.go 引本包 BudgetedRsiModel），
// agents 能力经 structured_invoke.go 的 AgentsBridge 由 agents 包 init 注册
// （spec 2026-11-16 §4 D-c1-1）。
//
// 文件目录：
//
//	member_optimizer/
//	├── doc.go                  # 包文档
//	├── schema.go               # schema.py 全量契约（3 枚举 + 20 结构体）
//	├── lever.go                # 杠杆分类与决策策略（lever.py）
//	├── loader.go               # 评估引用/病历载入 + 角色清单三级解析（loader.py）
//	├── role_attributor.go      # 第 1 步角色定责（role_attributor.py）
//	├── mechanism_attributor.go # 第 2 步病根归因（mechanism_attributor.py）
//	├── member_selector.go      # 第 3 步纯算术选择（member_selector.py）
//	├── hypothesis.go           # 不可变假设契约（hypothesis.py）
//	├── structured_invoke.go    # AgentsBridge 子包能力桥（包环倒置 D-c1-1）
//	├── py_helpers.go           # Python 语义辅助（str/repr/json dumps）
//	├── budget_model.go         # BudgetedRsiModel 真身（budget_model.py）
//	├── model_config.go         # 模型配置载入变换（model_config.py，桩已回填删除）
//	└── agents/                # agents 子包（factory/output/profiles + prompts embed + rails）
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/member_optimizer/
package memberoptimizer
