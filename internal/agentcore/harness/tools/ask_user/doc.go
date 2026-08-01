// Package ask_user 提供向用户提问的空壳工具（AskUserTool）。
//
// AskUserTool 的 invoke/stream 方法返回空 map{}，
// 真正的用户交互逻辑在 AskUserRail（harness/rails/interrupt）中通过中断机制完成。
// 此包仅负责工具注册（ToolCard + 空壳 MapFunction），供 AskUserRail.Init 调用创建。
//
// 文件目录：
//
//	ask_user/
//	├── doc.go            # 包文档
//	├── ask_user.go       # AskUserTool 空壳工具 + NewAskUserTool 工厂函数
//	└── ask_user_test.go  # 单元测试
//
// 对应 Python 代码：openjiuwen/harness/tools/ask_user.py
package ask_user
