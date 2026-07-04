package tools

// ──────────────────────────── 结构体 ────────────────────────────

// CronMetadataProvider cron 工具元数据提供者
type CronMetadataProvider struct{}

// CronListJobsMetadataProvider cron_list_jobs 遗留工具元数据提供者
type CronListJobsMetadataProvider struct{}

// CronGetJobMetadataProvider cron_get_job 遗留工具元数据提供者
type CronGetJobMetadataProvider struct{}

// CronCreateJobMetadataProvider cron_create_job 遗留工具元数据提供者
type CronCreateJobMetadataProvider struct{}

// CronUpdateJobMetadataProvider cron_update_job 遗留工具元数据提供者
type CronUpdateJobMetadataProvider struct{}

// CronDeleteJobMetadataProvider cron_delete_job 遗留工具元数据提供者
type CronDeleteJobMetadataProvider struct{}

// CronToggleJobMetadataProvider cron_toggle_job 遗留工具元数据提供者
type CronToggleJobMetadataProvider struct{}

// CronPreviewJobMetadataProvider cron_preview_job 遗留工具元数据提供者
type CronPreviewJobMetadataProvider struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

// cronDescription cron 工具双语描述
var cronDescription = map[string]string{
	"cn": `使用 action 接口：status、list、add、update、` +
		`remove、run、runs、wake，并兼容结构化 schedule/payload/delivery 字段。` +
		`处理"2分钟后""明天上午9点""下周一"这类时间时，优先根据系统提示中已提供的当前` +
		`日期与时间直接换算并调用 cron，不要为了简单的时间换算先调用 code 或 bash。` +
		`创建一次性提醒时，schedule.at 默认直接使用用户当前本地时区偏移来写，例如 +08:00；` +
		`除非用户明确要求，否则不要改写成 Z 或 UTC。` +
		`给当前聊天创建提醒时，优先使用 payload.kind=systemEvent 和 sessionTarget=current。` +
		`向用户确认创建结果时，优先按 schedule.at 里的原始时区/偏移表述，不要自行改写成 UTC。` +
		"\n\n" +
		`【投递频道】delivery.channel / targets：用户未明确指定时不填，系统自动使用当前对话渠道；**禁止从历史记录推断。**` +
		"\n\n" +
		`【重要：cron 表达式格式】只支持7段式(Quartz格式)：秒 分 时 日 月 周 年。` +
		`字段取值范围：秒(0-59)，分(0-59)，时(0-23)，日(1-31)，月(1-12)，周(1-7或?)，年(1970-2099或*)。` +
		`日和周字段：不能同时指定具体值，其中一个必须用?表示'不指定'。` +
		`年份字段：*表示跨年周期，固定年份只在该年执行。` +
		`真正只执行一次：所有字段均为固定值(无*和?)，如'0 30 17 29 4 ? 2026'表示2026年4月29日17:30:00执行一次。` +
		`例：每天9点 -> '0 0 9 * * ? *'；每15分钟 -> '0 */15 * * * ? *'；每周一9点 -> '0 0 9 ? * MON *'。` +
		`注意：一次性任务建议优先使用 schedule.at (ISO8601格式)，cron更适合周期性任务。` +
		"\n\n" +
		`【重要：cron 表达式限制】标准 cron 的 */X 语义是'当字段值能被 X 整除时触发'，` +
		`而非'每隔 X 单位触发'。只有当周期单位能被 X 整除时，间隔才是均匀的。以下是各字段的限制：` +
		"\n" +
		`- 秒/分(0-59)：*/X 仅支持 X 整除60的值：1/2/3/4/5/6/10/12/15/20/30。` +
		`  例如 */40 实际在每小时第0分和第40分触发（间隔40分→20分交替），并非每40分钟。` +
		`  用户要求'每隔40分钟'时，必须先告知此限制并让用户确认是否接受不均匀间隔，` +
		`  或建议改用整除60的间隔（如20分钟或30分钟）。未经用户确认不得直接创建。` +
		"\n" +
		`- 小时(0-23)：*/X 仅支持 X 整除24的值：1/2/3/4/6/8/12。` +
		`  例如 */5 实际在每天0/5/10/15/20时触发（间隔5h→4h→5h→4h交替），并非每5小时。` +
		`  用户要求'每隔5小时'时，必须告知限制并让用户确认，或建议改用整除24的间隔。` +
		"\n" +
		`- 日(1-31)：*/X 不可靠，因为不同月份天数不同（28/29/30/31）。` +
		`  例如 */15 在2月只触发1、15日（共2次），在31天月份触发1、16、31日（共3次）。` +
		`  用户要求'每隔X天'时，建议改用'每周X'或指定固定日期（如每月1号、15号）。` +
		"\n" +
		`- 月(1-12)：*/X 仅支持 X 整除12的值：1/2/3/4/6。` +
		`  例如 */5 实际在1/5/10月触发，并非每5个月均匀触发。` +
		"\n" +
		`- 周(1-7)：*/X 仅支持 X 整除7的值：1/7。1=SUN,7=SAT。` +
		`  例如 */2 实际在SUN/TUE/THU触发，并非'每隔2周'。` +
		`  用户要求'每隔2周'时，应直接指定具体星期几或建议简化为'每周一'。` +
		"\n" +
		`处理'每隔X分钟/小时/天'需求时，务必检查 X 是否整除对应周期单位；` +
		`若不整除，必须告知用户限制，让用户确认后再创建，或建议替代方案。`,

	"en": `Use the cron action interface. Supports status, list, add, update, ` +
		`remove, run, runs, and wake using structured schedule/payload/delivery fields. ` +
		`For requests like 'in 2 minutes', 'tomorrow at 9am', or 'next Monday', ` +
		`prefer converting the time directly from the current date/time already provided in the system prompt ` +
		`and call cron directly instead of using code or bash for simple time math. ` +
		`When creating one-shot reminders, write schedule.at using the user's current local timezone offset ` +
		`directly, for example +08:00; unless the user explicitly asks for it, do not rewrite it into Z or UTC. ` +
		`For reminders targeting the current chat, prefer payload.kind=systemEvent with sessionTarget=current. ` +
		`When confirming a created reminder to the user, prefer the original timezone/offset from schedule.at ` +
		`instead of rewriting it into UTC.` +
		"\n\n" +
		`[Delivery Channel] delivery.channel / targets: leave empty unless user explicitly specifies; ` +
		`system uses current channel. **Never infer from history.**` +
		"\n\n" +
		`[CRITICAL: Cron Expression Format] Only supports 7-field Quartz format: ` +
		`second minute hour day month dow year. ` +
		`Field ranges: ` +
		`second(0-59), minute(0-59), hour(0-23), day(1-31), month(1-12), dow(1-7 or ?), year(1970-2099 or *). ` +
		`Day and dow fields: cannot both have specific values; one must be '?' (no specific value). ` +
		`Year field: '*' for recurring, fixed year for one-shot within that year. ` +
		`True one-shot: all fields fixed (no '*' or '?'), e.g. '0 30 17 29 4 ? 2026' runs once at 2026-04-29 17:30:00. ` +
		`Examples: daily 9am -> '0 0 9 * * ? *'; every 15min -> '0 */15 * * * ? *'; ` +
		`every Monday 9am -> '0 0 9 ? * MON *'. ` +
		`Note: for one-shot tasks, prefer schedule.at (ISO8601 format); cron is better for recurring tasks.` +
		"\n\n" +
		`[CRITICAL: Cron Expression Limits] Standard cron's */X means 'trigger when the field value ` +
		`is divisible by X', NOT 'every X units'. Uniform intervals only work when the cycle unit ` +
		`is divisible by X. Field limits:` +
		"\n" +
		`- Second/Minute(0-59): */X only works for X dividing 60: 1/2/3/4/5/6/10/12/15/20/30. ` +
		`  Example: */40 triggers at minute 0 and 40 each hour (alternating 40min-20min gaps), ` +
		`NOT every 40 minutes. ` +
		`  When user requests 'every 40 minutes', MUST inform user of this limitation first ` +
		`  and let user confirm whether to accept uneven intervals, or suggest intervals that divide 60 ` +
		`(e.g. 20 or 30 minutes). Do NOT create without user confirmation.` +
		"\n" +
		`- Hour(0-23): */X only works for X dividing 24: 1/2/3/4/6/8/12. ` +
		`  Example: */5 triggers at hours 0/5/10/15/20 (alternating 5h-4h gaps), NOT every 5 hours. ` +
		`  When user requests 'every 5 hours', MUST inform and let user confirm, or suggest 4 or 6 hours.` +
		"\n" +
		`- Day(1-31): */X is unreliable due to varying month lengths (28/29/30/31 days). ` +
		`  Example: */15 triggers on day 1,15 in Feb (2 times), but 1,16,31 in 31-day months (3 times). ` +
		`  When user requests 'every X days', suggest using 'every week on day X' or fixed dates.` +
		"\n" +
		`- Month(1-12): */X only works for X dividing 12: 1/2/3/4/6. ` +
		`  Example: */5 triggers in Jan/May/Oct, NOT uniformly every 5 months.` +
		"\n" +
		`- Dow(1-7): */X only works for X dividing 7: 1/7. 1=SUN, 7=SAT. ` +
		`  Example: */2 triggers on SUN/TUE/THU, NOT 'every 2 weeks'. ` +
		`  When user requests 'every 2 weeks', suggest simplifying to a specific weekday.` +
		"\n" +
		`When handling 'every X minutes/hours/days' requests, always check if X divides the cycle unit. ` +
		`If not, MUST inform user of the limitation, let user confirm before creating, or suggest alternatives.`,
}

