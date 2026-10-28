package im_pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/message_handler/command_parser"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// IMPlatformAdapter 统一的平台适配器接口，同时服务入站和出站管线。
// 每个数字分身 IM 平台（飞书 11.16 / 企微 11.21）实现一个适配器，
// 注册后供 IMInboundPipeline 和 IMOutboundPipeline 共享使用。
//
// Python: im_inbound.py L67-127 (IMPlatformAdapter Protocol，10 方法 + channel_id
// 类属性经构造上下文隐含——注册表 key 即 channel_id)
type IMPlatformAdapter interface {
	// GetPrincipalUserID 返回数字分身主体用户 ID
	// Python: get_principal_user_id (L78)
	GetPrincipalUserID() string
	// GetPrincipalDisplayName 返回主体显示名
	// Python: get_principal_display_name (L81)
	GetPrincipalDisplayName() string
	// ResolveUserDisplayName 将用户 ID 解析为显示名
	// Python: resolve_user_display_name (L84)
	ResolveUserDisplayName(userID string) string
	// GetBotMentionTokens 返回机器人提及 token 列表
	// Python: get_bot_mention_tokens (L87)
	GetBotMentionTokens() []string
	// LoadRecentMessages 加载 thread 群聊历史（limit 默认 500）
	// Python: load_recent_messages (L90-92)
	LoadRecentMessages(threadID string, limit int) []IMHistoryMessage
	// BuildRelevanceMetadata 构建相关性 metadata 补丁
	// Python: build_relevance_metadata (L95-101)
	BuildRelevanceMetadata(metadata map[string]any, senderUserID string, relevant bool) map[string]any
	// PlatformName 平台显示名，用于 LLM prompt（飞书/企业微信）
	// Python: platform_name 属性 (L106-109)
	PlatformName() string
	// ReplyUserIDKey metadata 中设置回复目标用户 ID 的 key
	// 飞书: "reply_feishu_open_id"；企微: "reply_wecom_user_id"
	// Python: reply_user_id_key 属性 (L111-118)
	ReplyUserIDKey() string
	// UseKeywordOverride LLM 判断为 CHAT 但关键词命中时，是否覆盖为 DM。
	// Python Protocol 声明，全仓零消费——死字段保留，飞书/企微均返回 true（偏差①）。
	// Python: use_keyword_override 属性 (L120-123)
	UseKeywordOverride() bool
	// GetCandidateUserID 从 metadata 中提取候选私发目标用户 ID；无候选返回空串
	// Python: get_candidate_user_id (L125-127)
	GetCandidateUserID(metadata map[string]any) string
}

// imUserIDNameResolver 可选能力接口（非导出）：根据用户名解析平台用户 ID。
// 对齐 Python `hasattr(adapter, "resolve_user_id_by_name")` 动态探测；
// 消费点在 handleGroupFollowup 目标解析三层的第一层；飞书/企微真实适配器均实现
// （11.16/11.21），测试 fakeAdapter 按需实现。13.7 三态接口先例。
type imUserIDNameResolver interface {
	// ResolveUserIDByName 根据用户名在群聊历史中查找对应平台用户 ID
	ResolveUserIDByName(name string) string
}

// IMHistoryMessage IM 群聊历史消息。
// Python: im_inbound.py L59-64 (dataclass IMHistoryMessage)
type IMHistoryMessage struct {
	// UserID 发送者平台用户 ID
	UserID string `json:"user_id"`
	// UserName 发送者显示名
	UserName string `json:"user_name"`
	// Content 消息内容
	Content string `json:"content"`
	// TimestampMs 毫秒时间戳
	TimestampMs int64 `json:"timestamp_ms"`
}

// IMConversationProcessor 数字分身群聊消息的相关性判定与 LLM 改写处理器。
// Python: im_inbound.py L138-451 (IMConversationProcessor)
type IMConversationProcessor struct {
	// userProfilePath USER.md 路径（构造注入；空 → workspace.DeepAgentUserMDPath()）
	userProfilePath string
	// modelName 模型名（loadModelConfig 三级回退后的结果）
	modelName string
	// modelClientRaw react.model_client_config 原始 map（懒加载 LLM 时逐字段取）
	modelClientRaw map[string]any
	// llmDisabled 审查修复 M-09：react/model_client_config 存在但非 map 时
	// 置 true——对齐 Python AttributeError → except → llm=None（LLM 不激活）；
	// Go 原实现断言失败静默换空 map 走 env 回退，方向相反
	llmDisabled bool
	// llm 懒加载 LLM 实例；ensureLLM 失败保持 nil（改写恒回退）
	llm *llm.Model
}

