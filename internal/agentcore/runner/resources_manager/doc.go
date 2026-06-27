// Package resources_manager 提供 Agent/Tool/Workflow/Model/Prompt/SysOperation 全局注册表，
// 是 Runner 单例的核心依赖。
//
// 本包实现 ResourceMgr 门面类，统一管理所有资源的注册、获取、移除和标签分类。
// 内部通过 ResourceRegistry 聚合 7 个子管理器，TagMgr 维护资源与标签的双向索引。
//
// 设计模式：
//
//	ResourceMgr 作为门面，所有资源的 add/get/remove 操作通过内部核心方法统一流转。
//	子管理器分两类：基于 AbstractManager 的 Provider 模式（Agent/Workflow/Model），
//	和直接存储模式（Prompt/Tool/SysOperation）。
//	Provider 模式支持延迟加载，注册时传入工厂函数而非实例，获取时调用工厂创建实例。
//
// 文件目录：
//
//	resources_manager/
//	├── doc.go                    # 包文档
//	├── base.go                   # Provider 类型别名、Tag 常量、枚举
//	├── thread_safe_dict.go       # 泛型线程安全字典
//	├── abstract_manager.go       # 泛型抽象管理器
//	├── tag_manager.go            # 标签管理器，双向索引
//	├── resource_registry.go      # 聚合 7 个子管理器
//	├── agent_manager.go          # Agent 管理器（本地+分布式⤵️）
//	├── agent_team_manager.go     # AgentTeam 管理器（⤵️预留）
//	├── model_manager.go          # Model 管理器+trace 装饰
//	├── prompt_manager.go         # Prompt 管理器，直接存储
//	├── tool_manager.go           # Tool 管理器+MCP Server 全套
//	├── workflow_manager.go       # Workflow 管理器+trace 装饰
//	├── sys_operation_manager.go  # SysOperation 管理器（⤵️预留）
//	└── resource_manager.go       # ResourceMgr 门面类
//
// 对应 Python 代码：openjiuwen/core/runner/resources_manager/
package resources_manager
