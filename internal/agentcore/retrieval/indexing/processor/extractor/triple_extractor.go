package extractor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	jsonrepair "github.com/RealAlexandreAI/json-repair"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// chunkResult 单 chunk 抽取结果（方案 A 预分配槽位载体）。
type chunkResult struct {
	// triples 抽取成功的三元组
	triples []common.Triple
	// err 抽取失败错误
	err error
}

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

// 编译期断言 TripleExtractor 实现 Extractor
var _ Extractor = (*TripleExtractor)(nil)

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

// Extract 并发抽取 chunks 的三元组（方案 A：预分配槽位 + 信号量 + 收齐扫描）。
//
// 语义逐字对齐 Python asyncio.gather(return_exceptions=True)：
//   - 全部任务跑完才扫描（无 fail-fast 取消）
//   - 成功结果按 chunk 序合并
//   - 首错按 chunk 序（非完成序）选取
//   - BaseError 原样返回（对齐 isinstance first_error BaseError raise）
//   - 首错时成功结果不返回（对齐 Python extend 后随 raise 丢弃——偏差 §8-7）
//
// Python: triple_extractor.py TripleExtractor.extract（L47-145）
func (e *TripleExtractor) Extract(ctx context.Context, chunks []common.TextChunk, _ ...ExtractOption) ([]common.Triple, error) {
	results := make([]chunkResult, len(chunks)) // 空 chunks → 空 make，扫描零循环直接返回
	var wg sync.WaitGroup
	for i := range chunks {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// 对齐 async with self.limiter（包住 invoke+解析全流程）
			e.limiter <- struct{}{}
			defer func() { <-e.limiter }()
			results[i] = e.extractChunk(ctx, chunks[i]) // 各写各的槽位，无竞争
		}(i)
	}
	wg.Wait() // 收齐全部（gather 全跑完语义）

	allTriples := make([]common.Triple, 0)
	var firstErr error
	var firstErrChunkID string
	for i := range chunks { // 按 chunk 序扫描（对齐 for idx, result in enumerate）
		if results[i].err != nil {
			// 对齐 Python: logger.error(f"Task failed for chunk {chunk_id}: {result}")
			logger.Error(logComponent).
				Str("chunk_id", chunks[i].ID).
				Err(results[i].err).
				Msg("Task failed for chunk")
			if firstErr == nil {
				firstErr = results[i].err
				firstErrChunkID = chunks[i].ID
			}
			continue
		}
		allTriples = append(allTriples, results[i].triples...)
	}

	if firstErr != nil {
		var be *exception.BaseError
		if errors.As(firstErr, &be) { // isinstance(first_error, BaseError): raise 原样
			return nil, firstErr
		}
		return nil, exception.BuildError(
			exception.StatusRetrievalKbTripleExtractionProcessError,
			exception.WithParam("error_msg", firstErrChunkID+": "+firstErr.Error()),
			exception.WithCause(firstErr),
		)
	}
	return allTriples, nil
}

