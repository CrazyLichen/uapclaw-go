// Package lifecycle 提供会话与项目生命周期操作的持久化围栏。
//
// 资源文件就是提交点：它内嵌当前操作，使"写完日志但还没改指针"的崩溃
// 不会暴露一个未加围栏的资源。准入判定**不使用进程内缓存**——多进程
// （多个 AgentServer）必须看到同一份磁盘状态。
//
// 文件目录：
//
//	lifecycle/
//	├── doc.go         # 包文档
//	├── paths.go       # ID 校验、资源路径、会话目录解析
//	├── lock.go        # 跨进程文件锁、原子写、写围栏
//	├── operation.go   # 操作状态机（begin/claim/renew/update/complete/mark_finalizing）
//	├── guard.go       # 准入判定（guard/write_guard/runtime owner）
//	├── project.go     # 工程目录候选/记录/来源解析
//	├── query.go       # 投影、分页、事件快照
//	├── lock_unix.go   # Unix 跨进程锁实现
//	├── lock_windows.go # Windows 跨进程锁实现
//	├── process_unix.go # Unix 进程存活探测
//	└── process_windows.go # Windows 进程存活探测
//
// 对应 Python 代码：jiuwenswarm/server/runtime/session/lifecycle.py（805 行，39 函数）
package lifecycle
