// Package todo_render todo 复选框渲染，供 CLI 输出。
//
// 渲染四态复选框（☑ completed / ◐ in_progress / ☐ pending /
// ☒ cancelled）、⎿ 前缀清单与 ✓N ◐N ☐N 进度摘要。ParseTodoResult 双
// 策略解析工具结果：JSON（裸列表 / items|tasks|todos|result 包装 / 单
// 对象）与 SDK 文本格式（TodoCreateTool 与 TodoListTool 两种形态，
// 剥离 dict repr 尾部残留）。ApplyTodoModifyArgs 把 todo_modify 的六个
// 动作（update / delete / cancel / append / insert_after / insert_before）
// 套到已渲染清单上，让 CLI 在结果只有摘要时也能刷新视图。
//
// 文件目录：
//
//	todo_render/
//	├── doc.go                # 包文档
//	├── todo_render.go       # 渲染 / 解析 / modify 应用
//	└── todo_render_test.go
//
// 对应 Python 代码：openjiuwen/harness/cli/ui/todo_render.py（392 行）
package todo_render
