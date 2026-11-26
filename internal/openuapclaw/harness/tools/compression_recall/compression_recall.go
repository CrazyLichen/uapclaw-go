package compressionrecall

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/compressor/recall"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	harnessprompts "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/prompts/tools"
	harnessools "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools"
)

// ──────────────────────────── 常量 ────────────────────────────

// missHints 未命中提示模板（{archive_count}/{archive_list}/{recall_root} 占位）。
// Python: compression_recall.py:14 (_MISS_HINTS)
var missHints = map[string]string{
	"cn": "未找到匹配的原文片段。建议：1) 换关键词重试——使用同义词、另一种语言、或原文中的标识符" +
		"（如函数名、文件路径、错误码）；2) 省略 memory_id，跨本 session 的全部 {archive_count} 个压缩归档搜索" +
		"（{archive_list}）；3) 自行浏览归档根目录 {recall_root}——每个归档内 turns.jsonl 是轮次索引，" +
		"chunks/ 保存压缩前的原文。",
	"en": "No matching source chunks found. Suggestions: 1) retry with different keywords — synonyms, another " +
		"language, or identifiers from the original text (function names, file paths, error codes); 2) omit " +
		"memory_id to search across all {archive_count} compression archives of this session ({archive_list}); " +
		"3) browse the archive root {recall_root} yourself — turns.jsonl is the turn index and chunks/ holds " +
		"the original pre-compression text.",
}

// ──────────────────────────── 结构体 ────────────────────────────

// CompressionRecallTool 会话压缩归档召回工具（tool.Tool 完整实现）。
// Python: compression_recall.py:102 (CompressionRecallTool(Tool))
type CompressionRecallTool struct {
	card         *tool.ToolCard
	workspaceDir string
	language     string
}

// NewCompressionRecallTool 创建工具。
// Python: compression_recall.py:105 (__init__)
func NewCompressionRecallTool(workspaceDir string, language string, agentID string) (*CompressionRecallTool, *tool.ToolCard, error) {
	if language == "" {
		language = "cn"
	}
	card, err := harnessprompts.BuildToolCard(
		"recall_compressed_context",
		"CompressionRecallTool",
		language,
		map[string]string{},
		agentID,
	)
	if err != nil {
		return nil, nil, err
	}
	return &CompressionRecallTool{card: card, workspaceDir: workspaceDir, language: language}, card, nil
}

// ──────────────────────────── 导出函数（tool.Tool 实现） ────────────────────────────

// Card 返回工具卡片。
func (t *CompressionRecallTool) Card() *tool.ToolCard { return t.card }

// GetCallTimeout 沿用卡片级超时。
func (t *CompressionRecallTool) GetCallTimeout(_ map[string]any, defaultTimeout float64) float64 {
	return defaultTimeout
}

// Invoke 召回入口（B-15：query 由调用方从结构化 user content 抽取后传入）。
// Python: compression_recall.py:113 (invoke)
func (t *CompressionRecallTool) Invoke(_ context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	options := &tool.ToolCallOptions{}
	for _, apply := range opts {
		apply(options)
	}
	sessionID := ""
	if options.Session != nil {
		sessionID = options.Session.GetSessionID()
	}
	output := t.Recall(inputs, sessionID)
	return output.ToMap(), nil
}

