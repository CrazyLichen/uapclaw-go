// 多跳 agentic 检索器：任意 Retriever 之上的 LLM 查询改写与多轮融合。
//
// 底层为 GraphRetriever 时自动启用图扩展与三元组链接；其他 Retriever
// 走迭代改写 + 结果融合（GeAR 策略，arXiv:2507.17399）。
//
// Python: openjiuwen/core/retrieval/retriever/agentic_retriever.py (AgenticRetriever)
package retriever

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	jsonrepair "github.com/RealAlexandreAI/json-repair"
	"golang.org/x/sync/errgroup"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/utils"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgenticRetriever 多跳 agentic 检索器。
//
// 构造期类型断言 *GraphRetriever 判定图路径（isinstance 同构）；
// IndexTypeCarrier 探测推导默认模式（getattr 链同构）；TripleMemory
// 跨轮累积事实；LLM 三件套（读/改写/链接）软失败降级不中断循环。
type AgenticRetriever struct {
	// retriever 底层检索器（nil→155208）
	retriever Retriever
	// isGraph 构造期类型断言结果（isinstance(x, GraphRetriever) 同构）
	isGraph bool
	// defaultMode 默认检索模式（index_type 推导）
	defaultMode RetrievalMode
	// llm LLM 客户端（nil→155209）
	llm model_clients.BaseModelClient
	// maxIter 最大迭代轮数（默认 2；非法值 Warn 回落）
	maxIter int
}

// AgenticRetrieverOption 构造选项。
type AgenticRetrieverOption func(*AgenticRetriever)

// 编译期断言：AgenticRetriever 满足 Retriever 接口。
var _ Retriever = (*AgenticRetriever)(nil)

// ──────────────────────────── 常量 ────────────────────────────

// readPrompt 读步骤提示词（Python _READ_PROMPT 逐字节复制，md5
// baf019b8d8627cfaf88b60a29a673c74）。占位符 {docs}/{query}/{facts}
// 经 strings.NewReplacer 填充（13.17 buildPrompt 先例）。
const readPrompt = `
Your task is to find facts that help answer an input question.

You should present these facts as knowledge triples, which are structured as ("subject", "predicate", "object").
Example:
Question: When was Neville A. Stanton's employer founded?
Facts: [["Neville A. Stanton", "employer", "University of Southampton"], ["University of Southampton", "founded in", "1862"]]

Now you are given some documents:
{docs}

Based on these documents and preliminary facts that may be provided below, find additional supporting fact(s) that may help answer the following question.
Note: if the information you are given is insufficient, output only the relevant facts you can find.

Question: {query}
Facts: {facts}

Output the facts as a JSON array of triples, where each triple is an array of [subject, predicate, object].
Example output format: [["subject1", "predicate1", "object1"], ["subject2", "predicate2", "object2"]]
`

