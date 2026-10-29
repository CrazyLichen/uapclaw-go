package query_rewriter

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/output_parsers"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
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
// 从 cfg.ModelInfo 读取连接信息建 Model：ssl_cert/verify_ssl 走 Extra
// 防御读取（Python getattr + pydantic extra="allow" 同构，spec §4.2）；
// custom_headers 为声明字段直读（Python mode_info.py L19 声明字段，
// S-04：不再走 Extra 兜底——声明字段读取语义上优先于 extra 通道）；
// verify_ssl=true 且无证书时强制 false（Python 注释规则：
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

	// Python: mi = self.model_config.model_info（ssl_cert/verify_ssl 走
	// extra 字段防御读取；custom_headers 为声明字段直读——S-04）
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
	customHeaders := mi.CustomHeaders

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
			// 序列化为 JSON 字符串（Go Parts 对应多模态列表）。
			// T-04：utils.MarshalNoEscape 对齐 ensure_ascii=False——
			// Marshal 默认把 <>& 转义为 \u003c 等，含 HTML 的多模态
			// content 进提示词时 LLM 会看到转义字面串
			data, err := utils.MarshalNoEscape(content.Parts())
			if err != nil {
				return "", fmt.Errorf("序列化多模态 content 失败: %w", err)
			}
			text = string(data)
		}
		lines = append(lines, m.GetRole().String()+": "+text)
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), nil
}

// Compress 用 LLM 把一段历史消息压缩成结构化摘要（theme + summary）。
//
// 解析链：extractJSON → parseLLMJSON → 兜底 jsonOutputParser →
// schemaRepair；解析失败统一 155604，invoke 失败 155605
// （Python 三层 except 在 Go 统一消解为两条 155604 路径，spec §8-3）。
//
// Python: query_rewriter.py QueryRewriter.compress
func (q *QueryRewriter) Compress(ctx context.Context, raw []llmschema.BaseMessage) (map[string]any, error) {
	// Python L366: raw_text = self.msg_2_text(raw)
	rawText, err := q.Msg2Text(raw)
	if err != nil {
		return nil, err
	}
	// Python L367: compression_prompt = _fill_template(load_template, history=raw_text)
	template, err := q.LoadTemplate("compression")
	if err != nil {
		return nil, err
	}
	compressionPrompt := fillTemplate(template, templateKV{key: "history", value: rawText})

	// Python L369-373: invoke(model=..., SystemMessage, temperature=构造期已固定 0.0)
	resp, err := q.llm.Invoke(ctx,
		model_clients.NewMessagesParam(llmschema.NewSystemMessage(compressionPrompt)),
		model_clients.WithInvokeTemperature(0.0))
	if err != nil {
		// Python L374-379: 155605
		return nil, exception.BuildError(
			exception.StatusRetrievalQueryRewriterLlmInvokeFailed,
			exception.WithParam("error_msg", err.Error()),
			exception.WithCause(err),
		)
	}
	// Python L380: (compressed.content or "").strip()
	contentStr := ""
	if resp != nil {
		contentStr = strings.TrimSpace(resp.Content.Text())
	}
	// Python L381: content_json = _extract_json(content_str)
	contentJSON := extractJSON(contentStr)

	// Python L383-385: 解析链三层。M-03 双分支对齐：
	// Parse 出错 → Python 分支 2 文案（JSONDecodeError——Python 侧因 parse
	// 全捕获实际不可达，Go 的 error 返回是等价的解码失败信号，映射到该
	// 文案以保留具体解码错误 + 完整内容不截断）；
	// 非 dict → Python 分支 1——rune 截断 500
	parsed, ok := parseLLMJSON(contentJSON)
	if !ok {
		// 兜底：JsonOutputParser（markdown 代码块提取 + 直接解析）
		fallback, perr := q.jsonOutputParser.Parse(contentJSON)
		if perr != nil {
			// Python L395-400: 155604 "is not valid JSON: {e}; compressed: {content_json}"
			return nil, exception.BuildError(
				exception.StatusRetrievalQueryRewriterOutputInvalid,
				exception.WithParam("error_msg", "LLM compress output is not valid JSON: "+perr.Error()+"; compressed: "+contentJSON),
			)
		}
		if m, isMap := fallback.(map[string]any); isMap {
			parsed, ok = m, true
		}
	}
	if !ok {
		// Python L386-393: 非 dict → 155604（含 content[:500]，rune 语义）
		return nil, exception.BuildError(
			exception.StatusRetrievalQueryRewriterOutputInvalid,
			exception.WithParam("error_msg", "LLM compress output is not valid JSON (parse returned non-dict); content: "+truncateForMsg(contentJSON, 500)),
		)
	}
	// Python L394: _schema_repair(compressed_json, self._compress_output_schema)
	repaired := schemaRepair(parsed, q.compressOutputSchema)
	// Python L409: logger.info("Compress completed: %s", dict)
	logger.Info(logComponent).
		Str("theme", fmt.Sprint(repaired["theme"])).
		Str("summary", fmt.Sprint(repaired["summary"])).
		Msg("Compress completed")
	return repaired, nil
}

