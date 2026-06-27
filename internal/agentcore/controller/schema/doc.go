// Package schema 提供 Controller 领域的公共类型定义，包括事件类型、数据帧、任务状态枚举、
// 意图类型、输出载荷及任务实体等。
//
// 本包是 Controller 领域（6.19-6.22）的基础类型包，供 single_agent、session 等上层包引用，
// 不依赖任何 agentcore 子包（除 session/stream 用于 Schema 接口），避免循环依赖。
//
// 文件目录：
//
//	schema/
//	├── doc.go                # 包文档
//	├── dataframe.go          # DataFrame 数据帧接口及 Text/Json/File 实现（含多态 JSON 序列化）
//	├── event.go              # Event 事件接口、EventType 枚举及各事件类型（含多态 JSON 序列化）
//	├── controller_output.go  # ControllerOutput 输出载荷、输出分片及批量输出
//	├── intent.go             # IntentType 意图类型枚举及 Intent 结构体
//	├── task.go               # Task 任务实体（含校验、多态 JSON 序列化）
//	└── task_status.go        # TaskStatus 任务状态枚举
//
// 对应 Python 代码：openjiuwen/core/controller/schema/
package schema
