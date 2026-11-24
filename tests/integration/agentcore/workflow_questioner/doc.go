// Package workflow_questioner 提供 8.24 QuestionerComp 全链集成测试：
// 真实 Workflow 图编排 + pregel 引擎 + NodeSession 状态 + interaction 中断/
// checkpoint 恢复链，LLM 网络边界用 mockllm.MockModelClient。
//
// 对齐 Python test_runner_agent_resource_management 欠的 Questioner
// __interaction__ → 二次调用恢复场景（6.25 ⤵️ 回填兑现，spec §4.10）。
//
// 对应 Python 代码：openjiuwen/core/workflow/components/llm/questioner_comp.py
package workflow_questioner