// rewritePrompt 改写步骤提示词（Python _REWRITE_PROMPT 的 format 输出
// 形态——{{ }} 转义为单花括号后的语义等价常量，与 Python .format() 发给
// LLM 的串逐字节一致；占位符 {query}/{question_rewriting_history}/{triples}
// 三处保留）。偏差归档 spec §8-12：Python str.format 的 {{ 转义在 Go
// NewReplacer 无对应物，常量直接存转义后形态。
const rewritePrompt = `
Given a question and its associated retrieved knowledge triples, you
are asked to evaluate if the triples by themselves are sufficient
to formulate an answer to the original question.

You must respond with a JSON object in the following format:

{
  "sufficient": true/false,
  "next_question": "string or null"
}

- If the triples are sufficient, set "sufficient" to true and "next_question" to null
- If the triples are not sufficient, set "sufficient" to false and provide a 
suitable next question in the "next_question" field

When the triples are not sufficient, please think about the additional evidence that needs to be found to 
answer the original question, and then provide a suitable next question for retrieving this potential evidence. 
Note that you have access to all the question rewriting steps that have been performed already, if any. 
Please make sure that the next question is different from all the previous questions. Break it down into smaller questions if needed.
As the number of question rewriting steps that have been performed already increases, 
the next question should be more vague, optimising for retrieving at least some evidence that is relevant to the original question.

Here are some examples:

# Example 1:
Original Question: The Sentinelese language is the language of people of one of which islands in the Bay of Bengal ?
Knowledge triples:
(Sentinelese language, Indigenous to, Sentinelese people)
(Bay of Bengal, area, Andaman and Nicobar Islands)

Response:
{
  "sufficient": true,
  "next_question": null
}

# Example 2:
Original Question: Who is the coach of the team owned by David Beckham?
Knowledge triples:
(David Beckham, co-owned, Inter Miami CF)
(David Beckham, country of citizenship, United Kingdom)

Response:
{
  "sufficient": false,
  "next_question": "Who is the coach of Inter Miami CF?"
}

Now, please carefully consider the following case:

Question History:
Original Question: {query}
{question_rewriting_history}

Knowledge triples:
{triples}

Response (JSON only, no additional text):
`

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgenticRetriever 创建多跳 agentic 检索器。
//
// 对齐 Python __init__（L129-161）：retriever/llm_client 必填（None→
// 155208/155209，error_msg 逐字符）；max_iter 非法（非正整数）Warn
// 回落 2；isinstance 判图路径；getattr(retriever,"index_type",None)
// or "hybrid" 推导默认模式。
func NewAgenticRetriever(retriever Retriever, llm model_clients.BaseModelClient, opts ...AgenticRetrieverOption) (*AgenticRetriever, error) {
	if retriever == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverNotFound,
			exception.WithParam("error_msg", "retriever is required for AgenticRetriever"),
		)
	}
	if llm == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverLlmClientNotFound,
			exception.WithParam("error_msg", "llm_client is required for AgenticRetriever"),
		)
	}
	a := &AgenticRetriever{
		retriever: retriever,
		llm:       llm,
		maxIter:   2,
	}
	// isinstance(retriever, GraphRetriever)——同包类型断言（Q1 拍板）
	if _, ok := retriever.(*GraphRetriever); ok {
		a.isGraph = true
	}
	// index_type = getattr(retriever, "index_type", None) or "hybrid"
	indexType := ""
	if c, ok := retriever.(IndexTypeCarrier); ok {
		indexType = c.IndexType()
	}
	if indexType == "" {
		indexType = "hybrid"
	}
	switch indexType {
	case "vector":
		a.defaultMode = RetrievalModeVector
	case "bm25":
		a.defaultMode = RetrievalModeSparse
	default:
		a.defaultMode = RetrievalModeHybrid
	}
	for _, opt := range opts {
		opt(a)
	}
	return a, nil
}

// WithAgenticMaxIter 设置最大迭代轮数。
//
// 对齐 Python L148-154：isinstance(max_iter, int) and max_iter > 0 才
// 生效，否则 Warn 回落默认 2（不报错）。Go int 天然是 int，非 int 场景
// 编译期消解（偏差归档 spec §8-3）。
func WithAgenticMaxIter(n int) AgenticRetrieverOption {
	return func(a *AgenticRetriever) {
		if n > 0 {
			a.maxIter = n
			return
		}
		logger.Warn(logComponent).
			Int("max_iter", n).
			Msg("Invalid max_iter provided, falling back to default of 2")
		a.maxIter = 2
	}
}

// Retriever_ 返回底层检索器（对齐 Python self.retriever 属性；
// 命名带下划线避开与 Retriever 接口名的混淆）。
func (a *AgenticRetriever) Retriever_() Retriever { return a.retriever }

// LLM 返回 LLM 客户端（对齐 Python self.llm 属性）。
func (a *AgenticRetriever) LLM() model_clients.BaseModelClient { return a.llm }

// MaxIter 返回最大迭代轮数。
func (a *AgenticRetriever) MaxIter() int { return a.maxIter }

// IsGraphRetriever 底层是否为 GraphRetriever（对齐 Python property
// is_graph_retriever）。
func (a *AgenticRetriever) IsGraphRetriever() bool { return a.isGraph }

// DefaultMode 返回默认检索模式（对齐 Python property default_mode）。
func (a *AgenticRetriever) DefaultMode() RetrievalMode { return a.defaultMode }

