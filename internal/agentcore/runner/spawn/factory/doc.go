// Package factory 提供 Agent 创建工厂，根据 agent_type 创建对应的 Agent 实例。
//
// 对齐 Python: importlib.import_module(agent_module) → getattr(module, agent_class) → cls(**init_kwargs)
// Go 没有 importlib，用 switch 按类型名直接调用对应构造函数。
// 新增 Agent 类型时只需在 CreateByType 中加一个 case。
//
// 文件目录：
//
//	factory/
//	├── doc.go                      # 包文档
//	└── agent_creator_factory.go    # DefaultAgentCreator 实现
//
// 对应 Python 代码：无独立对应（Python 用 importlib 内建能力）
package factory
