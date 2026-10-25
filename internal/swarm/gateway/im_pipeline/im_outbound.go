package im_pipeline

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// IMOutboundPipeline 出站预处理管线：路由决策（群发 vs 私发）+ 追问前缀解析。
// 在 MessageHandler.PublishRobotMessages 入队前拦截执行。
// Python: im_outbound.py L53-441 (IMOutboundPipeline)
type IMOutboundPipeline struct {
	// mu adapters 并发保护
	mu sync.RWMutex
	// adapters 适配器注册表：channel_id → adapter
	adapters map[string]IMPlatformAdapter
	// llm 懒加载 LLM 实例（ensureLLM 失败/跳过恒 nil）
	llm *llm.Model
	// llmModelName LLM 模型名（init 成功时记录）
	llmModelName string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// classifyPromptTemplate DM/CHAT 分类提示词（Python 原文逐字）。
// Python: im_outbound.py L395-403
const classifyPromptTemplate = "请判断下面这条%[1]s机器人回复，是否应该私发给特定用户，而不是直接回到群里。\n\n" +
	"判断标准：\n" +
	"- 如果内容是在为该用户记录待办、设置提醒、安排日程、私下跟进、单独通知，输出 DM\n" +
	"- 如果内容是在公开回复群讨论、解释问题、同步信息、继续群聊协作，输出 CHAT\n" +
	"- 只输出 DM 或 CHAT，不要输出其他内容\n\n" +
	"%[2]s" +
	"目标用户：%[3]s\n" +
	"机器人回复：\n%[4]s"

// ──────────────────────────── 全局变量 ────────────────────────────

// skipEventTypes 出站跳过的事件类型集合。
// Python: im_outbound.py L30-35 _SKIP_EVENT_TYPES frozenset
var skipEventTypes = map[string]bool{
	"chat.delta":       true,
	"chat.tool_call":   true,
	"chat.tool_result": true,
	"todo.updated":     true,
}

// groupAckKeywords 群聊 ack 关键词（9 个，Python 逐字）。
// Python: im_outbound.py L37-47 _GROUP_ACK_KEYWORDS
var groupAckKeywords = []string{"待办", "提醒", "定时", "日程", "会议", "行程", "安排", "记下了", "记住了"}

// reGroupFollowup 群聊追问前缀（.+? 非贪婪，RE2 原生支持）。
// Python: im_outbound.py L49 _RE_GROUP_FOLLOWUP
var reGroupFollowup = regexp.MustCompile(`^\[群聊追问(?:@(.+?))?\]\s*`)

// reDMFollowup 私聊追问前缀。
// Python: im_outbound.py L50 _RE_DM_FOLLOWUP
var reDMFollowup = regexp.MustCompile(`^\[私聊追问\]\s*`)

// reWhitespace 空白移除（关键词兜底用）。
// Python: im_outbound.py L441 re.sub(r"\s+", "", ...)
var reWhitespace = regexp.MustCompile(`\s+`)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewIMOutboundPipeline 创建出站管线（空注册表）。
// Python: im_outbound.py L56-59 __init__
func NewIMOutboundPipeline() *IMOutboundPipeline {
	return &IMOutboundPipeline{adapters: map[string]IMPlatformAdapter{}}
}

// RegisterAdapter 注册适配器。
// Python: im_outbound.py L61-62 register_adapter
func (p *IMOutboundPipeline) RegisterAdapter(channelID string, adapter IMPlatformAdapter) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.adapters == nil {
		p.adapters = map[string]IMPlatformAdapter{}
	}
	p.adapters[channelID] = adapter
}

// UnregisterAdapter 注销适配器（不存在容忍）。
// Python: im_outbound.py L64-65 unregister_adapter
func (p *IMOutboundPipeline) UnregisterAdapter(channelID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.adapters, channelID)
}