// Retrieve 多跳检索主链。
//
// 对齐 Python retrieve（L173-194）：top_k 非正整数 → 155210（None 场景
// 编译期消解，偏差 §8-3）；mode 零值回落 defaultMode（13.19 偏差延续）；
// 图/通用路径分派。
func (a *AgenticRetriever) Retrieve(ctx context.Context, query string, topK int, scoreThreshold *float64, mode RetrievalMode, opts ...RetrieveOption) ([]common.RetrievalResult, error) {
	if topK <= 0 {
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverTopKInvalid,
			exception.WithParam("error_msg", "top_k is invalid, must be a positive integer"),
		)
	}
	resolvedMode := mode
	if mode == "" {
		resolvedMode = a.defaultMode
	}
	if a.isGraph {
		return a.retrieveWithGraph(ctx, query, topK, scoreThreshold, resolvedMode, opts...)
	}
	return a.retrieveGeneric(ctx, query, topK, scoreThreshold, resolvedMode, opts...)
}

// BatchRetrieve 批量并发检索。
//
// 对齐 Python batch_retrieve（L530-537）：裸 gather 全启动。Go 裸
// errgroup（不派生 ctx 不 cancel——13.19/13.23 先例，WithContext 会在
// 首错时取消兄弟协程，偏离 Python gather 语义）。Python 只透传 top_k
// 不透传 score_threshold/mode，Go 接口签名天然不含，1:1。
func (a *AgenticRetriever) BatchRetrieve(ctx context.Context, queries []string, topK int, opts ...RetrieveOption) ([][]common.RetrievalResult, error) {
	results := make([][]common.RetrievalResult, len(queries))
	var gg errgroup.Group
	for i, query := range queries {
		i, query := i, query
		gg.Go(func() error {
			ret, err := a.Retrieve(ctx, query, topK, nil, "", opts...)
			if err != nil {
				return err
			}
			results[i] = ret
			return nil
		})
	}
	if err := gg.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

// RetrieveSearchResults 返回 nil（Python 未覆写，继承基类空方法 None；
// 13.19 base.go 接口注释预留语义）。
func (a *AgenticRetriever) RetrieveSearchResults(context.Context, string, int, RetrievalMode, ...RetrieveOption) ([]common.SearchResult, error) {
	return nil, nil
}

// Close 关闭底层检索器（Python hasattr+iscoroutinefunction 双探测在
// Go 接口编译期消解，偏差归档 spec §8-4）。
func (a *AgenticRetriever) Close() {
	a.retriever.Close()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// retrieveWithGraph 图路径：图扩展 + 三元组链接 + 多轮融合。
//
// 对齐 Python _retrieve_with_graph（L196-251）：graph_expansion 默认
// true（kwargs.pop）；proximal _read 传当前轮 q、主 _read 传原始 query
// （Python L237 字面行为，偏差 §8-2）；末轮后 _linkPassages 把 memory
// 三元组链回 passages；RRF 输入 ret 在前 + history 在后（拼接序字面）。
func (a *AgenticRetriever) retrieveWithGraph(ctx context.Context, query string, topK int, scoreThreshold *float64, mode RetrievalMode, opts ...RetrieveOption) ([]common.RetrievalResult, error) {
	if err := a.assertGraphRetriever(); err != nil {
		return nil, err
	}
	options := NewRetrieveOptions(opts...)
	graphExpansion := true
	if options.GraphExpansion != nil {
		graphExpansion = *options.GraphExpansion
	}

	graphRetriever := a.retriever.(*GraphRetriever)
	queries := []string{query}
	var historyResults [][]common.RetrievalResult
	memory := common.NewTripleMemory()

	for turn := 1; turn <= a.maxIter; turn++ {
		q := queries[len(queries)-1]

		chunkRetriever, err := graphRetriever.GetRetrieverForMode(mode, true)
		if err != nil {
			return nil, err
		}
		chunkResults, err := chunkRetriever.Retrieve(ctx, q, topK, scoreThreshold, mode, opts...)
		if err != nil {
			return nil, err
		}

		if graphExpansion {
			proximalTriples := a.read(ctx, q, chunkResults, nil)
			linkedTriples, err := a.linkTriples(ctx, proximalTriples, mode, opts...)
			if err != nil {
				return nil, err
			}
			chunkResults, err = graphRetriever.GraphExpansion(ctx, q, chunkResults, linkedTriples, &topK, mode, opts...)
			if err != nil {
				return nil, err
			}
		}

		triples := a.read(ctx, query, chunkResults, memory.Memory())
		memory.BatchExtendMemory(triples)
		historyResults = append(historyResults, chunkResults)

		if turn >= a.maxIter {
			break
		}

		rewritten := a.rewrite(ctx, query, memory.TriplesStr(), queries)
		if rewritten == "" {
			break
		}
		queries = append(queries, rewritten)
	}

	ret, err := a.linkPassages(ctx, memory.Memory(), mode, opts...)
	if err != nil {
		return nil, err
	}
	fusionInput := append(ret, historyResults...)
	fused := utils.RRFFusion(fusionInput, 60)
	return truncateResults(fused, &topK), nil
}

// assertGraphRetriever 防御断言。
//
// 对齐 Python _assert_graph_retriever（L318-324）：非 GraphRetriever 调
// 图路径方法 → 155211。Go 类型系统保证仅 isGraph 分支可达（偏差 §8-10，
// 保留防御检查）。
func (a *AgenticRetriever) assertGraphRetriever() error {
	if _, ok := a.retriever.(*GraphRetriever); !ok {
		return exception.BuildError(
			exception.StatusRetrievalRetrieverInvalid,
			exception.WithParam("error_msg", "Underlying retriever must be a GraphRetriever to link passages"),
		)
	}
	return nil
}

// linkTriples 邻近三元组链接到 KB 三元组。
//
// 对齐 Python _link_triples（L415-479）：每 triple 一个
// retrieve_search_results（top_k=1）裸 gather（return_exceptions=True
// 全跑完语义，13.17 方案 A 无信号量版）；三分类聚合（error→failed 计数/
// 空列表→failed+Warn 类型日志/非空→取 [0]）；Deduplicate 按 ID 键；
// 映射 RetrievalResult（doc_id/chunk_id 非 string 收窄空串，偏差 §8-7；
// metadata nil 防御对齐 or {}）。
func (a *AgenticRetriever) linkTriples(ctx context.Context, triples []common.TripleFact, mode RetrievalMode, opts ...RetrieveOption) ([]common.RetrievalResult, error) {
	if err := a.assertGraphRetriever(); err != nil {
		return nil, err
	}
	graphRetriever := a.retriever.(*GraphRetriever)
	tripleRetriever, err := graphRetriever.GetRetrieverForMode(mode, false)
	if err != nil {
		return nil, err
	}

	// 预分配槽位 + WaitGroup：各 goroutine 写各的槽（gather 全跑完语义）
	searchResults := make([][]common.SearchResult, len(triples))
	failedFlags := make([]bool, len(triples))
	var wg sync.WaitGroup
	for i, triple := range triples {
		i, triple := i, triple
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, err := tripleRetriever.RetrieveSearchResults(ctx, tripleFactStr(triple), 1, mode, opts...)
			if err != nil {
				failedFlags[i] = true
				return
			}
			if len(results) == 0 {
				logger.Warn(logComponent).
					Str("type", fmt.Sprintf("%T", results)).
					Msg("Empty or unexpected result type during link triples")
				failedFlags[i] = true
				return
			}
			searchResults[i] = results
		}()
	}
	wg.Wait()

	var allTriples []common.SearchResult
	failed := 0
	for i := range triples {
		if failedFlags[i] {
			failed++
			continue
		}
		allTriples = append(allTriples, searchResults[i][0])
	}
	if failed > 0 {
		logger.Warn(logComponent).
			Int("failed", failed).
			Int("total", len(triples)).
			Msg("Triple linking failed for triples")
	}

	deduped := utils.Deduplicate(allTriples, func(r common.SearchResult) string { return r.ID })
	retrievalResults := make([]common.RetrievalResult, 0, len(deduped))
	for _, result := range deduped {
		metadata := result.Metadata
		if metadata == nil {
			metadata = map[string]any{}
		}
		docID, _ := metadata["doc_id"].(string)
		chunkID, _ := metadata["chunk_id"].(string)
		retrievalResults = append(retrievalResults, common.RetrievalResult{
			Text:     result.Text,
			Score:    result.Score,
			Metadata: metadata,
			DocID:    docID,
			ChunkID:  chunkID,
		})
	}
	return retrievalResults, nil
}

// linkPassages memory 三元组链接回 passages。
//
// 对齐 Python _link_passages（L481-528）：每 triple 一个 Retrieve
// （top_k=5）裸 gather 三分类聚合；成功项整列表保留（不取 [0]）；
// 返回 [][]RetrievalResult。
func (a *AgenticRetriever) linkPassages(ctx context.Context, triples []common.TripleFact, mode RetrievalMode, opts ...RetrieveOption) ([][]common.RetrievalResult, error) {
	if err := a.assertGraphRetriever(); err != nil {
		return nil, err
	}
	graphRetriever := a.retriever.(*GraphRetriever)
	chunkRetriever, err := graphRetriever.GetRetrieverForMode(mode, true)
	if err != nil {
		return nil, err
	}

	results := make([][]common.RetrievalResult, len(triples))
	failedFlags := make([]bool, len(triples))
	var wg sync.WaitGroup
	for i, triple := range triples {
		i, triple := i, triple
		wg.Add(1)
		go func() {
			defer wg.Done()
			ret, err := chunkRetriever.Retrieve(ctx, tripleFactStr(triple), 5, nil, mode, opts...)
			if err != nil {
				failedFlags[i] = true
				return
			}
			if len(ret) == 0 {
				logger.Warn(logComponent).
					Str("type", fmt.Sprintf("%T", ret)).
					Msg("Empty or unexpected result type during link passages")
				failedFlags[i] = true
				return
			}
			results[i] = ret
		}()
	}
	wg.Wait()

	var out [][]common.RetrievalResult
	failed := 0
	for i := range triples {
		if failedFlags[i] {
			failed++
			continue
		}
		out = append(out, results[i])
	}
	if failed > 0 {
		logger.Warn(logComponent).
			Int("failed", failed).
			Int("total", len(triples)).
			Msg("Link passages failed for triples")
	}
	return out, nil
}

// retrieveGeneric 通用路径：迭代查询改写 + RRF 融合。
//
// 对齐 Python _retrieve_generic（L253-296）：每轮底层 retrieve → _read
// 抽三元组入 TripleMemory → _rewrite 判充分性（空=break）；融合
// history_results 截 top_k。注意：主 _read 传当前轮 q（Python L278
// `self._read(q, ...)` 字面如此——与图路径 L237 传原始 query 在 Python
// 源码中本就不一致，各自字面复刻）。参考：Millions of GeAR-s
// （arXiv:2507.17399）。
func (a *AgenticRetriever) retrieveGeneric(ctx context.Context, query string, topK int, scoreThreshold *float64, mode RetrievalMode, opts ...RetrieveOption) ([]common.RetrievalResult, error) {
	queries := []string{query}
	var historyResults [][]common.RetrievalResult
	memory := common.NewTripleMemory()

	for turn := 1; turn <= a.maxIter; turn++ {
		q := queries[len(queries)-1]

		chunkResults, err := a.retriever.Retrieve(ctx, q, topK, scoreThreshold, mode, opts...)
		if err != nil {
			return nil, err
		}

		triples := a.read(ctx, q, chunkResults, memory.Memory())
		memory.BatchExtendMemory(triples)
		historyResults = append(historyResults, chunkResults)

		if turn >= a.maxIter {
			break
		}

		rewritten := a.rewrite(ctx, query, memory.TriplesStr(), queries)
		if rewritten == "" {
			break
		}
		queries = append(queries, rewritten)
	}

	fused := utils.RRFFusion(historyResults, 60)
	return truncateResults(fused, &topK), nil
}

// llmCall 单次 LLM 调用（软失败）。
//
// 对齐 Python _llm_call_async（L298-316）：Invoke 失败 Warn 返回 None
// （Go (空串, false)）；resp.content 的 hasattr 分支编译期消解
// （偏差 §8-4），Go AssistantMessage.Content.Text()。
func (a *AgenticRetriever) llmCall(ctx context.Context, prompt string) (string, bool) {
	resp, err := a.llm.Invoke(ctx,
		model_clients.NewMessagesParam(llmschema.NewUserMessage(prompt)),
		model_clients.WithInvokeTemperature(0.0))
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("Agentic LLM invocation failed")
		return "", false
	}
	if resp == nil {
		return "", false
	}
	return resp.Content.Text(), true
}

