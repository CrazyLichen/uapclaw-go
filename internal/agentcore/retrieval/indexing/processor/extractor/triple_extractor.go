package extractor

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TripleExtractor 基于 LLM 的 OpenIE 三元组抽取器。
//
// Python: openjiuwen/core/retrieval/indexing/processor/extractor/triple_extractor.py
type TripleExtractor struct {
	// llmClient LLM 客户端实例（Python: llm_client: Any → Go 具体类型，13.15 先例）
	llmClient *llm.Model
	// modelName 模型名（Python 死参数：存字段但 invoke 从不使用——偏差 §8-1）
	modelName string
	// temperature 温度参数（invoke 时透传）
	temperature float64
	// limiter 并发信号量（对齐 asyncio.Semaphore(max_concurrent)）
	limiter chan struct{}
}

// ──────────────────────────── 枚举 ────────────────────────────

// TripleExtractorOption TripleExtractor 构造选项函数。
type TripleExtractorOption func(*TripleExtractor)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore

	// tripleExtractionPrompt OpenIE 三元组抽取提示词模板。
	//
	// Python: triple_extractor.py TripleExtractor._build_prompt 内模板
	//（逐字复制，{{/}} 归一为 {/}——Python .format 转义字面花括号；
	// 禁止翻译改写——记忆 feedback_prompt_verbatim_copy 既定规则）。
	// 归一后 {title}/{passage} 为唯二命名槽。
	tripleExtractionPrompt = `# Instruction

Your task is to construct an RDF-style graph from the given title and passage.
Extract named entities and relationships, then return the result as exactly one valid JSON object.

Each triple should represent a meaningful relationship in the graph.
Each triple should contain at least one, and preferably two, named entities from the title or passage.
Clearly resolve pronouns to specific names whenever possible.

Return only one valid JSON object in this format:
{
  "named_entities": ["entity1", "entity2"],
  "triples": [
    ["subject1", "predicate1", "object1"],
    ["subject2", "predicate2", "object2"]
  ]
}

Requirements:
- Output valid JSON only. Do not use markdown, comments, or extra text.
- Return exactly one top-level JSON object.
- The top-level object must contain exactly two keys: "named_entities" and "triples".
- "named_entities" must be a JSON array of strings.
- "triples" must be a JSON array.
- Each item in "triples" must be a JSON array of exactly three strings.
- Do not output tuples, objects, or arrays with more than three elements inside "triples".
- Use double quotes for all JSON strings.
- If no triples are found, return {"named_entities": [...], "triples": []}.
- Resolve pronouns to specific names when possible.
- Prefer triples that use at least one, and preferably two, named entities from the title or passage.
- Keep entity and predicate wording consistent with the source language.
- Do not include duplicate triples.

# Demonstration 1

Title:
Magic Johnson

Passage:
After winning a national championship with Michigan State in 1979, Johnson was selected first overall in the 1979 NBA draft by the Lakers, leading the team to five NBA championships during their "Showtime" era.

Output:
{
  "named_entities": [
    "Michigan State",
    "national championship",
    "1979",
    "Magic Johnson",
    "National Basketball Association",
    "Los Angeles Lakers",
    "NBA Championship"
  ],
  "triples": [
    ["Magic Johnson", "member of sports team", "Michigan State"],
    ["Michigan State", "award", "national championship"],
    ["Michigan State", "award date", "1979"],
    ["Magic Johnson", "draft pick number", "1"],
    ["Magic Johnson", "drafted in", "1979"],
    ["Magic Johnson", "drafted by", "Los Angeles Lakers"],
    ["Magic Johnson", "member of sports team", "Los Angeles Lakers"],
    ["Magic Johnson", "league", "National Basketball Association"],
    ["Los Angeles Lakers", "league", "National Basketball Association"],
    ["Los Angeles Lakers", "award received", "NBA Championship"]
  ]
}

# Demonstration 2

Title:
Elden Ring

Passage:
Elden Ring is a 2022 action role-playing game developed by FromSoftware. It was directed by Hidetaka Miyazaki with worldbuilding provided by American fantasy writer George R. R. Martin.

Output:
{
  "named_entities": [
    "Elden Ring",
    "2022",
    "action role-playing game",
    "FromSoftware",
    "Hidetaka Miyazaki",
    "United States of America",
    "fantasy",
    "George R. R. Martin"
  ],
  "triples": [
    ["Elden Ring", "publication", "2022"],
    ["Elden Ring", "genre", "action role-playing game"],
    ["Elden Ring", "publisher", "FromSoftware"],
    ["Elden Ring", "director", "Hidetaka Miyazaki"],
    ["Elden Ring", "screenwriter", "George R. R. Martin"],
    ["George R. R. Martin", "country of citizenship", "United States of America"],
    ["George R. R. Martin", "genre", "fantasy"]
  ]
}

# Input

Title:
{title}

Passage:
{passage}
`
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言 TripleExtractor 实现 Extractor（Task 6 实现 Extract/Process 后恢复）
// var _ Extractor = (*TripleExtractor)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithTripleTemperature 设置温度参数。
func WithTripleTemperature(t float64) TripleExtractorOption {
	return func(e *TripleExtractor) { e.temperature = t }
}

// WithTripleMaxConcurrent 设置最大并发数。
func WithTripleMaxConcurrent(n int) TripleExtractorOption {
	return func(e *TripleExtractor) { e.limiter = make(chan struct{}, n) }
}

// NewTripleExtractor 构造 TripleExtractor。
// 默认 temperature=0.0 / maxConcurrent=50（对齐 Python 默认参数）。
//
// Python: triple_extractor.py TripleExtractor.__init__
func NewTripleExtractor(llmClient *llm.Model, modelName string, opts ...TripleExtractorOption) *TripleExtractor {
	e := &TripleExtractor{
		llmClient:   llmClient,
		modelName:   modelName,
		temperature: 0.0,
		limiter:     make(chan struct{}, 50),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildPrompt 构建 OpenIE 三元组抽取提示词。
//
// title 空 → "Untitled"（对齐 Python title or "Untitled"）；strings.NewReplacer
// 仅替换 {title}/{passage} 唯二命名槽——不用 fmt.Sprintf（正文大量花括号会炸）。
// 渲染结果与 Python .format() 逐字节等价。
//
// Python: triple_extractor.py TripleExtractor._build_prompt
func (e *TripleExtractor) buildPrompt(passage, title string) string {
	if title == "" {
		title = "Untitled"
	}
	return strings.NewReplacer("{title}", title, "{passage}", passage).Replace(tripleExtractionPrompt)
}
