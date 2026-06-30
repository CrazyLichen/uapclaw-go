// Package schema 提供多 Agent 团队的类型定义，包括 TeamCard、EventDrivenTeamCard、
// TeamCardInterface 接口，以及 BaseTeam、AgentTeamProvider、TeamAgentProvider、
// TeamOption、TeamOptions、TeamConfig 等核心契约类型，以及 Communicable 可通信接口。
//
// 本包是 multi_agent 的子包，对应 Python 的 openjiuwen/core/multi_agent/schema/ 目录。
// TeamCard 定义团队的不可变元数据（身份、成员列表、主题、版本、标签）。
// EventDrivenTeamCard 扩展 TeamCard，增加订阅映射用于事件驱动消息路由。
// TeamCardInterface 嵌入 schema.CardInterface 通用只读接口，TeamCard 和 EventDrivenTeamCard 均实现此接口，
// 被 BaseTeam.Card() 返回。
// BaseTeam 是多 Agent 团队核心行为契约接口，从 multi_agent 主包迁移至此以解决循环依赖。
// AgentTeamProvider 和 TeamAgentProvider 是团队/Agent 资源提供者函数类型。
// TeamOption/TeamOptions 是团队调用选项，TeamConfig 是团队运行时配置。
// Communicable 是 Agent 间 P2P/Pub-Sub 通信接口，通过类型断言获取。
//
// 依赖约束：本包只依赖 common/schema/（BaseCard、CardInterface）、single_agent/schema/（AgentCard）、
// single_agent/interfaces/（BaseAgent）、session/stream/（StreamMode、Schema），
// 不引用 multi_agent 包层的其他文件，避免循环依赖。
//
// 文件目录：
//
//	schema/
//	├── doc.go              # 包文档
//	├── communicable.go     # Communicable 可通信接口（P2P/Pub-Sub）
//	├── team_card.go        # TeamCardInterface + TeamCard + EventDrivenTeamCard
//	                       # + 构造函数 + TeamCardOption/EventDrivenTeamCardOption + With* + String
//	└── team_interface.go   # BaseTeam + AgentTeamProvider + TeamAgentProvider
//	                       # + TeamOption/TeamOptions/TeamConfig + 构造函数 + With* + 链式配置方法
//
// 对应 Python 代码：openjiuwen/core/multi_agent/schema/
package schema
