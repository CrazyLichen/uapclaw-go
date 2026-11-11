package recommendation

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	skillruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RecommendationDecision 引擎决定主动告知用户的内容。
// Python: proactive_actions.py:89 RecommendationDecision
type RecommendationDecision struct {
	// Type 推荐类型：skill_recommend / task_reminder / need_exploration
	Type string
	// Target skill 名、待办描述或探索方向
	Target string
	// Reason 内部原因（供 LLM 生成话术用）
	Reason string
	// Urgency 紧急度 0.0..1.0
	Urgency float64
}

// AnalysisResult 统一 LLM 分析调用的输出。
// Python: proactive_actions.py:282 AnalysisResult
type AnalysisResult struct {
	// Decision 本轮决策；nil 表示无推荐
	Decision *RecommendationDecision
}

// ProactiveTriggerRequest 一次主动推荐触发请求的具名参数封装。
//
// 把 session_id / query / decision / channel_id / on_delivered / rec_id /
// style_rules_section 这一组「本次推荐请求」相关参数封装到一起，
// 让 TriggerMainAgent 与 triggerCallback 的签名都降到 2 参。
// server / triggerCallback 作为运行时依赖不入此结构。
// Query 可由 TriggerMainAgent 内部用 directive prompt 拼好后填入，故外部构造时允许为空。
//
// Python: server/runtime/proactive_adapter.py:66 ProactiveTriggerRequest
type ProactiveTriggerRequest struct {
	// SessionID 目标会话标识
	SessionID string
	// Decision 本轮决策
	Decision *RecommendationDecision
	// ChannelID 目标通道，空表示用最活跃会话
	ChannelID string
	// Query 指令式 query（由 TriggerMainAgent 填充）
	Query string
	// OnDelivered 真正送达后的回调，参数为主 agent 生成的话术全文
	OnDelivered func(generatedMessage string)
	// RecID 推荐唯一标识（供前端反馈关联）
	RecID string
	// StyleRulesSection 话术层梯度，注入 directive prompt
	StyleRulesSection string
}

