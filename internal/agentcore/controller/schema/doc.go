// Package schema 提供 Controller 领域的公共类型定义，包括任务状态枚举等。
//
// 本包是 Controller 领域（6.19-6.22）的基础类型包，供 single_agent、session 等上层包引用，
// 不依赖任何 agentcore 子包，避免循环依赖。
//
// 文件目录：
//
//	schema/
//	├── doc.go            # 包文档
//	└── task_status.go    # TaskStatus 任务状态枚举
//
// 对应 Python 代码：openjiuwen/core/controller/schema/task.py
package schema