// Apply 对出站消息执行路由决策，原地修改 msg.metadata/payload。
// 常规短路分支返回 nil；error 仅记录不可恢复错误（当前恒 nil，挂接方统一
// 降级 Warn 不阻塞入队）。
// Python: im_outbound.py L128-230 apply（步骤顺序不可调换）：
//  1. 非数字分身；2. 跳过事件；3. 非群聊；4. 无适配器；5. 空 content；
//  6. 群聊追问 pending 清除（先于前缀解析）；7. 追问前缀解析；
//  8. reply_scope 已设/无候选；9. 继续日志；10. 关键词兜底；11. LLM 分类三路日志；
//  12. 兜底合并；13. false → 群聊；14. true → meta 四键升级 DM
func (p *IMOutboundPipeline) Apply(ctx context.Context, msg *schema.Message) error {
	meta := msg.Metadata
	if meta == nil {
		meta = map[string]any{}
	}

	// 步骤1：非数字分身（msg 标志或 metadata avatar_mode）
	isDigitalAvatar := msg.GroupDigitalAvatar || truthy(meta["avatar_mode"])
	if !isDigitalAvatar {
		return nil
	}

	// 步骤2：跳过事件类型
	payload := msg.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	eventType, _ := payload["event_type"].(string)
	if skipEventTypes[eventType] {
		return nil
	}

	// 步骤3：非群聊
	chatType, _ := meta["chat_type"].(string)
	if strings.TrimSpace(chatType) != "group" {
		return nil
	}

	// 步骤4：无适配器
	p.mu.RLock()
	adapter := p.adapters[msg.ChannelID]
	p.mu.RUnlock()
	if adapter == nil {
		return nil
	}

	// 步骤5：空内容
	content := extractContent(msg)
	if content == "" {
		return nil
	}

	// 步骤6：群聊追问 pending 清除（必须在追问前缀解析之前）
	answeredUserID, _ := meta["interaction_answered_user_id"].(string)
	answeredUserID = strings.TrimSpace(answeredUserID)
	if answeredUserID != "" {
		sessionID := strings.TrimSpace(msg.SessionID)
		if sessionID != "" {
			if existing := routing.FindGroupPending(sessionID, answeredUserID); existing != nil {
				existing.Remove()
				logger.Info(logComponent).
					Str("event_type", "clear_pending").
					Str("session_id", sessionID).
					Str("user_id", answeredUserID).
					Msg("cleared pending interaction")
			}
		}
	}

	// 步骤7：追问前缀解析（优先于原有路由逻辑）
	if groupMatch := reGroupFollowup.FindStringSubmatch(content); groupMatch != nil {
		p.handleGroupFollowup(msg, meta, adapter, content, groupMatch)
		return nil
	}
	if reDMFollowup.MatchString(content) {
		p.handleDMFollowup(msg, meta, adapter, content)
		return nil
	}

	// 步骤8：原有路由决策
	replyScope, _ := meta["reply_scope"].(string)
	if strings.TrimSpace(replyScope) != "" {
		return nil
	}
	candidateUserID := adapter.GetCandidateUserID(meta)
	if candidateUserID == "" {
		return nil
	}

	// 步骤9：继续处理日志
	logger.Info(logComponent).
		Str("event_type", "outbound_routing_continue").
		Str("channel_id", msg.ChannelID).
		Str("candidate", candidateUserID).
		Int("content_len", utf8.RuneCountInString(content)).
		Msg("outbound routing continue")

	// 步骤10：关键词兜底
	keywordHit := isPersonalActionReply(content)

	// 步骤11：LLM 分类（三路日志逐字段对齐）
	isPersonal, llmRaw := p.classifyPersonalAction(ctx, adapter.PlatformName(), meta, content)
	if isPersonal == nil {
		logger.Info(logComponent).
			Str("event_type", "llm_no_result").
			Str("channel_id", msg.ChannelID).
			Str("request_id", msg.ID).
			Bool("keyword_hit", keywordHit).
			Str("llm_output", llmRaw).
			Str("content_snippet", truncateStr(content, 100)).
			Msg("LLM no result, keyword fallback")
		v := keywordHit
		isPersonal = &v
	} else if *isPersonal {
		logger.Info(logComponent).
			Str("event_type", "llm_dm").
			Str("channel_id", msg.ChannelID).
			Str("request_id", msg.ID).
			Bool("keyword_hit", keywordHit).
			Str("llm_output", llmRaw).
			Str("content_snippet", truncateStr(content, 100)).
			Msg("LLM classified as DM")
	} else {
		logger.Info(logComponent).
			Str("event_type", "llm_chat").
			Str("channel_id", msg.ChannelID).
			Str("request_id", msg.ID).
			Bool("keyword_hit", keywordHit).
			Str("llm_output", llmRaw).
			Str("content_snippet", truncateStr(content, 100)).
			Msg("LLM classified as CHAT")
	}

	// 步骤12-13：false → 群聊不升级
	if !*isPersonal {
		logger.Info(logComponent).
			Str("event_type", "outbound_group_reply").
			Str("channel_id", msg.ChannelID).
			Str("request_id", msg.ID).
			Msg("classified as group chat, not upgrading to DM")
		return nil
	}

	// 步骤14：升级为私发（meta 四键）
	meta["reply_scope"] = "dm"
	meta[adapter.ReplyUserIDKey()] = candidateUserID
	replyReason := orStr(meta["reply_candidate_reason"])
	if replyReason == "" {
		replyReason = "processor_target_user"
	}
	meta["reply_reason"] = strings.TrimSpace(replyReason)
	meta["reply_personal_action"] = true
	msg.Metadata = meta

	logger.Info(logComponent).
		Str("event_type", "outbound_upgraded_dm").
		Str("channel_id", msg.ChannelID).
		Str("request_id", msg.ID).
		Str("reply_user_id_key", adapter.ReplyUserIDKey()).
		Msg("upgraded to DM")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractContent 提取出站回复文本。
// Python: im_outbound.py L360-371 _extract_content
func extractContent(msg *schema.Message) string {
	payload := msg.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	content, ok := payload["content"]
	if !ok {
		return ""
	}
	switch c := content.(type) {
	case string:
		return strings.TrimSpace(c)
	case map[string]any:
		for _, key := range []string{"output", "text", "message"} {
			if val, ok := c[key].(string); ok && strings.TrimSpace(val) != "" {
				return strings.TrimSpace(val)
			}
		}
		return ""
	default:
		if truthy(content) {
			return strings.TrimSpace(fmt.Sprint(content))
		}
		return ""
	}
}

// isPersonalActionReply 关键词兜底。
// Python: im_outbound.py L439-441 _is_personal_action_reply
func isPersonalActionReply(content string) bool {
	normalized := reWhitespace.ReplaceAllString(content, "")
	if normalized == "" {
		return false
	}
	for _, kw := range groupAckKeywords {
		if strings.Contains(normalized, kw) {
			return true
		}
	}
	return false
}

// ensureLLM 懒加载 LLM（与入站 Processor 独立两份）。差异：temperature=0.1/top_p=0.1；
// api_key/api_base 缺失守卫（仅出站有，Python L90-96）→ Warn 跳过；
// client_provider 用 .get 默认（key 缺失 → "OpenAI"，非 or 链无 env 回退）；
// model_name 三级回退整体 strip；timeout 走 NewModelClientConfig 默认 60。
// Python: im_outbound.py L69-124 _ensure_llm
func (p *IMOutboundPipeline) ensureLLM() bool {
	if p.llm != nil {
		return true
	}

	cfgBase := map[string]any{}
	if cfg := hookscfg.GetGlobalConfigForScope(); cfg != nil {
		if base, err := cfg.Load(); err == nil {
			cfgBase = base
		}
	}
	react, _ := cfgBase["react"].(map[string]any)
	mccRaw, _ := react["model_client_config"].(map[string]any)
	if mccRaw == nil {
		mccRaw = map[string]any{}
	}

	apiKey := strings.TrimSpace(orStr(mccRaw["api_key"], os.Getenv("API_KEY")))
	apiBase := strings.TrimSpace(orStr(mccRaw["api_base"], os.Getenv("API_BASE")))
	apiBase = strings.TrimSuffix(apiBase, "/chat/completions")
	modelName := strings.TrimSpace(orStr(react["model_name"], os.Getenv("MODEL_NAME"), "gpt-4o"))
	clientProvider := "OpenAI"
	if v, exists := mccRaw["client_provider"]; exists {
		if s, ok := v.(string); ok {
			clientProvider = s
		}
	}

	// 缺失守卫（仅出站）
	if apiKey == "" || apiBase == "" {
		logger.Warn(logComponent).
			Str("event_type", "llm_skipped").
			Str("api_key", apiKeyState(apiKey)).
			Str("api_base", apiKeyState(apiBase)).
			Msg("IMOutboundPipeline LLM skipped: missing API_KEY/API_BASE")
		return false
	}

	rawHeaders := mccRaw["custom_headers"]
	if !truthy(rawHeaders) {
		rawHeaders = os.Getenv("CUSTOM_HEADERS")
	}
	customHeaders := headersToStringMap(config.ParseCustomHeaders(rawHeaders))

	clientCfg, err := llmschema.NewModelClientConfig(
		clientProvider, apiKey, apiBase,
		llmschema.WithVerifySSL(false),
		llmschema.WithCustomHeaders(customHeaders),
	)
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Msg("IMOutboundPipeline LLM init failed")
		return false
	}
	modelCfg := llmschema.NewModelRequestConfig(
		llmschema.WithModelName(modelName),
		llmschema.WithTemperature(0.1),
		llmschema.WithTopP(0.1),
	)
	model, err := llm.NewModel(clientCfg, modelCfg)
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Msg("IMOutboundPipeline LLM init failed")
		return false
	}
	p.llm = model
	p.llmModelName = modelName
	logger.Info(logComponent).
		Str("event_type", "llm_initialized").
		Str("model", modelName).
		Str("provider", clientProvider).
		Str("api_base", truncateWithSuffix(apiBase, 30)).
		Msg("IMOutboundPipeline LLM initialized")
	return true
}

