// Package wswrite 提供 WebSocket 连接的串行化写出能力。
//
// gorilla/websocket 的 Conn.WriteMessage 非并发安全——同一连接上的并发
// 写会触发 "concurrent write to websocket connection" panic。Python
// websockets 库的 send 天然受单事件循环串行化，移植代码中读循环线程
// （本地 handler 的 res/event 帧）与消息派发线程（定向/广播出站）会并发
// 写同一连接，必须显式互斥。
//
// 本包以进程级注册表为每个连接分配写锁；gateway 与 channel_manager/web
// 两个连接体系（gateway → web 存在依赖边，锁表不能放 gateway 包）共用
// 同一注册表，保证任意写点组合之间互斥。
//
// 文件目录：
//
//	wswrite/
//	├── doc.go            # 包文档
//	└── guard.go          # 连接写锁注册表
//
// 对应 Python 代码：无（Python websockets 库事件循环天然串行化 send）。
package wswrite