// cronFieldDescriptions cron 工具字段双语描述
var cronFieldDescriptions = map[string]map[string]string{
	"action": {
		"cn": "要执行的 cron 操作",
		"en": "Cron action to execute",
	},
	"job": {
		"cn": "用于 add 的任务对象；支持结构化字段和兼容层字段",
		"en": "Job object for add; supports structured fields and compatibility fields",
	},
	"jobId": {
		"cn": "用于 update/remove/run/runs 的任务 ID",
		"en": "Job id used by update/remove/run/runs",
	},
	"patch": {
		"cn": "用于 update 的补丁对象",
		"en": "Patch object used by update",
	},
	"includeDisabled": {
		"cn": "list 时是否包含已禁用任务",
		"en": "Whether list should include disabled jobs",
	},
	"text": {
		"cn": "wake 动作要发送的提示文本",
		"en": "Wake text to inject for action=wake",
	},
	"mode": {
		"cn": "wake 的触发模式",
		"en": "Wake delivery mode",
	},
	"contextMessages": {
		"cn": "保留给上下文提示的兼容字段",
		"en": "Reserved compatibility field for context hints",
	},
	"name": {
		"cn": "任务名称",
		"en": "Job name",
	},
	"enabled": {
		"cn": "任务是否启用",
		"en": "Whether the job is enabled",
	},
	"schedule": {
		"cn": "结构化调度定义，支持 at/every/cron",
		"en": "Structured schedule definition supporting at/every/cron",
	},
	"schedule.kind": {
		"cn": "调度类型：at、every 或 cron",
		"en": "Schedule type: at, every, or cron",
	},
	"schedule.at": {
		"cn": "一次性执行时间，ISO 8601",
		"en": "One-shot execution time in ISO 8601",
	},
	"schedule.everyMs": {
		"cn": "循环间隔，毫秒",
		"en": "Recurring interval in milliseconds",
	},
	"schedule.anchorMs": {
		"cn": "every 调度的起始锚点毫秒时间戳",
		"en": "Anchor timestamp in milliseconds for every schedules",
	},
	"schedule.expr": {
		"cn": "cron表达式(Quartz格式)。7段式：秒 分 时 日 月 周 年。" +
			"日和周：不能同时指定具体值，其中一个用?。" +
			"年份：*跨年周期，固定年份只在该年执行。" +
			"一次性：所有字段固定值，如'0 0 17 28 3 ? 2026'。" +
			"例：每天9点'0 0 9 * * ? *'；每15分钟'0 */15 * * * ? *'。" +
			"详见工具描述。",
		"en": "Cron expression (Quartz format). 7-field: second minute hour day month dow year. " +
			"Day/dow: cannot both be specific; use '?' for one. " +
			"Year: '*' for recurring, fixed year limits to that year. " +
			"One-shot: all fields fixed, e.g. '0 0 17 28 3 ? 2026'. " +
			"Examples: daily 9am '0 0 9 * * ? *'; every 15min '0 */15 * * * ? *'. " +
			"See tool description.",
	},
	"schedule.tz": {
		"cn": "cron 调度使用的时区",
		"en": "Timezone used by cron schedules",
	},
	"schedule.staggerMs": {
		"cn": "cron 调度的可选抖动毫秒数",
		"en": "Optional cron jitter in milliseconds",
	},
	"payload": {
		"cn": "结构化任务负载，支持 systemEvent 或 agentTurn",
		"en": "Structured job payload supporting systemEvent or agentTurn",
	},
	"payload.kind": {
		"cn": "负载类型：systemEvent 或 agentTurn",
		"en": "Payload type: systemEvent or agentTurn",
	},
	"payload.text": {
		"cn": "systemEvent提醒文本。不要包含时间/频率信息（如'每隔40分钟'、'每天9点'）",
		"en": "Reminder text for systemEvent. Do NOT include time/frequency info",
	},
	"payload.message": {
		"cn": "agentTurn 发送给代理的消息",
		"en": "Message sent to the agent for agentTurn payloads",
	},
	"payload.model": {
		"cn": "agentTurn 可选模型覆盖",
		"en": "Optional model override for agentTurn",
	},
	"payload.thinking": {
		"cn": "agentTurn 的思考预算或模式",
		"en": "Thinking mode or budget for agentTurn",
	},
	"payload.timeoutSeconds": {
		"cn": "agentTurn 超时时间（秒）",
		"en": "Timeout in seconds for agentTurn",
	},
	"payload.allowUnsafeExternalContent": {
		"cn": "是否允许不安全的外部内容",
		"en": "Whether unsafe external content is allowed",
	},
	"payload.lightContext": {
		"cn": "是否使用轻量上下文执行",
		"en": "Whether to run with lighter context",
	},
	"payload.deliver": {
		"cn": "agentTurn 自带的投递策略字段",
		"en": "Embedded delivery strategy field for agentTurn",
	},
	"payload.channel": {
		"cn": "agentTurn 的默认投递频道",
		"en": "Default delivery channel for agentTurn",
	},
	"payload.to": {
		"cn": "agentTurn 的目标收件人",
		"en": "Target recipient for agentTurn",
	},
	"payload.bestEffortDeliver": {
		"cn": "是否最佳努力投递",
		"en": "Whether delivery should be best effort",
	},
	"payload.fallbacks": {
		"cn": "agentTurn 的回退投递列表",
		"en": "Fallback delivery list for agentTurn",
	},
	"delivery": {
		"cn": "提醒结果的投递方式",
		"en": "How reminder output should be delivered",
	},
	"delivery.mode": {
		"cn": "投递模式：none、announce 或 webhook",
		"en": "Delivery mode: none, announce, or webhook",
	},
	"delivery.channel": {
		"cn": "announce 模式投递频道。用户未明确指定时不填，系统自动使用当前对话渠道；**禁止从历史记录推断。**",
		"en": "Delivery channel for announce mode. Leave empty unless user explicitly specifies; " +
			"system uses current channel. **Never infer from history.**",
	},
	"delivery.to": {
		"cn": "目标收件人或会话标识",
		"en": "Target recipient or session identifier",
	},
	"delivery.accountId": {
		"cn": "投递账号标识",
		"en": "Account identifier for delivery",
	},
	"delivery.bestEffort": {
		"cn": "announce/webhook 是否最佳努力投递",
		"en": "Whether announce/webhook delivery is best effort",
	},
	"delivery.failureDestination": {
		"cn": "失败时的兜底投递目标",
		"en": "Fallback destination when delivery fails",
	},
	"sessionTarget": {
		"cn": "会话目标：main、isolated、current 或 session:<id>",
		"en": "Session target: main, isolated, current, or session:<id>",
	},
	"wakeMode": {
		"cn": "唤醒模式：now 或 next-heartbeat",
		"en": "Wake mode: now or next-heartbeat",
	},
	"deleteAfterRun": {
		"cn": "执行后是否自动删除该任务",
		"en": "Whether the job should be deleted after it runs",
	},
	"cron_expr": {
		"cn": "兼容层cron表达式(Quartz格式)。7段式：秒 分 时 日 月 周 年。" +
			"日和周：不能同时指定具体值，其中一个用?。" +
			"年份：*跨年周期，固定年份只在该年执行。" +
			"一次性：所有字段固定值，如'0 0 17 28 3 ? 2026'。" +
			"例：每天9点'0 0 9 * * ? *'；每15分钟'0 */15 * * * ? *'。" +
			"详见工具描述。",
		"en": "Compatibility cron expression (Quartz format). 7-field: second minute hour day month dow year. " +
			"Day/dow: cannot both be specific; use '?' for one. " +
			"Year: '*' for recurring, fixed year limits to that year. " +
			"One-shot: all fields fixed, e.g. '0 0 17 28 3 ? 2026'. " +
			"Examples: daily 9am '0 0 9 * * ? *'; every 15min '0 */15 * * * ? *'. " +
			"See tool description.",
	},
	"timezone": {
		"cn": "兼容层时区字段",
		"en": "Compatibility timezone field",
	},
	"wake_offset_seconds": {
		"cn": "兼容层提前唤醒秒数。默认 300，若用户未指定则使用 300 秒。",
		"en": "Compatibility wake offset in seconds. Default 300; use 300 seconds if user does not specify.",
	},
	"description": {
		"cn": "具体任务内容，到点执行时发给助手。不要包含时间/频率信息（如'每隔40分钟'、'每天9点'）",
		"en": "Task content sent to assistant at scheduled time. Do NOT include time/frequency info",
	},
	"targets": {
		"cn": "目标频道。用户未明确指定时不填，系统自动使用当前对话渠道；**禁止从历史记录推断。**",
		"en": "Target channel. Leave empty unless user explicitly specifies; " +
			"system uses current channel. **Never infer from history.**",
	},
}