// apiKeyState 守卫日志字段：set/empty。
func apiKeyState(v string) string {
	if v != "" {
		return "set"
	}
	return "empty"
}

// newHexID 生成 8 位 hex 随机 ID（对齐 secrets.token_hex(4)，偏差④）。
func newHexID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xffffffff)
	}
	return hex.EncodeToString(b)
}

// truncateWithSuffix 截断加省略号后缀（Python api_base[:30] + "..." if len>30，
// 字符语义 rune 域）。
func truncateWithSuffix(s string, max int) string {
	runes := []rune(s)
	if len(runes) > max {
		return string(runes[:max]) + "..."
	}
	return s
}

// classifyPersonalAction 使用 LLM 判断回复是否属于个人行动类内容。
// 返回 (decision, llmRaw)：decision true(DM)/false(CHAT)/nil(失败)。
// Python: im_outbound.py L373-436 _classify_personal_action
func (p *IMOutboundPipeline) classifyPersonalAction(ctx context.Context, platformName string, metadata map[string]any, content string) (*bool, string) {
	if !p.ensureLLM() {
		return nil, ""
	}

	targetName := orStr(metadata["reply_target_name"])
	if targetName == "" {
		targetName = "目标用户"
	}
	originalQuery := orStr(metadata["avatar_original_query"])

	querySection := ""
	if originalQuery != "" {
		querySection = fmt.Sprintf("群聊中的原始提问：\n%s\n\n", truncateStr(originalQuery, 300))
	}

	prompt := fmt.Sprintf(classifyPromptTemplate, platformName, querySection, targetName, truncateStr(content, 500))

	logger.Info(logComponent).
		Str("event_type", "classify_request").
		Str("model", p.llmModelName).
		Str("target_name", targetName).
		Int("content_len", utf8.RuneCountInString(content)).
		Str("prompt", prompt).
		Msg("LLM classify request")

	resp, err := p.llm.Invoke(ctx,
		model_clients.NewMessagesParam(llmschema.NewUserMessage(prompt)),
		model_clients.WithInvokeModel(p.llmModelName),
		model_clients.WithInvokeTemperature(0),
		model_clients.WithInvokeTimeout(60),
	)
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Msg("LLM classify delivery intent failed")
		return nil, ""
	}

	text := ""
	if resp != nil {
		// 多模态 content 时 Text() 空——对齐 Python isinstance(content,str) False
		text = strings.ToUpper(strings.TrimSpace(resp.Content.Text()))
	}
	logger.Info(logComponent).
		Str("event_type", "llm_raw_response").
		Str("text", text).
		Msg("LLM raw response")
	if strings.Contains(text, "DM") {
		v := true
		return &v, text
	}
	if strings.Contains(text, "CHAT") {
		v := false
		return &v, text
	}
	logger.Warn(logComponent).
		Str("text", text).
		Msg("LLM response unparsable as DM/CHAT")
	return nil, text
}

