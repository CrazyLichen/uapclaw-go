package tools

// ──────────────────────────── 全局变量 ────────────────────────────

// cronDescription cron 工具双语描述
var cronDescription = map[string]string{
	"cn": `使用 action 接口：status、list、add、update、remove、run、runs、wake，并兼容结构化 schedule/payload/delivery 字段。
处理"2分钟后""明天上午9点""下周一"这类时间时，优先根据系统提示中已提供的当前日期与时间直接换算并调用 cron，不要为了简单的时间换算先调用 code 或 bash。
创建一次性提醒时，schedule.at 默认直接使用用户当前本地时区偏移来写，例如 +08:00；除非用户明确要求，否则不要改写成 Z 或 UTC。
给当前聊天创建提醒时，优先使用 payload.kind=systemEvent 和 sessionTarget=current。
向用户确认创建结果时，优先按 schedule.at 里的原始时区/偏移表述，不要自行改写成 UTC。

【投递频道】delivery.channel / targets：用户未明确指定时不填，系统自动使用当前对话渠道；**禁止从历史记录推断。**

【重要：cron 表达式格式】只支持7段式(Quartz格式)：秒 分 时 日 月 周 年。
字段取值范围：秒(0-59)，分(0-59)，时(0-23)，日(1-31)，月(1-12)，周(1-7或?)，年(1970-2099或*)。
日和周字段：不能同时指定具体值，其中一个必须用?表示'不指定'。
年份字段：*表示跨年周期，固定年份只在该年执行。
真正只执行一次：所有字段均为固定值(无*和?)，如'0 30 17 29 4 ? 2026'表示2026年4月29日17:30:00执行一次。
例：每天9点 -> '0 0 9 * * ? *'；每15分钟 -> '0 */15 * * * ? *'；每周一9点 -> '0 0 9 ? * MON *'。
注意：一次性任务建议优先使用 schedule.at (ISO8601格式)，cron更适合周期性任务。

【重要：cron 表达式限制】标准 cron 的 */X 语义是'当字段值能被 X 整除时触发'，而非'每隔 X 单位触发'。只有当周期单位能被 X 整除时，间隔才是均匀的。以下是各字段的限制：
- 秒/分(0-59)：*/X 仅支持 X 整除60的值：1/2/3/4/5/6/10/12/15/20/30。
  例如 */40 实际在每小时第0分和第40分触发（间隔40分→20分交替），并非每40分钟。
  用户要求'每隔40分钟'时，必须先告知此限制并让用户确认是否接受不均匀间隔，或建议改用整除60的间隔（如20分钟或30分钟）。未经用户确认不得直接创建。
- 小时(0-23)：*/X 仅支持 X 整除24的值：1/2/3/4/6/8/12。
  例如 */5 实际在每天0/5/10/15/20时触发（间隔5h→4h→5h→4h交替），并非每5小时。
  用户要求'每隔5小时'时，必须告知限制并让用户确认，或建议改用整除24的间隔。
- 日(1-31)：*/X 不可靠，因为不同月份天数不同（28/29/30/31）。
  例如 */15 在2月只触发1、15日（共2次），在31天月份触发1、16、31日（共3次）。
  用户要求'每隔X天'时，建议改用'每周X'或指定固定日期（如每月1号、15号）。
- 月(1-12)：*/X 仅支持 X 整除12的值：1/2/3/4/6。
  例如 */5 实际在1/5/10月触发，并非每5个月均匀触发。
- 周(1-7)：*/X 仅支持 X 整除7的值：1/7。1=SUN,7=SAT。
  例如 */2 实际在SUN/TUE/THU触发，并非'每隔2周'。
  用户要求'每隔2周'时，应直接指定具体星期几或建议简化为'每周一'。
处理'每隔X分钟/小时/天'需求时，务必检查 X 是否整除对应周期单位；若不整除，必须告知用户限制，让用户确认后再创建，或建议替代方案。`,
	"en": `Use the cron action interface. Supports status, list, add, update, remove, run, runs, and wake using structured schedule/payload/delivery fields.
For requests like 'in 2 minutes', 'tomorrow at 9am', or 'next Monday', prefer converting the time directly from the current date/time already provided in the system prompt and call cron directly instead of using code or bash for simple time math.
When creating one-shot reminders, write schedule.at using the user's current local timezone offset directly, for example +08:00; unless the user explicitly asks for it, do not rewrite it into Z or UTC.
For reminders targeting the current chat, prefer payload.kind=systemEvent with sessionTarget=current.
When confirming a created reminder to the user, prefer the original timezone/offset from schedule.at instead of rewriting it into UTC.

[Delivery Channel] delivery.channel / targets: leave empty unless user explicitly specifies; system uses current channel. **Never infer from history.**

[CRITICAL: Cron Expression Format] Only supports 7-field Quartz format: second minute hour day month dow year.
Field ranges: second(0-59), minute(0-59), hour(0-23), day(1-31), month(1-12), dow(1-7 or ?), year(1970-2099 or *).
Day and dow fields: cannot both have specific values; one must be '?' (no specific value).
Year field: '*' for recurring, fixed year for one-shot within that year.
True one-shot: all fields fixed (no '*' or '?'), e.g. '0 30 17 29 4 ? 2026' runs once at 2026-04-29 17:30:00.
Examples: daily 9am -> '0 0 9 * * ? *'; every 15min -> '0 */15 * * * ? *'; every Monday 9am -> '0 0 9 ? * MON *'.
Note: for one-shot tasks, prefer schedule.at (ISO8601 format); cron is better for recurring tasks.

[CRITICAL: Cron Expression Limits] Standard cron's */X means 'trigger when the field value is divisible by X', NOT 'every X units'. Uniform intervals only work when the cycle unit is divisible by X. Field limits:
- Second/Minute(0-59): */X only works for X dividing 60: 1/2/3/4/5/6/10/12/15/20/30.
  Example: */40 triggers at minute 0 and 40 each hour (alternating 40min-20min gaps), NOT every 40 minutes.
  When user requests 'every 40 minutes', MUST inform user of this limitation first and let user confirm whether to accept uneven intervals, or suggest intervals that divide 60 (e.g. 20 or 30 minutes). Do NOT create without user confirmation.
- Hour(0-23): */X only works for X dividing 24: 1/2/3/4/6/8/12.
  Example: */5 triggers at hours 0/5/10/15/20 (alternating 5h-4h gaps), NOT every 5 hours.
  When user requests 'every 5 hours', MUST inform and let user confirm, or suggest 4 or 6 hours.
- Day(1-31): */X is unreliable due to varying month lengths (28/29/30/31 days).
  Example: */15 triggers on day 1,15 in Feb (2 times), but 1,16,31 in 31-day months (3 times).
  When user requests 'every X days', suggest using 'every week on day X' or fixed dates.
- Month(1-12): */X only works for X dividing 12: 1/2/3/4/6.
  Example: */5 triggers in Jan/May/Oct, NOT uniformly every 5 months.
- Dow(1-7): */X only works for X dividing 7: 1/7. 1=SUN, 7=SAT.
  Example: */2 triggers on SUN/TUE/THU, NOT 'every 2 weeks'.
  When user requests 'every 2 weeks', suggest simplifying to a specific weekday.
When handling 'every X minutes/hours/days' requests, always check if X divides the cycle unit. If not, MUST inform user of the limitation, let user confirm before creating, or suggest alternatives.`,
}