// ProactiveTriggerFunc 触发主 agent 跑一轮的回调，由 server 侧注入。
// 返回 true 表示已触发（后台异步跑），false 表示 session 忙 / 投递失败。
//
// Python: proactive_engine.py:279 set_trigger_main_agent_callback 注入的回调
type ProactiveTriggerFunc func(ctx context.Context, request *ProactiveTriggerRequest) (bool, error)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// proactiveIntLo proactive 数值配置项合法区间下界
	// Python: proactive_actions.py 未定义；proactive_engine.py:48 _PROACTIVE_INT_LO
	proactiveIntLo = 1
	// proactiveIntHi proactive 数值配置项合法区间上界
	// Python: proactive_engine.py:49 _PROACTIVE_INT_HI
	proactiveIntHi = 50
	// dailyCountRetentionDays 每日计数保留天数
	// Python: proactive_actions.py:56 timedelta(days=2)
	dailyCountRetentionDays = 2
	// defaultUrgency 紧急度默认值
	// Python: proactive_actions.py:73 default: float = 0.5
	defaultUrgency = 0.5
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// dailyCountsMu 保护 dailyCounts
	// Python: proactive_actions.py:32 _daily_counts_lock
	dailyCountsMu sync.Mutex
	// dailyCounts 日期（YYYY-MM-DD，UTC）→ 当日推荐次数
	// Python: proactive_actions.py:33 _daily_counts
	dailyCounts = make(map[string]int)
	// proactiveIntPattern 纯数字校验（挡住浮点/科学计数/字符串/空）
	// Python: proactive_engine.py:61 re.fullmatch(r"[0-9]+", raw)
	proactiveIntPattern = regexp.MustCompile(`^[0-9]+$`)
	// skillFrontmatterPattern SKILL.md frontmatter 与正文的切分
	// Python: proactive_actions.py:106 re.match(r"^---\s*\n(.*?)\n---\s*\n?(.*)", DOTALL)
	skillFrontmatterPattern = regexp.MustCompile(`(?s)^---\s*\n(.*?)\n---\s*\n?(.*)`)
	// skillFrontmatterLinePattern frontmatter 降级解析的逐行键值
	// Python: proactive_actions.py:121 re.match(r"^(\w[\w_-]*)\s*:\s*(.*)")
	skillFrontmatterLinePattern = regexp.MustCompile(`^(\w[\w_-]*)\s*:\s*(.*)`)
	// jsonFencePattern 提取 ```json 代码块
	// Python: proactive_actions.py:226 re.search(r"```(?:json)?\s*\n(.*?)\n```", DOTALL)
	jsonFencePattern = regexp.MustCompile("(?s)```(?:json)?\\s*\\n(.*?)\\n```")
	// jsonBracePattern 贪婪提取花括号块（注意：与 gradient_updater 的 raw_decode 版本是两套实现，不可合并）
	// Python: proactive_actions.py:229 re.search(r"\{.*\}", DOTALL)
	jsonBracePattern = regexp.MustCompile(`(?s)\{.*\}`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SafeProactiveInt 读取 proactive 数值配置项的防御性解析。
//
// config.yaml 被手改成非数字/浮点/超范围时，降级用 defaultValue + 告警，不让引擎崩溃。
// 先按字符串校验：挡住浮点(3.5)、科学计数(1e5)、字符串(abc)、空。
// Python int(3.5) 会静默截断成 3，不能直接转。
//
// Python: proactive_engine.py:52 _safe_proactive_int
func SafeProactiveInt(val any, defaultValue int) int {
	raw := ""
	if val != nil {
		raw = strings.TrimSpace(fmt.Sprintf("%v", val))
	}
	if !proactiveIntPattern.MatchString(raw) {
		logger.Warn(logComponent).Str("value", raw).Int("default", defaultValue).
			Msg("invalid int config, fallback to default")
		return defaultValue
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		logger.Warn(logComponent).Str("value", raw).Int("default", defaultValue).
			Msg("invalid int config, fallback to default")
		return defaultValue
	}
	if n < proactiveIntLo || n > proactiveIntHi {
		logger.Warn(logComponent).Str("value", raw).Int("lo", proactiveIntLo).
			Int("hi", proactiveIntHi).Int("default", defaultValue).
			Msg("int config out of range, fallback to default")
		return defaultValue
	}
	return n
}

// SafeUrgency 解析紧急度到 [0,1]；失败返回默认值。
//
// LLM 可能把 urgency 输出成字符串（"0.8"）或非数值；裸 float() 会抛异常并丢掉整个决策
// （type/target/reason 全丢）。
//
// Python: proactive_actions.py:73 _safe_urgency
func SafeUrgency(value any, defaultValue float64) float64 {
	var f float64
	switch typed := value.(type) {
	case float64:
		f = typed
	case float32:
		f = float64(typed)
	case int:
		f = float64(typed)
	case int64:
		f = float64(typed)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return defaultValue
		}
		f = parsed
	default:
		return defaultValue
	}
	// Python: if f != f or f < 0.0 —— NaN 或负数
	if math.IsNaN(f) || f < 0.0 {
		return defaultValue
	}
	// Python: return min(f, 1.0)
	return math.Min(f, 1.0)
}

// ParseProactiveSkillMd 解析 SKILL.md，提取 frontmatter 与正文摘要。
//
// 与 skill 包的 parseSKILLMd 是两套实现：本函数只取 name/description，
// 且 frontmatter 解析失败时降级为逐行键值解析（Python 同源实现）。
//
// Python: proactive_actions.py:100 _parse_skill_md
func ParseProactiveSkillMd(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := string(data)
	meta := map[string]any{}
	if m := skillFrontmatterPattern.FindStringSubmatch(text); m != nil {
		body := strings.TrimSpace(m[2])
		// Python: yaml.safe_load(frontmatter) 失败/非 dict 时降级逐行解析
		if loaded, ok := parseSimpleYAMLMap(m[1]); ok {
			for k, v := range loaded {
				meta[k] = v
			}
		}
		if len(meta) == 0 {
			for _, line := range strings.Split(m[1], "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				if kv := skillFrontmatterLinePattern.FindStringSubmatch(line); kv != nil {
					meta[kv[1]] = strings.Trim(strings.TrimSpace(kv[2]), `'"`)
				}
			}
		}
		if _, exists := meta["name"]; !exists {
			meta["name"] = stemOf(path)
		}
		if _, exists := meta["description"]; !exists {
			meta["description"] = truncateRunes(body, 500)
		}
	} else {
		meta["name"] = stemOf(path)
		meta["description"] = truncateRunes(text, 500)
	}
	return meta
}