// cronListJobsDescription cron_list_jobs 工具双语描述
var cronListJobsDescription = map[string]string{
	"cn": "列出所有 cron 定时任务。",
	"en": "List all cron jobs.",
}

// cronGetJobDescription cron_get_job 工具双语描述
var cronGetJobDescription = map[string]string{
	"cn": "根据任务 ID 获取单个 cron 定时任务的详细信息。",
	"en": "Get a single cron job by its ID.",
}

// cronCreateJobDescription cron_create_job 工具双语描述
var cronCreateJobDescription = map[string]string{
	"cn": `创建新的 cron 定时任务，使用扁平字段（name, cron_expr, timezone, targets, description, wake_offset_seconds）。` +
		"\n\n" +
		`【targets】用户未明确指定投递渠道时不填，系统自动使用当前对话渠道；**禁止从历史记录推断。**` +
		"\n\n" +
		`【重要：cron 表达式格式】只支持7段式(Quartz格式)：秒 分 时 日 月 周 年。` +
		`日和周字段：不能同时指定具体值，其中一个必须用?表示'不指定'。` +
		`年份字段：*表示跨年周期执行，固定年份只在该年执行。` +
		`真正只执行一次：所有字段均为固定值（无*和?），如'0 0 17 28 3 ? 2026'。` +
		`例：每天9点 -> '0 0 9 * * ? *'；每15分钟 -> '0 */15 * * * ? *'；每周一9点 -> '0 0 9 ? * MON *'。` +
		"\n\n" +
		`【重要：cron 表达式限制】标准 cron 的 */X 语义是'当字段值能被 X 整除时触发'，而非'每隔 X 单位触发'。` +
		`只有当周期单位能被 X 整除时，间隔才是均匀的。以下是各字段的限制：` +
		"\n" +
		`- 秒/分(0-59)：*/X 仅支持 X 整除60的值：1/2/3/4/5/6/10/12/15/20/30。` +
		`  例如 */40 实际在每小时第0分和第40分触发（间隔40分→20分交替），并非每40分钟。` +
		`  用户要求'每隔40分钟'时，必须先告知此限制并让用户确认是否接受不均匀间隔，` +
		`  或建议改用整除60的间隔（如20分钟或30分钟）。未经用户确认不得直接创建。` +
		"\n" +
		`- 小时(0-23)：*/X 仅支持 X 整除24的值：1/2/3/4/6/8/12。` +
		`  例如 */5 实际在每天0/5/10/15/20时触发（间隔5h→4h→5h→4h交替），并非每5小时。` +
		"\n" +
		`- 日(1-31)：*/X 不可靠，因为不同月份天数不同（28/29/30/31）。` +
		"\n" +
		`- 月(1-12)：*/X 仅支持 X 整除12的值：1/2/3/4/6。` +
		"\n" +
		`- 周(1-7)：*/X 仅支持 X 整除7的值：1/7。1=SUN,7=SAT。` +
		"\n\n" +
		`处理'每隔X分钟/小时/天'需求时，务必检查 X 是否整除对应周期单位；` +
		`若不整除，必须告知用户限制，让用户确认后再创建，或建议替代方案。`,

	"en": `Create a new cron job using flat fields (name, cron_expr, timezone, targets, description, wake_offset_seconds).` +
		"\n\n" +
		`[targets] Leave empty unless user explicitly specifies a channel; system uses current channel. **Never infer from history.**` +
		"\n\n" +
		`[CRITICAL: Cron Expression Format] Only supports 7-field Quartz format: second minute hour day month dow year. ` +
		`Day and dow fields: cannot both have specific values; one must be '?' (no specific value). ` +
		`Year field: '*' for recurring, fixed year for one-shot within that year. ` +
		`True one-shot: all fields fixed (no '*' or '?'), e.g. '0 0 17 28 3 ? 2026'. ` +
		`Examples: daily 9am -> '0 0 9 * * ? *'; every 15min -> '0 */15 * * * ? *'; every Monday 9am -> '0 0 9 ? * MON *'.` +
		"\n\n" +
		`[CRITICAL: Cron Expression Limits] Standard cron's */X means 'trigger when the field value is divisible by X', ` +
		`NOT 'every X units'. Uniform intervals only work when the cycle unit is divisible by X. Field limits:` +
		"\n" +
		`- Second/Minute(0-59): */X only works for X dividing 60: 1/2/3/4/5/6/10/12/15/20/30. ` +
		`  Example: */40 triggers at minute 0 and 40 each hour (alternating 40min-20min gaps), NOT every 40 minutes. ` +
		`  When user requests 'every 40 minutes', MUST inform user of this limitation first ` +
		`  and let user confirm whether to accept uneven intervals, or suggest intervals that divide 60. ` +
		`  Do NOT create without user confirmation.` +
		"\n" +
		`- Hour(0-23): */X only works for X dividing 24: 1/2/3/4/6/8/12. ` +
		`  Example: */5 triggers at hours 0/5/10/15/20 (alternating 5h-4h gaps), NOT every 5 hours.` +
		"\n" +
		`- Day(1-31): */X is unreliable due to varying month lengths (28/29/30/31 days).` +
		"\n" +
		`- Month(1-12): */X only works for X dividing 12: 1/2/3/4/6.` +
		"\n" +
		`- Dow(1-7): */X only works for X dividing 7: 1/7. 1=SUN, 7=SAT.` +
		"\n\n" +
		`When handling 'every X minutes/hours/days' requests, always check if X divides the cycle unit. ` +
		`If not, MUST inform user and let user confirm before creating.`,
}