// ──────────────────────────── 结构体 ────────────────────────────

// CronMetadataProvider cron 工具元数据提供者
type CronMetadataProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetCronMetadataProviderInputParams 构建 cron 工具的参数 Schema
func GetCronMetadataProviderInputParams(language string) map[string]any {
	lang := language; if lang != "cn" && lang != "en" { lang = "cn" }
	p := map[string]map[string]string{
		"action":           {"cn": "要执行的 cron 操作", "en": "Cron action to execute"},
		"job":              {"cn": "用于 add 的任务对象；支持结构化字段和兼容层字段", "en": "Job object for add; supports structured fields and compatibility fields"},
		"jobId":            {"cn": "用于 update/remove/run/runs 的任务 ID", "en": "Job id used by update/remove/run/runs"},
		"patch":            {"cn": "用于 update 的补丁对象", "en": "Patch object used by update"},
		"includeDisabled":  {"cn": "list 时是否包含已禁用任务", "en": "Whether list should include disabled jobs"},
		"text":             {"cn": "wake 动作要发送的提示文本", "en": "Wake text to inject for action=wake"},
		"mode":             {"cn": "wake 的触发模式", "en": "Wake delivery mode"},
		"contextMessages":  {"cn": "保留给上下文提示的兼容字段", "en": "Reserved compatibility field for context hints"},
	}
	d := func(key string) string { if v, ok := p[key][lang]; ok { return v }; return p[key]["cn"] }
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":          map[string]any{"type": "string", "enum": []any{"status", "list", "add", "update", "remove", "run", "runs", "wake"}, "description": d("action")},
			"job":             map[string]any{"type": "object", "description": d("job"), "additionalProperties": true},
			"jobId":           map[string]any{"type": "string", "description": d("jobId")},
			"patch":           map[string]any{"type": "object", "description": d("patch"), "additionalProperties": true},
			"includeDisabled": map[string]any{"type": "boolean", "description": d("includeDisabled")},
			"text":            map[string]any{"type": "string", "description": d("text")},
			"mode":            map[string]any{"type": "string", "enum": []any{"now", "next-heartbeat"}, "description": d("mode")},
			"contextMessages": map[string]any{"type": "integer", "description": d("contextMessages")},
		},
		"required":          []any{"action"},
		"additionalProperties": true,
	}
}

func (p *CronMetadataProvider) GetName() string { return "cron" }
func (p *CronMetadataProvider) GetDescription(language string) string {
	if d, ok := cronDescription[language]; ok { return d }
	return cronDescription["cn"]
}
func (p *CronMetadataProvider) GetInputParams(language string) map[string]any {
	return GetCronMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() { RegisterToolProvider(&CronMetadataProvider{}) }