// DetectAllSkills 扫描磁盘发现可用 skill，过滤为「当下真正能用」的集合。
//
// 只暴露已安装且 enabled=true 的 skill：
//   - 未安装：已卸载的目录不在磁盘；builtin 目录里尚未安装的跳过（installed=false）
//   - 已安装但被禁用：skill 级禁用（enabled=false）与「插件禁用但未 reload」两种状态
//     只动 state、目录仍在原位，磁盘扫描会照扫到，故交叉 state 剔除
//   - 插件级禁用并 reload 过的目录被物理移进 `_disabled_plugins`（下划线开头），已跳过
//
// 返回 (已安装技能名集合, 候选 skill 列表)。
//
// Python: proactive_actions.py:133 _get_all_skills（无参，内部取 get_agent_skills_dir）
func DetectAllSkills() (map[string]struct{}, []map[string]any) {
	return detectAllSkillsIn(workspace.AgentSkillsDir(), getBuiltinSkillsDirForRecommendation())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadDisabledSkills 读取已禁用技能名单（供测试注入）。
// Python: skilldev.load_execution_disabled_skills()
var loadDisabledSkills = skillruntime.LoadExecutionDisabledSkills

// detectAllSkillsIn 在指定目录组合上执行技能发现，便于测试注入隔离目录。
//
// 单列出来是因为 workspace.AgentSkillsDir() 底层 cache 了首个取值，
// 测试无法用环境变量重定向；注入目录让候选池/禁用过滤逻辑可被独立验证。
func detectAllSkillsIn(agentSkillsDir, builtinDir string) (map[string]struct{}, []map[string]any) {
	skills := make([]map[string]any, 0)
	seen := make(map[string]struct{})

	type dirSource struct {
		dir         string
		source      string
		isInstalled bool
	}
	sources := []dirSource{
		{dir: agentSkillsDir, source: "local", isInstalled: true},
		{dir: builtinDir, source: "builtin", isInstalled: false},
	}

	for _, src := range sources {
		if src.dir == "" {
			continue
		}
		entries, err := os.ReadDir(src.dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
				continue
			}
			mdPath := filepath.Join(src.dir, entry.Name(), "SKILL.md")
			if _, statErr := os.Stat(mdPath); statErr != nil {
				continue
			}
			meta := ParseProactiveSkillMd(mdPath)
			if meta == nil {
				continue
			}
			name, _ := meta["name"].(string)
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			if name == "" {
				meta["name"] = entry.Name()
			}
			meta["installed"] = src.isInstalled
			if _, exists := meta["source"]; !exists {
				meta["source"] = src.source
			}
			if _, exists := meta["description"]; !exists {
				if body, ok := meta["body"].(string); ok {
					meta["description"] = body
				}
			}
			delete(meta, "body")
			skills = append(skills, meta)
		}
	}

	// 只推「能用」的：候选池与校验集合都只保留已安装的 skill
	installedSkills := make([]map[string]any, 0, len(skills))
	for _, s := range skills {
		if installed, _ := s["installed"].(bool); installed {
			installedSkills = append(installedSkills, s)
		}
	}

	// 交叉 state：剔除已安装但被禁用的 skill
	if disabled := loadDisabledSkills(); len(disabled) > 0 {
		disabledSet := make(map[string]struct{}, len(disabled))
		for _, name := range disabled {
			disabledSet[name] = struct{}{}
		}
		filtered := installedSkills[:0]
		for _, s := range installedSkills {
			name, _ := s["name"].(string)
			if _, isDisabled := disabledSet[name]; isDisabled {
				continue
			}
			filtered = append(filtered, s)
		}
		installedSkills = filtered
	}

	installedNames := make(map[string]struct{}, len(installedSkills))
	for _, s := range installedSkills {
		if name, ok := s["name"].(string); ok && name != "" {
			installedNames[name] = struct{}{}
		}
	}
	return installedNames, installedSkills
}

// ──────────────────────────── 导出函数（续）────────────────────────────