// cronUpdateJobDescription cron_update_job 工具双语描述
var cronUpdateJobDescription = map[string]string{
	"cn": "使用扁平字段更新已有的 cron 定时任务。",
	"en": "Update an existing cron job with a flat patch dict.",
}

// cronDeleteJobDescription cron_delete_job 工具双语描述
var cronDeleteJobDescription = map[string]string{
	"cn": "根据任务 ID 删除 cron 定时任务。",
	"en": "Delete a cron job by its ID.",
}

// cronToggleJobDescription cron_toggle_job 工具双语描述
var cronToggleJobDescription = map[string]string{
	"cn": "启用或禁用指定的 cron 定时任务。",
	"en": "Enable or disable a cron job.",
}

// cronPreviewJobDescription cron_preview_job 工具双语描述
var cronPreviewJobDescription = map[string]string{
	"cn": "预览 cron 定时任务的下 N 次计划执行时间。",
	"en": "Preview next N scheduled run times for a cron job.",
}

// legacyFieldDescriptions 遗留 cron 工具字段双语描述
var legacyFieldDescriptions = map[string]map[string]string{
	"job_id": {
		"cn": "任务 ID",
		"en": "Job ID",
	},
	"job_id_to_look_up": {
		"cn": "要查询的任务 ID",
		"en": "The job ID to look up",
	},
	"job_id_to_update": {
		"cn": "要更新的任务 ID",
		"en": "Job ID to update",
	},
	"job_id_to_delete": {
		"cn": "要删除的任务 ID",
		"en": "Job ID to delete",
	},
	"job_id_to_toggle": {
		"cn": "要启用/禁用的任务 ID",
		"en": "Job ID",
	},
	"job_id_to_preview": {
		"cn": "要预览的任务 ID",
		"en": "Job ID",
	},
	"patch": {
		"cn": "要更新的字段",
		"en": "Fields to update",
	},
	"enabled": {
		"cn": "是否启用该任务",
		"en": "Whether to enable the job",
	},
	"count": {
		"cn": "预览的执行次数（1-50，默认 5）",
		"en": "Number of runs to preview (1-50, default 5)",
	},
	"name": {
		"cn": "任务名称",
		"en": "Job name",
	},
	"cron_expr": {
		"cn": "Cron表达式(Quartz格式)。7段式：秒 分 时 日 月 周 年。" +
			"日和周：不能同时指定具体值，其中一个用?。" +
			"年份：*跨年周期，固定年份只在该年执行。" +
			"一次性：所有字段固定值，如'0 0 17 28 3 ? 2026'。" +
			"例：每天9点'0 0 9 * * ? *'；每15分钟'0 */15 * * * ? *'。" +
			"详见工具描述。",
		"en": "Cron expression (Quartz format). 7-field: second minute hour day month dow year. " +
			"Day/dow: cannot both be specific; use '?' for one. " +
			"Year: '*' for recurring, fixed year limits to that year. " +
			"One-shot: all fields fixed, e.g. '0 0 17 28 3 ? 2026'. " +
			"Examples: daily 9am '0 0 9 * * ? *'; every 15min '0 */15 * * * ? *'. " +
			"See tool description.",
	},
	"timezone": {
		"cn": "时区，如 Asia/Shanghai",
		"en": "Timezone, e.g. Asia/Shanghai",
	},
	"targets": {
		"cn": "目标频道。用户未明确指定时不填，系统自动使用当前对话渠道；**禁止从历史记录推断。**",
		"en": "Target channel. Leave empty unless user explicitly specifies; " +
			"system uses current channel. **Never infer from history.**",
	},
	"legacy_enabled": {
		"cn": "是否启用",
		"en": "Whether to enable the job",
	},
	"legacy_description": {
		"cn": "具体任务内容，到点执行时发给助手。不要包含时间/频率信息（如'每隔40分钟'、'每天9点'）",
		"en": "Task content sent to assistant at scheduled time. Do NOT include time/frequency info",
	},
	"wake_offset_seconds": {
		"cn": "提前多少秒执行，默认 300。若用户未指定，则默认使用 300 秒。",
		"en": "Wake offset in seconds, default 300. If user does not specify, use 300 seconds by default.",
	},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetCronJobInputParams 构建 cron job 对象的参数 Schema
func GetCronJobInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	d := func(key string) string {
		if v, ok := cronFieldDescriptions[key][lang]; ok {
			return v
		}
		return cronFieldDescriptions[key]["cn"]
	}

	return map[string]any{
		"type":                 "object",
		"required":             []any{},
		"additionalProperties": true,
		"properties": map[string]any{
			"name":    map[string]any{"type": "string", "description": d("name")},
			"enabled": map[string]any{"type": "boolean", "description": d("enabled")},
			"schedule": map[string]any{
				"type":                 "object",
				"description":         d("schedule"),
				"required":             []any{},
				"additionalProperties": true,
				"properties": map[string]any{
					"kind":      map[string]any{"type": "string", "enum": []any{"at", "every", "cron"}, "description": d("schedule.kind")},
					"at":        map[string]any{"type": "string", "description": d("schedule.at")},
					"everyMs":   map[string]any{"type": "integer", "description": d("schedule.everyMs")},
					"anchorMs":  map[string]any{"type": "integer", "description": d("schedule.anchorMs")},
					"expr":      map[string]any{"type": "string", "description": d("schedule.expr")},
					"tz":        map[string]any{"type": "string", "description": d("schedule.tz")},
					"staggerMs": map[string]any{"type": "integer", "description": d("schedule.staggerMs")},
				},
			},
			"payload": map[string]any{
				"type":                 "object",
				"description":         d("payload"),
				"required":             []any{},
				"additionalProperties": true,
				"properties": map[string]any{
					"kind":                      map[string]any{"type": "string", "enum": []any{"systemEvent", "agentTurn"}, "description": d("payload.kind")},
					"text":                      map[string]any{"type": "string", "description": d("payload.text")},
					"message":                   map[string]any{"type": "string", "description": d("payload.message")},
					"model":                     map[string]any{"type": "string", "description": d("payload.model")},
					"thinking":                  map[string]any{"type": "string", "description": d("payload.thinking")},
					"timeoutSeconds":            map[string]any{"type": "integer", "description": d("payload.timeoutSeconds")},
					"allowUnsafeExternalContent": map[string]any{"type": "boolean", "description": d("payload.allowUnsafeExternalContent")},
					"lightContext":              map[string]any{"type": "boolean", "description": d("payload.lightContext")},
					"deliver":                   map[string]any{"type": "string", "description": d("payload.deliver")},
					"channel":                   map[string]any{"type": "string", "description": d("payload.channel")},
					"to":                        map[string]any{"type": "string", "description": d("payload.to")},
					"bestEffortDeliver":         map[string]any{"type": "boolean", "description": d("payload.bestEffortDeliver")},
					"fallbacks": map[string]any{
						"type":        "array",
						"description": d("payload.fallbacks"),
						"items":       map[string]any{"type": "string", "description": d("payload.fallbacks")},
					},
				},
			},
			"delivery": map[string]any{
				"type":                 "object",
				"description":         d("delivery"),
				"required":             []any{},
				"additionalProperties": true,
				"properties": map[string]any{
					"mode":               map[string]any{"type": "string", "enum": []any{"none", "announce", "webhook"}, "description": d("delivery.mode")},
					"channel":            map[string]any{"type": "string", "description": d("delivery.channel")},
					"to":                 map[string]any{"type": "string", "description": d("delivery.to")},
					"accountId":          map[string]any{"type": "string", "description": d("delivery.accountId")},
					"bestEffort":         map[string]any{"description": d("delivery.bestEffort")},
					"failureDestination": map[string]any{"description": d("delivery.failureDestination")},
				},
			},
			"sessionTarget":      map[string]any{"type": "string", "description": d("sessionTarget")},
			"wakeMode":           map[string]any{"type": "string", "enum": []any{"now", "next-heartbeat"}, "description": d("wakeMode")},
			"deleteAfterRun":     map[string]any{"type": "boolean", "description": d("deleteAfterRun")},
			"cron_expr":          map[string]any{"type": "string", "description": d("cron_expr")},
			"timezone":           map[string]any{"type": "string", "description": d("timezone")},
			"wake_offset_seconds": map[string]any{"type": "integer", "description": d("wake_offset_seconds")},
			"description":        map[string]any{"type": "string", "description": d("description")},
			"targets":            map[string]any{"type": "string", "description": d("targets")},
		},
	}
}

