// Package command_parser 提供 Gateway slash 命令解析器。
//
// 解析 IM 渠道消息中的 /new_session、/mode、/switch、/skills list、
// /branch、/rewind 等控制指令，返回结构化解析结果。
//
// 文件目录：
//
//	command_parser/
//	├── doc.go              # 包文档
//	├── slash_command.go    # Slash 命令解析器
//	└── slash_command_test.go # 解析器测试
//
// 对应 Python 代码：jiuwenswarm/gateway/message_handler/command_parser/
package command_parser
