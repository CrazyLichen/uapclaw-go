package reranker

import (
	"context"
	"fmt"
	"math"
	"strings"

	reranker "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/utils"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/version"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChatReranker 基于 Chat Completion 的重排序客户端。
//
// 利用 Chat 模型 + logit_bias 限制输出为 yes/no，
// 从 logprobs 中提取 P("yes") 作为相关性分数。
// 每次只能对 1 个文档进行重排序。
//
// 对应 Python: openjiuwen/core/retrieval/reranker/chat_reranker.py (ChatReranker)
type ChatReranker struct {
	// StandardReranker 嵌入标准重排序器
	*StandardReranker
	// yesNoIDs "yes" 和 "no" 的 token ID
	yesNoIDs [2]int
	// endPoint API 端点，覆盖为 "/chat/completions"
	endPoint string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// chatEndPoint Chat Completion API 端点
	chatEndPoint = "/chat/completions"
	// docTemplate 文档模板
	docTemplate = "<Document>: {doc}"
	// systemInstruct 系统指令
	systemInstruct = `Judge whether the Document meets the requirements based on the Query and the Instruct provided. Note that the answer can only be "yes" or "no".`
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 确保编译时接口合规
var _ reranker.BaseReranker = (*ChatReranker)(nil)

// 抑制未使用导入警告
var _ = fmt.Sprintf

// ──────────────────────────── 导出函数 ────────────────────────────

// NewChatReranker 创建 Chat 重排序客户端。
// 必须提供 config.YesNoIDs（长度为 2 的有效 int 数组）。
// 记录实验性功能警告日志。
// 对齐 Python: ChatReranker.__init__
func NewChatReranker(config reranker.RerankerConfig, opts ...StandardRerankerOption) (*ChatReranker, error) {
	// 记录实验性功能警告
	logger.Warn(logComponent).
		Str("event_type", "chat_reranker_experimental").
		Str("version", version.Version).
		Msg("ChatReranker 支持处于实验阶段，请注意")

	// 校验 YesNoIDs
	if config.YesNoIDs == [2]int{} {
		return nil, exception.ValidateError(exception.StatusRetrievalRerankerInputInvalid,
			exception.WithParam("error_msg", `ChatReranker 要求在 RerankerConfig 中指定 "yes_no_ids"`),
		)
	}

	// 去除 APIBase 尾部的 /rerank 后缀（NewStandardReranker 会处理）
	// ChatReranker 使用 /chat/completions 端点
	sr, err := NewStandardReranker(config, opts...)
	if err != nil {
		return nil, err
	}

	c := &ChatReranker{
		StandardReranker: sr,
		yesNoIDs:         config.YesNoIDs,
		endPoint:         chatEndPoint,
	}

	return c, nil
}

// TestCompatibility 测试服务是否支持基于 chat completion 的重排序。
// 对齐 Python: ChatReranker.test_compatibility
func (c *ChatReranker) TestCompatibility(ctx context.Context) (bool, error) {
	disabled := false
	_, err := c.RerankSync(ctx, "test", []string{"test"}, reranker.RerankOption{InstructEnabled: &disabled})
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "chat_reranker_compatibility_error").
			Err(err).
			Msg("所选服务不支持基于 chat completion 的重排序")
		return false, err
	}
	return true, nil
}

// Rerank 覆盖 StandardReranker.Rerank，增加 size=1 校验
func (c *ChatReranker) Rerank(ctx context.Context, query string, docs []string, opts ...reranker.RerankOption) (map[string]float64, error) {
	docsAny := make([]any, len(docs))
	for i, d := range docs {
		docsAny[i] = d
	}
	return c.doRerank(ctx, query, docsAny, resolveOption(opts...))
}

// RerankDocs 覆盖 StandardReranker.RerankDocs
func (c *ChatReranker) RerankDocs(ctx context.Context, query string, docs []*reranker.Document, opts ...reranker.RerankOption) (map[string]float64, error) {
	docsAny := make([]any, len(docs))
	for i, d := range docs {
		docsAny[i] = d
	}
	return c.doRerank(ctx, query, docsAny, resolveOption(opts...))
}