// Stream 不支持流式。
// Python: compression_recall.py:140 (stream 空实现)
func (t *CompressionRecallTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// Recall 直接召回入口（rail/扩展消费）。
func (t *CompressionRecallTool) Recall(inputs map[string]any, sessionID string) harnessools.ToolOutput {
	memoryID := strings.TrimSpace(stringField(inputs, "memory_id"))
	query := strings.TrimSpace(stringField(inputs, "query"))
	if sessionID == "" {
		return harnessools.ToolOutput{Success: false, Error: "compression recall requires the current runtime session"}
	}
	result, err := recall.RecallCompressedContext(t.workspaceDir, sessionID, memoryID, query)
	if err != nil {
		return harnessools.ToolOutput{Success: false, Error: err.Error()}
	}
	chunks, _ := result["chunks"].([]any)
	if len(chunks) == 0 {
		result["hint"] = buildMissHint(result, t.language)
	}
	data := map[string]any{}
	for key, value := range result {
		data[key] = value
	}
	data["content"] = renderRecallContent(result, t.language)
	return harnessools.ToolOutput{Success: true, Data: data}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildMissHint 未命中提示渲染。
// Python: compression_recall.py:29 (_build_miss_hint)
func buildMissHint(result map[string]any, language string) string {
	archives, _ := result["archives_in_session"].([]any)
	var names []string
	for _, item := range archives {
		if m, ok := item.(map[string]any); ok {
			if id, ok := m["memory_id"].(string); ok {
				names = append(names, id)
			}
		}
	}
	archiveList := strings.Join(names, ", ")
	if archiveList == "" {
		archiveList = "-"
	}
	recallRoot := ""
	if v, ok := result["recall_root"].(string); ok && v != "" {
		recallRoot = v
	} else if archivePath, ok := result["archive_path"].(string); ok {
		recallRoot = filepath.Dir(archivePath)
	}
	template, ok := missHints[language]
	if !ok {
		template = missHints["cn"]
	}
	template = strings.ReplaceAll(template, "{archive_count}", fmt.Sprintf("%d", len(archives)))
	template = strings.ReplaceAll(template, "{archive_list}", archiveList)
	return strings.ReplaceAll(template, "{recall_root}", recallRoot)
}

// renderRecallContent 召回结果渲染为模型可见紧凑文本。
// Python: compression_recall.py:44 (_render_recall_content)
func renderRecallContent(result map[string]any, language string) string {
	chunks, _ := result["chunks"].([]any)
	if len(chunks) == 0 {
		if hint, ok := result["hint"].(string); ok {
			return hint
		}
		return ""
	}
	archivePath, _ := result["archive_path"].(string)
	recallRoot, _ := result["recall_root"].(string)
	if recallRoot == "" && archivePath != "" {
		recallRoot = filepath.Dir(archivePath)
	}
	archiveCount := 0
	if archives, ok := result["archives_in_session"].([]any); ok {
		archiveCount = len(archives)
	}
	returnedTokens, _ := result["returned_tokens"].(int)
	truncated, _ := result["truncated"].(bool)
	source, _ := result["memory_id"].(string)
	var header, chunkTpl string
	if language == "en" {
		truncNote := ""
		if truncated {
			truncNote = ", budget reached, more chunks remain in the archive"
		}
		header = fmt.Sprintf(
			"Recalled %d source chunk(s) from compression archive %s (~%d tokens%s).\n"+
				"This session has %d compression archive(s); current archive: %s "+
				"(turns.jsonl is the turn index, chunks/ holds this compression's source text)\n"+
				"Archive root: %s — all compressed archives of this session live here; "+
				"subdirectory names are {timestamp}_{first 10 chars of query}, so you can browse "+
				"other archives by time and topic yourself",
			len(chunks), orDash(source, language), returnedTokens, truncNote, archiveCount, archivePath, recallRoot,
		)
		chunkTpl = "\n\n--- chunk %d | archive %s | %s | %s | score %.2f ---\n%s"
	} else {
		truncNote := ""
		if truncated {
			truncNote = "，已达上限，归档中还有未返回的片段"
		}
		header = fmt.Sprintf(
			"已从压缩归档 %s 召回 %d 个原文片段（约 %d tokens%s）。\n本 session 共有 %d 个压缩归档；当前归档目录：%s"+
				"（turns.jsonl 为轮次索引，chunks/ 为该次压缩的原文）\n归档根目录：%s —— 本会话所有压缩落盘都在此目录下，"+
				"子目录名格式为 {时间戳}_{query前10字符}，需要其他压缩内容时可按时间和主题自行浏览查找",
			orDash(source, language), len(chunks), returnedTokens, truncNote, archiveCount, archivePath, recallRoot,
		)
		chunkTpl = "\n\n--- 片段 %d | 归档 %s | %s | %s | 相关度 %.2f ---\n%s"
	}
	parts := []string{header}
	for index, item := range chunks {
		chunk, ok := item.(map[string]any)
		if !ok {
			continue
		}
		body, _ := chunk["content"].(string)
		parts = append(parts, fmt.Sprintf(chunkTpl,
			index+1,
			stringField(chunk, "memory_id"),
			stringField(chunk, "turn_id"),
			stringField(chunk, "chunk_id"),
			floatField(chunk, "score"),
			strings.TrimSpace(body),
		))
	}
	return strings.Join(parts, "")
}

func stringField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func floatField(m map[string]any, key string) float64 {
	v, _ := m[key].(float64)
	return v
}

func orDash(source string, language string) string {
	if source == "" {
		if language == "en" {
			return "(searched across all archives)"
		}
		return "（跨全部归档搜索）"
	}
	return source
}
