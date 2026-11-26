package recall

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	fsupport "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/compressor/support"
	cesupport "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/support"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ArchiveSchemaVersion 归档 schema 版本
	// Python: archive.py ARCHIVE_SCHEMA_VERSION
	ArchiveSchemaVersion = 1
	// DefaultChunkSizeTokens 归档切分的默认单块 token 上限
	// Python: archive.py DEFAULT_CHUNK_SIZE_TOKENS
	DefaultChunkSizeTokens = 3000
	// DefaultChunkOverlapTokens 归档切分的默认相邻块重叠
	// Python: archive.py DEFAULT_CHUNK_OVERLAP_TOKENS
	DefaultChunkOverlapTokens = 300
	// RecallDirName 归档根目录名
	// Python: archive.py RECALL_DIR_NAME
	RecallDirName = "compression_recall"
)

// bareSectionHeaders 渲染产生的裸角色头（无内容提示），chunk slug 跳过它们。
// Python: archive.py _BARE_SECTION_HEADERS
var bareSectionHeaders = map[string]bool{
	"user": true, "assistant": true, "assistant reasoning": true,
}

// ──────────────────────────── 结构体 ────────────────────────────

// CompressionArchive 一次压缩归档的落盘位置。
// Python: archive.py:33 (CompressionArchive)
type CompressionArchive struct {
	// MemoryID 归档唯一标识（时间戳或 uuid hex）
	MemoryID string
	// Path 归档目录绝对路径
	Path string
}

