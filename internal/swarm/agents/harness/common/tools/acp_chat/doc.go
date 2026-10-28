// Package acpchat 提供 acp_chat 工具：把提示转发给配置于 acp_agents 的外部 ACP 子代理（10.6.24d）。
//
// 按 profile 缓存 AcpStdioClient（每 profile 互斥锁防并发 spawn），
// new_session=true 时重启子进程与会话。
//
// 文件目录：
//
//	acp_chat/
//	├── doc.go           # 包文档
//	├── client_cache.go  # profile 缓存与互斥
//	└── tool.go          # acp_chat 工具本体
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/tools/acp_chat/tool.py
package acpchat
