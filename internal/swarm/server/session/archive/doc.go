// Package archive 提供会话归档与永久删除的异步收尾服务。
//
// 它把"删除/归档"拆成两段：接受阶段快速返回（落操作记录 + 挂写围栏 +
// 逻辑标记），后台 finalizer 完成停运行时、刷写队列、删目录、清 mailbox 等
// 重活；失败有界重试并落退避时刻，进程重启后由恢复循环接着做完。
//
// 文件目录：
//
//	archive/
//	├── doc.go       # 包文档
//	├── service.go   # 装配、生命周期钩子接口、常量与退避
//	├── accept.go    # 接受阶段（Session / acceptSessionCleanup / acceptedPayload）
//	├── finalize.go  # 后台收尾（spawnFinalizer / finalizeSession 三件套 / 归档移动）
//	├── recover.go   # 恢复循环与失败落盘
//	└── query.go     # 查询（listSessions / unarchive / cron 会话）
//
// 对应 Python 代码：jiuwenswarm/server/runtime/session/session_archive.py
// （1744 行，SessionArchiveService 40 方法 + 2 模块级函数）
package archive
