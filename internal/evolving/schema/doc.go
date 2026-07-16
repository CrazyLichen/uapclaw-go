// Package schema 提供自演化框架的共享契约类型与协议常量。
//
// 本包是演化框架的"协议层"，定义了 Operator、Optimizer、Trainer 等组件
// 共享的数据结构和常量。本包零外部依赖，可被 agentcore/operator 和
// internal/evolving 下的所有子包安全导入，不会形成循环依赖。
//
// 文件目录：
//
//	schema/             # Schema 类型定义
//	├── doc.go        # 包文档
//	├── protocol.go   # 协议常量（一比一复刻 Python agent_evolving/protocols.py）
//	└── update.go     # UpdateKey/UpdateMode/UpdateEffect/UpdateValue/ApplyResult/normalize
//
// 对应 Python 代码：openjiuwen/agent_evolving/protocols.py · openjiuwen/agent_evolving/types.py
package schema
