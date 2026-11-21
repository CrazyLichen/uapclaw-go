// Package workflow_llm_comp 提供 8.18 LLMComp 组件全链集成测试。
//
// 真实跨子系统链路：workflow（Invoke 门面 + createWorkflowSession 装配）→
// graph（CompiledGraph/Pregel 调度 + Vertex 四钩子派发 + kwargs context 传递）→
// workflow/components/llm（LLMExecutable 校验/提示词组装/格式化 + Model 注入缝）
// → session（NewComponentSession 门面 + state）→ runner/callback（组件事件）。
// 对齐 Python llm_comp.py 的 invoke/stream 双入口与 json/text 格式化行为。
//
// 文件目录：
//
//	workflow_llm_comp/
//	├── doc.go               # 包文档
//	└── llm_comp_test.go    # LLMComp 全链用例（批式 + 流式 + json 格式化）
//
// 对应 Python 代码：openjiuwen/core/workflow/components/llm/llm_comp.py
package workflow_llm_comp
