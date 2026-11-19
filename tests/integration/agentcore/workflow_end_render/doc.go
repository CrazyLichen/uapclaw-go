// Package workflow_end_render 提供 8.17 End 组件流叶消费（D-17-3 修正）的集成测试。
//
// 真实跨子系统链路：graph/stream_actor（ActorManager 队列 + StreamReader 拉取）
// → graph.Vertex（preStream 信封 + transform 策略）→ workflow/components/flow
// （End 组件模板渲染消费 StreamReader 叶子）→ runner/callback
// （COMPONENT_STREAM_OUTPUT 逐帧事件）→ session/state（WorkflowCommitState）。
// 对齐 Python end_comp.py 的 AsyncGenerator 叶子消费行为（8.17 审查修正回归）。
//
// 文件目录：
//
//	workflow_end_render/
//	├── doc.go               # 包文档
//	└── end_render_test.go  # End 组件 StreamReader 叶子全链用例
//
// 对应 Python 代码：openjiuwen/core/workflow/components/flow/end_comp.py
package workflow_end_render
