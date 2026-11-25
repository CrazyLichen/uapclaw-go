package recall

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"regexp"
	"strconv"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TurnTopK 每次召回匹配的轮数上限
	// Python: retriever.py TURN_TOP_K
	TurnTopK = 2
	// ChunkTopK 每个匹配轮收集的 chunk 数上限
	// Python: retriever.py CHUNK_TOP_K
	ChunkTopK = 2
	// TurnScoreMinRatio 匹配轮入选的最低分比（相对本轮 top 分数）
	// Python: retriever.py TURN_SCORE_MIN_RATIO
	TurnScoreMinRatio = 0.3
	// MaxContentTokensEnv 内容预算的环境变量名
	// 偏差 D-24.2-5：Python 为 OPENJIUWEN_RECALL_MAX_CONTENT_TOKENS，本仓统一
	// OPENUAPCLAW_ 前缀。
	// Python: retriever.py MAX_CONTENT_TOKENS_ENV
	MaxContentTokensEnv = "OPENUAPCLAW_RECALL_MAX_CONTENT_TOKENS"
	// DefaultMaxContentTokens 默认内容预算
	// Python: retriever.py DEFAULT_MAX_CONTENT_TOKENS
	DefaultMaxContentTokens = 8000
)

// memoryIDRe memory_id 合法形态
// Python: retriever.py _MEMORY_ID_RE
var memoryIDRe = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)

// ──────────────────────────── 导出函数 ────────────────────────────

// CompressionRecallError 归档无法安全读取时抛出。
// Python: retriever.py:23 (CompressionRecallError)
type CompressionRecallError struct {
	// Msg 错误消息
	Msg string
}

// Error 实现 error 接口
func (e *CompressionRecallError) Error() string { return e.Msg }

// newRecallError 构造 CompressionRecallError
func newRecallError(msg string) *CompressionRecallError {
	return &CompressionRecallError{Msg: msg}
}