// GetCronInputParams 构建 cron 工具的参数 Schema
func GetCronInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	d := func(key string) string {
		if v, ok := cronFieldDescriptions[key][lang]; ok {
			return v
		}
		return cronFieldDescriptions[key]["cn"]
	}

	jobSchema := GetCronJobInputParams(language)
	jobSchema["description"] = d("job")

	patchSchema := GetCronJobInputParams(language)
	patchSchema["description"] = d("patch")

	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":          map[string]any{"type": "string", "enum": []any{"status", "list", "add", "update", "remove", "run", "runs", "wake"}, "description": d("action")},
			"job":             jobSchema,
			"jobId":           map[string]any{"type": "string", "description": d("jobId")},
			"patch":           patchSchema,
			"includeDisabled": map[string]any{"type": "boolean", "description": d("includeDisabled")},
			"text":            map[string]any{"type": "string", "description": d("text")},
			"mode":            map[string]any{"type": "string", "enum": []any{"now", "next-heartbeat"}, "description": d("mode")},
			"contextMessages": map[string]any{"type": "integer", "description": d("contextMessages")},
		},
		"required":          []any{"action"},
		"additionalProperties": true,
	}
}

// GetCronListJobsInputParams 构建 cron_list_jobs 工具的参数 Schema
func GetCronListJobsInputParams(language string) map[string]any {
	_ = language
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []any{},
	}
}

