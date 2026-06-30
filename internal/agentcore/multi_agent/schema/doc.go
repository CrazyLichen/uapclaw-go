// Package schema 提供多 Agent 团队的类型定义，包括 TeamCard、EventDrivenTeamCard
// 和 TeamCardInterface 接口。
//
// 本包是 multi_agent 的子包，对应 Python 的 openjiuwen/core/multi_agent/schema/ 目录。
// TeamCard 定义团队的不可变元数据（身份、成员列表、主题、版本、标签）。
// EventDrivenTeamCard 扩展 TeamCard，增加订阅映射用于事件驱动消息路由。
// TeamCardInterface 是完整 getter 接口，TeamCard 和 EventDrivenTeamCard 均实现此接口，
// 被 BaseTeam.Card() 返回。
//
// 依赖约束：本包只依赖 common/schema/（BaseCard）和 single_agent/schema/（AgentCard），
// 不引用 multi_agent 包层的其他文件，避免循环依赖。
//
// 文件目录：
//
//	schema/
//	├── doc.go           # 包文档
//	└── team_card.go     # TeamCardInterface + TeamCard + EventDrivenTeamCard
//	                     # + 构造函数 + TeamCardOption/EventDrivenTeamCardOption + With* + String
//
// 对应 Python 代码：openjiuwen/core/multi_agent/schema/
package schema
