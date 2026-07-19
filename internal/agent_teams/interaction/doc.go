// Package interaction 提供团队 Agent 的外部交互层。
//
// Interaction 层是 TeamAgent 对外暴露 interact() API 的核心实现——
// 团队 Agent 的「收件箱系统」。当外部用户有多种意图
// （直连 Leader、@某成员、广播、驱动 Human Agent Avatar）时，
// Interaction 层负责把自由文本或结构化输入正确路由到团队内部。
//
// 三种交互视角：
//   - God View — 直达 Leader DeepAgent（对齐历史 invoke 通道）
//   - Operator View — 以外部用户身份，@成员或广播
//   - Human-Agent View — 以注册的 human-agent 成员身份发言
//
// 文件目录：
//
//	interaction/
//	├── doc.go                # 包文档
//	├── payload.go            # 载荷类型（GodViewMessage/OperatorMessage/HumanAgentMessage/InteractPayload/DeliverResult/HumanAgentInboundEvent）
//	├── router.go             # 输入解析器（ParseInteractStr/ParseMention/IsReservedName/ResolveTargets/DeliverDirect）
//	├── user_inbox.go         # 用户侧收件箱（UserInbox）
//	└── human_agent_inbox.go  # Human-Agent 收件箱（HumanAgentInbox + 错误类型）
//
// 对应 Python 代码：openjiuwen/agent_teams/interaction/
package interaction