// turn 一次对话轮的归档分组。
// Python: archive.py:41 (_Turn)
type turn struct {
	turnID   string
	query    string
	answer   string
	messages []llm_schema.BaseMessage
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ArchiveCompressionMessages 把一次压缩替换掉的消息原子性归档。
// Python: archive.py:48 (archive_compression_messages)
func ArchiveCompressionMessages(
	mc iface.ModelContext,
	processorType string,
	originalMessages []llm_schema.BaseMessage,
	messagesToCompress []llm_schema.BaseMessage,
	precedingMessages []llm_schema.BaseMessage,
	chunkSizeTokens int,
	chunkOverlapTokens int,
) (*CompressionArchive, error) {
	if chunkOverlapTokens >= chunkSizeTokens {
		return nil, fmt.Errorf("chunk_overlap_tokens must be smaller than chunk_size_tokens")
	}
	workspaceDir := contextValue(mc, "workspace_dir")
	sessionID := contextValue(mc, "session_id")
	contextID := contextValue(mc, "context_id")
	if workspaceDir == "" {
		return nil, fmt.Errorf("compression recall requires a workspace directory")
	}
	if sessionID == "" {
		return nil, fmt.Errorf("compression recall requires a session id")
	}

	recallRoot := recallRoot(workspaceDir, sessionID)
	if err := validateArchiveRoot(workspaceDir, recallRoot); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(recallRoot, 0o755); err != nil {
		return nil, err
	}
	if err := validateArchiveRoot(workspaceDir, recallRoot); err != nil {
		return nil, err
	}
	memoryID, err := newMemoryID(recallRoot)
	if err != nil {
		return nil, err
	}
	fallbackQuery := latestRealUserContent(precedingMessages)
	turns := groupTurns(messagesToCompress, fallbackQuery)
	archiveQuery := latestNonEmptyQuery(turns)
	if archiveQuery == "" {
		archiveQuery = fallbackQuery
	}
	directoryName := fmt.Sprintf("%s_%s", memoryID, querySlug(archiveQuery))
	finalPath := filepath.Join(recallRoot, directoryName)
	temporaryPath := filepath.Join(recallRoot, fmt.Sprintf(".%s.%s.tmp", directoryName, uuid.New().String()))

	// 写临时目录 → os.replace 原子替换；失败时清理临时目录
	succeed := func() (err error) {
		defer func() {
			if err != nil {
				os.RemoveAll(temporaryPath)
			}
		}()
		chunksDir := filepath.Join(temporaryPath, "chunks")
		if err = os.MkdirAll(chunksDir, 0o755); err != nil {
			return err
		}
		var turnRecords []map[string]any
		turnRecords, err = writeTurns(turns, chunksDir, chunkSizeTokens, chunkOverlapTokens)
		if err != nil {
			return err
		}
		if err = writeJSONLines(filepath.Join(temporaryPath, "turns.jsonl"), turnRecords); err != nil {
			return err
		}
		raw := make([]map[string]any, 0, len(messagesToCompress))
		for _, message := range messagesToCompress {
			raw = append(raw, messageToJSON(message))
		}
		if err = writeJSON(filepath.Join(temporaryPath, "raw_messages.json"), raw); err != nil {
			return err
		}
		manifest := map[string]any{
			"schema_version":                 ArchiveSchemaVersion,
			"memory_id":                      memoryID,
			"session_id":                     sessionID,
			"context_id":                     contextID,
			"processor_type":                 processorType,
			"created_at":                     time.Now().UTC().Format("2006-01-02T15:04:05.000000Z07:00"),
			"source_message_count":           len(messagesToCompress),
			"original_context_message_count": len(originalMessages),
			"turn_count":                     len(turnRecords),
			"files": map[string]any{
				"turns":        "turns.jsonl",
				"raw_messages": "raw_messages.json",
				"chunks":       "chunks",
			},
		}
		return writeJSON(filepath.Join(temporaryPath, "manifest.json"), manifest)
	}()
	if succeed != nil {
		return nil, succeed
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		os.RemoveAll(temporaryPath)
		return nil, err
	}
	return &CompressionArchive{MemoryID: memoryID, Path: finalPath}, nil
}

// DeleteCompressionArchive 删除一个归档目录（失败压缩的回滚，best-effort）。
// Python: archive.py:120 (delete_compression_archive)
func DeleteCompressionArchive(archive *CompressionArchive) error {
	path := archive.Path
	resolvedRoot, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return err
	}
	resolvedPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("compression recall archive path escapes the recall root")
	}
	if !strings.HasPrefix(filepath.Base(resolvedPath), archive.MemoryID+"_") {
		return fmt.Errorf("compression recall archive directory does not match the memory id")
	}
	os.RemoveAll(resolvedPath)
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeTurns 逐轮渲染正文、切 chunk 并写盘，返回轮记录。
// Python: archive.py:134 (_write_turns)
func writeTurns(turns []turn, chunksDir string, chunkSizeTokens int, chunkOverlapTokens int) ([]map[string]any, error) {
	records := make([]map[string]any, 0, len(turns))
	for _, tr := range turns {
		body := renderTurn(tr.messages)
		bodies := splitText(body, chunkSizeTokens, chunkOverlapTokens)
		chunkPaths := make([]string, 0, len(bodies))
		for chunkIndex, chunkBody := range bodies {
			chunkID := fmt.Sprintf("%s_chunk_%03d", tr.turnID, chunkIndex)
			filename := fmt.Sprintf("%s_%s.md", chunkID, chunkSlug(chunkBody))
			relativePath := "chunks/" + filename
			chunkPaths = append(chunkPaths, relativePath)
			markdown := "---\n" +
				fmt.Sprintf("turn_id: %s\n", tr.turnID) +
				fmt.Sprintf("chunk_id: %s\n", chunkID) +
				fmt.Sprintf("chunk_index: %d\n", chunkIndex) +
				"---\n\n" +
				strings.TrimRight(chunkBody, " \t\n") + "\n"
			if err := os.WriteFile(filepath.Join(chunksDir, filename), []byte(markdown), 0o644); err != nil {
				return nil, err
			}
		}
		records = append(records, map[string]any{
			"turn_id":     tr.turnID,
			"query":       tr.query,
			"answer":      tr.answer,
			"chunk_paths": chunkPaths,
		})
	}
	return records, nil
}

