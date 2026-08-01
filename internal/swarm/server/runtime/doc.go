// Package runtime 提供 AgentServer 运行时管理组件。
//
// 包含 UapClaw（Agent 门面）、AgentManager（Agent 实例管理器）、
// AgentManager（Agent 实例管理器）、TenantAgentPool（AgentManager 单例管理器）、
// AgentConfigService（Agent 配置 CRUD）等运行时组件，
// 负责 Agent 实例的并发执行控制、任务调度、请求路由和配置管理。
//
// AgentManager（10.3.12）使用两层嵌套 map（channel_key → cache_key → agentEntry）
// 管理 UapClaw 实例，支持按通道/模式自动创建、GetAgentNoWait 三级模糊查找、
// 配置热重载、Agent 重建和取消在途任务。对齐 Python AgentManager。
//
// TenantAgentPool（10.3.14）是 AgentManager 的单例包装器，提供 GetInstance/ResetInstance
// 和对 ProcessMessage/ProcessMessageStream/Cleanup 的简单透传。对齐 Python TenantAgentPool。
//
// 文件目录：
//
//	runtime/
//	├── doc.go                # 包文档
//	├── uapclaw.go            # UapClaw Agent 门面（层级 0+1 已实现，层级 2-4 ⤵️）
//	├── build_user_prompt.go  # BuildUserPrompt 用户 prompt 包装
//	├── build_inputs.go       # BuildInputs adapter 输入构建
//	├── agent_manager.go      # AgentManager Agent 实例管理器（10.3.12 已实现）
//	├── agent_config.go       # AgentConfigService 配置 CRUD（10.3.13）
//	├── agent_config_yaml.go  # config.yaml react.subagents 联动
//	├── agent_config_llm.go   # LLM 生成 whenToUse + systemPrompt
//	├── tenant_pool.go        # TenantAgentPool 多租户 Agent 池化（10.3.14）
//	└── skill/                # 技能管理子包
//
// 对应 Python 代码：jiuwenswarm/server/runtime/
package runtime