// GetCronGetJobInputParams 构建 cron_get_job 工具的参数 Schema
func GetCronGetJobInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	d := func(key string) string {
		if v, ok := legacyFieldDescriptions[key][lang]; ok {
			return v
		}
		return legacyFieldDescriptions[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"job_id": map[string]any{"type": "string", "description": d("job_id_to_look_up")},
		},
		"required": []any{"job_id"},
	}
}

// GetCronCreateJobInputParams 构建 cron_create_job 工具的参数 Schema
func GetCronCreateJobInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	d := func(key string) string {
		if v, ok := legacyFieldDescriptions[key][lang]; ok {
			return v
		}
		return legacyFieldDescriptions[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":                map[string]any{"type": "string", "description": d("name")},
			"cron_expr":           map[string]any{"type": "string", "description": d("cron_expr")},
			"timezone":            map[string]any{"type": "string", "description": d("timezone"), "default": "Asia/Shanghai"},
			"targets":             map[string]any{"type": "string", "description": d("targets")},
			"enabled":             map[string]any{"type": "boolean", "description": d("legacy_enabled"), "default": true},
			"description":         map[string]any{"type": "string", "description": d("legacy_description")},
			"wake_offset_seconds": map[string]any{"type": "integer", "description": d("wake_offset_seconds"), "default": 300},
		},
		"required": []any{"name", "cron_expr", "timezone", "description"},
	}
}