// RecallCompressedContext 从当前会话的压缩归档按预算召回 chunk。
// 带 memory_id 时限定单归档检索；不带时检索会话全部归档并按归档内归一化合并
// （raw BM25 分数依赖语料，跨归档不可直接比较）。
// Python: retriever.py:27 (recall_compressed_context)
func RecallCompressedContext(
	workspaceDir string,
	sessionID string,
	memoryID string,
	query string,
) (map[string]any, error) {
	if workspaceDir == "" {
		return nil, newRecallError("compression recall requires a workspace directory")
	}
	if sessionID == "" {
		return nil, newRecallError("compression recall requires a session")
	}
	memoryID = strings.TrimSpace(memoryID)
	if memoryID != "" && !memoryIDRe.MatchString(memoryID) {
		return nil, newRecallError("memory_id is invalid")
	}
	if strings.TrimSpace(query) == "" {
		return nil, newRecallError("query is required")
	}

	sessionDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(sessionID)))[:12]
	sessionDir := fmt.Sprintf("%s-%s_context", safeFilenamePart(sessionID), sessionDigest)
	sessionRoot := filepath.Join(workspaceDir, "context", sessionDir, RecallDirName)
	if err := validateSessionRoot(workspaceDir, sessionRoot); err != nil {
		return nil, err
	}
	// Python: session_root.resolve()（非 strict，不做存在性检查），随后判 is_dir
	resolvedSessionRoot, _ := filepath.Abs(sessionRoot)
	if resolved, err := filepath.EvalSymlinks(resolvedSessionRoot); err == nil {
		resolvedSessionRoot = resolved
	}
	info, err := os.Stat(resolvedSessionRoot)
	if err != nil || !info.IsDir() {
		return nil, newRecallError("compression recall archive was not found in the current session")
	}

	archives := listSessionArchives(resolvedSessionRoot, sessionID)
	var result map[string]any
	if memoryID != "" {
		result, err = recallSingleArchive(resolvedSessionRoot, archives, memoryID, sessionID, query)
		if err != nil {
			return nil, err
		}
	} else {
		if len(archives) == 0 {
			return nil, newRecallError("compression recall archive was not found in the current session")
		}
		result, err = recallAllArchives(archives, query)
		if err != nil {
			return nil, err
		}
	}
	result["recall_root"] = resolvedSessionRoot
	archiveSummaries := make([]map[string]any, 0, len(archives))
	for _, item := range archives {
		archiveSummaries = append(archiveSummaries, map[string]any{
			"memory_id":  item["memory_id"],
			"created_at": item["created_at"],
			"turn_count": item["turn_count"],
		})
	}
	result["archives_in_session"] = archiveSummaries
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// listSessionArchives 列出会话的有效归档（按目录名时间戳升序）。
// Python: retriever.py:80 (_list_session_archives)
func listSessionArchives(sessionRoot string, sessionID string) []map[string]any {
	entries, err := os.ReadDir(sessionRoot)
	if err != nil {
		return nil
	}
	var archives []map[string]any
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(sessionRoot, name)
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || strings.HasPrefix(name, ".") {
			continue
		}
		manifestPath := filepath.Join(path, "manifest.json")
		if fi, err := os.Stat(manifestPath); err != nil || fi.IsDir() {
			continue
		}
		manifest, err := readJSONObject(manifestPath)
		if err != nil {
			continue
		}
		memoryID, _ := manifest["memory_id"].(string)
		if memoryID == "" || !strings.HasPrefix(name, memoryID+"_") {
			continue
		}
		if sid, _ := manifest["session_id"].(string); sid != sessionID {
			continue
		}
		createdAt, _ := manifest["created_at"].(string)
		turnCount := 0
		if v, ok := manifest["turn_count"].(float64); ok {
			turnCount = int(v)
		}
		archives = append(archives, map[string]any{
			"memory_id":  memoryID,
			"path":       path,
			"created_at": createdAt,
			"turn_count": turnCount,
		})
	}
	sort.Slice(archives, func(i, j int) bool {
		return archives[i]["path"].(string) < archives[j]["path"].(string)
	})
	return archives
}

// recallSingleArchive 单归档检索 + 预算截断。
// Python: retriever.py:109 (_recall_single_archive)
func recallSingleArchive(sessionRoot string, archives []map[string]any, memoryID string, sessionID string, query string) (map[string]any, error) {
	var matches []map[string]any
	for _, item := range archives {
		if item["memory_id"].(string) == memoryID {
			matches = append(matches, item)
		}
	}
	if len(matches) != 1 {
		// Python: 先尝试给出被拒原因（跨会话/ID 不匹配），再兜底"未找到"
		if err := raiseForRejectedCandidate(sessionRoot, memoryID, sessionID); err != nil {
			return nil, err
		}
		return nil, newRecallError("compression recall archive was not found in the current session")
	}
	archivePath, err := resolveInside(matches[0]["path"].(string), sessionRoot, "archive")
	if err != nil {
		return nil, err
	}
	result, err := searchArchive(archivePath, memoryID, query)
	if err != nil {
		return nil, err
	}
	chunks, truncated, returnedTokens := applyBudget(toChunkList(result["chunks"]))
	result["chunks"] = toAnyList(chunks)
	result["truncated"] = truncated
	result["returned_tokens"] = returnedTokens
	return result, nil
}

// scoredTurn 轮检索的带分记录
type scoredTurn struct {
	score float64
	index int
	turn  map[string]any
}