// InboundProcessResult 入站处理结果。
// Python: im_inbound.py L130-135 (dataclass InboundProcessResult——should_forward
// 默认 true / rewritten_content str|None → Go 空串表 None（偏差⑤）/ metadata_patch
// 默认空 dict / reason str)
type InboundProcessResult struct {
	// ShouldForward 是否继续转发（构造时显式填 true——struct 零值 false，唯一
	// false 分支是 irrelevant）
	ShouldForward bool
	// RewrittenContent 改写后的消息内容（空串表 Python None）
	RewrittenContent string
	// MetadataPatch metadata 补丁
	MetadataPatch map[string]any
	// Reason 分支原因（字面值逐字对齐 Python）
	Reason string
}

// IMInboundPipeline 数字分身入站管线（适配器注册表 + 入站 Apply）。
// Python: im_inbound.py L454-616 (IMInboundPipeline)
type IMInboundPipeline struct {
	// mu adapters 并发保护（注册/注销与 Apply 读并发）
	mu sync.RWMutex
	// adapters 适配器注册表：channel_id → adapter
	adapters map[string]IMPlatformAdapter
	// processor 消息处理器（对齐 Python processor or IMConversationProcessor()）
	processor *IMConversationProcessor
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识（Gateway 域）
const logComponent = logger.ComponentGateway

// SYSTEM_PROMPT_TEMPLATE 入站改写系统提示词（Python 原文逐字复制，含
// {principal_name}/{bot_mention_hint} 两占位符，渲染用 strings.ReplaceAll）。
// Python: im_inbound.py L21-56
const SYSTEM_PROMPT_TEMPLATE = "你是{principal_name}的数字分身，活跃在即时通讯群聊中。当群里有其他用户发送与{principal_name}相关的消息时，你的任务是改写这条消息，使其更清晰、更完整，以便后续帮助{principal_name}生成恰当的回复。\n\n## 消息格式说明\n\n收到的消息格式为：`[时间戳] [发送者]: 消息内容`\n例如：`[2026-03-20 14:44:34] [某人]: {bot_mention_hint} @{principal_name} 111`\n\n## 判断是否需要处理（按优先级顺序）\n\n### 必须回复的情况（不能输出[无需处理]）：\n\n1. **消息中@了机器人**：如果消息内容中出现机器人 mention（例如 {bot_mention_hint}），无论发送者是谁，都必须回复\n2. **其他人@了用户本人**：如果发送者不是{principal_name}本人，且消息中@了{principal_name}，必须回复\n\n### 由模型判断的情况：\n\n如果以上条件都不满足，则根据以下标准判断：\n- 当前消息中提到了{principal_name}的名字\n- 当前消息是对{principal_name}之前发言的回复或延续\n- 当前消息是群聊中需要{principal_name}参与讨论的话题\n- 当前消息是否与群里其他人所发历史消息有关\n\n如果判断为无关，输出：[无需处理]\n\n## 改写原则\n\n1. 明确发送者意图：对方想表达什么？是提问、请求、讨论还是闲聊？\n2. 补充上下文：如果消息涉及历史对话（如\"那个文件\"、\"刚才说的\"），补充具体信息\n3. 明确回复期望：对方期望什么样的回应？是解答问题、确认信息、还是参与讨论？\n4. 保留原意：不要改变消息的核心意图\n\n## 输出要求\n\n直接输出改写后的消息，不要添加任何解释或额外内容。"

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewIMConversationProcessor 创建处理器。
// Python: im_inbound.py L138-151 __init__(user_profile_path=None, model_name=None)
func NewIMConversationProcessor(userProfilePath, modelName string) *IMConversationProcessor {
	if userProfilePath == "" {
		userProfilePath = workspace.DeepAgentUserMDPath()
	}
	name, mcc, disabled := loadModelConfig(modelName)
	return &IMConversationProcessor{
		userProfilePath: userProfilePath,
		modelName:       name,
		modelClientRaw:  mcc,
		llmDisabled:     disabled,
	}
}

// Process 处理单条消息：七层分支顺序不可调换（执行顺序纪律），reason 字面值逐字保留。
// Python: im_inbound.py L171-256 process
func (proc *IMConversationProcessor) Process(ctx context.Context, msg *schema.Message, adapter IMPlatformAdapter, pendingContext string) InboundProcessResult {
	// 层1：非 chat.send 直接转发
	if msg.ReqMethod != schema.ReqMethodChatSend {
		return InboundProcessResult{ShouldForward: true, Reason: "non-chat-send"}
	}

	// 层2：空文本
	text := extractText(msg)
	if text == "" {
		return InboundProcessResult{ShouldForward: true, Reason: "empty-content"}
	}

	// 层3：控制消息（slash 命令文本）
	if command_parser.ControlMessageTexts[strings.TrimSpace(text)] {
		return InboundProcessResult{ShouldForward: true, Reason: "control-message"}
	}

	metadata := msg.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}

	// 层4：非群聊
	chatType := strings.ToLower(stripOrStr(metadata["im_chat_type"], metadata["chat_type"]))
	if chatType != "group" {
		return InboundProcessResult{ShouldForward: true, Reason: "non-group-chat"}
	}

	senderUserID := stripOrStr(metadata["im_sender_user_id"], metadata["open_id"], metadata["sender_id"])
	threadID := stripOrStr(metadata["im_thread_id"], metadata["feishu_chat_id"], msg.SessionID)

	principalUserID := strings.TrimSpace(adapter.GetPrincipalUserID())
	principalName := strings.TrimSpace(adapter.GetPrincipalDisplayName())
	if principalName == "" {
		principalName = "用户"
	}
	if principalUserID == "" {
		return InboundProcessResult{ShouldForward: true, Reason: "missing-principal-user"}
	}

	// 层5：必答规则
	if shouldAlwaysReply(text, metadata, senderUserID, principalUserID, principalName, adapter.GetBotMentionTokens()) {
		return InboundProcessResult{
			ShouldForward: true,
			MetadataPatch: adapter.BuildRelevanceMetadata(metadata, senderUserID, true),
			Reason:        "always-reply",
		}
	}

	// 层6-7：LLM 改写
	prompt := proc.buildPrompt(threadID, senderUserID, text,
		resolveTimestampMs(msg.Timestamp, metadata), principalName, adapter, pendingContext)
	rewrittenContent := proc.rewriteQuery(ctx, prompt, principalName, adapter)
	if rewrittenContent == "" {
		return InboundProcessResult{ShouldForward: true, Reason: "rewrite-failed"}
	}

	normalized := strings.TrimSpace(rewrittenContent)
	if normalized == "[无需处理]" {
		if hasImageContext(metadata) {
			return InboundProcessResult{ShouldForward: true, Reason: "image-fallback"}
		}
		return InboundProcessResult{ShouldForward: false, Reason: "irrelevant"}
	}

	return InboundProcessResult{
		ShouldForward:    true,
		RewrittenContent: normalized,
		MetadataPatch:    adapter.BuildRelevanceMetadata(metadata, senderUserID, true),
		Reason:           "rewritten",
	}
}