// RerankSync 覆盖 StandardReranker.RerankSync
func (c *ChatReranker) RerankSync(ctx context.Context, query string, docs []string, opts ...reranker.RerankOption) (map[string]float64, error) {
	docsAny := make([]any, len(docs))
	for i, d := range docs {
		docsAny[i] = d
	}
	return c.doRerankSync(ctx, query, docsAny, resolveOption(opts...))
}

// RerankDocsSync 覆盖 StandardReranker.RerankDocsSync
func (c *ChatReranker) RerankDocsSync(ctx context.Context, query string, docs []*reranker.Document, opts ...reranker.RerankOption) (map[string]float64, error) {
	docsAny := make([]any, len(docs))
	for i, d := range docs {
		docsAny[i] = d
	}
	return c.doRerankSync(ctx, query, docsAny, resolveOption(opts...))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// assembleParams 组装请求参数，严格限制 size=1。
// 覆盖 StandardReranker.assembleParams
// 对齐 Python: ChatReranker._assemble_params
func (c *ChatReranker) assembleParams(query string, docs []any, opt *reranker.RerankOption) (map[string]string, map[string]any, []string) {
	// 严格限制 size=1，校验在 doRerank 中执行，此处不做额外处理

	docIDs := make([]string, len(docs))
	texts := make([]string, len(docs))
	for i, doc := range docs {
		switch d := doc.(type) {
		case string:
			docIDs[i] = d
			texts[i] = d
		case *reranker.Document:
			docIDs[i] = d.ID
			texts[i] = d.Text
		}
	}

	headers := c.requestHeaders()
	params := c.requestParams(query, texts, 1, opt)

	return headers, params, docIDs
}

// requestParams 构造 chat completion 格式的请求参数。
// 覆盖 RerankerBase.requestParams
// 对齐 Python: ChatReranker._request_params
func (c *ChatReranker) requestParams(query string, documents []string, topN int, opt *reranker.RerankOption) map[string]any {
	doc := ""
	if len(documents) > 0 {
		doc = documents[0]
	}

	// 构造 instruct
	instruct := reranker.DefaultInstruct
	if opt != nil && opt.CustomInstruct != "" {
		instruct = opt.CustomInstruct
	}

	// 构造用户消息内容：queryTemplate + docTemplate
	content := reranker.FormatQuery(query, instruct)
	content += strings.Replace(docTemplate, "{doc}", doc, 1)

	messages := []map[string]any{
		{"role": "system", "content": systemInstruct},
		{"role": "user", "content": content},
	}

	params := map[string]any{
		"model":        c.config.ModelName,
		"messages":     messages,
		"temperature":  0,
		"max_tokens":   1,
		"logprobs":     true,
		"top_logprobs": 5,
		"logit_bias":   map[int]int{c.yesNoIDs[0]: 5, c.yesNoIDs[1]: 5},
	}

	// 合并 ExtraBody
	for k, v := range c.config.ExtraBody {
		params[k] = v
	}

	// 合并 ExtraParams
	if opt != nil && opt.ExtraParams != nil {
		for k, v := range opt.ExtraParams {
			params[k] = v
		}
	}

	return params
}

// parseResponse 解析 chat completion 响应中的 logprobs，计算相关性分数。
// 覆盖 RerankerBase.parseResponse
// 对齐 Python: ChatReranker._parse_response
// 修改：当 logprobs 不支持时返回 error，而非静默返回 0.0
func (c *ChatReranker) parseResponse(responseData map[string]any, docIDs []string) (map[string]float64, error) {
	yesScores := []float64{0}
	noScores := []float64{0}

	// 从 choices[0] 获取 choice
	choices, ok := responseData["choices"].([]any)
	if !ok || len(choices) == 0 {
		return map[string]float64{firstDocID(docIDs): 0.0}, nil
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return map[string]float64{firstDocID(docIDs): 0.0}, nil
	}

	// 获取 logprobs
	logprobsData, ok := choice["logprobs"].(map[string]any)
	if !ok || logprobsData == nil {
		// logprobs 不支持：记录 Error 日志并返回错误，对齐 Python: raise build_error
		logger.Error(logComponent).
			Str("event_type", "llm_call_error").
			Str("method", "ChatReranker.parseResponse").
			Str("model_provider", c.config.ModelName).
			Msg("服务不支持 logprobs，ChatReranker 无法工作")
		return nil, exception.BuildError(
			exception.StatusRetrievalRerankerRequestCallFailed,
			exception.WithParam("error_msg", "服务不支持 logprobs，ChatReranker 无法工作"),
		)
	}

	// 获取 content（优先）或 logprobs 本身
	content, ok := logprobsData["content"].([]any)
	if !ok || len(content) == 0 {
		// 尝试直接从 logprobs 获取
		return map[string]float64{firstDocID(docIDs): 0.0}, nil
	}

	// 获取 top_logprobs
	topLogprobs, ok := content[0].(map[string]any)
	if !ok {
		return map[string]float64{firstDocID(docIDs): 0.0}, nil
	}
	topLogprobsList, ok := topLogprobs["top_logprobs"].([]any)
	if !ok || len(topLogprobsList) == 0 {
		return map[string]float64{firstDocID(docIDs): 0.0}, nil
	}

	// 遍历 top_logprobs
	for _, item := range topLogprobsList {
		tokenInfo, ok := item.(map[string]any)
		if !ok {
			continue
		}
		tokenText, _ := tokenInfo["token"].(string)
		logprob, _ := tokenInfo["logprob"].(float64)

		// strip + casefold，对齐 Python token_text.strip().casefold()
		normalized := strings.ToLower(strings.TrimSpace(tokenText))

		if strings.HasPrefix(normalized, "yes") {
			yesScores = append(yesScores, math.Exp(logprob))
		} else if strings.HasPrefix(normalized, "no") {
			noScores = append(noScores, math.Exp(logprob))
		}
	}

	confidence := maxFloat(yesScores)
	totalProb := confidence + maxFloat(noScores)

	docID := firstDocID(docIDs)
	if totalProb == 0 {
		return map[string]float64{docID: 0.0}, nil
	}

	return map[string]float64{docID: confidence / totalProb}, nil
}

// doRerank 执行异步重排序，增加 size=1 校验
func (c *ChatReranker) doRerank(ctx context.Context, query string, docs []any, opt *reranker.RerankOption) (map[string]float64, error) {
	if len(docs) != 1 {
		return nil, exception.ValidateError(exception.StatusRetrievalRerankerInputInvalid,
			exception.WithParam("error_msg", "ChatReranker 输入必须是长度为 1 的 list[str | Document]"),
		)
	}

	headers, params, docIDs := c.assembleParams(query, docs, opt)

	cfg := utils.RetryConfig{
		MaxRetries: c.maxRetries,
		RetryWait:  c.retryWait,
		Task:       utils.TaskReranker,
	}

	result, err := utils.RequestWithRetry(ctx, c.httpClient, c.config.APIBase+c.endPoint, params, headers, cfg)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "llm_call_error").
			Str("method", "ChatRerank").
			Str("model_provider", c.config.ModelName).
			Err(err).
			Msg("ChatReranker 请求失败")
		return nil, err
	}

	return c.parseResponse(result, docIDs)
}

