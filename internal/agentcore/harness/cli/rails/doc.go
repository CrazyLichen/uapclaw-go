// Package rails CLI 层的用量与展示 rail。
//
// 两个 rail 挂进 DeepAgent 的回调框架，为 CLI 界面服务：
//
//   - TokenTrackingRail：挂 after_model_call，累计模型调用的 token 用量，
//     GetSummary 供 /status 与 /cost 命令读取。
//   - ToolTrackingRail：挂 before/after_tool_call，向会话流写 tool_call /
//     tool_result 块，供 CLI 渲染器以 Claude Code 风格展示工具活动。
//
// 文件目录：
//
//	rails/
//	├── doc.go           # 包文档
//	├── token_tracker.go # TokenTrackingRail（token 用量累计）
//	├── tool_tracker.go  # ToolTrackingRail（工具调用/结果展示块）
//	└── tracker_test.go # 两 rail 的单元测试
//
// 对应 Python 代码：openjiuwen/harness/cli/rails/
// （token_tracker.py 61 行、tool_tracker.py 145 行、__init__.py 13 行）
package rails