// NewIMInboundPipeline 创建入站管线。processor nil → 默认构造；adapters nil → 空 map。
// Python: im_inbound.py L454-462 __init__
func NewIMInboundPipeline(processor *IMConversationProcessor, adapters map[string]IMPlatformAdapter) *IMInboundPipeline {
	if processor == nil {
		processor = NewIMConversationProcessor("", "")
	}
	if adapters == nil {
		adapters = map[string]IMPlatformAdapter{}
	}
	return &IMInboundPipeline{
		adapters:  adapters,
		processor: processor,
	}
}

// RegisterAdapter 注册适配器。
// Python: im_inbound.py L464-465 register_adapter
func (p *IMInboundPipeline) RegisterAdapter(channelID string, adapter IMPlatformAdapter) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.adapters == nil {
		p.adapters = map[string]IMPlatformAdapter{}
	}
	p.adapters[channelID] = adapter
}

// UnregisterAdapter 注销适配器（不存在容忍）。
// Python: im_inbound.py L467-468 unregister_adapter
func (p *IMInboundPipeline) UnregisterAdapter(channelID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.adapters, channelID)
}

// Apply 对入站消息应用管线处理，返回是否继续转发。
// Python: im_inbound.py L470-616 apply（执行顺序纪律，步骤顺序不可调换）
func (p *IMInboundPipeline) Apply(ctx context.Context, msg *schema.Message) bool {
	// 步骤1：非数字分身直接放行
	if !msg.GroupDigitalAvatar {
		return true
	}

	// 步骤2：无适配器放行
	p.mu.RLock()
	adapter := p.adapters[msg.ChannelID]
	p.mu.RUnlock()
	if adapter == nil {
		return true
	}

	metadata := msg.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}

	// 步骤3：resume 消息分支
	if truthy(metadata["is_resume_message"]) {
		dmPendingID := stripOrStr(metadata["dm_pending_interaction_id"])
		if dmPendingID != "" {
			pi := routing.LoadPendingInteraction(dmPendingID)
			if pi != nil {
				answer := ""
				var paramsMap map[string]any
				if len(msg.Params) > 0 {
					_ = json.Unmarshal(msg.Params, &paramsMap)
				}
				if paramsMap != nil {
					answer = stripOrStr(paramsMap["query"], paramsMap["content"])
				}
				resumeContent := pi.BuildResumeContent(answer)
				logger.Info(logComponent).
					Str("event_type", "dm_resume").
					Str("interaction_id", dmPendingID).
					Str("answer", truncateStr(answer, 80)).
					Msg("dm resume")
				logger.Info(logComponent).
					Str("event_type", "resume_content").
					Str("resume_content", resumeContent).
					Msg("resume content")
				writeParamsQueryContent(msg, resumeContent)
				if msg.Metadata == nil {
					msg.Metadata = map[string]any{}
				}
				msg.Metadata["interaction_context"] = resumeContent
				pi.Status = routing.StatusCompleted
				pi.Save()
				pi.Remove()
			}
		}
		logger.Info(logComponent).
			Str("event_type", "resume_forwarded").
			Str("channel_id", msg.ChannelID).
			Str("msg_id", msg.ID).
			Msg("resume message forwarded directly")
		return true
	}

	// 步骤4：originalQuery 提取
	originalQuery := extractText(msg)

	// 步骤5：peekPending
	pendingContext := peekPending(msg)
	if pendingContext != "" {
		logger.Info(logComponent).
			Str("event_type", "pending_context").
			Str("pending_context", pendingContext).
			Msg("pending context")
	}

	// 步骤6：处理器
	result := p.processor.Process(ctx, msg, adapter, pendingContext)

	// 步骤7：metadata_patch 合并
	if len(result.MetadataPatch) > 0 {
		if msg.Metadata == nil {
			msg.Metadata = map[string]any{}
		}
		for k, v := range result.MetadataPatch {
			msg.Metadata[k] = v
		}
	}

	// 步骤8：rewritten 双写 params
	if result.RewrittenContent != "" {
		writeParamsQueryContent(msg, result.RewrittenContent)
	}

	// 步骤9：avatar_original_query 保存
	if originalQuery != "" {
		if msg.Metadata == nil {
			msg.Metadata = map[string]any{}
		}
		msg.Metadata["avatar_original_query"] = originalQuery
	}

	// 步骤10：pending 命中且放行 → resume 拼接
	// 审查修复 T-13（注释声明）：sender 三键读 metadata 引用（步骤7 patch
	// 合并后的值）；Python L478 入口快照（Process 入参处的 metadata）。
	// 现有适配器 patch 不含三键（interaction_context/avatar_*/principal_*
	// 等），引用与快照等价不触发；若未来 patch 写入 im_sender_user_id/
	// open_id/sender_id，需改为入口复制快照对齐 Python。
	if pendingContext != "" && result.ShouldForward {
		if msg.Metadata == nil {
			msg.Metadata = map[string]any{}
		}
		msg.Metadata["interaction_context"] = pendingContext
		senderUserID := stripOrStr(metadata["im_sender_user_id"], metadata["open_id"], metadata["sender_id"])
		if senderUserID != "" {
			msg.Metadata["interaction_answered_user_id"] = senderUserID
		}

		sessionID := strings.TrimSpace(msg.SessionID)
		if sessionID != "" && senderUserID != "" {
			pi := routing.FindGroupPending(sessionID, senderUserID)
			// 审查偏差⑧修复：对齐 Python 日志记完整 repr（原仅记 found 布尔）
			logger.Info(logComponent).
				Str("event_type", "find_group_pending").
				Str("session_id", sessionID).
				Str("sender_user_id", senderUserID).
				Str("find_group_pending", pendingRepr(pi)).
				Msg("find group pending")
			if pi != nil {
				answer := result.RewrittenContent
				if answer == "" {
					answer = originalQuery
				}
				resumeContent := pi.BuildResumeContent(answer)
				logger.Info(logComponent).
					Str("event_type", "resume_content").
					Str("resume_content", resumeContent).
					Msg("resume content")
				writeParamsQueryContent(msg, resumeContent)
			}
		}
	}

	// 步骤11：avatar_detail 四键 merge
	if result.ShouldForward && result.Reason != "non-group-chat" {
		principalName := strings.TrimSpace(adapter.GetPrincipalDisplayName())
		avatarDetail := map[string]any{
			"avatar_mode":         true,
			"avatar_channel_type": adapter.PlatformName(),
		}
		if principalName != "" {
			avatarDetail["avatar_principal_name"] = principalName
		}
		principalID := strings.TrimSpace(adapter.GetPrincipalUserID())
		if principalID != "" {
			avatarDetail["principal_user_id"] = principalID
		}
		if msg.Metadata == nil {
			msg.Metadata = map[string]any{}
		}
		for k, v := range avatarDetail {
			msg.Metadata[k] = v
		}
	}

	// 步骤12：尾部 Info 日志（九字段）
	rewrittenPreview := truncateStr(strings.ReplaceAll(result.RewrittenContent, "\n", "\\n"), 120)
	originalPreview := truncateStr(strings.ReplaceAll(originalQuery, "\n", "\\n"), 120)
	metadataKeys := make([]string, 0, len(result.MetadataPatch))
	for k := range result.MetadataPatch {
		metadataKeys = append(metadataKeys, k)
	}
	sort.Strings(metadataKeys)
	logger.Info(logComponent).
		Str("event_type", "inbound_pipeline").
		Str("channel_id", msg.ChannelID).
		Str("request_id", msg.ID).
		Bool("should_forward", result.ShouldForward).
		Str("reason", result.Reason).
		Bool("rewritten", result.RewrittenContent != "").
		Str("original_preview", originalPreview).
		Str("rewritten_preview", rewrittenPreview).
		Strs("metadata_keys", metadataKeys).
		Bool("pending", pendingContext != "").
		Msg("inbound pipeline applied")

	// 步骤13
	return result.ShouldForward
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadModelConfig 模型配置读取（与 react agent 一致：config.yaml → 环境变量 → 默认值）。
// Python: im_inbound.py L153-169 _load_model_config
//
// 审查修复 M-09：第三返回值 disabled——react/model_client_config 存在但
// 非 map 时置 true（Python cfg.get("react").get(...) 对非 dict 抛
// AttributeError → _ensure_llm except → llm=None，LLM 不激活）；Go 原
// 实现断言失败静默换空 map 走 env 回退链，若环境变量存在则 LLM 构造
// 成功激活——方向相反。
func loadModelConfig(modelNameOverride string) (string, map[string]any, bool) {
	cfgBase := map[string]any{}
	if cfg := hookscfg.GetGlobalConfigForScope(); cfg != nil {
		if base, err := cfg.Load(); err == nil {
			cfgBase = base
		}
	}
	react, _ := cfgBase["react"].(map[string]any)
	if rawReact, exists := cfgBase["react"]; exists && rawReact != nil {
		if _, isMap := rawReact.(map[string]any); !isMap {
			logger.Warn(logComponent).
				Str("config_key", "react").
				Str("actual_type", fmt.Sprintf("%T", rawReact)).
				Msg("IMConversationProcessor config 'react' is not a map, LLM disabled")
			return "gpt-4o", map[string]any{}, true
		}
	}
	if rawMcc, exists := react["model_client_config"]; exists && rawMcc != nil {
		if _, isMap := rawMcc.(map[string]any); !isMap {
			logger.Warn(logComponent).
				Str("config_key", "react.model_client_config").
				Str("actual_type", fmt.Sprintf("%T", rawMcc)).
				Msg("IMConversationProcessor config 'model_client_config' is not a map, LLM disabled")
			return "gpt-4o", map[string]any{}, true
		}
	}
	mcc, _ := react["model_client_config"].(map[string]any)
	if mcc == nil {
		mcc = map[string]any{}
	}
	name := orStr(modelNameOverride, react["model_name"], os.Getenv("MODEL_NAME"))
	if name == "" {
		name = "gpt-4o"
	}
	return name, mcc, false
}

