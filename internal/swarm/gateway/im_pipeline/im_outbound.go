package im_pipeline

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
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

// truncateWithSuffix 截断加省略号后缀（Python api_base[:30] + "..." if len>30）。
func truncateWithSuffix(s string, max int) string {
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