// groupTurns 按真实用户消息把待压缩消息分组为轮。
// Python: archive.py:171 (_group_turns)
func groupTurns(messages []llm_schema.BaseMessage, fallbackQuery string) []turn {
	type group struct {
		query    string
		messages []llm_schema.BaseMessage
	}
	var groups []group
	var currentMessages []llm_schema.BaseMessage
	currentQuery := fallbackQuery
	for _, message := range messages {
		if isRealUserMessage(message) && len(currentMessages) > 0 {
			groups = append(groups, group{query: currentQuery, messages: currentMessages})
			currentMessages = nil
			currentQuery = contentToQueryText(message.GetContent())
		} else if isRealUserMessage(message) {
			currentQuery = contentToQueryText(message.GetContent())
		}
		currentMessages = append(currentMessages, message)
	}
	if len(currentMessages) > 0 {
		groups = append(groups, group{query: currentQuery, messages: currentMessages})
	}

	turns := make([]turn, 0, len(groups))
	for index, g := range groups {
		var answerParts []string
		for _, message := range g.messages {
			if message.GetRole() != llm_schema.RoleTypeAssistant {
				continue
			}
			if text := contentToText(message.GetContent()); strings.TrimSpace(text) != "" {
				answerParts = append(answerParts, text)
			}
		}
		turns = append(turns, turn{
			turnID:   fmt.Sprintf("turn_%03d", index),
			query:    g.query,
			answer:   strings.Join(answerParts, "\n"),
			messages: g.messages,
		})
	}
	return turns
}

// renderTurn 把一轮消息渲染为 markdown 分节文本。
// Python: archive.py:204 (_render_turn)
func renderTurn(messages []llm_schema.BaseMessage) string {
	var sections []string
	for _, message := range messages {
		content := contentToText(message.GetContent())
		switch message.GetRole() {
		case llm_schema.RoleTypeUser:
			sections = append(sections, "## User\n"+content)
			continue
		case llm_schema.RoleTypeAssistant:
			if assistant, ok := message.(*llm_schema.AssistantMessage); ok {
				if assistant.ReasoningContent != "" {
					sections = append(sections, "## Assistant Reasoning\n"+assistant.ReasoningContent)
				}
				if content != "" {
					sections = append(sections, "## Assistant\n"+content)
				}
				for _, toolCall := range assistant.ToolCalls {
					name := toolCall.Name
					if name == "" {
						name = "unknown"
					}
					sections = append(sections, fmt.Sprintf("## Tool Call: %s\n%s", name, contentToText(toolCall.Arguments)))
				}
				continue
			}
			if content != "" {
				sections = append(sections, "## Assistant\n"+content)
			}
			continue
		case llm_schema.RoleTypeTool:
			toolCallID := ""
			if tool, ok := message.(*llm_schema.ToolMessage); ok {
				toolCallID = tool.ToolCallID
			}
			sections = append(sections, fmt.Sprintf("## Tool Result: %s\n%s", toolCallID, content))
			continue
		}
		role := message.GetRole().String()
		if role == "" {
			role = "message"
		}
		sections = append(sections, fmt.Sprintf("## %s\n%s", strings.Title(role), content))
	}
	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

// splitText 按 token×4 字节窗口切分正文，带重叠。
// Python str 按码点索引（text[end] 永远是完整字符），Go 侧以 []rune 等价实现；
// UTF-8 bytes / 4 是离线近似，避免 tokenizer 冷启动网络下载。
// Python: archive.py:230 (_split_text)
func splitText(text string, chunkSizeTokens int, chunkOverlapTokens int) []string {
	if text == "" {
		return nil
	}
	byteSize := chunkSizeTokens * 4
	byteOverlap := chunkOverlapTokens * 4
	if len(text) <= byteSize { // Go string len == UTF-8 字节数
		return []string{text}
	}
	runes := []rune(text)
	n := len(runes)
	var chunks []string
	start := 0
	for start < n {
		end := start
		usedBytes := 0
		for end < n {
			charBytes := runeLen(runes[end])
			if usedBytes+charBytes > byteSize {
				break
			}
			usedBytes += charBytes
			end++
		}
		if end == start {
			end++
		}
		chunks = append(chunks, string(runes[start:end]))
		if end >= n {
			break
		}
		nextStart := end
		overlapBytes := 0
		for nextStart > start {
			charBytes := runeLen(runes[nextStart-1])
			if overlapBytes+charBytes > byteOverlap {
				break
			}
			overlapBytes += charBytes
			nextStart--
		}
		if nextStart <= start {
			nextStart = end
		}
		start = nextStart
	}
	return chunks
}

// runeLen 单个 rune 的 UTF-8 字节数
func runeLen(r rune) int {
	switch {
	case r < 0x80:
		return 1
	case r < 0x800:
		return 2
	case r < 0x10000:
		return 3
	default:
		return 4
	}
}

// recallRoot 归档根目录：{workspace}/context/{safe_session}-{digest}_context/compression_recall。
// Python: archive.py:274 (_recall_root)
func recallRoot(workspaceDir string, sessionID string) string {
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(sessionID)))[:12]
	sessionDir := fmt.Sprintf("%s-%s_context", safeFilenamePart(sessionID), digest)
	return filepath.Join(workspaceDir, "context", sessionDir, RecallDirName)
}