// GetCronUpdateJobInputParams 构建 cron_update_job 工具的参数 Schema
func GetCronUpdateJobInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	d := func(key string) string {
		if v, ok := legacyFieldDescriptions[key][lang]; ok {
			return v
		}
		return legacyFieldDescriptions[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"job_id": map[string]any{"type": "string", "description": d("job_id_to_update")},
			"patch":  map[string]any{"type": "object", "description": d("patch"), "additionalProperties": true},
		},
		"required": []any{"job_id", "patch"},
	}
}

// GetCronDeleteJobInputParams 构建 cron_delete_job 工具的参数 Schema
func GetCronDeleteJobInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	d := func(key string) string {
		if v, ok := legacyFieldDescriptions[key][lang]; ok {
			return v
		}
		return legacyFieldDescriptions[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"job_id": map[string]any{"type": "string", "description": d("job_id_to_delete")},
		},
		"required": []any{"job_id"},
	}
}

// GetCronToggleJobInputParams 构建 cron_toggle_job 工具的参数 Schema
func GetCronToggleJobInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	d := func(key string) string {
		if v, ok := legacyFieldDescriptions[key][lang]; ok {
			return v
		}
		return legacyFieldDescriptions[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"job_id":  map[string]any{"type": "string", "description": d("job_id_to_toggle")},
			"enabled": map[string]any{"type": "boolean", "description": d("enabled")},
		},
		"required": []any{"job_id", "enabled"},
	}
}

// GetCronPreviewJobInputParams 构建 cron_preview_job 工具的参数 Schema
func GetCronPreviewJobInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	d := func(key string) string {
		if v, ok := legacyFieldDescriptions[key][lang]; ok {
			return v
		}
		return legacyFieldDescriptions[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"job_id": map[string]any{"type": "string", "description": d("job_id_to_preview")},
			"count":  map[string]any{"type": "integer", "description": d("count"), "default": 5},
		},
		"required": []any{"job_id"},
	}
}

// GetName 返回 cron 工具名称
func (p *CronMetadataProvider) GetName() string { return "cron" }