// read 读步骤：从 passages 抽知识三元组（软失败 → 空切片）。
//
// 对齐 Python _read（L374-413）：docs 跳过空文本截前 5 + "\n\n" join；
// facts 串 Python tuple repr（('s', 'p', 'o')）逗号空格 join，空兜底
// "None"（[3]string 零值不过滤——Python 非空 tuple 恒 True，偏差 §8-8）；
// jsonrepair 根非 list Warn；逐项非 list/长度≠3 丢弃；pyStr 逐元素。
func (a *AgenticRetriever) read(ctx context.Context, query string, passages []common.RetrievalResult, existingFacts []common.TripleFact) []common.TripleFact {
	var docParts []string
	for i, p := range passages {
		if i >= 5 {
			break
		}
		if p.Text != "" {
			docParts = append(docParts, p.Text)
		}
	}
	docs := strings.Join(docParts, "\n\n")
	factsStr := "None"
	if len(existingFacts) > 0 {
		parts := make([]string, 0, len(existingFacts))
		for _, fact := range existingFacts {
			parts = append(parts, pyTupleRepr(fact))
		}
		factsStr = strings.Join(parts, ", ")
	}
	prompt := strings.NewReplacer(
		"{docs}", docs,
		"{query}", query,
		"{facts}", factsStr,
	).Replace(readPrompt)

	content, ok := a.llmCall(ctx, prompt)
	if !ok {
		return nil
	}

	result, err := jsonrepair.RepairWithOptions(content, jsonrepair.RepairOptions{})
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("Failed to parse read response as JSON")
		return nil
	}
	arr, isList := result.Value.([]any)
	if !isList {
		logger.Warn(logComponent).Any("response", result.Value).Msg("Read response JSON is not a list")
		return nil
	}
	var triples []common.TripleFact
	for _, item := range arr {
		triple, isTriple := item.([]any)
		if !isTriple || len(triple) != 3 {
			continue
		}
		var fact common.TripleFact
		for j, elem := range triple {
			fact[j] = pyStr(elem)
		}
		triples = append(triples, fact)
	}
	return triples
}