// raiseForRejectedCandidate 报告显式请求的归档被拒的原因（存在时）。
// Python: retriever.py:130 (_raise_for_rejected_candidate)
func raiseForRejectedCandidate(sessionRoot string, memoryID string, sessionID string) error {
	matches, _ := filepath.Glob(filepath.Join(sessionRoot, memoryID+"_*"))
	var candidates []string
	for _, path := range matches {
		if info, err := os.Lstat(path); err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			candidates = append(candidates, path)
		}
	}
	if len(candidates) != 1 {
		return nil
	}
	manifest, err := readJSONObject(filepath.Join(candidates[0], "manifest.json"))
	if err != nil {
		return nil
	}
	if manifestSessionID(manifest) != sessionID {
		return newRecallError("compression recall archive does not belong to the current session")
	}
	if mid, _ := manifest["memory_id"].(string); mid != memoryID {
		return newRecallError("compression recall archive id does not match")
	}
	return nil
}

// manifestSessionID 读 manifest 的 session_id 字段
func manifestSessionID(manifest map[string]any) string {
	v, _ := manifest["session_id"].(string)
	return v
}

// normalizedScored 分数按归档内 top 归一化（keepRaw 时保留 raw_score）。
// Python: retriever.py:150 (_normalized_scored)
func normalizedScored(items []map[string]any, keepRaw bool) []map[string]any {
	topScore := 0.0
	for _, entry := range items {
		if s := entryScore(entry); s > topScore {
			topScore = s
		}
	}
	for _, entry := range items {
		if keepRaw {
			entry["raw_score"] = entry["score"]
		}
		if topScore > 0 {
			entry["score"] = entryScore(entry) / topScore
		}
	}
	return items
}

// recallAllArchives 全归档检索 + 归一化合并 + 预算截断。
// Python: retriever.py:162 (_recall_all_archives)
func recallAllArchives(archives []map[string]any, query string) (map[string]any, error) {
	var mergedChunks []map[string]any
	var mergedTurns []map[string]any
	for _, item := range archives {
		sub, err := searchArchive(item["path"].(string), item["memory_id"].(string), query)
		if err != nil {
			return nil, err
		}
		mergedChunks = append(mergedChunks, normalizedScored(toChunkList(sub["chunks"]), true)...)
		mergedTurns = append(mergedTurns, normalizedScored(toChunkList(sub["matched_turns"]), false)...)
	}
	// 主排序键为归档内归一化分数；并列（各归档 top chunk 均归一为 1.0）时回退 raw_score，
	// 保证强匹配归档排在偶然匹配归档之前。
	sort.SliceStable(mergedChunks, func(i, j int) bool {
		si, sj := entryScore(mergedChunks[i]), entryScore(mergedChunks[j])
		if si != sj {
			return si > sj
		}
		// 并列时回退 raw_score（Python: -chunk["raw_score"]）
		riVal, _ := mergedChunks[i]["raw_score"].(float64)
		rjVal, _ := mergedChunks[j]["raw_score"].(float64)
		if riVal != rjVal {
			return riVal > rjVal
		}
		return chunkIDOf(mergedChunks[i]) < chunkIDOf(mergedChunks[j])
	})
	sort.SliceStable(mergedTurns, func(i, j int) bool {
		si, sj := entryScore(mergedTurns[i]), entryScore(mergedTurns[j])
		if si != sj {
			return si > sj
		}
		return entryStr(mergedTurns[i], "memory_id")+entryStr(mergedTurns[i], "turn_id") <
			entryStr(mergedTurns[j], "memory_id")+entryStr(mergedTurns[j], "turn_id")
	})

	chunks, truncated, returnedTokens := applyBudget(mergedChunks)
	var matchedTurn map[string]any
	if len(mergedTurns) > 0 {
		matchedTurn = mergedTurns[0]
	}
	archivePath := ""
	if len(chunks) > 0 {
		archivePath = entryStr(chunks[0], "archive_path")
	} else if matchedTurn != nil {
		mid := entryStr(matchedTurn, "memory_id")
		for _, item := range archives {
			if item["memory_id"].(string) == mid {
				archivePath = item["path"].(string)
				break
			}
		}
	}
	return map[string]any{
		"memory_id":      "",
		"archive_path":   archivePath,
		"matched_turn":   matchedTurn,
		"matched_turns":  toAnyList(mergedTurns),
		"chunks":         toAnyList(chunks),
		"truncated":      truncated,
		"returned_tokens": returnedTokens,
	}, nil
}