// Process 处理文本分块（Processor 契约兼容入口），转发 Extract。
//
// Python: base.py Extractor.process → self.extract(chunks, **kwargs)
func (e *TripleExtractor) Process(ctx context.Context, chunks []common.TextChunk, opts ...ExtractOption) ([]common.Triple, error) {
	return e.Extract(ctx, chunks, opts...)
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

// parseTriples 解析 LLM 输出为 Triple 列表。
//
// 返回 (triples, parseSuccess)：硬解析失败 false；空三元组列表 true
// （对齐 Python ([], True)）；全 invalid → (空, false) 触发上层 155507。
//
// Python: triple_extractor.py TripleExtractor._parse_triples（L265-338）
func (e *TripleExtractor) parseTriples(content, docID, chunkID string) ([]common.Triple, bool) {
	triples := []common.Triple{}

	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		if len(lines) > 2 {
			content = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	result, err := jsonrepair.RepairWithOptions(content, jsonrepair.RepairOptions{})
	if err != nil {
		// 对齐 Python: logger.error("Failed to parse triples from content: %s. Content: %s", e, content[:200])
		logger.Error(logComponent).
			Err(err).
			Str("content", truncateContent(content, 200)).
			Msg("Failed to parse triples from content")
		return []common.Triple{}, false
	}

	var tripleList []any
	switch parsed := result.Value.(type) {
	case map[string]any:
		triplesField, exists := parsed["triples"]
		if !exists {
			return []common.Triple{}, false
		}
		list, isList := triplesField.([]any)
		if !isList {
			return []common.Triple{}, false
		}
		tripleList = list
	case []any:
		tripleList = parsed
	default:
		return []common.Triple{}, false
	}

	if len(tripleList) == 0 {
		return []common.Triple{}, true
	}

	invalidCount := 0
	for _, tripleData := range tripleList {
		arr, isArr := tripleData.([]any)
		if !isArr {
			// 对齐 isinstance(triple_data, (list, tuple))：JSON 解码无 tuple 形态
			invalidCount++
			continue
		}
		if len(arr) < 3 {
			invalidCount++
			continue
		}
		head := arr[:3]
		if hasInvalidHeadItem(head) {
			invalidCount++
			continue
		}
		triples = append(triples, common.Triple{
			Subject:   strings.TrimSpace(pyStr(head[0])),
			Predicate: strings.TrimSpace(pyStr(head[1])),
			Object:    strings.TrimSpace(pyStr(head[2])),
			// 对齐 metadata={"doc_id": doc_id, "chunk_id": chunk_id}
			Metadata: map[string]any{"doc_id": docID, "chunk_id": chunkID},
		})
	}

	if invalidCount > 0 {
		// 对齐 Python: logger.warning("Ignored %d invalid triples for chunk %s during parsing", invalid_count, chunk_id)
		logger.Warn(logComponent).
			Int("invalid_count", invalidCount).
			Str("chunk_id", chunkID).
			Msg("Ignored invalid triples for chunk during parsing")
	}

	return triples, len(triples) > 0
}

// hasInvalidHeadItem 检查三元组头部是否含嵌套容器或 null
//（对齐 any(isinstance(x, (list, tuple, dict)) or x is None for x in head)）。
func hasInvalidHeadItem(head []any) bool {
	for _, x := range head {
		switch x.(type) {
		case []any, map[string]any, nil:
			return true
		}
	}
	return false
}

// pyStr 对齐 Python str() 的字符串化（13.13 cellStr 同源语义，包内独立实现）。
//
// 仅做类型字符串化，不做 strip（对齐 str() 单步；strip 由 parseTriples
// 统一 TrimSpace 三字段）。
//
// jsonrepair Value 数字三态全覆盖（探针实测）：
//   - json.Number（合法 JSON 短路路径）：先 Atoi 整数字面原样，失败
//     ParseFloat 走 pyFloatStr 记法还原（对齐 Python json.loads 的
//     int/float 分野：str(1)="1"、str(1.0)="1.0"、str(1e16)="1e+16"）
//   - float64（深修复路径小数）：pyFloatStr
//   - int（深修复路径整数）：strconv.Itoa
//
// bool → "True"/"False"；string 原样；nil 不可达（hasInvalidHeadItem
// 已滤 None）——防御保留返回 "None"；其余 fmt.Sprint。
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
		// 整数字面优先（Python int 无界，str 原样）；浮点字面走记法还原
		if i, err := strconv.Atoi(val.String()); err == nil {
			_ = i
			return val.String()
		}
		if f, err := val.Float64(); err == nil {
			return pyFloatStr(f)
		}
		return val.String()
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

// pyFloatStr 还原 Python str(float) 记法（13.13 excel_value.go 同源语义，
// 包内独立实现）。abs ∈ [1e-4, 1e16) 用 f 记法（整数值补 ".0"），
// 其余 e 记法；nan/inf 特殊字面。
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

// metadataTitle 读取 chunk 元数据 title（对齐 chunk.metadata.get("title", "")）。
func metadataTitle(metadata map[string]any) string {
	if v, ok := metadata["title"]; ok {
		if s, isStr := v.(string); isStr {
			return s
		}
	}
	return ""
}

// truncateContent 截取字符串前 n 个字符用于日志输出（content[:200] 对齐）。
func truncateContent(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// extractChunk 单 chunk 抽取：构建提示词 → Invoke → 解析。
//
// 错误统一 wrap 155507（对齐 Python except Exception as e → build_error）。
// nil llmClient → 155507 硬失败（Python None.invoke AttributeError 被 wrap
// 的等价路径——偏差 §8-5）。
//
// Python: triple_extractor.py TripleExtractor.extract 内 _extract_chunk（L70-115）
func (e *TripleExtractor) extractChunk(ctx context.Context, chunk common.TextChunk) chunkResult {
	if e.llmClient == nil {
		return chunkResult{err: exception.BuildError(
			exception.StatusRetrievalKbTripleExtractionProcessError,
			exception.WithParam("error_msg", chunk.ID+": llm client is nil"),
		)}
	}

	// 对齐 prompt = self._build_prompt(chunk.text, chunk.metadata.get("title", ""))
	prompt := e.buildPrompt(chunk.Text, metadataTitle(chunk.Metadata))
	messages := model_clients.NewMessagesParam(llmschema.NewUserMessage(prompt))

	resp, err := e.llmClient.Invoke(ctx, messages,
		model_clients.WithInvokeTemperature(e.temperature))
	if err != nil {
		// 对齐 Python: logger.error(f"Failed to extract triples from chunk {chunk.id_}: {e}")
		logger.Error(logComponent).
			Str("chunk_id", chunk.ID).
			Err(err).
			Msg("Failed to extract triples from chunk")
		return chunkResult{err: exception.BuildError(
			exception.StatusRetrievalKbTripleExtractionProcessError,
			exception.WithParam("error_msg", chunk.ID+": "+err.Error()),
			exception.WithCause(err),
		)}
	}

	var content string
	if resp != nil {
		content = resp.Content.Text()
	}
	triples, ok := e.parseTriples(content, chunk.DocID, chunk.ID)
	if !ok {
		return chunkResult{err: exception.BuildError(
			exception.StatusRetrievalKbTripleExtractionProcessError,
			exception.WithParam("error_msg", chunk.ID+": LLM response could not be parsed as valid triple JSON"),
		)}
	}
	return chunkResult{triples: triples}
}
