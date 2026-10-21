// Package memoryrpc 提供 TUI 记忆管理面板的 5 个 RPC handler。
//
// 对齐 Python jiuwenswarm/agents/harness/common/memory_rpc.py：
// memory.list/edit/status/toggle/open 五个方法注册在 Gateway 本地 handler
// （非 E2A 链路），由 TUI 通道（tui_connect.py）消费。本章交付纯函数层，
// 注册链路属 11.15 TUI 通道职责。
//
// handler 返回 map[string]any 而非 error：对齐 Python「handler 从不抛异常，
// 异常由外层 wrapper（tui_connect 的 _memory_list 等）捕获后转 send_response(ok=False)」；
// Go 侧无 error 通道，等价机制是响应内嵌 result["error"] 字段（M-04），注册层
// 检测该字段转 ok=False。写回失败时 new_value 置 old_value 如实上报未变更，
// mkdir/touch 等前置操作失败时 result["error"] 携带原因。
//
// 文件目录：
//
//	memoryrpc/
//	├── doc.go           # 包文档
//	└── memory_rpc.go    # 5 导出 handler + 私有辅助函数 + config 写回
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/memory_rpc.py
package memoryrpc
