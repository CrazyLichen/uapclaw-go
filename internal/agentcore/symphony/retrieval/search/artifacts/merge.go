// merge.go 对齐 Python search/artifacts/merge.py（40 行）：命中→搜索结果组装。
// SearchResult 定义对齐 Python search/service/models.py:126-136（16.5 领地文件，
// 此处仅落地 merge.py 依赖的数据类；16.5 实施 service/models.go 时收口去重，
// 登记偏差同 D-16-4-2 模式）。
package artifacts

import "fmt"

// SearchResult 搜索结果（对齐 Python service/models.py SearchResult dataclass）
type SearchResult struct {
	// Method 检索方法
	Method string
	// Payloads 候选负载列表
	Payloads []string
	// CandidateRecords 候选记录
	CandidateRecords []map[string]any
	// SummaryLines 摘要行
	SummaryLines []string
	// SelectedPayload 首选负载（空串表 None）
	SelectedPayload string
	// SelectedRank 首选排名（无候选 -1）
	SelectedRank int
	// ElapsedMs 耗时毫秒
	ElapsedMs float64
	// TraceEvents 追踪事件
	TraceEvents []map[string]any
}

// HitsToSearchResult 命中列表组装为 SearchResult。
// 对齐 Python hits_to_search_result（merge.py:8-29）：payloads 取 resolved_payload、
// 摘要行 "N. choice -> payload (source=...)"、首选=首候选（rank 1，无候选 -1）。
func HitsToSearchResult(method string, source string, elapsedMs float64, traceEvents []map[string]any, candidateRecords []map[string]any) SearchResult {
	payloads := make([]string, 0, len(candidateRecords))
	for _, record := range candidateRecords {
		payloads = append(payloads, fmt.Sprintf("%v", record["resolved_payload"]))
	}
	summaryLines := make([]string, 0, len(candidateRecords))
	for i, record := range candidateRecords {
		summaryLines = append(summaryLines, formatAppendSummaryLine(i+1, record, source))
	}
	selectedPayload := ""
	selectedRank := -1
	if len(payloads) > 0 {
		selectedPayload = payloads[0]
		selectedRank = 1
	}
	return SearchResult{
		Method:           method,
		Payloads:         payloads,
		CandidateRecords: candidateRecords,
		SummaryLines:     summaryLines,
		SelectedPayload:  selectedPayload,
		SelectedRank:     selectedRank,
		ElapsedMs:        elapsedMs,
		TraceEvents:      traceEvents,
	}
}

// formatAppendSummaryLine 摘要行格式（对齐 merge.py:32-35）。
func formatAppendSummaryLine(index int, record map[string]any, source string) string {
	choiceID := fmt.Sprintf("%v", record["choice_id"])
	if choiceID == "<nil>" || choiceID == "" {
		choiceID = fmt.Sprintf("%v", record["raw_output"])
	}
	resolvedPayload := fmt.Sprintf("%v", record["resolved_payload"])
	return fmt.Sprintf("%d. %s -> %s (source=%s)", index, choiceID, resolvedPayload, source)
}
