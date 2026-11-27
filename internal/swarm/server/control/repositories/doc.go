// Package repositories Front Control 的项目控制面仓库。
//
// 对齐 Python jiuwenswarm/server/control/repositories/project_repository.py。
// 把控制面项目方法（list/info/pinned/get_sessions/get_cron_sessions/
// cron resolve binding）分派到 store 查询层；
// create/rename/pin 与全部 git 操作留在执行平面（挂账）。
//
// 文件目录：
//
//	repositories/
//	├── doc.go                   # 包文档
//	└── project_repository.go    # 项目请求统一入口（对齐 handle_project_request）
//
// 对应 Python 代码：jiuwenswarm/server/control/repositories/project_repository.py。
package repositories