// truthy 对齐 Python bool() 真值语义（JSON 域值：nil/false/空串/0/空容器为假）。
func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	case map[string]any:
		return len(t) > 0
	case []any:
		return len(t) > 0
	default:
		return true
	}
}

// pythonStr 对齐 Python str() 显示语义（JSON 域值）。
// 与 message_handler.pythonOrStr 先例同款语义复制（跨包非导出无法复用）；
// float 系数整除无小数（strconv.FormatFloat -1），bool 取 "True"/"False"。
func pythonStr(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "True"
		}
		return "False"
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	default:
		return fmt.Sprint(v)
	}
}

// orStr 对齐 Python str(a or b or ...)：取第一个 truthy 值转字符串，全 falsy 返回空串。
func orStr(values ...any) string {
	for _, v := range values {
		if truthy(v) {
			return pythonStr(v)
		}
	}
	return ""
}

// stripOrStr 对齐 Python str(a or b or ...).strip()：or 链取值后两端去空白。
// IM 元数据取值的标准模式（审查偏差①⑤修复）；TrimSpace 的 Unicode
// White_Space 语义与 Python str.strip() 等价。
func stripOrStr(values ...any) string {
	return strings.TrimSpace(orStr(values...))
}

// pendingRepr 对齐 Python 日志 %s 输出 PendingInteraction dataclass repr：
// nil → "None"，非 nil → JSON 序列化全景（审查偏差⑧修复）。
func pendingRepr(pi *routing.PendingInteraction) string {
	if pi == nil {
		return "None"
	}
	if data, err := json.Marshal(pi); err == nil {
		return string(data)
	}
	return fmt.Sprintf("%v", pi)
}