// handleGroupFollowup 处理群聊追问：目标解析三层（名字接口 → sender 三键 →
// candidate）→ metadata 三键 + payload.content 原文恢复 + PendingInteraction 落盘
// （gpq 确定性 ID）。
// Python: im_outbound.py L234-304 _handle_group_followup
func (p *IMOutboundPipeline) handleGroupFollowup(msg *schema.Message, meta map[string]any, adapter IMPlatformAdapter, content string, match []string) {
	targetName := strings.TrimSpace(match[1])
	stripped := strings.TrimSpace(reGroupFollowup.ReplaceAllString(content, ""))

	// 目标解析三层
	targetUserID := ""
	// 层1：名字接口（hasattr 动态探测 → Go 类型断言）
	if targetName != "" {
		if resolver, ok := adapter.(imUserIDNameResolver); ok {
			targetUserID = resolver.ResolveUserIDByName(targetName)
		}
	}
	// 层2：sender 三键回退
	if targetUserID == "" {
		senderUserID := orStr(meta["im_sender_user_id"], meta["open_id"], meta["sender_id"])
		if senderUserID != "" {
			targetUserID = senderUserID
			if targetName == "" {
				// hasattr(resolve_user_display_name) 恒真（主接口方法）
				targetName = adapter.ResolveUserDisplayName(senderUserID)
			}
		} else {
			// 层3：candidate 回退
			candidateUserID := adapter.GetCandidateUserID(meta)
			if candidateUserID != "" {
				targetUserID = candidateUserID
				if targetName == "" {
					targetName = orStr(meta["reply_target_name"])
				}
			}
		}
	}

	meta["interaction_mention_user"] = targetName
	meta["interaction_mention_user_id"] = targetUserID
	meta["interaction_question"] = true

	// payload.content 恢复（含前缀原文，对齐 Python payload 恢复）
	payload := msg.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	payload["content"] = content
	msg.Payload = payload

	sessionID := strings.TrimSpace(msg.SessionID)
	originContent := orStr(meta["avatar_original_query"])
	originSenderID := orStr(meta["im_sender_user_id"], meta["open_id"], meta["sender_id"])
	originSenderName := targetName

	pi := &routing.PendingInteraction{
		InteractionID:    fmt.Sprintf("gpq_%s_%s", sessionID, targetUserID),
		Mode:             routing.ModeGroup,
		OriginChannelID:  msg.ChannelID,
		OriginSessionID:  sessionID,
		OriginContent:    originContent,
		OriginSenderName: originSenderName,
		OriginSenderID:   originSenderID,
		Question:         stripped,
		TargetUserID:     targetUserID,
		TargetUserName:   targetName,
		OriginMetadata:   copyMeta(meta),
		Status:           routing.StatusPending,
	}
	pi.Save()

	msg.Metadata = meta
	logger.Info(logComponent).
		Str("event_type", "group_followup").
		Str("session_id", sessionID).
		Str("target_name", targetName).
		Str("target_user_id", targetUserID).
		Str("question", truncateStr(stripped, 80)).
		Msg("group followup created")
}