// searchArchive 单归档内两阶段 BM25 检索（chunk 不做预算截断）。
// Python: retriever.py:195 (_search_archive)
func searchArchive(archivePath string, memoryID string, query string) (map[string]any, error) {
	turnsPath, err := resolveInside(filepath.Join(archivePath, "turns.jsonl"), archivePath, "turn index")
	if err != nil {
		return nil, err
	}
	turns, err := readJSONLines(turnsPath)
	if err != nil {
		return nil, err
	}
	turnDocuments := make([]string, 0, len(turns))
	for _, tr := range turns {
		turnDocuments = append(turnDocuments, entryStr(tr, "query")+"\n"+entryStr(tr, "answer"))
	}
	turnScores := RankBM25(query, turnDocuments, 1.5, 0.75)
	var rankedTurns []scoredTurn
	for index, score := range turnScores {
		if score > 0 {
			rankedTurns = append(rankedTurns, scoredTurn{score: score, index: index, turn: turns[index]})
		}
	}
	sort.SliceStable(rankedTurns, func(i, j int) bool {
		if rankedTurns[i].score != rankedTurns[j].score {
			return rankedTurns[i].score > rankedTurns[j].score
		}
		return rankedTurns[i].index < rankedTurns[j].index
	})
	if len(rankedTurns) == 0 {
		return map[string]any{
			"memory_id":     memoryID,
			"archive_path":  archivePath,
			"matched_turn":  nil,
			"matched_turns": []any{},
			"chunks":        []any{},
		}, nil
	}

	topTurnScore := rankedTurns[0].score
	var matchedTurns []scoredTurn
	for _, item := range rankedTurns {
		if len(matchedTurns) >= TurnTopK {
			break
		}
		if item.score >= TurnScoreMinRatio*topTurnScore {
			matchedTurns = append(matchedTurns, item)
		}
	}

	candidates, err := collectChunkCandidates(archivePath, matchedTurns, query)
	if err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		candidate["memory_id"] = memoryID
		candidate["archive_path"] = archivePath
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		si, sj := entryScore(candidates[i]), entryScore(candidates[j])
		if si != sj {
			return si > sj
		}
		return chunkIDOf(candidates[i]) < chunkIDOf(candidates[j])
	})

	top := matchedTurns[0]
	matchedTurnMap := map[string]any{
		"memory_id": memoryID,
		"turn_id":   top.turn["turn_id"],
		"query":     entryStr(top.turn, "query"),
		"answer":    entryStr(top.turn, "answer"),
		"score":     top.score,
	}
	matchedTurnsList := make([]map[string]any, 0, len(matchedTurns))
	for _, item := range matchedTurns {
		matchedTurnsList = append(matchedTurnsList, map[string]any{
			"memory_id": memoryID,
			"turn_id":   item.turn["turn_id"],
			"query":     entryStr(item.turn, "query"),
			"answer":    entryStr(item.turn, "answer"),
			"score":     item.score,
		})
	}
	return map[string]any{
		"memory_id":     memoryID,
		"archive_path":  archivePath,
		"matched_turn":  matchedTurnMap,
		"matched_turns": toAnyList(matchedTurnsList),
		"chunks":        toAnyList(candidates),
	}, nil
}

// applyBudget 内容预算截断（超预算的 chunk 丢弃并置 truncated）。
// Python: retriever.py:249 (_apply_budget)
func applyBudget(candidates []map[string]any) ([]map[string]any, bool, int) {
	budget := maxContentTokens()
	var chunks []map[string]any
	returnedTokens := 0
	truncated := false
	for _, candidate := range candidates {
		chunkTokens := estimateTokens(entryStr(candidate, "content"))
		if returnedTokens+chunkTokens > budget {
			truncated = true
			continue
		}
		returnedTokens += chunkTokens
		chunks = append(chunks, candidate)
	}
	return chunks, truncated, returnedTokens
}