// ExtractOutputText 从 DeepAgent.Invoke 结果中提取模型文本输出。
//
// invoke 返回形如 {"output": <str>, "result_type": ...} 的 map。
// 严格只读 output；缺失或类型不符返回空串，让调用方当作「无决策」，
// 而不是误读 result_type（"answer"）当作输出导致 JSON 解析失败。
//
// Python: proactive_actions.py:202 _extract_output_text
func ExtractOutputText(invokeResult map[string]any) string {
	if invokeResult == nil {
		return ""
	}
	output, exists := invokeResult["output"]
	if !exists {
		return ""
	}
	switch typed := output.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	default:
		// 不兜底扫其他值——result_type 也是 str，误取会解析失败
		return ""
	}
}

// ExtractJSONFromResponse 从 LLM 响应中提取 JSON 字符串（支持 markdown 代码块）。
//
// 注意：本函数用**贪婪正则** `\{.*\}` 兜底；gradient_updater 用的是
// `json.Decoder.raw_decode` 版本（更严格，能忽略尾部脏文本）。两套实现语义不同，
// 对齐上游保持各自独立，**禁止合并**。
//
// Python: proactive_actions.py:225 _extract_json_from_response
func ExtractJSONFromResponse(text string) string {
	if m := jsonFencePattern.FindStringSubmatch(text); m != nil {
		return strings.TrimSpace(m[1])
	}
	if m := jsonBracePattern.FindString(text); m != "" {
		return strings.TrimSpace(m)
	}
	return ""
}

// AnalyzeAndDecide 通过 proactive agent 单次调用决定是否推荐。
//
// proactiveAgent 是轻量 DeepAgent（无 tools、无 task_loop、单轮），由
// BuildProactiveAgent 构造。走 agent 框架的 invoke 链路（rails / 模型选择 / 观测），
// 不手搓 Model/SystemMessage/UserMessage。
//
// reportText 是已渲染完整的情境报告（来自 SituationReport.RenderForLLM），
// 已含用户侧信息、对话摘要、历史推荐、待办承诺、日程与候选 skill，
// 故 prompt 只取 conversationSummary 一个占位。
//
// decisionRulesText 是决策层策略梯度（target/relation），注入分析 prompt 影响
// type/target/reason 的选择。
//
// language 取 config.yaml 的 preferred_language，选择 ZH/EN 模板。
//
// Python: proactive_actions.py:287 _analyze_and_decide
func AnalyzeAndDecide(
	ctx context.Context,
	reportText string,
	skills []map[string]any,
	proactiveAgent *harness.DeepAgent,
	decisionRulesText string,
	language string,
) *AnalysisResult {
	if decisionRulesText == "" {
		decisionRulesText = "（暂无）"
	}
	prompt := RenderPromptTemplate(UnifiedAnalysisPrompt(language), map[string]string{
		"conversation_summary": reportText,
		"decision_rules_text":  decisionRulesText,
	})

	// conversation_id 每次 tick 用唯一值：固定 id 会让 DeepAgent 的 context engine
	// 累积历次 tick 的 prompt+response（实测累积到 2904 条消息），导致决策极慢/超时。
	convID := fmt.Sprintf("proactive_analysis_%d", time.Now().UnixMilli())

	// 决策是一次性的：invoke 完立即释放 checkpointer 持久化的 session 记录，
	// 避免孤儿数据随每次 tick 增长。proactive_agent 无状态，不需跨 tick 保留 context。
	defer func() {
		if err := checkpointer.GetCheckpointer().Release(ctx, convID); err != nil {
			logger.Debug(logComponent).Err(err).Str("conversation_id", convID).
				Msg("checkpoint release failed")
		}
	}()

	result, err := proactiveAgent.Invoke(ctx, map[string]any{
		"query":           prompt,
		"conversation_id": convID,
	})
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("LLM analysis failed")
		return &AnalysisResult{}
	}

	content := ExtractOutputText(result)
	jsonStr := ExtractJSONFromResponse(content)
	if jsonStr == "" {
		return &AnalysisResult{}
	}

	var delta map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &delta); err != nil {
		return &AnalysisResult{}
	}
	if delta == nil {
		return &AnalysisResult{}
	}

	// 画像已废弃——所有推荐基于当前对话，只取 decision
	var decision *RecommendationDecision
	if rawDecision, ok := delta["decision"].(map[string]any); ok {
		decType, _ := rawDecision["type"].(string)
		decTarget, _ := rawDecision["target"].(string)
		if decType != "" && decTarget != "" {
			if decType == "skill_recommend" {
				// 校验：skill_recommend 的 target 必须是真实存在的 skill。
				// LLM 可能推荐历史对话里提过但已卸载的 skill，须拒绝。
				if !isValidSkillTarget(decTarget, skills) {
					logger.Warn(logComponent).Str("target", decTarget).
						Msg("decision rejected: skill_recommend target not in available skills")
					decision = nil
				} else {
					decision = buildDecision(rawDecision, decType, decTarget)
				}
			} else {
				decision = buildDecision(rawDecision, decType, decTarget)
			}
		}
	}

	if decision != nil {
		logger.Info(logComponent).Str("decision", decision.Type+":"+decision.Target).
			Msg("analysis decision")
	} else {
		logger.Info(logComponent).Str("decision", "null").Msg("analysis decision")
	}

	return &AnalysisResult{Decision: decision}
}

