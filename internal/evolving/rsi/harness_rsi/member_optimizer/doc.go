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
//   - 17.4c2（已落地）：action_groups 子包（policy/definitions/scheduling）/
//     worktree_coordinator / action_planner（13 校验器+agent+编排器）/
//     action_executor（三层并行波次+隔离手术区+确定性手术）/ plugin_manifest /
//     package_registry（executor 依赖闭包）/ execution_contract /
//     verification_slice（gopapy AST 安全检查）。
//   - 17.4c3（⤴️）：verification 全量（1,754 行）/ package_registry 验证归一面
//     收编 / path_layout / optimizer 门面+发布 / llm-tag 真模型集成测试回填
//     （D-c2-8）。
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
//	├── model_load.go           # load_member_optimizer_model（D-c2-3 移父包）
//	├── action_field.go         # MemberOptimizationAction 字段读取适配（D-c2-1）
//	├── action_planner.go       # 开单导出面（校验器群 13 件）
//	├── action_planner_impl.go  # 开单校验器+agent+编排器实现
//	├── action_executor.go      # 动刀工具函数群（快照/契约投影/资源指引）
//	├── action_executor_impl.go # 动刀 agent+登记簿同步+确定性手术
//	├── action_executor_orchestrator.go # 三层并行编排器
//	├── worktree_coordinator.go # 哈希 worktree 协调
//	├── plugin_manifest.go      # 登记簿播种与 JSON 编译回写
//	├── package_registry.go     # 登记簿规则（validate/normalize/upsert）
//	├── package_write_allowed.go # 包写允许核心（D-c2-5）
//	├── execution_contract.go   # 完成契约（束键/回退评估）
//	├── verification_slice.go   # gopapy AST 安全检查（D-c2-6/7）
//	├── action_groups/          # 动作组子包（policy/definitions/scheduling）
//	└── agents/                # agents 子包（factory/output/profiles + prompts embed + rails）
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/member_optimizer/
package memberoptimizer
