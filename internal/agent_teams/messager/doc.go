// Package messager 提供团队消息通信配置与构建能力。
//
// 本包定义团队消息通信（Messager）的配置结构体与构建函数，
// 对齐 Python 端 openjiuwen/agent_teams/messager/base.py 的实现。
// 支持多种通信后端（inprocess、pyzmq 等），通过配置选择后端实例。
//
// 文件目录：
//
//	messager/
//	├── doc.go           # 包文档
//	└── base.go          # MessagerTransportConfig 等配置结构体与构建函数
//
// 对应 Python 代码：openjiuwen/agent_teams/messager/base.py
package messager