// doRerankSync 执行同步重排序，增加 size=1 校验
func (c *ChatReranker) doRerankSync(ctx context.Context, query string, docs []any, opt *reranker.RerankOption) (map[string]float64, error) {
	if len(docs) != 1 {
		return nil, exception.ValidateError(exception.StatusRetrievalRerankerInputInvalid,
			exception.WithParam("error_msg", "ChatReranker 输入必须是长度为 1 的 list[str | Document]"),
		)
	}

	headers, params, docIDs := c.assembleParams(query, docs, opt)

	cfg := utils.RetryConfig{
		MaxRetries: c.maxRetries,
		RetryWait:  c.retryWait,
		Task:       utils.TaskReranker,
	}

	result, err := utils.RequestWithRetrySync(ctx, c.httpClient, c.config.APIBase+c.endPoint, params, headers, cfg)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "llm_call_error").
			Str("method", "ChatRerankSync").
			Str("model_provider", c.config.ModelName).
			Err(err).
			Msg("ChatReranker 同步请求失败")
		return nil, err
	}

	return c.parseResponse(result, docIDs)
}

// firstDocID 返回第一个文档 ID，用于 ChatReranker 结果
func firstDocID(docIDs []string) string {
	if len(docIDs) > 0 {
		return docIDs[0]
	}
	return ""
}

// maxFloat 返回 float64 切片中的最大值
func maxFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}