// isDigits 全 ASCII 数字判定（对齐 Python str.isdigit 的 ASCII 域；
// Unicode Nd 数字如 "١٢٣" 为声明容差——IM 元数据实际不会出现，审查偏差⑦）。
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// truncateStr 截断字符串（Python [:n] 字节域近似，resolveInboundReferences 8000 截断先例）。
func truncateStr(s string, n int) string {
	// Python [:n] 为字符语义（rune 域截断，多字节中文不切碎）；
	// resolveInboundReferences 8000 截断为纯 ASCII 场景，字节/字符等价。
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n])
	}
	return s
}

// extractText 提取消息文本。params 非 map → 空；query 为非空 strip 字符串优先，
// 否则 content 字符串（可为空串）。
// Python: im_inbound.py L258-268 _extract_text
func extractText(msg *schema.Message) string {
	var paramsMap map[string]any
	if len(msg.Params) == 0 {
		return ""
	}
	if err := json.Unmarshal(msg.Params, &paramsMap); err != nil {
		return ""
	}
	if paramsMap == nil {
		return "" // JSON null → Python isinstance(None, dict) False
	}
	if q, ok := paramsMap["query"].(string); ok && strings.TrimSpace(q) != "" {
		return q
	}
	if c, ok := paramsMap["content"].(string); ok {
		return c
	}
	return ""
}