// collectChunkCandidates 从每个匹配轮收集最多 ChunkTopK 个正分 chunk。
// Python: retriever.py:264 (_collect_chunk_candidates)
func collectChunkCandidates(archivePath string, matchedTurns []scoredTurn, query string) ([]map[string]any, error) {
	var candidates []map[string]any
	for _, item := range matchedTurns {
		tr := item.turn
		var chunkDocuments []string
		var chunkRecords []map[string]any
		rawPaths, _ := tr["chunk_paths"].([]any)
		for _, raw := range rawPaths {
			relativePath, _ := raw.(string)
			chunkPath, err := resolveRelativeFile(archivePath, relativePath)
			if err != nil {
				return nil, err
			}
			data, err := os.ReadFile(chunkPath)
			if err != nil {
				return nil, newRecallError("compression recall chunk could not be read")
			}
			markdown := string(data)
			chunkDocuments = append(chunkDocuments, NormalizeMarkdown(markdown))
			chunkRecords = append(chunkRecords, map[string]any{"path": chunkPath, "content": markdown})
		}

		chunkScores := RankBM25(query, chunkDocuments, 1.5, 0.75)
		type scoredChunk struct {
			score float64
			index int
			record map[string]any
		}
		var rankedChunks []scoredChunk
		for index, score := range chunkScores {
			if score > 0 {
				rankedChunks = append(rankedChunks, scoredChunk{score: score, index: index, record: chunkRecords[index]})
			}
		}
		sort.SliceStable(rankedChunks, func(i, j int) bool {
			if rankedChunks[i].score != rankedChunks[j].score {
				return rankedChunks[i].score > rankedChunks[j].score
			}
			return rankedChunks[i].index < rankedChunks[j].index
		})
		if len(rankedChunks) > ChunkTopK {
			rankedChunks = rankedChunks[:ChunkTopK]
		}
		for _, item := range rankedChunks {
			candidates = append(candidates, map[string]any{
				"chunk_id": strings.TrimSuffix(filepath.Base(entryStr(item.record, "path")), filepath.Ext(entryStr(item.record, "path"))),
				"turn_id":  tr["turn_id"],
				"score":    item.score,
				"path":     entryStr(item.record, "path"),
				"content":  entryStr(item.record, "content"),
			})
		}
	}
	return candidates, nil
}

// maxContentTokens 内容预算（环境变量 → 默认）。
// Python: retriever.py:301 (_max_content_tokens)
func maxContentTokens() int {
	raw := strings.TrimSpace(os.Getenv(MaxContentTokensEnv))
	if raw == "" {
		return DefaultMaxContentTokens
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return DefaultMaxContentTokens
	}
	if value <= 0 {
		return DefaultMaxContentTokens
	}
	return value
}

// estimateTokens 字节长度 /4 的离线估算（与归档口径一致，不初始化 tokenizer）。
// Python: retriever.py:312 (_estimate_tokens)
func estimateTokens(text string) int {
	return (len(text) + 3) / 4
}

// resolveRelativeFile 解析归档内相对路径的 chunk 文件（拒绝绝对/逃逸）。
// Python: retriever.py:319 (_resolve_relative_file)
func resolveRelativeFile(archivePath string, relativePath string) (string, error) {
	if relativePath == "" || filepath.IsAbs(relativePath) {
		return "", newRecallError("compression recall chunk path is invalid")
	}
	path, err := resolveInside(filepath.Join(archivePath, relativePath), archivePath, "chunk")
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(path); err != nil || fi.IsDir() {
		return "", newRecallError("compression recall chunk is invalid")
	}
	return path, nil
}

