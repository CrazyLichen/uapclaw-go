// Package tracker 决定「这个成员还有什么团队状态没被告知」，并记住。
//
// 团队状态（成员自己的身份、团队元数据、同伴名册、协作政策）过去走每轮提示词
// 附件：每次模型调用重新收集、重新渲染、重新追加。附件挂在窗口尾部且不写进
// 历史，因此永远无法从 KV 缓存命中，却是每次调用都要重新编码的固定开销。
//
// 本包驱动替代方案：状态在**出现时**写进对话历史，且只写新增部分。
//
//	TeamContextTracker ── 四通道增量宣告（identity / team_info / roster / policy）
//	taskNoticeTracker  ── 任务通知共享基线（claim / release / persist）
//	TaskOutcomeTracker ── F_119 评审通过（调度模式）
//	TaskRevisionTracker── F_129 任务被编辑（自主模式）
//
// 投递不在这里：进程内成员把文本折进上下文（rails.TeamPolicyRail 调用本包），
// 外部 CLI 成员走各自的发送通道（⤵️ 25.9）。
//
// 包归属偏差 D-9：上游是 agent_teams/team_context.py 单文件；Go 若放在父包，
// rails 要 import 它就会成环（agent_teams → rails → tracker），故独立为子包。
//
// 文件目录：
//
//	tracker/
//	└── team_context.go  # TeamContextTracker + 任务通知三兄弟
//
// 对应 Python 代码：openjiuwen/agent_teams/team_context.py (1007 行)
package tracker
