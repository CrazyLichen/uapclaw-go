// Package store Front Control 的项目磁盘查询层（只读）。
//
// Phase 1 Control 只拥有轻量项目读（list/info/sessions）；
// create/rename/pin 与全部 git 操作留在执行平面（Python project_adapter.py，挂账）。
//
// 文件目录：
//
//	store/
//	├── doc.go               # 包文档
//	├── work_mode.go         # 工作模式常量与解析（对齐 gateway/common/work_mode.py）
//	├── cron_session.go      # cron 会话目录名匹配（对齐 common/cron_session.py）
//	├── project_store.go     # projects.json 读路径（对齐 project_store.py 读子集）
//	├── project_queries.go   # 项目列表/详情/会话查询（对齐 project_queries.py）
//	└── session_info.go     # SessionInfo 投影与全量元数据收集（对齐 session_info.py）
//
// 对应 Python 代码：jiuwenswarm/server/control/store/（project_queries.py）
// 及 jiuwenswarm/server/runtime/session/（project_store.py / session_info.py）。
package store
