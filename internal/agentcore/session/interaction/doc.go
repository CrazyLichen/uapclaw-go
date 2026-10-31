// Package interaction 提供会话交互管理，支持工作流和 Agent 的用户输入中断-恢复机制。
//
// 本包实现三种交互模式：
//   - WorkflowInteraction：工作流节点交互，通过 pregel.GraphInterrupt 暂停图执行
//   - SimpleAgentInteraction：简单 Agent 交互，无输入队列管理
//   - AgentInteraction：完整 Agent 交互，含输入队列 + 检查点 + 流输出
//
// 中断-恢复机制：
//   - WorkflowInteraction 通过 panic(pregel.GraphInterrupt) 暂停工作流图执行（本体定义在 graph/pregel）
//   - Agent 侧通过 panic(AgentInterrupt) 暂停 Agent 执行
//   - 恢复时用户输入通过 InteractiveInput 注入到 session state，
//     交互实例从输入队列自动消费，无需再次中断
//
// 依赖接口（暂放本包，后续迁移）：
//   - InteractionOutputWriter / InteractionOutputWriterProvider → 5.10 时迁移到 session/stream 包
//   - ExecutableIDProvider → NodeSession 天然满足，AgentSession 不满足（Python 同理）
//
// 文件目录：
//
//	interaction/
//	├── doc.go                # 包文档
//	├── base.go               # ExecutableIDProvider 类型别名（→ interfaces 包）+ BaseInteraction + AgentInterrupt + 常量 + 依赖接口
//	├── interaction.go        # WorkflowInteraction + SimpleAgentInteraction + AgentInteraction + InteractionOutput（中断值装进 stream.OutputSchema）
//	└── interactive_input.go  # InteractiveInput 用户输入容器
//
// 对应 Python 代码：openjiuwen/core/session/interaction/
package interaction