// validateSessionRoot 会话根路径校验（不逃逸 workspace、无符号链接）。
// Python: retriever.py:328 (_validate_session_root)
func validateSessionRoot(workspaceDir string, sessionRoot string) error {
	resolvedWorkspace, err := filepath.Abs(workspaceDir)
	if err != nil {
		return newRecallError("compression recall session path is invalid")
	}
	rel, err := filepath.Rel(resolvedWorkspace, sessionRoot)
	if err != nil || strings.HasPrefix(rel, "..") {
		return newRecallError("compression recall session path is invalid")
	}
	current := workspaceDir
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		current = filepath.Join(current, part)
		if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return newRecallError("compression recall session path is invalid")
		}
	}
	if _, err := os.Lstat(sessionRoot); err == nil {
		if resolved, err := filepath.EvalSymlinks(sessionRoot); err == nil {
			if absRel, relErr := filepath.Rel(resolvedWorkspace, resolved); relErr != nil || strings.HasPrefix(absRel, "..") {
				return newRecallError("compression recall session path is invalid")
			}
		}
	}
	return nil
}

// resolveInside 路径必须留在 root 内（拒绝符号链接与逃逸）。
// Python: retriever.py:346 (_resolve_inside)
func resolveInside(path string, root string, kind string) (string, error) {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", newRecallError(fmt.Sprintf("compression recall %s symlink is not allowed", kind))
	}
	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		return "", newRecallError(fmt.Sprintf("compression recall %s path escapes the current session", kind))
	}
	resolvedPath, err := filepath.Abs(path)
	if err != nil {
		return "", newRecallError(fmt.Sprintf("compression recall %s path escapes the current session", kind))
	}
	if resolved, err := filepath.EvalSymlinks(resolvedPath); err == nil {
		resolvedPath = resolved
	}
	if resolved, err := filepath.EvalSymlinks(resolvedRoot); err == nil {
		resolvedRoot = resolved
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", newRecallError(fmt.Sprintf("compression recall %s path escapes the current session", kind))
	}
	if _, err := os.Stat(resolvedPath); err != nil {
		return "", newRecallError(fmt.Sprintf("compression recall %s was not found", kind))
	}
	return resolvedPath, nil
}

// readJSONObject 读 JSON 对象文件（失败/非对象 → 归档无效）。
// Python: retriever.py:360 (_read_json_object)
func readJSONObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, newRecallError("compression recall manifest is invalid")
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil || value == nil {
		return nil, newRecallError("compression recall manifest is invalid")
	}
	return value, nil
}

// readJSONLines 读 JSONL 文件。
// Python: retriever.py:370 (_read_json_lines)
func readJSONLines(path string) ([]map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, newRecallError("compression recall turn index is invalid")
	}
	var records []map[string]any
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var value map[string]any
		if err := json.Unmarshal([]byte(line), &value); err != nil || value == nil {
			return nil, newRecallError("compression recall turn index is invalid")
		}
		records = append(records, value)
	}
	return records, nil
}

// 注：Python retriever.py:386 另有一份 _safe_filename_part（与 archive.py 重复的
// 私有助手）；Go 同包不可重名，统一复用 archive.go 的实现（偏差：结构去重、行为一致）。

// ──────────────────────────── 小工具 ────────────────────────────

// entryScore 宽容读取 score 字段
func entryScore(entry map[string]any) float64 {
	switch v := entry["score"].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return 0
}

// entryStr 宽容读取字符串字段
func entryStr(entry map[string]any, key string) string {
	v, _ := entry[key].(string)
	return v
}

// chunkIDOf 宽容读取 chunk_id
func chunkIDOf(entry map[string]any) string {
	return entryStr(entry, "chunk_id")
}

// toChunkList any → []map[string]any
func toChunkList(v any) []map[string]any {
	switch t := v.(type) {
	case []map[string]any:
		return t
	case []any:
		out := make([]map[string]any, 0, len(t))
		for _, item := range t {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

// toAnyList map 列表 → []any
func toAnyList(items []map[string]any) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out
}
