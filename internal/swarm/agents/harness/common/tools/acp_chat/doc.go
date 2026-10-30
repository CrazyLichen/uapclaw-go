// Package acpchat 提供 acp_chat 工具：把提示转发给配置于 acp_agents 的外部 ACP 子代理（10.6.24d）。
//
// 按 profile 缓存 AcpStdioClient（每 profile 互斥锁防并发 spawn），
// new_session=true 时重启子进程与会话。
//
// 导出面（供 cmd 层 acp-chat 冒烟入口 10.4.6 复用）：
// LoadProfile / ClientSpec / BuildClientSpec——与工具路径共用同一套严格校验，
// 故冒烟入口对 args/env 类型错误的容忍度低于 Python cli.py（有意偏差，见该节 spec §6）。
//
// 文件目录：
//
//	acp_chat/
//	├── doc.go           # 包文档
//	├── client_cache.go  # profile 缓存与互斥 + profile 解析（LoadProfile/BuildClientSpec）
//	└── tool.go          # acp_chat 工具本体
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/tools/acp_chat/tool.py
package acpchat
