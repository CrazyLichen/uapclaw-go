// Package single_agent 提供 Agent 核心能力管理，包括 AbilityManager 注册与调度。
//
// AbilityManager 是 Agent 的能力注册与调度中心，管理四类 Ability
// （Tool / Workflow / Agent / McpServer）的完整生命周期：
// 注册管理、LLM 工具描述生成、并行执行、JSON 参数修复、路由分发。
//
// Workflow/Agent 接口定义从本包抽出至 interfaces 子包，
// 供 tracer 等外部包引用，避免 tracer → single_agent → context_engine 循环依赖。
//
// 文件目录：
//
//	single_agent/
//	├── doc.go                 # 包文档
//	├── ability/
//	│   ├── doc.go             # 子包文档
//	│   ├── ability_manager.go # AbilityManager 核心结构 + 注册/查询/执行
//	│   ├── ability_types.go   # Ability 联合类型 + AddAbilityResult + AbilityExecutionError
//	│   └── json_repair.go     # RepairToolArgumentsJSON + ParseToolArguments
//	├── agents/
//	│   ├── doc.go             # 子包文档
//	│   ├── react_agent.go     # ReActAgent — ReAct 循环 Agent（Think → Act → Observe）
//	│   ├── react_helpers.go   # ReAct 辅助函数
//	│   ├── react_invoke.go    # ReAct Invoke 执行逻辑
//	│   ├── react_model_call.go # ReAct 模型调用逻辑
//	│   └── react_prompt.go    # ReAct 提示词构建
//	├── config/
//	│   ├── doc.go             # 子包文档
//	│   └── agent_config.go    # ReActAgentConfig 结构体 + Option + AgentConfig 接口实现 + Validate
//	├── interfaces/
//	│   ├── doc.go             # 子包文档
//	│   ├── abilitymgr.go      # AbilityManager 接口
//	│   ├── agent.go           # Agent 接口 + AgentOption 类型
//	│   ├── callback.go        # Callback 接口
//	│   └── workflow.go        # Workflow 接口 + WorkflowOption 类型
//	├── interrupt/
//	│   ├── doc.go             # 子包文档
//	│   ├── exception.go       # 中断异常类型
//	│   ├── handler.go         # 中断处理器
//	│   ├── payload.go         # 中断请求/响应载体
//	│   ├── response.go        # 中断响应
//	│   └── state.go           # 中断状态
//	├── prompts/
//	│   ├── doc.go             # 子包文档
//	│   └── builder.go         # 提示词构建器
//	├── rail/
//	│   ├── doc.go             # 子包文档
//	│   └── executor.go        # RailExecutor 执行器
//	├── schema/
//	│   ├── doc.go             # 子包文档
//	│   ├── agent_card.go      # AgentCard 结构体 + 构造函数 + Ability 接口实现
//	│   ├── agent_result.go    # Part/Artifact/AgentResult 结果模型
//	│   ├── execute_result.go  # ExecuteResult 执行结果类型
//	│   ├── exception.go       # ToolInterruptException（实现 error 接口）
//	│   ├── response.go        # InterruptRequest + ToolCallInterruptRequest
//	│   └── state.go           # 常量 + 中断状态类型
//	└── skills/
//	    ├── doc.go             # 子包文档
//	    ├── skill.go           # Skill 模型 — 技能元数据结构体
//	    ├── skill_manager.go   # SkillManager — 技能注册/注销/查询 + YAML front-matter 加载
//	    ├── skill_util.go      # SkillUtil — 高层门面，组合 SkillManager + RemoteSkillUtil
//	    └── remote_skill_util.go # GitHubTree/GitHubError/RemoteSkillUtil — GitHub 远程技能下载
//
// 对应 Python 代码：openjiuwen/core/single_agent/ability_manager.py
package single_agent
