// Package messager 提供团队消息通信接口与实现。
//
// 本包定义团队消息通信（Messager）接口、配置结构体、InProcessMessager 实现，
// 对齐 Python 端 openjiuwen/agent_teams/messager/ 的实现。
// 支持多种通信后端（inprocess、pyzmq 等），通过配置选择后端实例。
// InProcessMessager 使用进程内全局 Bus 做 pub-sub 和 P2P，消息直接传递无序列化。
//
// 文件目录：
//
//	messager/
//	├── doc.go           # 包文档
//	├── messager.go      # Messager 接口定义 + MessagerHandler 类型
//	├── inprocess.go     # InProcessMessager 实现 + 全局 Bus
//	└── base.go          # MessagerTransportConfig 配置结构体与 CreateMessager 工厂函数
//
// 对应 Python 代码：openjiuwen/agent_teams/messager/
package messager