// GetDescription 返回 cron 工具描述
func (p *CronMetadataProvider) GetDescription(language string) string {
	if d, ok := cronDescription[language]; ok {
		return d
	}
	return cronDescription["cn"]
}

// GetInputParams 返回 cron 工具参数 Schema
func (p *CronMetadataProvider) GetInputParams(language string) map[string]any {
	return GetCronInputParams(language)
}

// GetName 返回 cron_list_jobs 工具名称
func (p *CronListJobsMetadataProvider) GetName() string { return "cron_list_jobs" }

// GetDescription 返回 cron_list_jobs 工具描述
func (p *CronListJobsMetadataProvider) GetDescription(language string) string {
	if d, ok := cronListJobsDescription[language]; ok {
		return d
	}
	return cronListJobsDescription["cn"]
}

// GetInputParams 返回 cron_list_jobs 工具参数 Schema
func (p *CronListJobsMetadataProvider) GetInputParams(language string) map[string]any {
	return GetCronListJobsInputParams(language)
}

// GetName 返回 cron_get_job 工具名称
func (p *CronGetJobMetadataProvider) GetName() string { return "cron_get_job" }

// GetDescription 返回 cron_get_job 工具描述
func (p *CronGetJobMetadataProvider) GetDescription(language string) string {
	if d, ok := cronGetJobDescription[language]; ok {
		return d
	}
	return cronGetJobDescription["cn"]
}

// GetInputParams 返回 cron_get_job 工具参数 Schema
func (p *CronGetJobMetadataProvider) GetInputParams(language string) map[string]any {
	return GetCronGetJobInputParams(language)
}

// GetName 返回 cron_create_job 工具名称
func (p *CronCreateJobMetadataProvider) GetName() string { return "cron_create_job" }

// GetDescription 返回 cron_create_job 工具描述
func (p *CronCreateJobMetadataProvider) GetDescription(language string) string {
	if d, ok := cronCreateJobDescription[language]; ok {
		return d
	}
	return cronCreateJobDescription["cn"]
}

// GetInputParams 返回 cron_create_job 工具参数 Schema
func (p *CronCreateJobMetadataProvider) GetInputParams(language string) map[string]any {
	return GetCronCreateJobInputParams(language)
}

// GetName 返回 cron_update_job 工具名称
func (p *CronUpdateJobMetadataProvider) GetName() string { return "cron_update_job" }

// GetDescription 返回 cron_update_job 工具描述
func (p *CronUpdateJobMetadataProvider) GetDescription(language string) string {
	if d, ok := cronUpdateJobDescription[language]; ok {
		return d
	}
	return cronUpdateJobDescription["cn"]
}

// GetInputParams 返回 cron_update_job 工具参数 Schema
func (p *CronUpdateJobMetadataProvider) GetInputParams(language string) map[string]any {
	return GetCronUpdateJobInputParams(language)
}

// GetName 返回 cron_delete_job 工具名称
func (p *CronDeleteJobMetadataProvider) GetName() string { return "cron_delete_job" }

// GetDescription 返回 cron_delete_job 工具描述
func (p *CronDeleteJobMetadataProvider) GetDescription(language string) string {
	if d, ok := cronDeleteJobDescription[language]; ok {
		return d
	}
	return cronDeleteJobDescription["cn"]
}

// GetInputParams 返回 cron_delete_job 工具参数 Schema
func (p *CronDeleteJobMetadataProvider) GetInputParams(language string) map[string]any {
	return GetCronDeleteJobInputParams(language)
}

// GetName 返回 cron_toggle_job 工具名称
func (p *CronToggleJobMetadataProvider) GetName() string { return "cron_toggle_job" }

// GetDescription 返回 cron_toggle_job 工具描述
func (p *CronToggleJobMetadataProvider) GetDescription(language string) string {
	if d, ok := cronToggleJobDescription[language]; ok {
		return d
	}
	return cronToggleJobDescription["cn"]
}

// GetInputParams 返回 cron_toggle_job 工具参数 Schema
func (p *CronToggleJobMetadataProvider) GetInputParams(language string) map[string]any {
	return GetCronToggleJobInputParams(language)
}

// GetName 返回 cron_preview_job 工具名称
func (p *CronPreviewJobMetadataProvider) GetName() string { return "cron_preview_job" }

// GetDescription 返回 cron_preview_job 工具描述
func (p *CronPreviewJobMetadataProvider) GetDescription(language string) string {
	if d, ok := cronPreviewJobDescription[language]; ok {
		return d
	}
	return cronPreviewJobDescription["cn"]
}

// GetInputParams 返回 cron_preview_job 工具参数 Schema
func (p *CronPreviewJobMetadataProvider) GetInputParams(language string) map[string]any {
	return GetCronPreviewJobInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	RegisterToolProvider(&CronMetadataProvider{})
	RegisterToolProvider(&CronListJobsMetadataProvider{})
	RegisterToolProvider(&CronGetJobMetadataProvider{})
	RegisterToolProvider(&CronCreateJobMetadataProvider{})
	RegisterToolProvider(&CronUpdateJobMetadataProvider{})
	RegisterToolProvider(&CronDeleteJobMetadataProvider{})
	RegisterToolProvider(&CronToggleJobMetadataProvider{})
	RegisterToolProvider(&CronPreviewJobMetadataProvider{})
}