// TriggerMainAgent 触发主 agent 跑一轮并生成推荐话术。
//
// 构造含决策（type/target/reason）的指令式 query，交给 triggerCallback
// （由 server 侧注入），驱动目标会话的 process_message_stream。主 agent 随后：
//   - 用自身口吻生成推荐话术 → 进其 context engine（save_contexts 持久化，后续对话可见）
//   - 通过正常 E2A 推送路径把输出流式推给前端
//
// 避让：triggerCallback 内部检查 is_deep_agent_executing_for_session，
// 目标 session 忙时返回 false（跳过本次 tick，下个 tick 再来）。
//
// Returns:
//
//	true 表示已触发（后台异步跑），false 表示 busy / 投递失败。
//
// Python: proactive_actions.py:398 _trigger_main_agent
func TriggerMainAgent(
	ctx context.Context,
	triggerCallback ProactiveTriggerFunc,
	request *ProactiveTriggerRequest,
) bool {
	if triggerCallback == nil || request == nil {
		return false
	}
	decision := request.Decision
	if decision == nil {
		return false
	}

	// 指令语言跟随 config.yaml preferred_language——en 时模型收到的整条指令都是英文，
	// 产出话术才是英文。此前指令硬编码中文，信封里 preferred_response_language=en
	// 的权重压不过整段中文指令，导致「agent 设为英文、推荐仍推中文」。
	query := RenderPromptTemplate(DirectivePrompt(PreferredLanguage()), map[string]string{
		"rec_type":            decision.Type,
		"target":              decision.Target,
		"reason":              decision.Reason,
		"style_rules_section": request.StyleRulesSection,
	})
	request.Query = query

	triggered, err := triggerCallback(ctx, request)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("trigger_main_agent failed")
		return false
	}
	return triggered
}

// TodayRecommendCount 返回当日（UTC）已推荐次数。
// Python: proactive_actions.py:41 _today_recommend_count
func TodayRecommendCount() int {
	dailyCountsMu.Lock()
	defer dailyCountsMu.Unlock()
	return dailyCounts[todayKey()]
}

// IncrementDailyCount 当日计数 +1 并返回新值。
// Python: proactive_actions.py:46 _increment_daily_count
func IncrementDailyCount() int {
	dailyCountsMu.Lock()
	defer dailyCountsMu.Unlock()
	key := todayKey()
	dailyCounts[key]++
	return dailyCounts[key]
}

// PruneDailyCounts 清理 2 天前的计数 key。
// Python: proactive_actions.py:53 _prune_daily_counts
func PruneDailyCounts() {
	cutoff := time.Now().UTC().AddDate(0, 0, -dailyCountRetentionDays).Format("2006-01-02")
	dailyCountsMu.Lock()
	defer dailyCountsMu.Unlock()
	for key := range dailyCounts {
		if key < cutoff {
			delete(dailyCounts, key)
		}
	}
}

// ResetDailyCounts 清空每日计数（仅测试用；Python 无对应符号）。
func ResetDailyCounts() {
	dailyCountsMu.Lock()
	defer dailyCountsMu.Unlock()
	dailyCounts = make(map[string]int)
}

