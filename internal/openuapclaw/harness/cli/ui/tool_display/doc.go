// Package tool_display 工具展示格式化（名字映射、参数与结果摘要）。
//
// 提供 Claude Code 风格的工具展示头与摘要：
//
//	● Read(src/main.py)      — 工具调用头（GetDisplayName + FormatToolArgs）
//	⎿  Read 42 lines         — 结果摘要（FormatToolResult）
//
// 20 个内置工具名映射；未登记的名字回退标题化。read_file 优先取
// tool_meta.line_count 的结构化行数。FormatWritePreview 输出带行号的
// 前 5 行预览。
//
// 文件目录：
//
//	tool_display/
//	├── doc.go                # 包文档
//	├── tool_display.go      # GetDisplayName / FormatToolArgs / FormatToolResult / FormatWritePreview
//	└── tool_display_test.go
//
// 对应 Python 代码：openjiuwen/harness/cli/ui/tool_display.py（292 行）
package tool_display