// handleDMFollowup 处理私聊追问：reply_scope=dm + interaction_question + 条件候选键 +
// payload.content 原文恢复 + PendingInteraction 落盘（iact 随机 hex ID，无 TargetUserName）。
// Python: im_outbound.py L306-356 _handle_dm_followup
func (p *IMOutboundPipeline) handleDMFollowup(msg *schema.Message, meta map[string]any, adapter IMPlatformAdapter, content string) {
	stripped := strings.TrimSpace(reDMFollowup.ReplaceAllString(content, ""))

	meta["reply_scope"] = "dm"
	meta["interaction_question"] = true

	candidateUserID := adapter.GetCandidateUserID(meta)
	if candidateUserID != "" {
		meta[adapter.ReplyUserIDKey()] = candidateUserID
	}

	payload := msg.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	payload["content"] = content
	msg.Payload = payload

	sessionID := strings.TrimSpace(msg.SessionID)
	originContent := orStr(meta["avatar_original_query"])
	originSenderName := orStr(meta["reply_target_name"])
	originSenderID := orStr(meta["im_sender_user_id"], meta["open_id"], meta["sender_id"])
	targetUserID := candidateUserID

	pi := &routing.PendingInteraction{
		InteractionID:    fmt.Sprintf("iact_%s_%s", msg.ChannelID, newHexID()),
		Mode:             routing.ModeDM,
		OriginChannelID:  msg.ChannelID,
		OriginSessionID:  sessionID,
		OriginContent:    originContent,
		OriginSenderName: originSenderName,
		OriginSenderID:   originSenderID,
		Question:         stripped,
		TargetUserID:     targetUserID,
		OriginMetadata:   copyMeta(meta),
		Status:           routing.StatusPending,
	}
	pi.Save()

	msg.Metadata = meta
	logger.Info(logComponent).
		Str("event_type", "dm_followup").
		Str("session_id", sessionID).
		Str("target_user_id", targetUserID).
		Str("question", truncateStr(stripped, 80)).
		Msg("dm followup created")
}

// copyMeta 浅拷贝 metadata（防御性拷贝，对齐 Python dict(meta)）。
func copyMeta(meta map[string]any) map[string]any {
	result := make(map[string]any, len(meta))
	for k, v := range meta {
		result[k] = v
	}
	return result
}
