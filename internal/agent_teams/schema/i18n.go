package schema

import (
	"fmt"
	"strings"
	"sync"
)

// ──────────────────────────── 枚举 ────────────────────────────

// Language 支持的语言代码。
// Python: Language = Literal["cn", "en"]
// 从 agent_teams 根包搬入，打断 schema→agent_teams 循环依赖。
type Language string

const (
	// LanguageCN 中文
	LanguageCN Language = "cn"
	// LanguageEN 英文
	LanguageEN Language = "en"
)

// ──────────────────────────── 常量 ────────────────────────────

// defaultLanguage 默认语言
const defaultLanguage Language = LanguageCN

// ──────────────────────────── 全局变量 ────────────────────────────

// currentLanguage 当前全局语言，受 mu 保护
var (
	currentLanguage Language = defaultLanguage
	mu              sync.RWMutex
)

// STRINGS 多语言字符串映射表
var STRINGS = map[Language]map[string]string{
	LanguageCN: {
		// Python: schema/blueprint.py
		"blueprint.default_persona": "天才项目管理专家",
		// Python: tools/team.py
		"team.shutdown_request_content": "当前任务已全部完成，请结束流程",
		"team.cancel_request_content":   "当前任务有变动，请停止执行当前任务，重新尝试认领合适任务",
		// agent/dispatcher.py — 成员生命周期事件
		"dispatcher.member_online":            "[成员事件] 成员 {target_id} 已上线",
		"dispatcher.member_restarted":         "[成员事件] 成员 {target_id} 已重启 (第{restart_count}次)",
		"dispatcher.member_status_changed":    "[成员事件] 成员 {target_id} 状态变更: {old_status} → {new_status}",
		"dispatcher.member_execution_changed": "[成员事件] 成员 {target_id} 执行状态变更: {old_status} → {new_status}",
		"dispatcher.member_shutdown":          "[成员事件] 成员 {target_id} 已关闭",
		"dispatcher.member_canceled":          "[成员事件] 成员 {target_id} 已取消",
		// agent/dispatcher.py — 过期认领催促
		"dispatcher.stale_claim_header": "检测到你已认领且超过 10 分钟未完成的任务（共 {count} 个），请继续推进：",
		"dispatcher.stale_claim_self":   "[催促] 你已认领的任务 [{task_id}] {title} 已超过 10 mins 仍未完成，请继续推进：{content}",
		// agent/dispatcher.py — 任务指派通知
		"dispatcher.task_assigned_to_self": "[任务指派] 任务 [{task_id}] 已指派给你，请通过 view_task 工具查看任务详情并执行。",
		// agent/dispatcher.py — 计划审批
		"dispatcher.task_plan_approved_to_self": "[计划已批准] 任务 [{task_id}] 的执行计划已通过。请开始执行，完成后用 claim_task(status='completed') 标记完成。{feedback}",
		"dispatcher.task_plan_rejected_to_self": "[计划需修改] 任务 [{task_id}] 的执行计划未通过。请根据反馈修改并重新调用 submit_plan。反馈：{feedback}",
		// agent/dispatcher.py — 消息格式化
		"dispatcher.msg_type_broadcast": "广播消息",
		"dispatcher.msg_type_direct":    "单播消息",
		"dispatcher.msg_received":       "[收到{msg_type}] message_id={message_id}, 来自: {sender}\n内容: {content}\n提示: 如果对方在提问或等待回复，请务必通过 send_message 工具回复 {sender}",
		// ── 25.0b：时间渲染（timefmt.go）──
		"time.just_now":    "刚刚",
		"time.seconds_ago": "{value} 秒前",
		"time.minutes_ago": "{value} 分钟前",
		"time.hours_ago":   "{value} 小时前",
		"time.days_ago":    "{value} 天前",
		"time.unknown":     "时间未知",
		// ── 25.0b：入站 XML 标签的 note 正文（inbound_render.go / external/format.go）──
		// 与上面 dispatcher.msg_received 那套扁平模板分开：原话与框架提示分落
		// <team-inbound> 与 <team-note> 两个标签。扁平模板留给 external 的老路径。
		"dispatcher.reply_hint": "如果对方在提问或等待回复，请务必通过 send_message 工具回复 {sender}。",
		// user 伪成员没有 agent 进程，纯文本回复会被静默丢弃，所以这里用无条件措辞：
		// 通用提示里的「如果对方在提问」会让模型说服自己不回复，而丢掉的 user 回复是不可见的。
		"dispatcher.reply_hint_user": "这条消息来自 user——委托本团队工作的**团队外部真人**，不是团队成员，不在成员名册里。" +
			"你必须调用 send_message(to=\"user\") 把答复发回用户；" +
			"你写在回复正文里的任何文字都不会送达用户，不调用工具就等于没有回复。",
		// 「严格禁止」这个措辞是承重的：没有它，头像 LLM 会漂移成自主回复。
		"hitt.silence_note": "**这是给控制者看的通知，不是要你执行的指令**，运行时已把它原样转给控制者。\n" +
			"**严格禁止任何自主行为**：禁止主动回复发送方 / 指派方（包括调用 send_message）、" +
			"禁止自主调用 member_complete_task / claim_task / 文件 / shell 等任何工具去回应或推进、" +
			"禁止用纯文本输出表达意图或承诺。\n" +
			"**保持静默**，只有控制者在 Inbox 里下达明确指令后才能行动。",
		// 「不要」这个措辞是承重的：否则模型看到名册变动就去寒暄。
		"team_context.roster_announcement_note": "以上只是名册公告，不是给你的指令。" +
			"**不要**因为看到成员变动就去和相关成员打招呼、确认或寒暄；" +
			"只有当你手上的任务确实需要对方配合时才发消息。",
		// team_context.py TaskOutcomeTracker (F_119)：通过的评审不唤醒作者，
		// 改为搭其下一次输入投递。
		"team_context.task_verified_intro": "你此前提交的以下任务已通过验收并完成，无需再做任何事，也不要为此联系 leader：",
		// team_context.py TaskRevisionTracker (F_129)：成员离线期间的编辑搭其下一次输入。
		"team_context.task_revised_intro": "你名下的以下任务在你上次查看后被修改过（任务仍归你，无需重新认领）。" +
			"请先用 view_task(action='get', task_id=...) 查看最新要求，按需调整后继续；提交前必须看过最新版本：",
		// agent/dispatcher.py — 空闲成员催促
		"dispatcher.all_done_persistent":    "所有任务已完成。请汇总本轮工作成果。团队继续保持运行，等待新的任务指令。",
		"dispatcher.all_done_temporary":     "所有任务已完成。请汇总团队工作成果，然后依次调用 shutdown_member 关闭所有成员，等待所有成员状态转为 shutdown 后，调用 clean_team 解散团队。",
		"dispatcher.leader_task_board":      "当前任务看板如下，请审查：\n- 是否需要调整任务（增删、修改、调整依赖）\n- 就绪任务是否需要指派给 teammate\n- 整体进度是否符合预期",
		"dispatcher.teammate_task_list":     "当前任务列表如下：\n- 请认领适合你领域的待领取任务\n- 了解相关任务的执行者，必要时与他们协调配合",
		"dispatcher.task_unassigned_marker": " (待领取)",
		// agent/dispatcher.py — 过期 pending 催促
		"dispatcher.stale_pending_header": "[催促建议] 以下任务已长时间处于 pending 状态未被认领，请评估每个任务最适合哪位成员，并通过 send_message 工具点名对方让其使用 claim_task 认领：",
		// HITT — 保留 human_agent 成员
		"hitt.human_agent_display_name":    "人类成员",
		"hitt.human_agent_default_persona": "外部用户在团队里的代理（avatar）。所有动作都由对应的真人通过 Inbox 驱动；可使用文件、任务、工作空间等工具替用户完成事务，但不主动发声、不自主认领任务。",
		"hitt.human_agent_spawned":         "[成员事件] 人类成员 human_agent 已加入团队",
		"hitt.task_assigned_to_self_human": "[任务指派给控制者] 你被指派了新任务 [{task_id}] {title}。\n**这是给控制者看的通知，不是给你的工作指令**；运行时已经把通知原样展示给控制者。\n**严格禁止任何自主行为**：禁止主动回复发起指派的成员、禁止自主调用 send_message / member_complete_task / claim_task / 文件 / shell 等任何工具去回应或推进任务、禁止用纯文本输出表达意图或承诺。\n**保持静默**，等控制者在 Inbox 里下达明确指令后再行动。",
		"hitt.msg_received_for_human":      "[转发给控制者的{msg_type}] message_id={message_id}, 来自: {sender}\n内容: {content}\n**这条消息已经原样转给控制者，不是要你回应的指令**。\n**严格禁止任何自主行为**：禁止主动回复发送方（包括调用 send_message）、禁止自主调用任何其它工具去回应或采取行动、禁止用纯文本输出表达意图或承诺。\n**保持静默**，等控制者在 Inbox 里明确指示你转告或回复时再调 send_message。",
		// ── workspace_meta 工作空间元数据 ──
		"workspace_meta.action": "操作类型：lock（获取文件锁）、unlock（释放文件锁）、locks（列出所有活跃锁）、history（查看文件版本历史）",
		"workspace_meta.path":   "目标文件的相对路径（lock/unlock/history 时必填）",
		// ── 25.1d：异步后台任务反馈（async_tools.go）──
		"async_tool.launched":       "[后台任务] {tool} 已启动（task_id={task_id}）。完成后结果会自动回灌给你，无需轮询；你可以继续处理其他输入。",
		"async_tool.completed":      "[后台任务完成] 工具={tool}\n{result}",
		"async_tool.failed":         "[后台任务失败] 工具={tool}，错误={error}",
		"async_tool.spilled_notice": "[完整输出过大，已写入磁盘 {path}。调用 async_task_output(task_id='{task_id}') 取回全文。]",
		// ── 25.1g：reliability/ 子系统（i18n.py reliability.* 逐字复制）──
		"reliability.steer_self_correct":         "⚙️[可靠性] 检测到 {kind}：{summary}。请停止重复无效操作，改换策略或换用其他工具。",
		"reliability.report_leader":              "[可靠性告警] {summary}。请评估该成员状态并决定处理方式（发消息提醒 / 取消任务 / 停止成员 / 新建成员或任务）。",
		"reliability.escalate_user":              "[可靠性·严重] {summary}。已超出自动处理范围，建议立即上报控制者 / 用户决策。",
		"reliability.model_unavailable":          "模型 {model} 不可用：{reason}。{no_substitute}，该成员无法自动切换。请立即调用 ask_user 告知用户并询问处理方式（改用其他模型新建成员接手 / 等待恢复后重试 / 修复配置后重试 / 终止任务）；用户答复前，不要再给该成员派发任务或催促",
		"reliability.model_route_switched":       "模型组中{model}{reason}，已自动切换到组内其他可用模型",
		"reliability.model_route_interrupted":    "模型组中{model}{reason}，本轮已中断，将自动重跑并由组内其他可用模型重新生成",
		"reliability.model_route_other":          "组内其他模型",
		"reliability.model_not_in_group":         "指定的模型不在当前模型组中",
		"reliability.model_no_substitute_pinned": "模型组中没有可以替代它的同名模型",
		"reliability.model_no_substitute_group":  "模型组中的其他模型也都不可用",
		"reliability.model_label":                "{name}（{where}）",
		"reliability.model_label_ranked":         "第 {position} 顺位 {name}（{where}）",
		"reliability.model_failure_separator":    "；",
		"reliability.model_failure":              "{model}{reason}",
		"reliability.reason_api_key":             "API Key 校验未通过（HTTP {status}）",
		"reliability.reason_not_found":           "在配置的地址上找不到该模型（HTTP 404）",
		"reliability.reason_rate_limited":        "被限流（HTTP 429）",
		"reliability.reason_server_error":        "服务端出错（HTTP {status}）",
		"reliability.reason_rejected":            "拒绝了本次请求（HTTP {status}）",
		"reliability.reason_rejected_detail":     "拒绝了本次请求（HTTP {status}）：{detail}",
		"reliability.reason_timeout":             "响应超时",
		"reliability.reason_unreachable":         "无法连接",
		"reliability.reason_unavailable":         "暂时不可用",
		"reliability.reason_failed":              "调用失败",
		"reliability.reason_interrupted":         "回答到一半中断（{reason}）",
		"reliability.route_notice":               "[模型路由通知] {summary}。已自动处理，不需要调整成员或任务。{scope}如需告知用户，只说明哪个模型（模型组顺位、提供商、地址）出了什么问题、已改用哪个模型、用户需要做什么（例如检查该模型的 API Key）；不要推测对成员的影响。",
		"reliability.route_notice_scope_group":   "这是模型组的状态，不是某个成员的：没有指定模型的成员（包括你）都已改用新模型，不要说成某个成员原来在用这个模型。",
		"reliability.route_notice_scope_pinned":  "这只影响指定使用 {model} 的成员，它们已改用新模型；其他成员不受这条通知影响，不要说成整个团队都已切换。",
		"reliability.external_runtime_retrying":  "[三方运行时] 成员 {member_name}（{agent_kind}，模型 {model}）正在自动重试：{category}。进度 {attempt}/{max_attempts}，原因：{reason_message}。诊断字段：http_status={http_status}，sdk_error_code={sdk_error_code}。本轮不结束，成员状态不变，等待 SDK 后续结果。{summary}",
	},
	LanguageEN: {
		// Python: schema/blueprint.py
		"blueprint.default_persona": "Genius project management expert",
		// Python: tools/team.py
		"team.shutdown_request_content": "All tasks are complete. Please wrap up and exit.",
		"team.cancel_request_content":   "The current task has changed. Stop executing it and try claiming a suitable task again.",
		// agent/dispatcher.py — 成员生命周期事件
		"dispatcher.member_online":            "[Member Event] Member {target_id} is online",
		"dispatcher.member_restarted":         "[Member Event] Member {target_id} restarted (attempt {restart_count})",
		"dispatcher.member_status_changed":    "[Member Event] Member {target_id} status changed: {old_status} → {new_status}",
		"dispatcher.member_execution_changed": "[Member Event] Member {target_id} execution status changed: {old_status} → {new_status}",
		"dispatcher.member_shutdown":          "[Member Event] Member {target_id} has shut down",
		"dispatcher.member_canceled":          "[Member Event] Member {target_id} has been canceled",
		// agent/dispatcher.py — 过期认领催促
		"dispatcher.stale_claim_header": "Detected {count} task(s) you claimed that have been open for over 10 minutes. Please push forward:",
		"dispatcher.stale_claim_self":   "[Nudge] Your claimed task [{task_id}] {title} has been open for over 10 mins. Please continue: {content}",
		// agent/dispatcher.py — 任务指派通知
		"dispatcher.task_assigned_to_self": "[Task Assigned] Task [{task_id}] has been assigned to you. Use view_task to inspect the details and start working on it.",
		// agent/dispatcher.py — 计划审批
		"dispatcher.task_plan_approved_to_self": "[Plan Approved] Your execution plan for task [{task_id}] was approved. Start execution and call claim_task(status='completed') when done. {feedback}",
		"dispatcher.task_plan_rejected_to_self": "[Plan Rejected] Your execution plan for task [{task_id}] needs revision. Update it and call submit_plan again. Feedback: {feedback}",
		// agent/dispatcher.py — 消息格式化
		"dispatcher.msg_type_broadcast": "broadcast",
		"dispatcher.msg_type_direct":    "direct message",
		"dispatcher.msg_received":       "[Received {msg_type}] message_id={message_id}, from: {sender}\ncontent: {content}\ntip: If the sender is asking or waiting for a reply, make sure to reply to {sender} via send_message",
		// ── 25.0b: time rendering 时间渲染文案（timefmt.go） ──
		"time.just_now":    "just now",
		"time.seconds_ago": "{value}s ago",
		"time.minutes_ago": "{value}m ago",
		"time.hours_ago":   "{value}h ago",
		"time.days_ago":    "{value}d ago",
		"time.unknown":     "unknown time",
		// ── 25.0b: note bodies for inbound XML tags 入站标签提示正文 ──
		"dispatcher.reply_hint": "If the sender is asking or waiting for a reply, be sure to reply to {sender} via send_message.",
		"dispatcher.reply_hint_user": "This message is from user — the **human outside the team** who commissioned this team's work. " +
			"They are not a team member and do not appear in the roster. " +
			"You MUST call send_message(to=\"user\") to deliver your reply; " +
			"any text you write in your reply body never reaches the user, " +
			"so not calling the tool means you did not reply at all.",
		"hitt.silence_note": "**This is a notification for your controller, NOT an instruction " +
			"for you to act on**; the runtime has already surfaced it to the " +
			"controller as-is.\n" +
			"**Autonomous behavior is strictly forbidden**: do not reply to " +
			"the sender / assigner (including via send_message), do not " +
			"autonomously call member_complete_task / claim_task / file tools " +
			"/ shell tools or any other tool to respond or push work forward, " +
			"and do not emit plain-text intent or promises.\n" +
			"**Stay silent** and act only after the controller issues an " +
			"explicit instruction via the Inbox.",
		"team_context.roster_announcement_note": "The above is a roster announcement, not an instruction for you. " +
			"Do **NOT** greet, check in with, or otherwise message a member just " +
			"because the roster changed; message someone only when the work you " +
			"are actually holding needs them.",
		// Python: team_context.py TaskOutcomeTracker (F_119)
		"team_context.task_verified_intro": "These tasks you submitted earlier have passed review and are completed. " +
			"Nothing more is needed — do not contact the leader about them:",
		// Python: team_context.py TaskRevisionTracker (F_129)
		"team_context.task_revised_intro": "These tasks of yours were edited after you last read them (they are still yours — no need to " +
			"re-claim). Read the latest requirements with view_task(action='get', task_id=...), adjust as " +
			"needed and continue; you must have read the latest revision before submitting:",
		// agent/dispatcher.py — 空闲成员催促
		"dispatcher.all_done_persistent":    "All tasks are complete. Please summarize this round's results. The team remains running and awaits new task instructions.",
		"dispatcher.all_done_temporary":     "All tasks are complete. Summarize the team's work, then call shutdown_member for each member in turn, wait until all members reach status shutdown, and finally call clean_team to disband the team.",
		"dispatcher.leader_task_board":      "Current task board — please review:\n- Whether any tasks need adjustment (add/remove/edit/dependencies)\n- Whether ready tasks should be assigned to a teammate\n- Whether the overall progress matches expectations",
		"dispatcher.teammate_task_list":     "Current task list:\n- Claim pending tasks that fit your domain\n- Know who is working on related tasks and coordinate when needed",
		"dispatcher.task_unassigned_marker": " (unassigned)",
		// agent/dispatcher.py — 过期 pending 催促
		"dispatcher.stale_pending_header": "[Nudge suggestion] The following tasks have been pending unclaimed for a long time. Decide which member fits each task best, then use send_message to call them out and ask them to claim via claim_task:",
		// HITT — 保留 human_agent 成员
		"hitt.human_agent_display_name":    "Human Member",
		"hitt.human_agent_default_persona": "An external user's avatar on the team. Every action is driven by the corresponding human via the Inbox; uses file, task, and workspace tools to act on the user's behalf, but does not speak up on its own and does not autonomously claim tasks.",
		"hitt.human_agent_spawned":         "[Member Event] Human member 'human_agent' joined the team",
		"hitt.task_assigned_to_self_human": "[Task Assigned For Controller] You have been assigned task [{task_id}] \"{title}\".\n**This is a notification for your controller, NOT a work instruction for you**; the runtime has already surfaced the notification to the controller as-is.\n**Autonomous behavior is strictly forbidden**: do not reply to the assigner, do not autonomously call send_message / member_complete_task / claim_task / file tools / shell tools or any other tool to act on the assignment, and do not emit plain-text intent or promises.\n**Stay silent** and act only after the controller issues an explicit instruction via the Inbox.",
		"hitt.msg_received_for_human":      "[For-Controller {msg_type}] message_id={message_id}, from: {sender}\ncontent: {content}\n**This message has already been surfaced to your controller as-is; it is NOT an instruction for you to act on**.\n**Autonomous behavior is strictly forbidden**: do not reply to the sender (including via send_message), do not autonomously call any other tool to respond or take action, and do not emit plain-text intent or promises.\n**Stay silent** and only call send_message after the controller explicitly instructs you via the Inbox to relay or reply.",
		// ── workspace_meta 工作空间元数据 ──
		"workspace_meta.action": "Operation type: lock (acquire file lock), unlock (release file lock), locks (list active locks), history (view file version history)",
		"workspace_meta.path":   "Relative path of the target file (required for lock/unlock/history)",
		// ── 25.1d：async background-tool feedback (async_tools.go) ──
		"async_tool.launched":       "[Background task] {tool} started (task_id={task_id}). The result will be fed back to you automatically on completion — do not poll; you may continue handling other input.",
		"async_tool.completed":      "[Background task completed] tool={tool}\n{result}",
		"async_tool.failed":         "[Background task failed] tool={tool}, error={error}",
		"async_tool.spilled_notice": "[Full output was large and written to disk at {path}. Call async_task_output(task_id='{task_id}') to retrieve it.]",
		// ── 25.1g：reliability/ 子系统（i18n.py reliability.* 逐字复制）──
		"reliability.steer_self_correct":         "[reliability] Detected {kind}: {summary}. Stop repeating the ineffective action; change strategy or use a different tool.",
		"reliability.report_leader":              "[reliability alert] {summary}. Assess this member's state and decide how to handle it (send a reminder / cancel the task / stop the member / spawn a new member or task).",
		"reliability.escalate_user":              "[reliability critical] {summary}. Beyond automated handling; escalate to the controller/user for a decision now.",
		"reliability.model_unavailable":          "model {model} is unavailable: {reason}. {no_substitute}, so this member cannot switch by itself. Call ask_user now to tell the user and ask how to proceed (spawn a new member on another model to take over / retry after it recovers / retry after fixing the configuration / stop the task); until the user answers, do not assign work to or nudge this member",
		"reliability.model_route_switched":       "{model} in the model group {reason}; switched to another available model in the group automatically",
		"reliability.model_route_interrupted":    "{model} in the model group {reason}; this round was interrupted and will be re-run on another available model in the group automatically",
		"reliability.model_route_other":          "another model in the group",
		"reliability.model_not_in_group":         "the pinned model is not in the current model group",
		"reliability.model_no_substitute_pinned": "No same-name model in the model group can replace it",
		"reliability.model_no_substitute_group":  "No other model in the model group is available either",
		"reliability.model_label":                "{name} ({where})",
		"reliability.model_label_ranked":         "#{position} {name} ({where})",
		"reliability.model_failure_separator":    "; ",
		"reliability.model_failure":              "{model} {reason}",
		"reliability.reason_api_key":             "rejected the API key (HTTP {status})",
		"reliability.reason_not_found":           "was not found at its endpoint (HTTP 404)",
		"reliability.reason_rate_limited":        "is rate limited (HTTP 429)",
		"reliability.reason_server_error":        "returned a server error (HTTP {status})",
		"reliability.reason_rejected":            "rejected the request (HTTP {status})",
		"reliability.reason_rejected_detail":     "rejected the request (HTTP {status}): {detail}",
		"reliability.reason_timeout":             "did not respond in time",
		"reliability.reason_unreachable":         "could not be reached",
		"reliability.reason_unavailable":         "is temporarily unavailable",
		"reliability.reason_failed":              "failed",
		"reliability.reason_interrupted":         "stopped in the middle of its answer ({reason})",
		"reliability.route_notice":               "[model route notice] {summary}. Already handled; no member or task changes needed. {scope}If you tell the user, state only which model (its place in the model group, provider, endpoint) failed and why, which model is used instead, and what the user should do (for example, check that model's API key); do not speculate about the impact on members.",
		"reliability.route_notice_scope_group":   "This is the state of the model group, not of one member: every member not pinned to a model (you included) now uses the replacement model, so do not say a particular member was using the failed model. ",
		"reliability.route_notice_scope_pinned":  "This only affects the members pinned to {model}, which now use the replacement model; other members are not affected by this notice, so do not say the whole team switched. ",
		"reliability.external_runtime_retrying":  "[external runtime] Member {member_name} ({agent_kind}, model {model}) is auto-retrying: {category}. Progress: {attempt}/{max_attempts}. Reason: {reason_message}. Diagnostics: http_status={http_status}, sdk_error_code={sdk_error_code}. The round stays open and member status is unchanged; awaiting the next SDK result. {summary}",
	},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetLanguage 设置全局语言。
// Python: set_language(lang)
func SetLanguage(lang Language) error {
	if _, ok := STRINGS[lang]; !ok {
		supported := make([]string, 0, len(STRINGS))
		for k := range STRINGS {
			supported = append(supported, string(k))
		}
		return fmt.Errorf("不支持的语言 '%s'，支持的语言: %v", lang, supported)
	}
	mu.Lock()
	currentLanguage = lang
	mu.Unlock()
	return nil
}

// GetLanguage 获取当前全局语言。
// Python: get_language()
func GetLanguage() Language {
	mu.RLock()
	defer mu.RUnlock()
	return currentLanguage
}

// T 解析本地化字符串。
// Python: t(key, **kwargs)
// 从 agent_teams 根包搬入，打断 schema→agent_teams 循环依赖。
// key 为点分查找键（如 "dispatcher.member_online"），
// kwargs 为可选的插值参数映射。
// 当 key 在当前语言中缺失时，尝试在默认语言（cn）中查找，
// 仍缺失则返回 error（对齐 Python: KeyError 可恢复行为，不再 panic）。
func T(key string, kwargs ...map[string]any) (string, error) {
	mu.RLock()
	lang := currentLanguage
	mu.RUnlock()

	table, ok := STRINGS[lang]
	if !ok {
		table = STRINGS[defaultLanguage]
	}

	raw, ok := table[key]
	if !ok {
		// 回退到默认语言
		if table = STRINGS[defaultLanguage]; table != nil {
			if raw, ok = table[key]; ok {
				// 找到回退
			} else {
				return "", fmt.Errorf("缺失 i18n key '%s'，语言 '%s' 和默认语言均无此键", key, lang)
			}
		} else {
			return "", fmt.Errorf("缺失 i18n key '%s'，语言 '%s'", key, lang)
		}
	}

	if len(kwargs) > 0 && len(kwargs[0]) > 0 {
		return formatMap(raw, kwargs[0]), nil
	}
	return raw, nil
}

// ReplyHintFor 解析某条入站消息的 reply-hint note 正文。
// user 伪成员不是名册成员、也没有 agent 进程，用纯文本答复它会被静默丢弃，
// 所以该情形用无条件措辞（dispatcher.reply_hint_user）；其余发送者用通用的
// 条件措辞（dispatcher.reply_hint，带 {sender} 占位符）。
// Python: reply_hint_for(sender) (openjiuwen/agent_teams/i18n.py L1122-1138)
func ReplyHintFor(sender string) (string, error) {
	if sender == UserPseudoMemberName {
		return T("dispatcher.reply_hint_user")
	}
	return T("dispatcher.reply_hint", map[string]any{"sender": sender})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// formatMap 将模板中的 {key} 占位符替换为 kwargs 中的值。
// Python: str.format_map(kwargs)
func formatMap(template string, kwargs map[string]any) string {
	result := template
	for k, v := range kwargs {
		result = strings.ReplaceAll(result, "{"+k+"}", fmt.Sprintf("%v", v))
	}
	return result
}