// rewrite 改写步骤：判充分性，不足则生成下一问（软失败 → 空串=break）。
//
// 对齐 Python _rewrite（L326-372）：历史行 "Rewritten Question {i}: {q}"
// 逐行 join（history[1:] 起），单元素历史兜底 "(No rewriting steps yet)"；
// jsonrepair 根非 dict Warn；sufficient 缺省 False；next_question 非空
// 才返回。类型收窄偏差 §8-6：next_question 必须 string，非 string 视为
// falsy → 空串（Python 病态输入会进循环，Go 强类型收窄）。
func (a *AgenticRetriever) rewrite(ctx context.Context, query, triplesStr string, questionHistory []string) string {
	historyStr := "(No rewriting steps yet)"
	if len(questionHistory) > 1 {
		lines := make([]string, 0, len(questionHistory)-1)
		for i, q := range questionHistory[1:] {
			lines = append(lines, fmt.Sprintf("Rewritten Question %d: %s", i+1, q))
		}
		historyStr = strings.Join(lines, "\n")
	}
	prompt := strings.NewReplacer(
		"{query}", query,
		"{triples}", triplesStr,
		"{question_rewriting_history}", historyStr,
	).Replace(rewritePrompt)

	content, ok := a.llmCall(ctx, prompt)
	if !ok {
		return ""
	}

	result, err := jsonrepair.RepairWithOptions(content, jsonrepair.RepairOptions{})
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("Failed to parse rewrite response as JSON")
		return ""
	}
	obj, isDict := result.Value.(map[string]any)
	if !isDict {
		logger.Warn(logComponent).Any("response", result.Value).Msg("Rewrite response JSON is not an object")
		return ""
	}
	sufficient := pyTruthy(obj["sufficient"])
	nextQuestion, _ := obj["next_question"].(string)
	if sufficient || nextQuestion == "" {
		return ""
	}
	return nextQuestion
}