// resolveTimestampMs 解析毫秒时间戳。
// Python: im_inbound.py L270-277 _resolve_timestamp_ms（bool 是 int 子类：True→1）。
// 审查偏差⑦修复：字符串须 isdigit 纯数字（±号/小数点拒绝→回退）；浮点仅整数值
// 采用（JSON 整数 Python 解析为 int 直用；小数浮点 Python isinstance(raw_ts,int)
// False → 回退，不截断采用）。
func resolveTimestampMs(timestamp float64, metadata map[string]any) int64 {
	if raw, ok := metadata["timestamp_ms"]; ok {
		switch v := raw.(type) {
		case int:
			return int64(v)
		case int64:
			return v
		case float64:
			if v == math.Trunc(v) {
				return int64(v)
			}
		case bool:
			if v {
				return 1
			}
			return 0
		case string:
			trimmed := strings.TrimSpace(v)
			if isDigits(trimmed) {
				if n, err := strconv.Atoi(trimmed); err == nil {
					return int64(n)
				}
			}
		}
	}
	return int64(timestamp * 1000)
}

// hasImageContext 判断 metadata 是否携带图片上下文。
// Python: im_inbound.py L279-286 _has_image_context
func hasImageContext(metadata map[string]any) bool {
	if mt, ok := metadata["msg_type"]; ok {
		if s, isStr := mt.(string); isStr && strings.TrimSpace(s) == "image" {
			return true
		}
	}
	merged, ok := metadata["merged_msg_types"].([]any)
	if !ok {
		return false
	}
	for _, item := range merged {
		if s, isStr := item.(string); isStr && strings.TrimSpace(s) == "image" {
			return true
		}
	}
	return false
}

// shouldAlwaysReply 三条件任一命中即必答。bot_name 在入口读一次（偏差②）。
// Python: im_inbound.py L288-325 _should_always_reply
func shouldAlwaysReply(text string, metadata map[string]any, senderUserID, principalUserID, principalName string, botMentions []string) bool {
	// 条件1：提及列表（im_mentioned_user_ids or mentioned_open_ids）
	// 审查偏差⑥修复：空数组/空串等 falsy 值同样回退（对齐 Python or 链，
	// 原 == nil 仅 nil 回退）。
	mentioned := metadata["im_mentioned_user_ids"]
	if !truthy(mentioned) {
		mentioned = metadata["mentioned_open_ids"]
	}
	if list, ok := mentioned.([]any); ok {
		for _, item := range list {
			if strings.TrimSpace(pythonStr(item)) == principalUserID {
				return true
			}
		}
	}

	// bot_name 入口读一次（Python 循环内读同一全局单例返回同值，等价偏差②）
	botName := ""
	if cfg := hookscfg.GetGlobalConfigForScope(); cfg != nil {
		base, err := cfg.Load()
		if err != nil {
			logger.Debug(logComponent).Err(err).
				Msg("Failed to get bot_name from config")
		} else {
			botName = orStr(base["bot_name"])
			if botName == "" {
				if channels, ok := base["channels"].(map[string]any); ok {
					if wecom, ok := channels["wecom"].(map[string]any); ok {
						botName = orStr(wecom["bot_name"])
					}
				}
			}
			botName = strings.TrimSpace(botName)
		}
	}

	// 条件2：循环 bot_mentions，同轮检查 token 与 bot_name
	for _, token := range botMentions {
		if token != "" && strings.Contains(text, token) {
			return true
		}
		if botName != "" && strings.Contains(text, botName) {
			return true
		}
	}

	// 条件3：非本人发送者 + @主体名
	isNotMe := senderUserID != "" && senderUserID != principalUserID
	if isNotMe && principalName != "" && strings.Contains(text, "@"+principalName) {
		return true
	}
	return false
}

