// Package adapter 提供 Agent 适配器接口与工厂。
//
// 定义 AgentAdapter 接口——Agent SDK 后端的最小能力集，
// 以及 CreateAdapter 工厂函数按 SDK+Mode 创建适配器实例。
// 额外定义 3 个可选接口（ContextCompressor/DreamingController/GatewayDisconnectHandler），
// DeepAdapter 和 CodeAdapter 均实现，JiuWenClaw 门面通过类型断言调用。
//
// 三种模式适配器：
//   - DeepAdapter：Deep SDK 适配器，agent/plan/fast/team 模式均使用
//   - CodeAdapter：Code 模式适配器，组合委托 DeepAdapter，仅覆盖 CreateInstance
//   - Python 中无独立 AgentAdapter，agent 模式直接使用 DeepAdapter
//
// 文件目录：
//
//	adapter/
//	├── doc.go                    # 包文档
//	├── interface.go              # AgentAdapter 接口 + 3 个可选接口
//	├── factory.go                # CreateAdapter 工厂 + ResolveSDKChoice
//	├── deep_adapter.go           # DeepAdapter 核心结构体 + 8 接口方法 + 模型构建 + CWD 种子
//	├── deep_adapter_rails.go     # ~20 个 rail builder + buildAgentRails + updateRailsForMode
//	├── deep_adapter_mcp.go       # MCP 管理 6 方法
//	├── deep_adapter_a2x.go       # A2X 客户端 5 方法 + Cron 上下文 2 方法（⤵️）
//	├── deep_adapter_tools.go     # Tool 同步 5 方法 + 多模态配置 + ToolCards 构建 + 工具名常量
//	├── deep_adapter_slash.go     # Slash 命令 5 个 + governance approval（⤵️）
//	├── deep_adapter_evolution.go # EvolutionWatcher + ContextCompressor + Recap（⤵️）
//	├── deep_adapter_team.go      # TeamSkillApproval + team 分流（⤵️）
//	├── deep_adapter_stream.go    # parseStreamChunk 15+ 种 chunk + usage 累加器
//	├── deep_adapter_dreaming.go  # DreamingController 接口实现（⤵️）
//	├── deep_adapter_config.go    # RuntimeConfig + Profile/Prompt/Subagent + createSysOperation
//	├── evolution/                # Evolution 事件分类/状态提取/推送辅助（10.3.9）
//	│   ├── doc.go                # 包文档
//	│   ├── helpers.go            # 3 结构体 + 常量/变量 + ~22 导出函数
//	│   └── helpers_test.go       # 单元测试
//	└── code_adapter.go           # CodeAdapter Code 模式适配器 + 可选接口委托
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/interface_deep.py
package adapter
