// Package mcp 提供 MCP 连接状态存储（<workspace>/mcp/state.json）。
//
// connect/disconnect 变更此文件；get_mcp_servers 将其与 config.yaml 手写的
// mcp.servers 合并，上层（adapter/handlers/registry）读一份合并列表。state
// 的 enabled 标志只是 TUI/全局默认开关——web 渠道忽略它，按 chat.send 的
// mcp 字段加载。
//
// 本批（16.4b D-16-4-6 回填）落地读取侧全量：技能发现链消费
// ConnectedMcpSkillDirs（连接中 MCP 的捆绑技能目录）。写侧 upsert/remove/
// set_state/mounts 消费方是 MCP 连接章节（缺口已登记 10.3.31g）⤵️ 随该章节
// 一比一移植，本批仅落 _save 原语供写侧复用与测试。
//
// 文件目录：
//
//	mcp/
//	├── doc.go           # 包文档
//	└── state_store.go  # state.json 读取侧 + _save 原语
//
// 对应 Python 代码：jiuwenswarm/server/runtime/mcp/state_store.py（442 行；
// 本文件为读取侧全量 + _save，写侧 ⤵️ 登记见上）
package mcp