// Rewrite 在当前会话上下文里把用户 query 改写为独立可检索的
// standalone_query；历史达 compressRange 时先触发一次压缩（失败降级
// 为原文写回），rewrite 提示词只携带最近 compressRange 条历史。
//
// Python: query_rewriter.py QueryRewriter.rewrite
func (q *QueryRewriter) Rewrite(ctx context.Context, query string) (map[string]any, error) {
	// Python L439-443: 非 str 或空白 → 155606（Go 入参已 string，
	// 只剩空白校验；非 str 分支编译期消解）
	if strings.TrimSpace(query) == "" {
		return nil, exception.BuildError(
			exception.StatusRetrievalQueryRewriterInputInvalid,
			exception.WithParam("error_msg", "query must be a non-empty string"),
		)
	}
	// Python L444: load_template("intention_completion")
	promptTemplate, err := q.LoadTemplate("intention_completion")
	if err != nil {
		return nil, err
	}

	// Python L447: 全量历史判断是否触发压缩
	historyFull, err := q.sessionCtx.GetMessages(0, true)
	if err != nil {
		return nil, err
	}

	// Python L449-467: 压缩触发 + 降级双通道
	if len(historyFull) >= q.compressRange {
		compressed, cerr := q.Compress(ctx, historyFull)
		if cerr == nil {
			// Python L452-456: json.dumps(summary) 写 compressed_history。
			// T-04：utils.MarshalNoEscape 对齐 ensure_ascii=False
			historyJSON, merr := utils.MarshalNoEscape(compressed)
			if merr == nil {
				q.sessionCtx.SetMessages([]llmschema.BaseMessage{
					llmschema.NewSystemMessage(string(historyJSON), llmschema.WithMessageName("compressed_history")),
				}, true)
			}
		} else {
			// Python L457-467: BaseError 降级——Warning + 原文写回 original_history
			logger.Warn(logComponent).
				Err(cerr).
				Msg("Query rewriter compress failed, falling back to original history")
			fallbackText, _ := q.Msg2Text(historyFull)
			q.sessionCtx.SetMessages([]llmschema.BaseMessage{
				llmschema.NewSystemMessage(fallbackText, llmschema.WithMessageName("original_history")),
			}, true)
		}
	}

	// Python L470-473: 只取最近 compressRange 条防提示词无限膨胀
	historyForRewrite, err := q.sessionCtx.GetMessages(q.compressRange, true)
	if err != nil {
		return nil, err
	}
	// Python L474: msg_2_text(history_for_rewrite)
	historyText, err := q.Msg2Text(historyForRewrite)
	if err != nil {
		return nil, err
	}
	// Python L476-480: 模板填 history + query（kwargs 声明序——T-03：
	// history 先替换，其值中的字面 {query} 占位符随后被 query 值替换）
	completionPrompt := fillTemplate(promptTemplate,
		templateKV{key: "history", value: historyText},
		templateKV{key: "query", value: query},
	)

	// Python L484-488: invoke（temperature 构造期已固定 0.0）
	resp, err := q.llm.Invoke(ctx,
		model_clients.NewMessagesParam(llmschema.NewSystemMessage(completionPrompt)),
		model_clients.WithInvokeTemperature(0.0))
	if err != nil {
		// Python L489-494: 155605
		return nil, exception.BuildError(
			exception.StatusRetrievalQueryRewriterLlmInvokeFailed,
			exception.WithParam("error_msg", err.Error()),
			exception.WithCause(err),
		)
	}
	// Python L495-496: strip + extract_json
	contentStr := ""
	if resp != nil {
		contentStr = strings.TrimSpace(resp.Content.Text())
	}
	contentJSON := extractJSON(contentStr)

	// Python L498-508: 解析链三层（同 Compress，M-03 双分支对齐：
	// Parse error → 分支 2 文案完整不截断；非 dict → 分支 1 rune 截 500）
	parsed, ok := parseLLMJSON(contentJSON)
	if !ok {
		fallback, perr := q.jsonOutputParser.Parse(contentJSON)
		if perr != nil {
			// Python L510-515: 155604 "is not valid JSON: {e}; content: {content_json}"
			return nil, exception.BuildError(
				exception.StatusRetrievalQueryRewriterOutputInvalid,
				exception.WithParam("error_msg", "LLM rewrite output is not valid JSON: "+perr.Error()+"; content: "+contentJSON),
			)
		}
		if m, isMap := fallback.(map[string]any); isMap {
			parsed, ok = m, true
		}
	}
	if !ok {
		// Python L501-508: 非 dict → 155604（含 content[:500]，rune 语义）
		return nil, exception.BuildError(
			exception.StatusRetrievalQueryRewriterOutputInvalid,
			exception.WithParam("error_msg", "LLM rewrite output is not valid JSON (parse returned non-dict); content: "+truncateForMsg(contentJSON, 500)),
		)
	}
	// Python L509: _schema_repair(rewrote_json, self._rewrite_output_schema)
	return schemaRepair(parsed, q.rewriteOutputSchema), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// truncateForMsg 截取前 n 个字符（rune）用于错误消息（Python content[:500]
// code point 语义——M-03：字节切片在 CJK 3 字节中间会截出无效 UTF-8）。
func truncateForMsg(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
