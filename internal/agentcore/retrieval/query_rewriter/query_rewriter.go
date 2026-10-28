package query_rewriter

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/output_parsers"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// QueryRewriter 查询重写器：把多轮对话中的省略/指代 query 改写成
// 独立可检索的 standalone_query；历史达阈值时用 LLM 压缩历史。
//
// 只消费 sessionCtx 的 GetMessages/SetMessages；当前轮消息的追加
// 由调用方负责（rewrite → 检索 → add_messages 顺序）。
//
// Python: query_rewriter.py QueryRewriter
type QueryRewriter struct {
	// modelConfig LLM 配置（model_provider + model_info）
	modelConfig *llmschema.ModelConfig
	// compressRange 历史条数达此值触发一次压缩；亦为 rewrite 提示词
	// 携带的历史消息上限（防无限膨胀）
	compressRange int
	// sessionCtx 当前会话上下文（Python self.context，字段名避让 ctx 参数）
	sessionCtx ceinterface.ModelContext
	// promptLang 提示词语言后缀（zh/en）
	promptLang string
	// llm 内部 LLM 客户端
	llm *llm.Model
	// jsonOutputParser 解析兜底器
	jsonOutputParser *output_parsers.JsonOutputParser
	// compressOutputSchema 压缩输出 schema（theme: list / summary: string）
	compressOutputSchema map[string]schemaFieldType
	// rewriteOutputSchema 改写输出 schema（8 字段）
	rewriteOutputSchema map[string]schemaFieldType
	// templateCache 模板缓存：(base, lang) → 内容
	templateCache map[promptCacheKey]string
	// cacheMu 模板缓存并发保护（Python asyncio 单线程隐式安全的 Go 显式化）
	cacheMu sync.Mutex
}

// promptCacheKey 模板缓存键。
type promptCacheKey struct {
	base string
	lang string
}

// QueryRewriterOption QueryRewriter 构造选项函数。
type QueryRewriterOption func(*QueryRewriter)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultCompressRange 默认压缩阈值（Python 默认参数 20）
	defaultCompressRange = 20
	// defaultPromptLang 默认提示词语言
	defaultPromptLang = "zh"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithCompressRange 设置压缩阈值（构造内 max(n,1) 钳制，对齐 Python L245）。
func WithCompressRange(n int) QueryRewriterOption {
	return func(q *QueryRewriter) { q.compressRange = n }
}

// WithPromptLang 设置提示词语言后缀（空串构造后回落 "zh"）。
func WithPromptLang(lang string) QueryRewriterOption {
	return func(q *QueryRewriter) { q.promptLang = lang }
}

// WithModelClientOption 注入外部 Model（测试通道；传入则跳过内部构造）。
func WithModelClientOption(m *llm.Model) QueryRewriterOption {
	return func(q *QueryRewriter) { q.llm = m }
}