// buildPrompt 构建改写 prompt 四段拼接。
// Python: im_inbound.py L327-371 _build_prompt
func (proc *IMConversationProcessor) buildPrompt(threadID, senderUserID, text string, timestampMs int64, principalName string, adapter IMPlatformAdapter, pendingContext string) string {
	var parts []string
	parts = append(parts, "=== 群聊历史消息 ===")
	history := adapter.LoadRecentMessages(threadID, 500)
	if len(history) > 0 {
		parts = append(parts, fmt.Sprintf("最近 %d 条消息：\n", len(history)))
		for _, m := range history {
			dt := time.UnixMilli(m.TimestampMs).Format("2006-01-02 15:04:05")
			name := m.UserName
			if name == "" {
				name = "未知用户"
			}
			parts = append(parts, fmt.Sprintf("[%s] [%s]: %s", dt, name, m.Content))
		}
		parts = append(parts, "")
	} else {
		parts = append(parts, "暂无历史消息\n")
	}

	parts = append(parts, "=== 用户画像 ===")
	userProfile := proc.loadUserProfile()
	if userProfile == "" {
		userProfile = "暂无用户画像信息"
	}
	parts = append(parts, userProfile)
	parts = append(parts, "")

	if pendingContext != "" {
		parts = append(parts, "=== 待回答的追问 ===")
		parts = append(parts, pendingContext)
		parts = append(parts, "")
	}

	parts = append(parts, "=== 当前消息 ===")
	senderName := adapter.ResolveUserDisplayName(senderUserID)
	if senderName == "" {
		senderName = "未知用户"
	}
	dt := time.UnixMilli(timestampMs).Format("2006-01-02 15:04:05")
	parts = append(parts, fmt.Sprintf("[%s] [%s]: %s", dt, senderName, text))
	parts = append(parts, "")

	return strings.Join(parts, "\n")
}

// loadUserProfile 读取 USER.md；不存在/读失败 → 空（读失败 Warn）。
// Python: im_inbound.py L373-380 _load_user_profile
func (proc *IMConversationProcessor) loadUserProfile() string {
	data, err := os.ReadFile(proc.userProfilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn(logComponent).Err(err).
				Str("path", proc.userProfilePath).
				Msg("Failed to read USER.md")
		}
		return ""
	}
	return string(data)
}

// ensureLLM 懒加载 LLM 实例（仅入站 Processor 这一份，与出站独立）。
// Python: im_inbound.py L382-417 _ensure_llm（无 api_key/api_base 缺失守卫——
// 失败靠构造抛错进 except；Go NewModelClientConfig.Validate 空 key/base 返回
// error，同走 Warn 回退路径，终效一致）
func (proc *IMConversationProcessor) ensureLLM() *llm.Model {
	if proc.llm != nil {
		return proc.llm
	}
	// 审查修复 M-09：构造期类型守卫标记的 disabled——对齐 Python
	// AttributeError → except → llm=None（改写恒回退），不走 env 回退
	if proc.llmDisabled {
		return nil
	}
	modelCfg := llmschema.NewModelRequestConfig(
		llmschema.WithModelName(proc.modelName),
		llmschema.WithTemperature(0.2),
		llmschema.WithTopP(0.7),
	)
	mcc := proc.modelClientRaw
	if mcc == nil {
		mcc = map[string]any{}
	}
	apiKey := strings.TrimSpace(orStr(mcc["api_key"], os.Getenv("API_KEY")))
	apiBase := strings.TrimSpace(orStr(mcc["api_base"], os.Getenv("API_BASE")))
	apiBase = strings.TrimSuffix(apiBase, "/chat/completions")
	clientProvider := orStr(mcc["client_provider"], os.Getenv("MODEL_PROVIDER"), "OpenAI")
	rawHeaders := mcc["custom_headers"]
	if !truthy(rawHeaders) {
		rawHeaders = os.Getenv("CUSTOM_HEADERS")
	}
	customHeaders := headersToStringMap(config.ParseCustomHeaders(rawHeaders))

	clientCfg, err := llmschema.NewModelClientConfig(
		clientProvider, apiKey, apiBase,
		llmschema.WithClientID("im_conversation_processor_client"),
		llmschema.WithVerifySSL(false),
		llmschema.WithTimeout(180.0),
		llmschema.WithCustomHeaders(customHeaders),
	)
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Msg("IMConversationProcessor init LLM failed, fallback to original message")
		return nil
	}
	model, err := llm.NewModel(clientCfg, modelCfg)
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Msg("IMConversationProcessor init LLM failed, fallback to original message")
		return nil
	}
	proc.llm = model
	return proc.llm
}

