package im_pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
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
	// llm 懒加载 LLM 实例；ensureLLM 失败保持 nil（改写恒回退）
	llm *llm.Model
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
	name, mcc := loadModelConfig(modelName)
	return &IMConversationProcessor{
		userProfilePath: userProfilePath,
		modelName:       name,
		modelClientRaw:  mcc,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadModelConfig 模型配置读取（与 react agent 一致：config.yaml → 环境变量 → 默认值）。
// Python: im_inbound.py L153-169 _load_model_config
func loadModelConfig(modelNameOverride string) (string, map[string]any) {
	cfgBase := map[string]any{}
	if cfg := hookscfg.GetGlobalConfigForScope(); cfg != nil {
		if base, err := cfg.Load(); err == nil {
			cfgBase = base
		}
	}
	react, _ := cfgBase["react"].(map[string]any)
	mcc, _ := react["model_client_config"].(map[string]any)
	if mcc == nil {
		mcc = map[string]any{}
	}
	name := orStr(modelNameOverride, react["model_name"], os.Getenv("MODEL_NAME"))
	if name == "" {
		name = "gpt-4o"
	}
	return name, mcc
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

// truncateStr 截断字符串（Python [:n] 字节域近似，resolveInboundReferences 8000 截断先例）。
func truncateStr(s string, n int) string {
	if len(s) > n {
		return s[:n]
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
// Python: im_inbound.py L270-277 _resolve_timestamp_ms（bool 是 int 子类：True→1）
func resolveTimestampMs(timestamp float64, metadata map[string]any) int64 {
	if raw, ok := metadata["timestamp_ms"]; ok {
		switch v := raw.(type) {
		case int:
			return int64(v)
		case int64:
			return v
		case float64:
			return int64(v)
		case bool:
			if v {
				return 1
			}
			return 0
		case string:
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				return int64(n)
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
	mentioned := metadata["im_mentioned_user_ids"]
	if mentioned == nil {
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
