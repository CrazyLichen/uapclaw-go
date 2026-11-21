// Package workflow_branch_comp 8.19 BranchComp 全链集成测试。
// 真实跨子系统链路：workflow（门面 + AddConditionalConnection BranchRouter 分支 +
// createWorkflowSession 装配）→ graph（CompiledGraph/Pregel 调度 + 条件边 dispatch +
// CNF 屏障解析）→ components/condition（表达式求值器）→ components/flow
// （BranchComponent/BranchRouter）→ session/state（全局状态取值）。
// 对齐 Python: workflow components branch 真实链场景。
package workflow_branch_comp