// pyTupleRepr Python tuple repr：('s', 'p', 'o')——单引号+逗号空格。
// str(fact) 进 facts 串（提示词注入），字面复刻 Python repr 形态。
func pyTupleRepr(f common.TripleFact) string {
	quoted := make([]string, 3)
	for i, s := range f {
		quoted[i] = "'" + s + "'"
	}
	return "(" + strings.Join(quoted, ", ") + ")"
}

// pyTruthy Python truthy 判定（sufficient 字段：response_json.get
// ("sufficient", False)）——缺失 nil falsy；bool 直判；数值非零 truthy；
// 其余容器非空 truthy 由 fmt 兜底（Go 只可能收到 JSON 标量）。
func pyTruthy(v any) bool {
	switch val := v.(type) {
	case nil:
		return false
	case bool:
		return val
	case float64:
		return val != 0
	case json.Number:
		f, err := val.Float64()
		return err == nil && f != 0
	case string:
		return val != ""
	default:
		return true
	}
}

// tripleFactStr 三元组转检索串（" ".join(map(str, triple))——TripleFact
// 元素已是 string，直接空格 join）。
func tripleFactStr(triple common.TripleFact) string {
	return strings.Join(triple[:], " ")
}

// pyStr 标量转 Python str() 形态（读取侧复刻，偏差 §8-9）。
//
// 13.17 extractor 包 pyStr 同源语义（私有复刻，不动 extractor 公开面——
// 读取侧/写入侧管线分立，13.23 pyAnyStr 先例）：
//   - nil → "None"；bool → "True"/"False"；string 原样
//   - json.Number（合法 JSON 短路路径）：整数字面原样，浮点走 pyFloatStr
//   - float64（深修复路径小数）：pyFloatStr；int/int64：十进制
func pyStr(v any) string {
	switch val := v.(type) {
	case nil:
		return "None"
	case string:
		return val
	case bool:
		if val {
			return "True"
		}
		return "False"
	case json.Number:
		s := val.String()
		if _, err := strconv.Atoi(s); err == nil {
			return s
		}
		if isPureIntegerLiteral(s) {
			return s
		}
		if f, err := val.Float64(); err == nil {
			return pyFloatStr(f)
		}
		return s
	case float64:
		return pyFloatStr(val)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	default:
		return fmt.Sprint(v)
	}
}

// pyFloatStr 还原 Python str(float) 记法（13.17 pyFloatStr 同源复刻）。
// abs ∈ [1e-4, 1e16) 用 f 记法（整数值补 ".0"），其余 e 记法；
// nan/inf 特殊字面。
func pyFloatStr(f float64) string {
	if math.IsNaN(f) {
		return "nan"
	}
	if math.IsInf(f, 1) {
		return "inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	abs := math.Abs(f)
	if abs == 0 || (abs >= 1e-4 && abs < 1e16) {
		s := strconv.FormatFloat(f, 'f', -1, 64)
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		return s
	}
	return strconv.FormatFloat(f, 'e', -1, 64)
}

// isPureIntegerLiteral 判纯整数字面量：可选符号 + 全数字（无小数点/指数）。
// 13.17 同源复刻：Atoi 溢出的超界整数字面原样透传（Python int 无界）。
func isPureIntegerLiteral(s string) bool {
	if s == "" {
		return false
	}
	body := s
	if body[0] == '+' || body[0] == '-' {
		body = body[1:]
	}
	if body == "" {
		return false
	}
	for _, r := range body {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
