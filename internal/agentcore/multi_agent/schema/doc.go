// Package schema 提供多 Agent 团队的类型定义，包括 TeamCard 等身份卡片。
//
// 本包是 multi_agent 的子包，对应 Python 的 openjiuwen/core/multi_agent/schema/ 目录。
// TeamCard 定义团队的不可变元数据（身份、成员列表、主题、版本、标签），
// 被 BaseTeam.Card() 返回，也被 EventDrivenTeamCard(8.29) 继承。
//
// 依赖约束：本包只依赖 common/schema/（BaseCard）和 single_agent/schema/（AgentCard），
// 不引用 multi_agent 包层的其他文件，避免循环依赖。
//
// 文件目录：
//
//	schema/
//	├── doc.go           # 包文档
//	└── team_card.go     # TeamCard 结构体 + 构造函数 + TeamCardOption + String
//
// 对应 Python 代码：openjiuwen/core/multi_agent/schema/
package schema