// validateArchiveRoot 校验归档根不逃逸 workspace 且路径中无符号链接。
// Python: archive.py:280 (_validate_archive_root)
func validateArchiveRoot(workspaceDir string, recallRootPath string) error {
	resolvedWorkspace, err := filepath.Abs(workspaceDir)
	if err != nil {
		return fmt.Errorf("compression recall path escapes the workspace")
	}
	rel, err := filepath.Rel(resolvedWorkspace, recallRootPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("compression recall path escapes the workspace")
	}
	current := workspaceDir
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		current = filepath.Join(current, part)
		if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("compression recall path contains a symbolic link")
		}
	}
	if resolved, err := filepath.EvalSymlinks(recallRootPath); err == nil {
		if absRel, relErr := filepath.Rel(resolvedWorkspace, resolved); relErr != nil || strings.HasPrefix(absRel, "..") {
			return fmt.Errorf("compression recall path escapes the workspace")
		}
	}
	return nil
}

// newMemoryID 生成归档唯一标识：UTC 时间戳，冲突时回退 uuid hex。
// Python: archive.py:298 (_new_memory_id)
func newMemoryID(recallRootPath string) (string, error) {
	candidate := time.Now().UTC().Format("20060102T150405.000000Z")
	candidate = strings.ReplaceAll(candidate, ".", "")
	matches, err := filepath.Glob(filepath.Join(recallRootPath, candidate+"_*"))
	if err != nil {
		return uuid.New().String(), nil
	}
	if len(matches) > 0 {
		return uuid.New().String(), nil
	}
	return candidate, nil
}

// querySlug 查询文本 → 目录名 slug（最长 10 字符）。
// Python: archive.py:308 (_query_slug)
func querySlug(query string) string {
	return textSlug(query, 10, "no-query")
}

// chunkSlug chunk 正文 → 文件名 slug（首个有效行，跳过裸角色头）。
// Python: archive.py:317 (_chunk_slug)
func chunkSlug(body string) string {
	for _, line := range strings.Split(body, "\n") {
		text := strings.Trim(line, " \t")
		text = strings.TrimLeft(text, "#")
		text = strings.Trim(text, " \t")
		text = strings.Trim(text, "\uFFFD")
		text = strings.Trim(text, " \t")
		if text != "" && !bareSectionHeaders[strings.ToLower(text)] {
			return textSlug(text, 40, "empty")
		}
	}
	return "empty"
}