// NewQueryRewriter 构造查询重写器：绑定 LLM 配置与会话上下文。
//
// 从 cfg.ModelInfo 读取连接信息建 Model：ssl_cert/verify_ssl/custom_headers
// 走 Extra 防御读取（Python getattr + pydantic extra="allow" 同构，
// spec §4.2）；verify_ssl=true 且无证书时强制 false（Python 注释规则：
// BaseModelClient 要求 verify_ssl=true 时必须配证书）。
//
// Python: query_rewriter.py QueryRewriter.__init__
func NewQueryRewriter(cfg *llmschema.ModelConfig, ctx ceinterface.ModelContext, opts ...QueryRewriterOption) (*QueryRewriter, error) {
	q := &QueryRewriter{
		modelConfig:   cfg,
		compressRange: defaultCompressRange,
		sessionCtx:    ctx,
		promptLang:    defaultPromptLang,
		templateCache: map[promptCacheKey]string{},
		compressOutputSchema: map[string]schemaFieldType{
			"theme":   schemaFieldList,
			"summary": schemaFieldString,
		},
		rewriteOutputSchema: map[string]schemaFieldType{
			"before":           schemaFieldString,
			"intention":        schemaFieldString,
			"standalone_query": schemaFieldString,
			"references":       schemaFieldDict,
			"missing":          schemaFieldList,
			"typo":             schemaFieldList,
			"gibberish":        schemaFieldList,
			"from_history":     schemaFieldString,
		},
	}
	for _, opt := range opts {
		opt(q)
	}
	// Python: self.compress_range = max(compress_range, 1)
	if q.compressRange < 1 {
		q.compressRange = 1
	}
	// Python: self.prompt_lang = prompt_lang or "zh"
	if q.promptLang == "" {
		q.promptLang = defaultPromptLang
	}
	q.jsonOutputParser = output_parsers.NewJsonOutputParser()

	// 注入通道：已提供 Model 则跳过内部构造（测试先例 13.17）
	if q.llm != nil {
		return q, nil
	}

	// Python: mi = self.model_config.model_info（ssl_cert/verify_ssl/
	// custom_headers 走 extra 字段防御读取）
	mi := cfg.ModelInfo
	sslCert := ""
	if v, ok := mi.Extra["ssl_cert"].(string); ok {
		sslCert = v
	}
	verifySSL := true
	if v, ok := mi.Extra["verify_ssl"].(bool); ok {
		verifySSL = v
	}
	// BaseModelClient 要求 verify_ssl=true 时必须配证书；无证书强制关闭
	// （Python L252-254）
	if verifySSL && sslCert == "" {
		verifySSL = false
	}
	var customHeaders map[string]string
	if v, ok := mi.Extra["custom_headers"].(map[string]string); ok {
		customHeaders = v
	}

	clientOpts := []llmschema.ModelClientConfigOption{
		llmschema.WithTimeout(float64(mi.Timeout)),
		llmschema.WithVerifySSL(verifySSL),
		llmschema.WithCustomHeaders(customHeaders),
	}
	if sslCert != "" {
		clientOpts = append(clientOpts, llmschema.WithSSLCert(sslCert))
	}
	clientCfg, err := llmschema.NewModelClientConfig(cfg.ModelProvider, mi.APIKey, mi.APIBase, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("创建 ModelClientConfig 失败: %w", err)
	}
	requestCfg := llmschema.NewModelRequestConfig(
		llmschema.WithModelName(mi.ModelName),
		llmschema.WithTemperature(0.0),
		llmschema.WithTopP(mi.TopP),
	)
	model, err := llm.NewModel(clientCfg, requestCfg)
	if err != nil {
		return nil, fmt.Errorf("创建 Model 失败: %w", err)
	}
	q.llm = model
	return q, nil
}

// LoadTemplate 加载提示词模板（语言后缀路由 + 缓存）。
//
// Python: query_rewriter.py QueryRewriter.load_template
// （OSError 读失败分支在 go:embed 下编译期消解，spec §8-2）
func (q *QueryRewriter) LoadTemplate(promptBase string) (string, error) {
	key := promptCacheKey{base: promptBase, lang: q.promptLang}
	q.cacheMu.Lock()
	defer q.cacheMu.Unlock()
	if cached, ok := q.templateCache[key]; ok {
		return cached, nil
	}
	name := "prompts/" + promptBase + "_" + q.promptLang + ".md"
	content, err := fs.ReadFile(promptsFS, name)
	if err != nil {
		return "", exception.BuildError(
			exception.StatusRetrievalQueryRewriterPromptNotFound,
			exception.WithParam("error_msg", "prompt file not found: "+name),
		)
	}
	q.templateCache[key] = string(content)
	return string(content), nil
}

// Msg2Text 格式化消息列表为 "role: content" 纯文本。
//
// messages 为 nil 时读全量上下文历史；多模态 content 走 JSON 序列化
// （Python json.dumps ensure_ascii=False）。
//
// Python: query_rewriter.py QueryRewriter.msg_2_text
func (q *QueryRewriter) Msg2Text(messages []llmschema.BaseMessage) (string, error) {
	if messages == nil {
		var err error
		messages, err = q.sessionCtx.GetMessages(0, true)
		if err != nil {
			return "", err
		}
	}
	lines := make([]string, 0, len(messages))
	for _, m := range messages {
		content := m.GetContent()
		var text string
		if content.IsText() {
			text = content.Text()
		} else {
			// Python: json.dumps(content, ensure_ascii=False)——list/dict
			// 序列化为 JSON 字符串（Go Parts 对应多模态列表）
			data, err := json.Marshal(content.Parts())
			if err != nil {
				return "", fmt.Errorf("序列化多模态 content 失败: %w", err)
			}
			text = string(data)
		}
		lines = append(lines, m.GetRole().String()+": "+text)
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), nil
}