// rewriteQuery 调用 LLM 改写消息。
// Python: im_inbound.py L419-451 _rewrite_query
func (proc *IMConversationProcessor) rewriteQuery(ctx context.Context, prompt, principalName string, adapter IMPlatformAdapter) string {
	model := proc.ensureLLM()
	if model == nil {
		return ""
	}

	botMentions := adapter.GetBotMentionTokens()
	botMentionHint := "@机器人"
	if len(botMentions) > 0 {
		botMentionHint = strings.Join(botMentions, " / ")
	}
	systemPrompt := SYSTEM_PROMPT_TEMPLATE
	systemPrompt = strings.ReplaceAll(systemPrompt, "{principal_name}", principalName)
	systemPrompt = strings.ReplaceAll(systemPrompt, "{bot_mention_hint}", botMentionHint)

	resp, err := model.Invoke(ctx,
		model_clients.NewDictsMessagesParam([]map[string]any{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": prompt},
		}),
		model_clients.WithInvokeModel(proc.modelName),
	)
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Msg("IMConversationProcessor LLM rewrite failed, fallback to original message")
		return ""
	}
	if resp != nil && resp.Content.IsText() {
		if text := strings.TrimSpace(resp.Content.Text()); text != "" {
			return text
		}
	}
	return ""
}

// headersToStringMap custom_headers map[string]any → map[string]string
// （非字符串值跳过，runtime_inheritance.go customHeadersToStringMap 先例语义）。
func headersToStringMap(m map[string]any) map[string]string {
	if m == nil {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			result[k] = s
		}
	}
	return result
}

// peekPending 查找群聊追问并拼四段中文模板（逐字对齐 Python L630-640）。
// Python: im_inbound.py L618-640 _peek_pending
func peekPending(msg *schema.Message) string {
	metadata := msg.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	sessionID := strings.TrimSpace(msg.SessionID)
	senderUserID := stripOrStr(metadata["im_sender_user_id"], metadata["open_id"], metadata["sender_id"])
	if sessionID == "" || senderUserID == "" {
		return ""
	}
	pi := routing.FindGroupPending(sessionID, senderUserID)
	if pi == nil {
		return ""
	}
	targetName := pi.TargetUserName
	if targetName == "" {
		targetName = "用户"
	}
	return "【追问上下文】你之前在处理以下任务时向 " + targetName + " 追问了信息：\n" +
		"- 原始请求：" + pi.OriginContent + "\n" +
		"- 你的追问：" + pi.Question + "\n" +
		"现在 " + targetName + " 已回复，请综合「原始请求」和「用户回复」中的所有信息继续完成任务，" +
		"原始请求中已明确提供的信息（如时间、地点等）直接使用即可，不要再次追问。" +
		"不要与群聊历史中的其他任务混淆。"
}

// writeParamsQueryContent params 双写：query 恒写 + content 条件写（仅当原 params
// 已含 content 键）。params 非 map（JSON 数组/null）→ 重建空 map（对齐 Python
// `if not isinstance(msg.params, dict): msg.params = {}`）。
// 先例：resolveInboundReferences 回写模式（forward_loop.go L629-637）。
func writeParamsQueryContent(msg *schema.Message, value string) {
	var paramsMap map[string]any
	if len(msg.Params) > 0 {
		if err := json.Unmarshal(msg.Params, &paramsMap); err != nil {
			paramsMap = map[string]any{}
		}
	}
	if paramsMap == nil {
		paramsMap = map[string]any{}
	}
	paramsMap["query"] = value
	if _, has := paramsMap["content"]; has {
		paramsMap["content"] = value
	}
	if updated, err := json.Marshal(paramsMap); err == nil {
		msg.Params = json.RawMessage(updated)
	}
}
