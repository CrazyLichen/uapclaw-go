// Package control 提供配置面板读链服务：get_panel 开关墙汇总与四个只读方法。
//
// 对齐上游迁移后的双链结构：Gateway 只做转发，读/写真相源都在 Runtime（server）
// 侧的磁盘文件（config.yaml + .env）。本包读链对齐 jiuwenswarm/server/control/；
// 写链（config.set/save_all 等）在同目录 configpanel 子包。
// 读配置一律重读磁盘文件而非进程内存——修复 Python 原注释记载的跨进程时序 bug
// （os.environ 过期导致设置页显示旧模型）。
//
// 文件目录：
//
//	control/
//	├── doc.go              # 包文档
//	├── a2ui_config.go     # A2UI 开关解析（Front-safe，无 Runtime 依赖）
//	└── config_service.go  # 开关墙 GetPanel + 四只读方法分派
//
// 对应 Python 代码：jiuwenswarm/server/control/{config_service.py, a2ui_config.py}
package control
