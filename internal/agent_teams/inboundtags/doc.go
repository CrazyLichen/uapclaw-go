// Package inboundtags 渲染团队成员收到的入站消息 XML 标签。
//
// 团队成员的 harness 输入混了两类东西：另一个成员或用户发来的**原始消息**，
// 以及运行时包裹在它周围的**框架元数据与指令**（message id、时间、回复提示、
// 事件通知）。早期的 i18n 模板把两者拼成一整条扁平字符串，模型分不清「谁说的」
// 与「框架加的」。本包改用语义化 XML，给模型一条清晰边界：
//
//   - <team-inbound> 原样包裹原始消息，sender / message_id / type / time 作为属性
//   - <team-clock> 每次准入输入附带一个当前时间锚点
//   - <team-event> 包裹框架事件，kind 属性标明类型
//   - <team-context> 包裹团队既成事实（自己的身份、团队元数据）
//   - <team-policy> 包裹外部 CLI 成员 system prompt 里的团队准则
//   - <team-note> 携带框架附加的提示或约束，嵌套在所修饰块的内部作为最后一个子元素
//
// 这些都是纯结构函数：调用方传入动态数据与已本地化的文本片段（由 schema.T 解析），
// 拿回一段 XML 字符串。文案不进这一层，双语契约留在 i18n（唯一真相），
// 标签结构留在这里。type / kind / for 的属性值是稳定的英文契约 token，
// **永不本地化**，因为 inbound-tags 系统提示词段按名字引用它们。
//
// 本包是**零依赖叶子包**：不 import agent_teams 的任何子包，因此父包
// agent_teams（转发出这些符号）与 agent_teams/tracker（直接使用）都能安全依赖它。
//
// 文件目录：
//
//	inboundtags/
//	├── doc.go          # 包文档
//	└── inbound_tags.go # 六类标签渲染 + 被取代快照剪枝
//
// 对应 Python 代码：openjiuwen/agent_teams/inbound_render.py (417 行)
package inboundtags
