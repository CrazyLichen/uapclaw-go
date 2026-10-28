// 多跳 agentic 检索器：任意 Retriever 之上的 LLM 查询改写与多轮融合。
//
// 底层为 GraphRetriever 时自动启用图扩展与三元组链接；其他 Retriever
// 走迭代改写 + 结果融合（GeAR 策略，arXiv:2507.17399）。
//
// Python: openjiuwen/core/retrieval/retriever/agentic_retriever.py (AgenticRetriever)
package retriever

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
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
Original Question: 


Knowledge triples:


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

// Retrieve 多跳检索主链（Task 3 实现；Python retrieve L173-194）。
func (a *AgenticRetriever) Retrieve(ctx context.Context, query string, topK int, scoreThreshold *float64, mode RetrievalMode, opts ...RetrieveOption) ([]common.RetrievalResult, error) {
	return nil, nil // TODO(Task3): 155210 校验 + mode 回落 + 图/通用分派
}

// BatchRetrieve 批量并发检索（Task 3 实现；Python batch_retrieve L530-537）。
func (a *AgenticRetriever) BatchRetrieve(ctx context.Context, queries []string, topK int, opts ...RetrieveOption) ([][]common.RetrievalResult, error) {
	return nil, nil // TODO(Task3): 裸 errgroup
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