// textSlug 通用 slug：空白折叠为 _，非法字符替换为 -，截断后消毒。
// Python: archive.py:334 (_text_slug)
func textSlug(text string, maxChars int, fallback string) string {
	spaceRe := regexp.MustCompile(`\s+`)
	compact := spaceRe.ReplaceAllString(strings.TrimSpace(text), "_")
	runes := []rune(compact)
	if len(runes) > maxChars {
		compact = string(runes[:maxChars])
	}
	unsafeRe := regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]+`)
	safe := strings.Trim(unsafeRe.ReplaceAllString(compact, "-"), "-._")
	if safe == "" {
		return fallback
	}
	return safe
}

// safeFilenamePart 值 → 安全文件名片段（最长 120）。
// Python: archive.py:340 (_safe_filename_part)
func safeFilenamePart(value string) string {
	re := regexp.MustCompile(`[^A-Za-z0-9_.-]+`)
	safe := strings.Trim(re.ReplaceAllString(value, "-"), "-.")
	runes := []rune(safe)
	if len(runes) > 120 {
		safe = string(runes[:120])
	}
	if safe == "" {
		return "unknown"
	}
	return safe
}

// latestRealUserContent 最后一条真实用户消息的查询文本。
// Python: archive.py:345 (_latest_real_user_content)
func latestRealUserContent(messages []llm_schema.BaseMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if isRealUserMessage(messages[i]) {
			return contentToQueryText(messages[i].GetContent())
		}
	}
	return ""
}

// latestNonEmptyQuery 最后一个非空轮查询。
// Python: archive.py:352 (_latest_non_empty_query)
func latestNonEmptyQuery(turns []turn) string {
	for i := len(turns) - 1; i >= 0; i-- {
		if strings.TrimSpace(turns[i].query) != "" {
			return turns[i].query
		}
	}
	return ""
}

// isRealUserMessage 真实用户消息判定：user 角色 + 内容不以内部前缀开头。
// Python: archive.py:359 (_is_real_user_message)
func isRealUserMessage(message llm_schema.BaseMessage) bool {
	if message.GetRole() != llm_schema.RoleTypeUser {
		return false
	}
	content := strings.TrimLeft(contentToQueryText(message.GetContent()), " \t\n")
	for _, prefix := range fsupport.InternalUserPrefixes {
		if strings.HasPrefix(content, prefix) {
			return false
		}
	}
	return true
}

// contentToQueryText 从结构化内容提取用户可见文本（用于查询标注）。
// Python: archive.py:366 (_content_to_query_text)
func contentToQueryText(content any) string {
	mc, _ := content.(llm_schema.MessageContent)
	if mc.IsText() || mc.Parts() == nil {
		return stripChannelEnvelope(contentToText(content))
	}
	var texts []string
	for _, part := range mc.Parts() {
		if part.Text != "" && strings.TrimSpace(part.Text) != "" {
			texts = append(texts, part.Text)
		}
	}
	if len(texts) == 0 {
		return ""
	}
	return stripChannelEnvelope(contentToText(strings.Join(texts, "\n")))
}

// stripChannelEnvelope 解包「引导行 + JSON 信封」的渠道包裹（仅 content 字段非空字符串时）。
// Python: archive.py:394 (_strip_channel_envelope)
func stripChannelEnvelope(text string) string {
	stripped := strings.TrimSpace(text)
	head, payload, found := strings.Cut(stripped, "\n")
	payload = strings.TrimSpace(payload)
	if !found || strings.HasPrefix(strings.TrimLeft(head, " \t"), "{") || !strings.HasPrefix(payload, "{") {
		return text
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		return text
	}
	if inner, ok := envelope["content"].(string); ok && strings.TrimSpace(inner) != "" {
		return inner
	}
	return text
}

// contentToText 内容净化为文本（BM25 索引与召回渲染共用）。
// Go 侧 MessageContent 为结构体，先解包纯文本形态再净化（Python content 即字符串）。
// Python: archive.py:420 (_content_to_text)
func contentToText(content any) string {
	if mc, ok := content.(llm_schema.MessageContent); ok {
		content = mc.String()
	}
	return cesupport.SanitizeContentForText(content)
}

// messageToJSON 消息 → JSON map（model_dump 等价）。
// Python: archive.py:428 (_message_to_json)
func messageToJSON(message llm_schema.BaseMessage) map[string]any {
	data, err := json.Marshal(message)
	if err != nil {
		return map[string]any{"role": message.GetRole().String(), "error": fmt.Sprintf("%v", err)}
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]any{"role": message.GetRole().String()}
	}
	return m
}

// contextValue 上下文取值（workspace_dir/session_id/context_id）。
// Python: archive.py:435 (_context_value)
func contextValue(mc iface.ModelContext, method string) string {
	if mc == nil {
		return ""
	}
	switch method {
	case "workspace_dir":
		return mc.WorkspaceDir()
	case "session_id":
		return mc.SessionID()
	case "context_id":
		return mc.ContextID()
	}
	return ""
}

// writeJSON 缩进 JSON 落盘。
// Python: archive.py:443 (_write_json)
func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// writeJSONLines 紧凑 JSONL 落盘。
// Python: archive.py:447 (_write_json_lines)
func writeJSONLines(path string, values []map[string]any) error {
	var sb strings.Builder
	for _, value := range values {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		sb.Write(data)
		sb.WriteString("\n")
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// utf8CharBytes 已由 runeLen 取代（splitText 改按 rune 索引）

// ExtractQueryText 结构化 user content 的 query 文本抽取（B-15 诊断/扩展点）。
// Python: archive.py content_to_query_text（内容判型：纯文本直取，多模态 parts 拼接）。
func ExtractQueryText(content any) string { return contentToQueryText(content) }
