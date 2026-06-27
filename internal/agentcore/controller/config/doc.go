// Package config 提供 Controller 领域的配置定义。
//
// ControllerConfig 定义了 Controller 及其子组件（TaskManager、EventQueue、
// TaskScheduler、EventHandler）的运行参数，包括任务调度、事件队列、
// 意图识别、完成信号和流配置。
//
// 文件目录：
//
//	config/
//	├── doc.go                 # 包文档
//	└── controller_config.go   # ControllerConfig 配置定义
//
// 对应 Python 代码：openjiuwen/core/controller/config.py
package config
