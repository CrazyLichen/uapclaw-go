// Package renderer 流式渲染器——把 OutputSchema 块映射成终端输出。
//
// 八种块类型：llm_output（绿色 ● 前缀流式输出并累计）/ llm_reasoning
// （默认隐藏）/ answer（无 llm_output 时兜底）/ __interaction__
// （回调 + 收集挂起）/ message（⚙ 暗色）/ tool_call（● Name(args)）/
// tool_result（⎿ 摘要 + write 预览）/ todo.updated（复选框清单）。
// controller_output 的 task_failed 载荷以红 ✗ 展示。
//
// Rich Console → io.Writer + ANSI 码；零块警告与全不可见警告逐字对齐。
//
// 文件目录：
//
//	renderer/
//	├── doc.go           # 包文档
//	├── renderer.go      # RenderStream / RenderToolCall / RenderToolResult
//	└── renderer_test.go
//
// 对应 Python 代码：openjiuwen/harness/cli/ui/renderer.py（420 行）
package renderer