// PreferredLanguage 读取 config.yaml 的 preferred_language，缺省 "zh"。
//
// 本 tick 的语言基准：决策/指令/梯度渲染共用，与正常聊天的回复语言同源（改设置即时生效）。
// Python: proactive_engine.py:396 str(get_config().get("preferred_language") or "zh")
func PreferredLanguage() string {
	return preferredLanguageFrom(loadRecommendationConfig)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadRecommendationConfig 加载 config.yaml（对齐 Python get_config()）。
// 抽成变量便于测试注入，覆盖加载失败分支。
var loadRecommendationConfig = func() (map[string]any, error) {
	c, err := config.New("", config.WithNormalize(config.NormalizeConfig))
	if err != nil {
		return nil, err
	}
	return c.Load()
}

// preferredLanguageFrom 从配置读取语言，任一步失败回落 "zh"。
func preferredLanguageFrom(load func() (map[string]any, error)) string {
	cfgMap, err := load()
	if err != nil {
		return "zh"
	}
	if lang, ok := cfgMap["preferred_language"].(string); ok && lang != "" {
		return lang
	}
	return "zh"
}

// todayKey 返回当日 UTC 日期键。
// Python: proactive_actions.py:36-38 _today_key
func todayKey() string {
	return time.Now().UTC().Format("2006-01-02")
}

// buildDecision 由原始 map 构造决策对象。
// Python: proactive_actions.py:362-374 RecommendationDecision(...)
func buildDecision(raw map[string]any, decType, decTarget string) *RecommendationDecision {
	reason, _ := raw["reason"].(string)
	urgencyRaw, exists := raw["urgency"]
	if !exists {
		urgencyRaw = defaultUrgency
	}
	return &RecommendationDecision{
		Type:    decType,
		Target:  decTarget,
		Reason:  reason,
		Urgency: SafeUrgency(urgencyRaw, defaultUrgency),
	}
}

// isValidSkillTarget 校验 target 是否在候选 skill 名单里。
// Python: proactive_actions.py:353-360 valid_skill_names 校验
func isValidSkillTarget(target string, skills []map[string]any) bool {
	for _, s := range skills {
		if name, ok := s["name"].(string); ok && name == target {
			return true
		}
	}
	return false
}

// parseSimpleYAMLMap 解析单层 key: value 的 YAML 片段。
//
// 只覆盖 frontmatter 常见的扁平形态（Python 侧用 yaml.safe_load）；
// 解析失败或非扁平结构返回 ok=false，让调用方降级为逐行解析。
func parseSimpleYAMLMap(frontmatter string) (map[string]any, bool) {
	result := make(map[string]any)
	for _, line := range strings.Split(frontmatter, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// 嵌套/列表结构不支持，交给降级路径
		if strings.HasPrefix(trimmed, "-") || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			return nil, false
		}
		kv := skillFrontmatterLinePattern.FindStringSubmatch(trimmed)
		if kv == nil {
			return nil, false
		}
		result[kv[1]] = strings.Trim(strings.TrimSpace(kv[2]), `'"`)
	}
	if len(result) == 0 {
		return nil, false
	}
	return result, true
}

// stemOf 返回路径的文件名主干（去扩展名）。
// Python: Path.stem
func stemOf(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// truncateRunes 按 rune 截断到 n 个字符（对齐 Python 的 s[:500] 字符语义，不切坏多字节）。
func truncateRunes(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n])
}

// getBuiltinSkillsDirForRecommendation 返回包内置技能源目录。
//
// 逻辑与 skill 包的 getBuiltinSkillsDir 一致（BUILTIN_SKILLS_DIR 已移除，
// 只用 ResourcesDir 磁盘三级回退 + 两级 primary/fallback）：
//   - primary  = <resources>/agent/workspace/skills
//   - fallback = <resources>/agent/skills（不检查存在性，交由调用方判定）
//
// Python: common/utils.py:2388 get_builtin_skills_dir
func getBuiltinSkillsDirForRecommendation() string {
	resDir, err := pathutil.ResourcesDir()
	if err != nil {
		return ""
	}
	primary := filepath.Join(resDir, "agent", "workspace", "skills")
	if info, statErr := os.Stat(primary); statErr == nil && info.IsDir() {
		return primary
	}
	return filepath.Join(resDir, "agent", "skills")
}
