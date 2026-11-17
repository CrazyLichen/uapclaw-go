// Package vcs 提供会话上下文状态的 JSON-native 编解码。
//
// 本包对应 Python openjiuwen/core/session/vcs/ 包。消息无 discriminated
// union，按 role 派发到具体子类，避免基类反序列化静默丢弃 tool_calls /
// tool_call_id 字段。本批仅移植 codec.py；同包其余文件因全仓无消费方暂不
// 移植（见 25.1f spec D-2）。
//
// 文件目录：
//
//	vcs/
//	├── doc.go      # 包文档
//	└── codec.go    # 消息与上下文状态的编解码
//
// 对应 Python 代码：openjiuwen/core/session/vcs/codec.py(68)
package vcs
