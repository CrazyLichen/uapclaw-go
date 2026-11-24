// Package discovery 内 skill_index 结构化目录适配器。
// 对齐 Python openjiuwen/symphony/discovery/directory_toolkit.py（1,407 行）。
//
// 文件内声明顺序对齐 Python 源码：
// 常量 → _Row/_Execution/_CursorState → _tool_card/_DirectoryFunction →
// InstalledSkillsDirectoryToolkit → 模块级函数（_list_row 起）。
package discovery

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"

	lexruntime "github.com/uapclaw/uapclaw-go/internal/agentcore/symphony/retrieval/search/runtime"
)

// ──────────────────────────── 常量 ────────────────────────────

// 输出预算与限制常量（对齐 directory_toolkit.py:22-38）
const (
	minOutputChars        = 512
	maxOutputChars        = 48000
	maxLines              = 5000
	skillDescriptionChars = 180
	defaultSearchPageSize = 10
	maxCursors            = 32
	cursorFooterChars     = 96
	shortenedMarker       = "[skill_index output shortened to fit output budget]"
	directoryToolIDDomain = "openjiuwen.skill-index-tool.v1\x00"
	maxPathsItems         = 32
	maxPathChars          = 4096
	maxIdentifierChars    = 512
	maxQueryChars         = 512
	maxPerQueryLimit      = 10
	maxSimpleLimit        = 10
	snippetChars          = 180
	directoryDescLimit    = 300
	listDescLimit         = 240
	selectWhenLimit       = 96
)

// 操作/视图/匹配等枚举集合（对齐 :29-36）
var (
	operations         = []string{"list", "search", "read"}
	modelOperations    = []string{"list", "search"}
	listViews          = []string{"names", "details", "tree"}
	searchMatches      = []string{"content", "name", "path"}
	searchResults      = []string{"files", "matches"}
	readModes          = []string{"full", "head", "range"}
	pipelineOps        = []string{"limit", "slice", "filter", "count"}
	outputModes        = []string{"entries", "count"}
	allowedStageFields = map[string]bool{
		"operation": true, "query": true, "lines": true, "start_line": true,
		"end_line": true, "case_insensitive": true, "invert": true, "fixed_strings": true,
	}
)

// skillIndexDefaults 简单请求缺省参数（对齐 _SKILL_INDEX_DEFAULTS :39-60）。
func skillIndexDefaults() map[string]any {
	return map[string]any{
		"view":                      "names",
		"recursive":                 false,
		"directory_entry":           false,
		"directories_only":          false,
		"query":                     nil,
		"queries":                   nil,
		"per_query_limit":           nil,
		"match":                     "content",
		"result":                    "files",
		"case_insensitive":          true,
		"fixed_strings":             false,
		"max_depth":                 nil,
		"read_mode":                 "full",
		"line_count":                nil,
		"start_line":                nil,
		"end_line":                  nil,
		"pipeline":                  nil,
		"output_mode":               "entries",
		"max_output_chars":          nil,
		"disable_output_truncation": false,
	}
}

// cardProperties 工具卡允许的入参键（对齐 _tool_card input_params properties）
var cardProperties = map[string]bool{"operation": true, "category": true, "query": true, "cursor": true}

// ──────────────────────────── 结构体 ────────────────────────────

// row 一行输出（对齐 _Row）
type row struct {
	text     string
	workerID string
}

// execution 一次操作的行集（对齐 _Execution）
type execution struct {
	rows       []row
	totalCount int
	complete   bool
}

// cursorState 翻页游标状态（对齐 _CursorState）
type cursorState struct {
	operation   string
	category    string
	hasCategory bool
	// query string 或 []string（对齐 str | tuple | None）
	query       any
	offset      int
	pageSize    int
	fingerprint string
}

// directoryFunction 工具入参规范化层（对齐 _DirectoryFunction.invoke）
// 说明：Python 以 LocalFunction 子类覆写 invoke 做拒绝与归一化；
// Go 侧以独立函数 skillIndexToolInvoke 承载同一层职责。
func skillIndexToolInvoke(tk *InstalledSkillsDirectoryToolkit, inputs map[string]any) (SkillDCICommandResult, error) {
	if inputs != nil {
		unexpected := make([]string, 0)
		for k := range inputs {
			if !cardProperties[k] {
				unexpected = append(unexpected, k)
			}
		}
		if len(unexpected) > 0 {
			sort.Strings(unexpected)
			return SkillDCICommandResult{}, fmt.Errorf(
				"Unknown skill_index arguments: %s. "+
					"Only operation, category, query, cursor are accepted. "+
					`Use {"operation":"list","category":"A > B"} or `+
					`{"operation":"search","query":["keywords"],"category":"A > B"}; `+
					"omit category for root/global. Disk paths and globs belong to file tools. "+
					"For the next page, copy [next] unchanged.", strings.Join(unexpected, ", "))
		}
		normalized := make(map[string]any, len(inputs))
		for k, v := range inputs {
			normalized[k] = v
		}
		if category, ok := normalized["category"].(string); ok && strings.TrimSpace(category) == "" {
			delete(normalized, "category")
		}
		query, hasQuery := normalized["query"]
		if op, _ := normalized["operation"].(string); op == "search" {
			if q, ok := query.(string); ok {
				normalized["query"] = []any{q}
			}
		} else if op == "list" && hasQuery {
			if list, ok := query.([]any); ok && len(list) == 0 {
				delete(normalized, "query")
			}
		}
		return tk.SkillIndex(normalized)
	}
	return tk.SkillIndex(nil)
}

// InstalledSkillsDirectoryToolkit 单一 live 目录视图上的类型化操作面（对齐主类）
type InstalledSkillsDirectoryToolkit struct {
	environment           *SkillFS
	defaultMaxOutputChars int
	toolID                string
	notice                *IncrementalNoticeSession
	observedMetaPaths     map[string]string
	cursors               map[string]cursorState
	cursorOrder           []string // 插入序（对齐 dict 迭代淘汰队首）
	mu                    sync.Mutex
	closed                bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInstalledSkillsDirectoryToolkit 构造（对齐 __init__ :174-201）。
func NewInstalledSkillsDirectoryToolkit(environment *SkillFS, sessionScope string, incrementalNoticeMaxChars int) (*InstalledSkillsDirectoryToolkit, error) {
	if environment == nil {
		return nil, fmt.Errorf("environment must be a SkillFS")
	}
	defaultMax := environment.Settings().MaxOutputChars
	if defaultMax < minOutputChars || defaultMax > maxOutputChars {
		return nil, fmt.Errorf("settings.max_output_chars must be between 512 and 48000")
	}
	scope := strings.TrimSpace(sessionScope)
	if scope == "" {
		return nil, fmt.Errorf("session_scope must be non-empty")
	}
	digest := sha256.Sum256([]byte(directoryToolIDDomain + scope))
	toolID := SkillIndexToolName + "__" + fmt.Sprintf("%x", digest)
	notice, err := NewIncrementalNoticeSession(scope, environment.SelectionCards(false), incrementalNoticeMaxChars)
	if err != nil {
		return nil, err
	}
	return &InstalledSkillsDirectoryToolkit{
		environment:           environment,
		defaultMaxOutputChars: defaultMax,
		toolID:                toolID,
		notice:                notice,
		observedMetaPaths:     map[string]string{},
		cursors:               map[string]cursorState{},
	}, nil
}

// Environment 取底层 SkillFS（对齐 environment 属性）。
func (t *InstalledSkillsDirectoryToolkit) Environment() *SkillFS { return t.environment }

// ToolID 取工具标识（对齐 tool_id 属性）。
func (t *InstalledSkillsDirectoryToolkit) ToolID() string { return t.toolID }

// SkillIndex 运行一次串行化结构化目录操作（对齐 skill_index :211-254）。
func (t *InstalledSkillsDirectoryToolkit) SkillIndex(arguments map[string]any) (SkillDCICommandResult, error) {
	limit := popArg(arguments, "limit")
	paths := popArg(arguments, "paths")
	// 高级参数白名单校验（unexpected = 剩余键 - 缺省键）
	known := map[string]bool{
		"operation": true, "category": true, "query": true, "cursor": true, "skills": true,
	}
	for k := range skillIndexDefaults() {
		known[k] = true
	}
	unexpected := make([]string, 0)
	for k := range arguments {
		if !known[k] {
			unexpected = append(unexpected, k)
		}
	}
	if len(unexpected) > 0 {
		sort.Strings(unexpected)
		return SkillDCICommandResult{}, fmt.Errorf("skill_index() got an unexpected keyword argument '%s'", unexpected[0])
	}
	cursor := popArg(arguments, "cursor")
	skills := popArg(arguments, "skills")
	operation := popArg(arguments, "operation")
	category := popArg(arguments, "category")
	query := popArg(arguments, "query")
	hasAdvanced := len(arguments) > 0
	if cursor != nil {
		if skills != nil || limit != nil || paths != nil || hasAdvanced {
			return SkillDCICommandResult{}, fmt.Errorf("cursor cannot be combined with advanced arguments")
		}
		t.mu.Lock()
		defer t.mu.Unlock()
		if t.closed {
			return SkillDCICommandResult{}, fmt.Errorf("skill_index toolkit is closed")
		}
		return t.continueOp(strOf(operation), strOf(cursor), category, query)
	}
	simpleRequest := paths == nil && !hasAdvanced && skills == nil
	if category != nil && paths != nil {
		return SkillDCICommandResult{}, fmt.Errorf("category and paths are mutually exclusive")
	}
	merged := map[string]any{
		"operation": operation,
		"skills":    skills,
		"paths":     paths,
	}
	for k, v := range skillIndexDefaults() {
		merged[k] = v
	}
	for k, v := range arguments {
		merged[k] = v
	}
	merged["query"] = query
	merged["category"] = category
	if simpleRequest {
		if err := applySimpleDefaults(merged, limit); err != nil {
			return SkillDCICommandResult{}, err
		}
	} else if limit != nil {
		return SkillDCICommandResult{}, fmt.Errorf("limit cannot be combined with advanced arguments")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return SkillDCICommandResult{}, fmt.Errorf("skill_index toolkit is closed")
	}
	return t.execute(merged)
}

// GetTools 暴露 skill_index 单工具（对齐 get_tools :256-261）。
// 工具卡与入参归一层见 skillIndexToolInvoke 与 toolCard。
func (t *InstalledSkillsDirectoryToolkit) GetTools() []toolEntry {
	return []toolEntry{{
		ToolID: t.toolID,
		Name:   SkillIndexToolName,
		Card:   toolCard(t.toolID),
		Invoke: func(inputs map[string]any) (SkillDCICommandResult, error) { return skillIndexToolInvoke(t, inputs) },
	}}
}

// Aclose 关闭工具集（对齐 aclose :263-266）。
func (t *InstalledSkillsDirectoryToolkit) Aclose() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	t.cursors = map[string]cursorState{}
	t.cursorOrder = nil
}

// Close 同步关闭（对齐 close :268-270）。
func (t *InstalledSkillsDirectoryToolkit) Close() {
	t.closed = true
	t.cursors = map[string]cursorState{}
	t.cursorOrder = nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// popArg 取键并删除（模拟 Python kwargs.pop）。
func popArg(arguments map[string]any, key string) any {
	v, ok := arguments[key]
	if ok {
		delete(arguments, key)
	}
	return v
}

// strOf 断言字符串（nil → ""）。
func strOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// continueOp 游标续页（对齐 _continue :272-307）。
func (t *InstalledSkillsDirectoryToolkit) continueOp(operation, cursorToken string, category, query any) (SkillDCICommandResult, error) {
	operation, err := enumOf(operation, modelOperations, "operation")
	if err != nil {
		return SkillDCICommandResult{}, err
	}
	token, err := nonempty(cursorToken, "cursor")
	if err != nil {
		return SkillDCICommandResult{}, err
	}
	if len([]rune(token)) > 128 {
		return SkillDCICommandResult{}, fmt.Errorf("cursor must not exceed 128 characters")
	}
	state, ok := t.cursors[token]
	if !ok {
		return SkillDCICommandResult{}, fmt.Errorf("Unknown or expired skill_index cursor")
	}
	if operation != state.operation {
		return SkillDCICommandResult{}, fmt.Errorf("cursor belongs to operation=%s", state.operation)
	}
	if cat, isStr := category.(string); category != nil && (!isStr || cat != state.category) {
		return SkillDCICommandResult{}, fmt.Errorf("cursor category differs from the original request; omit cursor to change category")
	}
	originalQueries := cursorQueries(state.query)
	if query != nil {
		incoming, err := queriesOf(query, nil)
		if err != nil {
			return SkillDCICommandResult{}, err
		}
		if !equalStringSlices(incoming, originalQueries) {
			return SkillDCICommandResult{}, fmt.Errorf("cursor query differs from the original request; omit cursor to start a new search")
		}
	}
	t.removeCursor(token)
	arguments := map[string]any{
		"operation": state.operation,
		"skills":    nil,
		"paths":     nil,
		"category":  nil,
		"query":     nil,
	}
	for k, v := range skillIndexDefaults() {
		arguments[k] = v
	}
	if state.hasCategory {
		arguments["category"] = state.category
	}
	if q, ok := state.query.([]string); ok {
		list := make([]any, len(q))
		for i, s := range q {
			list[i] = s
		}
		arguments["query"] = list
	} else if state.query != nil {
		arguments["query"] = state.query
	}
	arguments["_page_size"] = state.pageSize
	arguments["_page_offset"] = state.offset
	arguments["_cursor_fingerprint"] = state.fingerprint
	if state.operation == "list" {
		arguments["view"] = "details"
	}
	return t.execute(arguments)
}

// cursorQueries 归一 state.query 到 []string。
func cursorQueries(q any) []string {
	switch v := q.(type) {
	case string:
		return []string{v}
	case []string:
		return v
	}
	return nil
}

// equalStringSlices 切片等值。
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// removeCursor 消费游标（保持插入序）。
func (t *InstalledSkillsDirectoryToolkit) removeCursor(token string) {
	delete(t.cursors, token)
	for i, k := range t.cursorOrder {
		if k == token {
			t.cursorOrder = append(t.cursorOrder[:i], t.cursorOrder[i+1:]...)
			break
		}
	}
}

// execute 执行一次操作（对齐 _execute :309-450）。
func (t *InstalledSkillsDirectoryToolkit) execute(arguments map[string]any) (SkillDCICommandResult, error) {
	operation, err := enumOf(strOf(arguments["operation"]), operations, "operation")
	if err != nil {
		return SkillDCICommandResult{}, err
	}
	view := t.environment.Directory()
	category := popArg(arguments, "category")
	pageSize := popArg(arguments, "_page_size")
	pageOffset := 0
	if v := popArg(arguments, "_page_offset"); v != nil {
		if n, ok := v.(int); ok {
			pageOffset = n
		}
	}
	expectedFingerprint := popArg(arguments, "_cursor_fingerprint")
	simplePage := pageSize != nil
	arguments["_simple_page"] = simplePage
	artifact := t.environment.Artifact()
	if expectedFingerprint != nil && artifact.Fingerprint != strOf(expectedFingerprint) {
		return SkillDCICommandResult{}, fmt.Errorf("Skill index changed; start a new list or search request")
	}
	if operation == "read" {
		if category != nil {
			return SkillDCICommandResult{}, fmt.Errorf("category is valid only for list and search")
		}
	} else {
		categoryPaths, err := categoryPathsOf(view, category, arguments["paths"])
		if err != nil {
			return SkillDCICommandResult{}, err
		}
		arguments["paths"] = categoryPaths
	}
	pipeline, err := validateRequest(operation, arguments)
	if err != nil {
		return SkillDCICommandResult{}, err
	}
	paths, err := pathsOf(arguments["paths"], false)
	if err != nil {
		return SkillDCICommandResult{}, err
	}
	budget, err := outputBudget(arguments["max_output_chars"], arguments["disable_output_truncation"], t.defaultMaxOutputChars)
	if err != nil {
		return SkillDCICommandResult{}, err
	}
	var exec execution
	switch operation {
	case "list":
		exec, err = t.list(view, paths, arguments)
	case "search":
		exec, err = t.search(view, paths, arguments)
	default:
		var skills []string
		skills, err = t.readSkills(view, arguments["skills"], arguments["paths"])
		if err != nil {
			return SkillDCICommandResult{}, err
		}
		exec, err = t.read(view, skills, arguments)
	}
	if err != nil {
		return SkillDCICommandResult{}, err
	}
	rows, pipelineComplete, countValue, transformedCount, err := applyPipeline(exec.rows, pipeline, strOf(arguments["output_mode"]))
	if err != nil {
		return SkillDCICommandResult{}, err
	}
	complete := exec.complete && pipelineComplete
	allRows := rows
	if ps, ok := pageSize.(int); ok && ps == 0 {
		// 分支行整页呈现；技能候选页有界（对齐 :347-349）
		hasSkill := false
		for _, r := range rows {
			if r.workerID != "" {
				hasSkill = true
				break
			}
		}
		if hasSkill {
			pageSize = 10
		} else {
			if len(rows) < 1 {
				pageSize = 1
			} else {
				pageSize = len(rows)
			}
		}
	}
	pageSizeInt, paging := pageSize.(int)
	if paging {
		pageEnd := pageOffset + pageSizeInt
		if pageEnd > len(allRows) {
			pageEnd = len(allRows)
		}
		rows = allRows[min(pageOffset, len(allRows)):pageEnd]
		complete = complete && pageOffset+len(rows) >= len(allRows)
	}
	returnedSkillCount := map[string]bool{}
	returnedCategoryCount := 0
	for _, r := range rows {
		if r.workerID != "" {
			returnedSkillCount[r.workerID] = true
		}
		if strings.HasPrefix(strings.TrimLeft(r.text, " \t"), "- [category]") {
			returnedCategoryCount++
		}
	}
	previouslyShown := false
	if operation == "search" && paging {
		allShown := true
		for id := range returnedSkillCount {
			if !t.observedMetaPathHas(id) {
				allShown = false
				break
			}
		}
		previouslyShown = allShown
	}
	summary := map[string]any{
		"operation":               operation,
		"returned_skill_count":    len(returnedSkillCount),
		"returned_category_count": returnedCategoryCount,
		"previously_shown":        previouslyShown,
	}
	reserve := 0
	if paging && len(rows) > 0 {
		reserve += cursorFooterChars
	}
	fitBudget := -1
	if budget > 0 {
		fitBudget = budget - reserve
		if fitBudget < minOutputChars/2 {
			fitBudget = minOutputChars / 2
		}
	}
	fitted, modelContent, observed, shortened, shownCount := fitRows(rows, summary, fitBudget, countValue, !paging)
	if fitted == "" && len(pipeline) == 0 {
		empty := emptyMessage(operation, category)
		if budget < 0 {
			fitted = empty
			if modelContent == "" {
				modelContent = empty
			} else {
				modelContent = modelContent + "\n\n" + empty
			}
		} else {
			remaining := budget - len([]rune(modelContent)) - 2
			if remaining > 3 {
				fitted = compact(empty, remaining)
				modelContent = modelContent + "\n\n" + fitted
			}
		}
	}
	nextCursor := ""
	hasMore := paging && pageOffset+shownCount < len(allRows)
	if hasMore {
		token, err := t.saveCursor(cursorState{
			operation:   operation,
			category:    strOf(category),
			hasCategory: category != nil,
			query:       cursorQuery(arguments),
			offset:      pageOffset + shownCount,
			pageSize:    pageSizeInt,
			fingerprint: artifact.Fingerprint,
		})
		if err != nil {
			return SkillDCICommandResult{}, err
		}
		nextCursor = token
		footer := "[next] " + compactJSON(map[string]any{"operation": operation, "cursor": nextCursor})
		fitted = appendNote(fitted, footer, budget)
		modelContent = appendNote(modelContent, footer, budget)
	} else if paging && shortened {
		note := "[truncated] Candidate details shortened."
		fitted = appendNote(fitted, note, budget)
		modelContent = appendNote(modelContent, note, budget)
	}
	if paging {
		fitted = modelContent
	}
	cards := map[string]map[string]string{}
	for _, item := range artifact.Items {
		desc := item.Description
		if desc == "" {
			desc = item.Name
		}
		cards[item.WorkerID] = map[string]string{"name": item.WorkerID, "description": desc}
	}
	deliveredModel, reminder := t.notice.Append(modelContent, cards, budget)
	if reminder != "" {
		if fitted == "" {
			fitted = reminder
		} else {
			fitted = fitted + "\n\n" + reminder
		}
		modelContent = deliveredModel
	}
	for _, workerID := range observed {
		metaPath, err := view.MetadataPath(workerID)
		if err != nil {
			continue
		}
		normalized, err := view.NormalizePath(metaPath)
		if err != nil {
			continue
		}
		t.observedMetaPaths[normalized] = workerID
	}
	diagnostics := map[string]any{
		"operation":                  operation,
		"output":                     fitted,
		"observed_skill_ids":         observed,
		"error":                      false,
		"ok":                         true,
		"truncated":                  shortened,
		"truncation_reason":          nil,
		"total_count":                resultEntryCount(countValue, transformedCount, rows, pipeline, pipelineComplete, exec.totalCount),
		"shown_count":                shownCount,
		"result_complete":            !hasMore && complete && !shortened,
		"next_cursor":                nil,
		"disable_output_truncation":  arguments["disable_output_truncation"] == true,
		"effective_max_output_chars": budget,
		"skillfs_layout":             artifact.Layout,
		"candidate_count":            len(artifact.Items),
		"estimated_candidate_tokens": candidateTokens(artifact.Items),
		"candidate_budget_tokens":    t.environment.Settings().CandidateBudgetTokens(),
		"index_state":                artifact.IndexState,
		"runtime":                    "Symphony.SkillIndex",
	}
	if nextCursor != "" {
		diagnostics["next_cursor"] = nextCursor
	}
	if shortened {
		diagnostics["truncation_reason"] = "max_output_chars"
	}
	return SkillDCICommandResult{ModelContent: modelContent, DetailedOutput: diagnostics}, nil
}

// observedMetaPathHas 是否见过该技能的 metadata 路径。
func (t *InstalledSkillsDirectoryToolkit) observedMetaPathHas(workerID string) bool {
	for _, id := range t.observedMetaPaths {
		if id == workerID {
			return true
		}
	}
	return false
}

// resultEntryCount 计算诊断 total_count（对齐 :354-362 优先级链）。
// countValue/transformedCount 以 nil 表示 Python None。
func resultEntryCount(countValue, transformedCount *int, rows []row, pipeline []map[string]any, pipelineComplete bool, execTotal int) int {
	if countValue != nil {
		return *countValue
	}
	if transformedCount != nil {
		return *transformedCount
	}
	if len(pipeline) > 0 && pipelineComplete {
		return rowCount(rows)
	}
	return execTotal
}

// saveCursor 保存游标（对齐 _save_cursor :452-459，LRU 淘汰队首）。
func (t *InstalledSkillsDirectoryToolkit) saveCursor(state cursorState) (string, error) {
	buf := make([]byte, 12)
	for {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		token := base64.RawURLEncoding.EncodeToString(buf)
		if _, exists := t.cursors[token]; !exists {
			t.cursors[token] = state
			t.cursorOrder = append(t.cursorOrder, token)
			for len(t.cursors) > maxCursors {
				oldest := t.cursorOrder[0]
				t.cursorOrder = t.cursorOrder[1:]
				delete(t.cursors, oldest)
			}
			return token, nil
		}
	}
}

// list list 操作（对齐 _list :461-479）。
func (t *InstalledSkillsDirectoryToolkit) list(view *SkillDirectoryView, paths []string, arguments map[string]any) (execution, error) {
	listView, err := enumOf(strOf(arguments["view"]), listViews, "view")
	if err != nil {
		return execution{}, err
	}
	recursive := arguments["recursive"] == true || listView == "tree"
	maxDepth, err := positiveOf(arguments["max_depth"], "max_depth", true)
	if err != nil {
		return execution{}, err
	}
	if maxDepth == 0 {
		maxDepth = -1 // Python None = 无限制
	}
	directoriesOnly := arguments["directories_only"] == true
	var entries []DirectoryEntry
	if listView == "tree" {
		entries = view.TreeEntries(paths, maxDepth, directoriesOnly)
	} else {
		entries = view.Entries(paths, maxDepth, recursive, directoriesOnly, arguments["directory_entry"] == true)
	}
	rows := make([]row, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, listRow(e, listView, view))
	}
	return execution{rows: rows, totalCount: len(entries), complete: true}, nil
}

// search search 操作（对齐 _search :481-576）。
func (t *InstalledSkillsDirectoryToolkit) search(view *SkillDirectoryView, paths []string, arguments map[string]any) (execution, error) {
	match, err := enumOf(strOf(arguments["match"]), searchMatches, "match")
	if err != nil {
		return execution{}, err
	}
	resultMode, err := enumOf(strOf(arguments["result"]), searchResults, "result")
	if err != nil {
		return execution{}, err
	}
	caseInsensitive := arguments["case_insensitive"] == true
	fixedStrings := arguments["fixed_strings"] == true
	maxDepth, err := positiveOf(arguments["max_depth"], "max_depth", true)
	if err != nil {
		return execution{}, err
	}
	if maxDepth == 0 {
		maxDepth = -1 // Python None = 无限制
	}
	var scopedRecords []SkillRecord
	var scopedEntries []DirectoryEntry
	if match == "content" {
		scopedRecords = view.ScopedRecords(paths, maxDepth)
	} else {
		scopedEntries = view.SearchableEntries(paths, maxDepth)
	}
	queries, err := queriesOf(arguments["query"], arguments["queries"])
	if err != nil {
		return execution{}, err
	}
	perQueryLimit, err := positiveOf(arguments["per_query_limit"], "per_query_limit", true)
	if err != nil {
		return execution{}, err
	}
	if perQueryLimit > maxPerQueryLimit {
		return execution{}, fmt.Errorf("per_query_limit must not exceed %d", maxPerQueryLimit)
	}
	simplePage := arguments["_simple_page"] == true
	limit := -1
	if len(queries) > 1 && !simplePage {
		if perQueryLimit > 0 {
			limit = perQueryLimit
		} else {
			limit = 5
		}
	} else if perQueryLimit > 0 {
		limit = perQueryLimit
	}
	selectedByID := map[string]any{}
	queryIndexesByID := map[string][]int{}
	queryCounts := []int{}
	selectedGroups := [][]any{}
	allMatches := map[string]bool{}
	for queryIndex, currentQuery := range queries {
		var matches []any
		if match == "content" {
			records, err := t.searchContentOne(scopedRecords, currentQuery, caseInsensitive, fixedStrings, simplePage)
			if err != nil {
				return execution{}, err
			}
			if simplePage && len(records) == 0 {
				cats := searchCategories(view, paths, currentQuery, maxDepth)
				for _, c := range cats {
					matches = append(matches, c)
				}
			} else {
				for _, r := range records {
					matches = append(matches, r)
				}
			}
		} else {
			entries := searchEntryOne(view, scopedEntries, currentQuery, match, caseInsensitive)
			for _, e := range entries {
				matches = append(matches, e)
			}
		}
		identities := make([]string, 0, len(matches))
		for _, item := range matches {
			identities = append(identities, searchIdentity(item))
		}
		for _, id := range identities {
			allMatches[id] = true
		}
		selected := matches
		if limit >= 0 && len(matches) > limit {
			selected = matches[:limit]
		}
		selectedGroups = append(selectedGroups, selected)
		queryCounts = append(queryCounts, len(selected))
		for _, item := range selected {
			identity := searchIdentity(item)
			if _, exists := selectedByID[identity]; !exists {
				selectedByID[identity] = item
				queryIndexesByID[identity] = []int{}
			}
			if len(queries) > 1 {
				queryIndexesByID[identity] = append(queryIndexesByID[identity], queryIndex+1)
			}
		}
	}
	order := []string{}
	if simplePage && len(selectedGroups) > 1 {
		maxLen := 0
		for _, g := range selectedGroups {
			if len(g) > maxLen {
				maxLen = len(g)
			}
		}
		for rank := 0; rank < maxLen; rank++ {
			for _, group := range selectedGroups {
				if rank < len(group) {
					order = append(order, searchIdentity(group[rank]))
				}
			}
		}
	} else {
		for _, group := range selectedGroups {
			for _, item := range group {
				order = append(order, searchIdentity(item))
			}
		}
	}
	seen := map[string]bool{}
	dedup := order[:0]
	for _, id := range order {
		if !seen[id] {
			seen[id] = true
			dedup = append(dedup, id)
		}
	}
	order = dedup
	rows := []row{}
	for _, identity := range order {
		item := selectedByID[identity]
		indexes := queryIndexesByID[identity]
		if entry, ok := item.(DirectoryEntry); ok {
			if entry.Kind == "dir" {
				if simplePage {
					rows = append(rows, searchDirectoryRow(entry, view, nil))
				} else {
					rows = append(rows, searchDirectoryRow(entry, view, indexes))
				}
			} else {
				record, _ := view.RecordByID(entry.WorkerID)
				rows = append(rows, searchRow(record, view, indexes, ""))
			}
			continue
		}
		record := item.(SkillRecord)
		if resultMode == "files" {
			matchedQueries := queries
			if len(indexes) > 0 {
				matchedQueries = nil
				for _, idx := range indexes {
					matchedQueries = append(matchedQueries, queries[idx-1])
				}
			}
			snippet := ""
			if simplePage {
				snippet = t.environment.ContentMatchSnippet(record, matchedQueries)
			}
			if snippet != "" {
				snippet = safeMatchSnippet(snippet)
			}
			if simplePage {
				rows = append(rows, searchRow(record, view, nil, snippet))
			} else {
				rows = append(rows, searchRow(record, view, indexes, snippet))
			}
			continue
		}
		matchedQueries := queries
		if len(indexes) > 0 {
			matchedQueries = nil
			for _, idx := range indexes {
				matchedQueries = append(matchedQueries, queries[idx-1])
			}
		}
		snippets := t.matchedSnippets(record, matchedQueries, match, caseInsensitive, fixedStrings, record.SkillFile, view.CategoryText(record.WorkerID))
		for _, snippet := range snippets {
			rows = append(rows, searchRow(record, view, indexes, snippet))
		}
	}
	complete := limit < 0
	for _, count := range queryCounts {
		if limit >= 0 && count >= limit {
			complete = false
		}
	}
	totalCount := len(allMatches)
	if resultMode == "matches" {
		totalCount = rowCount(rows)
	}
	return execution{rows: rows, totalCount: totalCount, complete: complete}, nil
}

// searchCategories 类别回落检索（对齐 _search_categories :578-597）。
func searchCategories(view *SkillDirectoryView, paths []string, query string, maxDepth int) []DirectoryEntry {
	entries := map[string]DirectoryEntry{}
	for _, entry := range view.TreeEntries(paths, maxDepth, true) {
		if entry.Path != "/" {
			entries[entry.Path] = entry
		}
	}
	keys := make([]string, 0, len(entries))
	docs := make([]lexruntime.LexicalDocument, 0, len(entries))
	for p, e := range entries {
		keys = append(keys, p)
		docs = append(docs, lexruntime.LexicalDocument{Key: p, Name: e.Label, Description: directoryDescription(e.Description, 300)})
	}
	index := lexruntime.NewLexicalIndex(docs)
	hits, err := index.SearchTerms(query, keys)
	if err != nil {
		return nil
	}
	out := make([]DirectoryEntry, 0, len(hits))
	for _, hit := range hits {
		if e, ok := entries[hit.Key]; ok {
			out = append(out, e)
		}
	}
	return out
}

// searchContentOne 单查询内容检索（对齐 _search_content_one :599-616）。
func (t *InstalledSkillsDirectoryToolkit) searchContentOne(records []SkillRecord, query string, caseInsensitive, fixedStrings, termMode bool) ([]SkillRecord, error) {
	identifiers, err := t.environment.SearchContent(records, query, caseInsensitive, fixedStrings, termMode)
	if err != nil {
		return nil, err
	}
	byID := map[string]SkillRecord{}
	for _, r := range records {
		byID[r.WorkerID] = r
	}
	out := make([]SkillRecord, 0, len(identifiers))
	for _, id := range identifiers {
		if r, ok := byID[id]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// searchEntryOne 名称/路径检索（对齐 _search_entry_one :618-654）。
func searchEntryOne(view *SkillDirectoryView, entries []DirectoryEntry, query, match string, caseInsensitive bool) []DirectoryEntry {
	value := query
	if caseInsensitive {
		value = strings.ToLower(value)
	}
	wildcard := strings.ContainsAny(value, "*?[")
	type ranked struct {
		rank  int
		entry DirectoryEntry
	}
	matched := []ranked{}
	for _, entry := range entries {
		var candidates []string
		if entry.Kind == "skill" {
			record, _ := view.RecordByID(entry.WorkerID)
			if match == "name" {
				candidates = []string{path.Base(entry.Path), record.WorkerID, record.Name}
			} else {
				candidates = []string{record.SkillFile}
			}
		} else {
			if match == "name" {
				base := path.Base(entry.Path)
				if base == "/" || base == "." {
					base = "."
				}
				candidates = []string{base, entry.Label}
			} else {
				candidates = []string{entry.Path}
			}
		}
		compared := make([]string, len(candidates))
		for i, c := range candidates {
			if caseInsensitive {
				compared[i] = strings.ToLower(c)
			} else {
				compared[i] = c
			}
		}
		pattern := "*" + value + "*"
		if wildcard {
			pattern = value
		}
		hit := false
		for _, c := range compared {
			if fnmatchCase(pattern, c) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		rank := 2
		if wildcard {
			rank = 3
		} else {
			for _, c := range compared {
				if r := literalMatchRank(c, value); r < rank {
					rank = r
				}
			}
		}
		matched = append(matched, ranked{rank: rank, entry: entry})
	}
	sort.SliceStable(matched, func(i, j int) bool {
		a, b := matched[i], matched[j]
		la, lb := strings.ToLower(a.entry.Path), strings.ToLower(b.entry.Path)
		if la != lb {
			return la < lb
		}
		if a.entry.Path != b.entry.Path {
			return a.entry.Path < b.entry.Path
		}
		return a.entry.Kind < b.entry.Kind
	})
	out := make([]DirectoryEntry, 0, len(matched))
	for _, m := range matched {
		out = append(out, m.entry)
	}
	return out
}

// matchedSnippets matches 结果多片段（对齐 _matched_snippets :656-712）。
func (t *InstalledSkillsDirectoryToolkit) matchedSnippets(record SkillRecord, queries []string, match string, caseInsensitive, fixedStrings bool, skillPath, categoryText string) []string {
	if match != "content" {
		return nil
	}
	matchers := make([]func(string) bool, 0, len(queries))
	for _, q := range queries {
		m, err := lexruntime.CompileMatcher(q, caseInsensitive, fixedStrings)
		if err != nil {
			continue
		}
		matchers = append(matchers, m)
	}
	matches := func(value string) bool {
		for _, m := range matchers {
			if m(value) {
				return true
			}
		}
		return false
	}
	type field struct {
		label string
		value string
	}
	aliasText := strings.Join(record.Aliases, "\n")
	fields := []field{
		{"id", record.WorkerID},
		{"name", record.Name},
		{"alias", aliasText},
		{"description", record.Description},
		{"body", t.environment.ReadBody(record)},
		{"category", categoryText},
		{"path", skillPath},
	}
	snippets := []string{}
	seen := map[string]bool{}
	for _, f := range fields {
		lines := splitLinesOrSelf(f.value)
		for i, line := range lines {
			if !matches(line) {
				continue
			}
			safe := compact(SanitizeModelText(line), snippetChars)
			if safe == "" {
				continue
			}
			location := f.label
			if f.label == "body" {
				location = fmt.Sprintf("%s:%d", f.label, i+1)
			}
			snippet := fmt.Sprintf("%s: %s", location, safe)
			if !seen[snippet] {
				seen[snippet] = true
				snippets = append(snippets, snippet)
			}
		}
	}
	if len(snippets) > 0 {
		return snippets
	}
	// 正则可能跨行命中：回退到整段有界摘录（对齐 :705-712）
	for _, f := range fields {
		if matches(f.value) {
			safe := compact(SanitizeModelText(f.value), snippetChars)
			if safe != "" {
				return []string{fmt.Sprintf("%s: %s", f.label, safe)}
			}
		}
	}
	return nil
}

// splitLinesOrSelf 按行拆分，空串回落原值单行（对齐 splitlines() or (value,)）。
func splitLinesOrSelf(value string) []string {
	lines := splitLines(value)
	if len(lines) == 0 {
		return []string{value}
	}
	return lines
}

// splitLines 按 \n 拆分（保留语义与 Python str.splitlines 的常规行界一致）。
func splitLines(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, "\n")
	// Python splitlines 会吞掉尾部空行后的空段；Go Split 保留尾空段，
	// 与 Python 行为对齐：尾随 \n 不产生额外空行。
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// readSkills 解析 read 的技能清单（对齐 _read_skills :714-730）。
func (t *InstalledSkillsDirectoryToolkit) readSkills(view *SkillDirectoryView, skills, legacyPaths any) ([]string, error) {
	if skills != nil && legacyPaths != nil {
		return nil, fmt.Errorf("skills and paths are mutually exclusive")
	}
	if legacyPaths != nil {
		raw, err := pathsOf(legacyPaths, true)
		if err != nil {
			return nil, err
		}
		resolved := []string{}
		seen := map[string]bool{}
		for _, p := range raw {
			normalized, err := view.NormalizePath(p)
			if err != nil {
				return nil, err
			}
			workerID, ok := t.observedMetaPaths[normalized]
			if !ok {
				return nil, fmt.Errorf("read accepts only Skills returned by an earlier skill_index result")
			}
			if !seen[workerID] {
				seen[workerID] = true
				resolved = append(resolved, workerID)
			}
		}
		return resolved, nil
	}
	return identifiers(skills, "skills", true)
}

// read read 操作（对齐 _read :732-755）。
func (t *InstalledSkillsDirectoryToolkit) read(view *SkillDirectoryView, skills []string, arguments map[string]any) (execution, error) {
	mode, err := enumOf(strOf(arguments["read_mode"]), readModes, "read_mode")
	if err != nil {
		return execution{}, err
	}
	rows := []row{}
	for _, skill := range skills {
		record, ok := view.RecordByID(skill)
		if !ok {
			return execution{}, fmt.Errorf("Unknown Skill: %s", skill)
		}
		content := metadataCard(record)
		lines := splitLines(content)
		if mode == "head" {
			lineCount, err := positiveOf(arguments["line_count"], "line_count", true)
			if err != nil {
				return execution{}, err
			}
			n := 10
			if lineCount > 0 {
				n = lineCount
			}
			if n > len(lines) {
				n = len(lines)
			}
			lines = lines[:n]
		} else if mode == "range" {
			start, err := positiveOf(arguments["start_line"], "start_line", false)
			if err != nil {
				return execution{}, err
			}
			end, err := positiveOf(arguments["end_line"], "end_line", false)
			if err != nil {
				return execution{}, err
			}
			if end < start {
				return execution{}, fmt.Errorf("end_line must be greater than or equal to start_line")
			}
			lo := start - 1
			if lo > len(lines) {
				lo = len(lines)
			}
			if end > len(lines) {
				end = len(lines)
			}
			lines = lines[lo:end]
		}
		rows = append(rows, row{text: strings.Join(lines, "\n"), workerID: record.WorkerID})
	}
	return execution{rows: rows, totalCount: len(rows), complete: true}, nil
}

// ──────────────────────────── 工具卡 ────────────────────────────

// toolEntry 工具暴露条目（Go 侧 LocalFunction 等价载体）。
type toolEntry struct {
	ToolID string
	Name   string
	Card   map[string]any
	Invoke func(inputs map[string]any) (SkillDCICommandResult, error)
}

// toolCard 工具卡描述（对齐 _tool_card :86-141）。
func toolCard(toolID string) map[string]any {
	return map[string]any{
		"id":          toolID,
		"name":        SkillIndexToolName,
		"description": "Read-only Skill catalogue, not a filesystem tool. [category] is a virtual group, not a disk directory or Skill; counts include descendants. [skill] names a Skill; path is its real SKILL.md for file tools, not an input here. Calls do not change a working directory. Search returns ranked text matches, not verified capabilities. No query translation.",
		"input_params": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operation": map[string]any{
					"type":        "string",
					"enum":        modelOperations,
					"description": "list: direct subcategories and Skills. search: names, aliases, descriptions and SKILL.md including descendants; not other package files.",
				},
				"category": map[string]any{
					"type":        "string",
					"minLength":   1,
					"description": "Scope: returned category name or A > B chain, never a disk path. Omit for root/global on each call.",
				},
				"query": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string", "minLength": 1, "maxLength": 512},
					"minItems":    1,
					"maxItems":    8,
					"description": `Search only. Array even for one query: ["PDF OCR"]. Keep task-specific terms; use the language of Skill descriptions (often English). Independent queries, interleaved and deduplicated. Terms need not all match; no Skill matches yields category hints.`,
				},
				"cursor": map[string]any{
					"type":        "string",
					"minLength":   1,
					"maxLength":   128,
					"description": "Copy [next] unchanged. To change query/category, omit cursor.",
				},
			},
			"required":             []string{"operation"},
			"additionalProperties": false,
		},
		"exposure":      "direct",
		"parallel_safe": false,
		"stateless":     false,
	}
}

// ──────────────────────────── 行渲染模块级函数 ────────────────────────────

// listRow list 行渲染（对齐 _list_row :758-777）。
func listRow(entry DirectoryEntry, view string, directory *SkillDirectoryView) row {
	indent := ""
	if view == "tree" {
		indent = strings.Repeat("  ", entry.Depth)
	}
	if entry.Kind == "dir" {
		description := directoryDescription(entry.Description, listDescLimit)
		count := directory.SkillCount(entry.Path)
		unit := "skills"
		if count == 1 {
			unit = "skill"
		}
		detail := strings.TrimRight(fmt.Sprintf("  desc: Contains %d %s. %s", count, unit, description), " ")
		if view == "names" {
			detail = ""
		}
		return row{text: fmt.Sprintf("%s- [category] %s%s", indent, categoryFromPath(directory, entry.Path), detail)}
	}
	record, _ := directory.RecordByID(entry.WorkerID)
	category := skillCategory(directory, entry.WorkerID)
	detail := ""
	if view != "names" && entry.Description != "" {
		detail = fmt.Sprintf("  desc: %s", compact(entry.Description, skillDescriptionChars))
	}
	return row{
		text:     fmt.Sprintf("%s- [skill] %s  category: %s  path: %s%s", indent, entry.WorkerID, category, skillPath(record), detail),
		workerID: entry.WorkerID,
	}
}

// searchRow search 技能行（对齐 _search_row :780-799）。
func searchRow(record SkillRecord, directory *SkillDirectoryView, indexes []int, snippet string) row {
	description := compact(orStr(record.Description, record.Name), skillDescriptionChars)
	fields := []string{
		fmt.Sprintf("- [skill] %s", record.WorkerID),
		fmt.Sprintf("category: %s", skillCategory(directory, record.WorkerID)),
	}
	if len(indexes) > 0 {
		parts := make([]string, len(indexes))
		for i, idx := range indexes {
			parts[i] = fmt.Sprintf("%d", idx)
		}
		fields = append(fields, fmt.Sprintf("matches: %s", strings.Join(parts, ", ")))
	}
	if snippet != "" {
		fields = append(fields, fmt.Sprintf("match: %s", snippet))
	}
	fields = append(fields, fmt.Sprintf("desc: %s", description), fmt.Sprintf("path: %s", skillPath(record)))
	return row{text: strings.Join(fields, "  "), workerID: record.WorkerID}
}

// searchDirectoryRow search 类别行（对齐 _search_directory_row :802-811）。
func searchDirectoryRow(entry DirectoryEntry, directory *SkillDirectoryView, indexes []int) row {
	mapping := ""
	if len(indexes) > 0 {
		parts := make([]string, len(indexes))
		for i, idx := range indexes {
			parts[i] = fmt.Sprintf("%d", idx)
		}
		mapping = fmt.Sprintf("  matches: %s", strings.Join(parts, ", "))
	}
	description := directoryDescription(entry.Description, directoryDescLimit)
	count := directory.SkillCount(entry.Path)
	unit := "skills"
	if count == 1 {
		unit = "skill"
	}
	detail := strings.TrimRight(fmt.Sprintf("  desc: Contains %d %s. %s", count, unit, description), " ")
	return row{text: fmt.Sprintf("- [category] %s%s%s", categoryFromPath(directory, entry.Path), mapping, detail)}
}

// skillCategory 技能所属类别链（对齐 _skill_category :814-816）。
func skillCategory(directory *SkillDirectoryView, workerID string) string {
	recordPath := directory.RecordPathByID(workerID)
	return categoryFromPath(directory, path.Dir(recordPath))
}

// categoryFromPath 路径 → "A > B" 类别链（对齐 _category_from_path :819-826）。
func categoryFromPath(directory *SkillDirectoryView, dirPath string) string {
	labels := []string{}
	current := dirPath
	for current != "" && current != "." && current != "/" {
		label := path.Base(current)
		if node, ok := directory.NodeByPath(current); ok {
			label = node.Label
		}
		labels = append(labels, inlineText(label))
		current = path.Dir(current)
	}
	for i, j := 0, len(labels)-1; i < j; i, j = i+1, j-1 {
		labels[i], labels[j] = labels[j], labels[i]
	}
	if len(labels) == 0 {
		return "ROOT"
	}
	return strings.Join(labels, " > ")
}

// skillPath 技能文件路径（对齐 _skill_path :829-830）。
func skillPath(record SkillRecord) string { return inlineText(record.SkillFile) }

// inlineText 单字段安全渲染（不产生新 Markdown 行；对齐 _inline_text :833-836）。
func inlineText(value any) string {
	s := SanitizeModelText(value)
	b, _ := json.Marshal(s)
	out := string(b)
	return out[1 : len(out)-1]
}

// safeMatchSnippet 内容片段安全化（对齐 _safe_match_snippet :839-843）。
func safeMatchSnippet(value string) string {
	idx := strings.Index(value, ": ")
	if idx > 0 {
		label := value[:idx]
		switch label {
		case "name", "alias", "description", "body", "category":
			return fmt.Sprintf("%s: %s", label, SanitizeModelText(value[idx+2:]))
		}
	}
	return SanitizeModelText(value)
}

// directoryDescription 目录描述清洗（对齐 _directory_description :846-870）。
func directoryDescription(value string, limit int) string {
	text := SanitizeModelText(value)
	selectWhen := ""
	semanticLines := []string{}
	paragraphs := regexp.MustCompile(`\n\s*\n`).Split(text, -1)
	for _, paragraph := range paragraphs {
		current := []string{}
		for _, rawLine := range splitLines(paragraph) {
			line := strings.TrimSpace(rawLine)
			lowered := strings.ToLower(line)
			if strings.HasPrefix(lowered, "select when:") {
				if selectWhen == "" {
					selectWhen = strings.TrimSpace(line[min(1, len(line)):])
					if idx := strings.Index(line, ":"); idx >= 0 {
						selectWhen = strings.TrimSpace(line[idx+1:])
					}
				}
				continue
			}
			if strings.HasPrefix(lowered, "covers ") || strings.HasPrefix(lowered, "representative ") || strings.HasPrefix(lowered, "don't select when:") {
				continue
			}
			if line != "" {
				current = append(current, line)
			}
		}
		if len(current) > 0 && len(semanticLines) == 0 {
			semanticLines = current
		}
	}
	semantic := ""
	if len(semanticLines) > 0 {
		l := limit
		if l > 180 {
			l = 180
		}
		semantic = compact(strings.Join(semanticLines, " "), l)
	}
	routing := ""
	if selectWhen != "" {
		routing = fmt.Sprintf("Select when: %s", compact(selectWhen, selectWhenLimit))
	}
	parts := []string{}
	if semantic != "" {
		parts = append(parts, semantic)
	}
	if routing != "" {
		parts = append(parts, routing)
	}
	return compact(strings.Join(parts, " "), limit)
}

// searchIdentity 检索项身份键（对齐 _search_identity :873-876）。
func searchIdentity(item any) string {
	switch v := item.(type) {
	case SkillRecord:
		return "skill\x00" + v.WorkerID
	case DirectoryEntry:
		return v.Kind + "\x00" + v.Path
	}
	return ""
}

// metadataCard read 元数据卡（对齐 _metadata_card :879-890）。
func metadataCard(record SkillRecord) string {
	lines := []string{
		fmt.Sprintf("# %s", orStr(record.Name, record.WorkerID)),
		"",
		fmt.Sprintf("- Description: %s", orStr(orStr(record.Description, record.Name), record.WorkerID)),
		fmt.Sprintf("- Source: %s", orStr(record.Source, "local")),
	}
	if record.Version != "" {
		lines = append(lines, fmt.Sprintf("- Version: %s", record.Version))
	}
	if record.Author != "" {
		lines = append(lines, fmt.Sprintf("- Author: %s", record.Author))
	}
	return strings.Join(lines, "\n")
}

// ──────────────────────────── 管道与拟合 ────────────────────────────

// applyPipeline 行管道（对齐 _apply_pipeline :893-950）。
// 返回 countValue/transformedCount 以 nil 表示 Python None。
func applyPipeline(rows []row, pipeline []map[string]any, outputMode string) ([]row, bool, *int, *int, error) {
	if _, err := enumOf(outputMode, outputModes, "output_mode"); err != nil {
		return nil, false, nil, nil, err
	}
	current := make([]row, len(rows))
	copy(current, rows)
	complete := true
	var countValue *int
	countSeen := false
	if len(pipeline) > 0 {
		expanded := []row{}
		for _, r := range current {
			lines := splitLinesOrSelf(r.text)
			for _, line := range lines {
				expanded = append(expanded, row{text: line, workerID: r.workerID})
			}
		}
		current = expanded
		for _, stage := range pipeline {
			stageOp, err := enumOf(strOf(stage["operation"]), pipelineOps, "pipeline operation")
			if err != nil {
				return nil, false, nil, nil, err
			}
			switch stageOp {
			case "limit":
				countValue = nil
				limit, err := positiveOf(stage["lines"], "pipeline lines", false)
				if err != nil {
					return nil, false, nil, nil, err
				}
				complete = complete && len(current) <= limit
				if limit < len(current) {
					current = current[:limit]
				}
			case "slice":
				countValue = nil
				start, err := positiveOf(stage["start_line"], "pipeline start_line", false)
				if err != nil {
					return nil, false, nil, nil, err
				}
				end, err := positiveOf(stage["end_line"], "pipeline end_line", false)
				if err != nil {
					return nil, false, nil, nil, err
				}
				if end < start {
					return nil, false, nil, nil, fmt.Errorf("pipeline end_line must be >= start_line")
				}
				complete = complete && start == 1 && end >= len(current)
				lo := start - 1
				if lo > len(current) {
					lo = len(current)
				}
				if end > len(current) {
					end = len(current)
				}
				current = current[lo:end]
			case "filter":
				countValue = nil
				query := strOf(stage["query"])
				if query == "" {
					return nil, false, nil, nil, fmt.Errorf("filter query must be non-empty")
				}
				if len([]rune(query)) > 512 {
					return nil, false, nil, nil, fmt.Errorf("filter query must not exceed 512 characters")
				}
				invert := stage["invert"] == true
				caseInsensitive := stage["case_insensitive"] == true
				matcher, err := lexruntime.CompileMatcher(query, caseInsensitive, stage["fixed_strings"] == true)
				if err != nil {
					return nil, false, nil, nil, err
				}
				filtered := []row{}
				for _, r := range current {
					if matcher(r.text) != invert {
						filtered = append(filtered, r)
					}
				}
				current = filtered
			default: // count
				n := rowCount(current)
				countValue = &n
				countSeen = true
				current = []row{{text: fmt.Sprintf("%d", n)}}
			}
		}
	}
	if outputMode == "count" {
		n := rowCount(current)
		countValue = &n
		countSeen = true
		current = []row{{text: fmt.Sprintf("%d", n)}}
	}
	var transformedCount *int
	if countSeen && countValue == nil {
		n := rowCount(current)
		transformedCount = &n
	}
	return current, complete, countValue, transformedCount, nil
}

// fitRows 输出预算拟合（对齐 _fit_rows :953-1006）。
// budget<0 表示禁用截断（Python None）。
func fitRows(rows []row, summary map[string]any, budget int, countValue *int, showShortenedMarker bool) (string, string, []string, bool, int) {
	header := boundedSummaryLine(summary, budget)
	selected := []row{}
	shortened := false
	for _, r := range rows {
		texts := []string{}
		for _, s := range selected {
			texts = append(texts, s.text)
		}
		texts = append(texts, r.text)
		body := strings.Join(texts, "\n")
		candidate := fmt.Sprintf("%s\n\n%s", header, body)
		if budget >= 0 && len([]rune(candidate)) > budget {
			shortened = true
			break
		}
		selected = append(selected, r)
	}
	for {
		finalSummary := map[string]any{
			"operation":               summary["operation"],
			"returned_skill_count":    countDistinctSkills(selected),
			"returned_category_count": countCategoryRows(selected),
		}
		if v, ok := summary["previously_shown"]; ok {
			finalSummary["previously_shown"] = v
		}
		header = boundedSummaryLine(finalSummary, budget)
		bodyLines := make([]string, 0, len(selected))
		for _, s := range selected {
			bodyLines = append(bodyLines, s.text)
		}
		body := strings.Join(bodyLines, "\n")
		var candidate string
		if body != "" {
			candidate = fmt.Sprintf("%s\n\n%s", header, body)
		} else {
			candidate = header
		}
		if budget < 0 || len([]rune(candidate)) <= budget || len(selected) == 0 {
			break
		}
		selected = selected[:len(selected)-1]
		shortened = true
	}
	if shortened && len(selected) == 0 && len(rows) > 0 && budget >= 0 {
		firstSkill := 0
		if rows[0].workerID != "" {
			firstSkill = 1
		}
		firstCategory := 0
		if strings.HasPrefix(strings.TrimLeft(rows[0].text, " \t"), "- [category]") {
			firstCategory = 1
		}
		fallbackSummary := map[string]any{
			"operation":               summary["operation"],
			"returned_skill_count":    firstSkill,
			"returned_category_count": firstCategory,
		}
		header = boundedSummaryLine(fallbackSummary, budget)
		available := budget - len([]rune(header)) - 2
		if available > 3 {
			selected = append(selected, row{text: compact(rows[0].text, available), workerID: rows[0].workerID})
		}
	}
	bodyLines := []string{}
	for _, s := range selected {
		bodyLines = append(bodyLines, s.text)
	}
	if shortened && showShortenedMarker {
		joined := strings.Join(append(append([]string{header, ""}, bodyLines...), shortenedMarker), "\n")
		if budget < 0 || len([]rune(joined)) <= budget {
			bodyLines = append(bodyLines, shortenedMarker)
		}
	}
	body := strings.Join(bodyLines, "\n")
	model := header
	if body != "" {
		model = fmt.Sprintf("%s\n\n%s", header, body)
	}
	observed := []string{}
	seen := map[string]bool{}
	for _, s := range selected {
		if s.workerID != "" && !seen[s.workerID] {
			seen[s.workerID] = true
			observed = append(observed, s.workerID)
		}
	}
	shownCount := len(selected)
	if countValue != nil && len(selected) > 0 {
		shownCount = *countValue
	}
	return body, model, observed, shortened, shownCount
}

// countDistinctSkills 选中行的去重技能数。
func countDistinctSkills(rows []row) int {
	seen := map[string]bool{}
	for _, r := range rows {
		if r.workerID != "" {
			seen[r.workerID] = true
		}
	}
	return len(seen)
}

// countCategoryRows 类别行计数。
func countCategoryRows(rows []row) int {
	n := 0
	for _, r := range rows {
		if strings.HasPrefix(strings.TrimLeft(r.text, " \t"), "- [category]") {
			n++
		}
	}
	return n
}

// rowCount 行数（对齐 _row_count :1009-1010）。
func rowCount(rows []row) int {
	total := 0
	for _, r := range rows {
		n := len(splitLines(r.text))
		if n < 1 {
			n = 1
		}
		total += n
	}
	return total
}

// summaryLine 汇总头选择（对齐 _summary_line :1013-1026）。
func summaryLine(summary map[string]any) string {
	skillCount, _ := summary["returned_skill_count"].(int)
	categoryCount, _ := summary["returned_category_count"].(int)
	operation, _ := summary["operation"].(string)
	previouslyShown, _ := summary["previously_shown"].(bool)
	if skillCount != 0 && categoryCount == 0 {
		if previouslyShown {
			return "## Skills — all names previously shown"
		}
		return "## Skills"
	}
	if categoryCount != 0 && skillCount == 0 {
		if operation == "search" {
			return "## Category hints — matching groups, not Skills"
		}
		return "## Categories"
	}
	if categoryCount != 0 {
		if operation == "search" {
			return "## Skills and category hints"
		}
		return "## Entries"
	}
	if operation == "search" {
		return "## Skills"
	}
	return "## Results"
}

// boundedSummaryLine 预算内汇总头（对齐 _bounded_summary_line :1029-1031）。
func boundedSummaryLine(summary map[string]any, budget int) string {
	line := summaryLine(summary)
	if budget < 0 {
		return line
	}
	return compact(line, budget)
}

// ──────────────────────────── 请求校验与解析 ────────────────────────────

// queriesOf 查询解析（对齐 _queries :1034-1055）。
func queriesOf(query, queriesArg any) ([]string, error) {
	if query != nil && queriesArg != nil {
		return nil, fmt.Errorf("query and queries are mutually exclusive")
	}
	if q, ok := query.(string); ok && strings.HasPrefix(strings.TrimLeft(q, " \t"), "[") {
		var decoded []any
		if err := json.Unmarshal([]byte(q), &decoded); err == nil {
			allStr := len(decoded) > 0
			for _, item := range decoded {
				if _, ok := item.(string); !ok {
					allStr = false
					break
				}
			}
			if len(decoded) == 0 {
				allStr = true
			}
			if allStr {
				query = decoded
			}
		}
	}
	var batch []any
	batchIsQueryList := false
	switch v := query.(type) {
	case []any:
		batch = v
		batchIsQueryList = true
	case []string:
		batch = make([]any, len(v))
		for i, s := range v {
			batch[i] = s
		}
		batchIsQueryList = true
	}
	if batch == nil {
		if l, ok := queriesArg.([]any); ok {
			batch = l
		} else if l, ok := queriesArg.([]string); ok {
			batch = make([]any, len(l))
			for i, s := range l {
				batch[i] = s
			}
		}
	}
	if batch != nil {
		minimum := 2
		name := "queries"
		if batchIsQueryList {
			minimum = 1
			name = "query"
		}
		if len(batch) < minimum || len(batch) > 8 {
			return nil, fmt.Errorf("%s must contain %d-8 strings", name, minimum)
		}
		values := []string{}
		seen := map[string]bool{}
		for _, item := range batch {
			v, err := boundedQuery(item, name+" item")
			if err != nil {
				return nil, err
			}
			if !seen[v] {
				seen[v] = true
				values = append(values, v)
			}
		}
		if len(values) < minimum {
			return nil, fmt.Errorf("%s must contain at least %d distinct value(s)", name, minimum)
		}
		return values, nil
	}
	v, err := boundedQuery(query, "query")
	if err != nil {
		return nil, err
	}
	return []string{v}, nil
}

// cursorQuery 游标内的查询快照（对齐 _cursor_query :1058-1062）。
func cursorQuery(arguments map[string]any) any {
	if strOf(arguments["operation"]) != "search" {
		return nil
	}
	values, err := queriesOf(arguments["query"], arguments["queries"])
	if err != nil {
		return nil
	}
	if len(values) == 1 {
		return values[0]
	}
	return values
}

// applySimpleDefaults 简单请求缺省（对齐 _apply_simple_defaults :1065-1088）。
func applySimpleDefaults(arguments map[string]any, limit any) error {
	operation, err := enumOf(strOf(arguments["operation"]), operations, "operation")
	if err != nil {
		return err
	}
	resultLimit, err := positiveOf(limit, "limit", true)
	if err != nil {
		return err
	}
	if resultLimit == 0 {
		resultLimit = 10
	}
	if resultLimit > maxSimpleLimit {
		return fmt.Errorf("limit must not exceed %d for %s", maxSimpleLimit, operation)
	}
	switch operation {
	case "list":
		if arguments["query"] != nil || arguments["skills"] != nil {
			return fmt.Errorf("list accepts only category and limit")
		}
		arguments["view"] = "details"
		if limit != nil {
			arguments["_page_size"] = resultLimit
		} else {
			arguments["_page_size"] = 0
		}
		arguments["_page_offset"] = 0
	case "search":
		if arguments["skills"] != nil {
			return fmt.Errorf("skills are valid only for read")
		}
		queries, err := queriesOf(arguments["query"], nil)
		if err != nil {
			return err
		}
		ps := defaultSearchPageSize * len(queries)
		if ps > 20 {
			ps = 20
		}
		if limit != nil {
			ps = resultLimit
		}
		arguments["_page_size"] = ps
		arguments["_page_offset"] = 0
	default:
		if arguments["query"] != nil {
			return fmt.Errorf("query is valid only for search")
		}
	}
	return nil
}

// categoryPathsOf 类别 → 视图路径（对齐 _category_paths :1091-1120）。
func categoryPathsOf(directory *SkillDirectoryView, category, legacyPaths any) ([]string, error) {
	if category == nil {
		return pathsOf(legacyPaths, false)
	}
	if legacyPaths != nil {
		return nil, fmt.Errorf("category and paths are mutually exclusive")
	}
	value, err := nonempty(category, "category")
	if err != nil {
		return nil, err
	}
	if value == "/" || value == "ROOT" {
		return []string{"/"}, nil
	}
	parts := []string{}
	for _, part := range strings.Split(value, ">") {
		if p := strings.TrimSpace(part); p != "" {
			parts = append(parts, p)
		}
	}
	normalized := strings.Join(parts, " > ")
	matches := []string{}
	for p := range directory.NodeByPathMap() {
		if p == "/" {
			continue
		}
		if strings.ToLower(categoryFromPath(directory, p)) == strings.ToLower(normalized) {
			matches = append(matches, p)
		}
	}
	if len(matches) == 0 && !strings.Contains(normalized, ">") {
		for p, node := range directory.NodeByPathMap() {
			if p == "/" {
				continue
			}
			if strings.ToLower(strings.TrimSpace(node.Label)) == strings.ToLower(normalized) {
				matches = append(matches, p)
			}
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("Unknown Skill category: %s", value)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("Ambiguous Skill category; use its full category chain: %s", value)
	}
	return []string{matches[0]}, nil
}

// validateRequest 请求合法性（对齐 _validate_request :1123-1284）。
func validateRequest(operation string, arguments map[string]any) ([]map[string]any, error) {
	listFields := arguments["view"] != "names" ||
		arguments["recursive"] != false ||
		arguments["directory_entry"] != false ||
		arguments["directories_only"] != false
	searchFields := arguments["query"] != nil ||
		arguments["queries"] != nil ||
		arguments["per_query_limit"] != nil ||
		arguments["match"] != "content" ||
		arguments["result"] != "files" ||
		arguments["case_insensitive"] != true ||
		arguments["fixed_strings"] != false
	readFields := arguments["read_mode"] != "full" ||
		arguments["line_count"] != nil ||
		arguments["start_line"] != nil ||
		arguments["end_line"] != nil
	if operation != "list" && listFields {
		return nil, fmt.Errorf("view, recursive, directory_entry, and directories_only are only valid for list")
	}
	if operation != "search" && searchFields {
		return nil, fmt.Errorf("query, queries, per_query_limit, match, result, and search flags are only valid for search")
	}
	if operation != "read" && readFields {
		return nil, fmt.Errorf("read_mode and line range fields are only valid for read")
	}
	if operation == "read" && arguments["skills"] == nil && arguments["paths"] == nil {
		return nil, fmt.Errorf("skills are required for read")
	}
	if operation != "read" && arguments["skills"] != nil {
		return nil, fmt.Errorf("skills are valid only for read")
	}
	if operation == "read" && arguments["skills"] != nil && arguments["paths"] != nil {
		return nil, fmt.Errorf("skills and paths are mutually exclusive")
	}
	if operation == "list" {
		view, err := enumOf(strOf(arguments["view"]), listViews, "view")
		if err != nil {
			return nil, err
		}
		if view == "tree" {
			if arguments["recursive"] != false || arguments["directory_entry"] != false {
				return nil, fmt.Errorf("recursive and directory_entry are not valid with tree view")
			}
		} else if arguments["max_depth"] != nil || arguments["directories_only"] != false {
			return nil, fmt.Errorf("max_depth and directories_only require tree view")
		}
	} else if operation == "search" {
		match := strOf(arguments["match"])
		if match != "content" && arguments["result"] != "files" {
			return nil, fmt.Errorf("result is only valid for content search")
		}
		if match != "content" && arguments["fixed_strings"] != false {
			return nil, fmt.Errorf("fixed_strings is only valid for content search")
		}
	} else {
		if arguments["max_depth"] != nil {
			return nil, fmt.Errorf("max_depth is only valid for list and search")
		}
		readMode, err := enumOf(strOf(arguments["read_mode"]), readModes, "read_mode")
		if err != nil {
			return nil, err
		}
		lineCount := arguments["line_count"]
		startLine := arguments["start_line"]
		endLine := arguments["end_line"]
		if readMode == "full" && (lineCount != nil || startLine != nil || endLine != nil) {
			return nil, fmt.Errorf("line_count/start_line/end_line require head or range read_mode")
		}
		if readMode == "head" && (startLine != nil || endLine != nil) {
			return nil, fmt.Errorf("start_line/end_line are valid only for range read_mode")
		}
		if readMode == "range" && lineCount != nil {
			return nil, fmt.Errorf("line_count is valid only for head read_mode")
		}
		if readMode == "range" && (startLine == nil || endLine == nil) {
			return nil, fmt.Errorf("start_line and end_line are required for range read_mode")
		}
	}
	pipeline := arguments["pipeline"]
	outputMode, err := enumOf(strOf(arguments["output_mode"]), outputModes, "output_mode")
	if err != nil {
		return nil, err
	}
	batchQueries := arguments["queries"] != nil
	if outputMode == "count" {
		if operation == "read" {
			return nil, fmt.Errorf("count output_mode is valid only for list and search")
		}
		if pipeline != nil {
			return nil, fmt.Errorf("count output_mode cannot be combined with pipeline")
		}
	}
	if batchQueries && outputMode != "entries" {
		return nil, fmt.Errorf("queries require output_mode=entries")
	}
	if pipeline == nil {
		return nil, nil
	}
	pipelineList, ok := pipeline.([]any)
	if !ok {
		return nil, fmt.Errorf("pipeline must be an array")
	}
	var normalized []map[string]any
	for index, rawStage := range pipelineList {
		stageMap, ok := rawStage.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("pipeline stages must be objects")
		}
		unexpected := []string{}
		for k := range stageMap {
			if !allowedStageFields[k] {
				unexpected = append(unexpected, k)
			}
		}
		if len(unexpected) > 0 {
			sort.Strings(unexpected)
			return nil, fmt.Errorf("pipeline[%d] has unsupported fields: %s", index, strings.Join(unexpected, ", "))
		}
		stage, err := enumOf(strOf(stageMap["operation"]), pipelineOps, fmt.Sprintf("pipeline[%d].operation", index))
		if err != nil {
			return nil, err
		}
		var query any
		if stageMap["query"] != nil {
			q, err := boundedQuery(stageMap["query"], fmt.Sprintf("pipeline[%d].query", index))
			if err != nil {
				return nil, err
			}
			query = q
		}
		lines, err := positiveOf(stageMap["lines"], fmt.Sprintf("pipeline[%d].lines", index), true)
		if err != nil {
			return nil, err
		}
		if lines > maxLines {
			return nil, fmt.Errorf("pipeline[%d].lines must not exceed %d", index, maxLines)
		}
		start, err := positiveOf(stageMap["start_line"], fmt.Sprintf("pipeline[%d].start_line", index), true)
		if err != nil {
			return nil, err
		}
		end, err := positiveOf(stageMap["end_line"], fmt.Sprintf("pipeline[%d].end_line", index), true)
		if err != nil {
			return nil, err
		}
		insensitive := stageMap["case_insensitive"] == true
		invert := stageMap["invert"] == true
		fixed := stageMap["fixed_strings"] == true
		hasQuery := query != nil
		hasLines := lines > 0
		hasStart := start > 0
		hasEnd := end > 0
		switch stage {
		case "limit":
			if !hasLines {
				return nil, fmt.Errorf("pipeline[%d].lines is required for limit", index)
			}
			if hasQuery || hasStart || hasEnd || insensitive || invert || fixed {
				return nil, fmt.Errorf("pipeline[%d] has fields not valid for limit", index)
			}
			normalized = append(normalized, map[string]any{"operation": stage, "lines": lines})
		case "slice":
			if !hasStart || !hasEnd {
				return nil, fmt.Errorf("pipeline[%d] slice requires start_line and end_line", index)
			}
			if end < start {
				return nil, fmt.Errorf("pipeline[%d].end_line must be >= start_line", index)
			}
			if hasQuery || hasLines || insensitive || invert || fixed {
				return nil, fmt.Errorf("pipeline[%d] has fields not valid for slice", index)
			}
			normalized = append(normalized, map[string]any{"operation": stage, "start_line": start, "end_line": end})
		case "filter":
			if !hasQuery {
				return nil, fmt.Errorf("pipeline[%d].query is required for filter", index)
			}
			if hasLines || hasStart || hasEnd {
				return nil, fmt.Errorf("pipeline[%d] has fields not valid for filter", index)
			}
			normalized = append(normalized, map[string]any{
				"operation": stage, "query": query,
				"case_insensitive": insensitive, "invert": invert, "fixed_strings": fixed,
			})
		default: // count
			if hasQuery || hasLines || hasStart || hasEnd || insensitive || invert || fixed {
				return nil, fmt.Errorf("pipeline[%d] count takes no other fields", index)
			}
			normalized = append(normalized, map[string]any{"operation": stage})
		}
	}
	perQueryLimit := arguments["per_query_limit"]
	if batchQueries {
		if len(normalized) == 0 {
			return nil, nil
		}
		if len(normalized) != 1 || strOf(normalized[0]["operation"]) != "limit" {
			return nil, fmt.Errorf("queries support only one limit pipeline stage")
		}
		aliasLimit, _ := normalized[0]["lines"].(int)
		if aliasLimit > 10 {
			return nil, fmt.Errorf("pipeline[0].lines must not exceed 10 for queries")
		}
		if perQueryLimit != nil {
			pq, ok := perQueryLimit.(int)
			if !ok || pq != aliasLimit {
				return nil, fmt.Errorf("per_query_limit conflicts with pipeline limit")
			}
		}
		arguments["per_query_limit"] = aliasLimit
		return nil, nil
	}
	if perQueryLimit == nil {
		return normalized, nil
	}
	if len(normalized) == 0 {
		return nil, nil
	}
	pq, _ := perQueryLimit.(int)
	limitStages := 0
	var limitLines int
	for _, stage := range normalized {
		if strOf(stage["operation"]) == "limit" {
			limitStages++
			limitLines, _ = stage["lines"].(int)
		}
	}
	if len(normalized) != 1 || limitStages != 1 || limitLines != pq {
		return nil, fmt.Errorf("per_query_limit conflicts with pipeline limit")
	}
	return nil, nil
}

// literalMatchRank 字面命中等级（对齐 _literal_match_rank :1287-1292）。
func literalMatchRank(candidate, query string) int {
	if candidate == query {
		return 0
	}
	if strings.HasPrefix(candidate, query) {
		return 1
	}
	return 2
}

// boundedQuery 查询词校验（对齐 _bounded_query :1295-1301）。
func boundedQuery(value any, name string) (string, error) {
	query, err := nonempty(value, name)
	if err != nil {
		return "", err
	}
	if strings.Contains(query, "\x00") {
		return "", fmt.Errorf("%s must not contain a null byte", name)
	}
	if len([]rune(query)) > maxQueryChars {
		return "", fmt.Errorf("%s must not exceed %d characters", name, maxQueryChars)
	}
	return query, nil
}

// pathsOf 路径数组校验（对齐 _paths :1304-1318）。
func pathsOf(value any, required bool) ([]string, error) {
	if value == nil {
		if required {
			return nil, fmt.Errorf("paths are required for read")
		}
		return []string{"/"}, nil
	}
	list, ok := value.([]any)
	if !ok {
		if sl, ok2 := value.([]string); ok2 && len(sl) > 0 {
			list = make([]any, len(sl))
			for i, v := range sl {
				list[i] = v
			}
		} else {
			return nil, fmt.Errorf("paths must be a non-empty array")
		}
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("paths must be a non-empty array")
	}
	if len(list) > maxPathsItems {
		return nil, fmt.Errorf("paths must contain at most %d items", maxPathsItems)
	}
	paths := []string{}
	seen := map[string]bool{}
	for _, item := range list {
		p, err := nonempty(item, "path")
		if err != nil {
			return nil, err
		}
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	for _, p := range paths {
		if strings.Contains(p, "\x00") {
			return nil, fmt.Errorf("paths must not contain null bytes")
		}
		if len([]rune(p)) > maxPathChars {
			return nil, fmt.Errorf("paths must not exceed %d characters", maxPathChars)
		}
	}
	return paths, nil
}

// identifiers 标识数组校验（对齐 _identifiers :1321-1335）。
func identifiers(values any, name string, required bool) ([]string, error) {
	if values == nil {
		if required {
			return nil, fmt.Errorf("%s are required", name)
		}
		return nil, nil
	}
	list, ok := values.([]any)
	if !ok || len(list) == 0 {
		return nil, fmt.Errorf("%s must be a non-empty array", name)
	}
	if len(list) > maxPathsItems {
		return nil, fmt.Errorf("%s must contain at most %d items", name, maxPathsItems)
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, item := range list {
		v, err := nonempty(item, name)
		if err != nil {
			return nil, err
		}
		if !seen[v] {
			seen[v] = true
			ids = append(ids, v)
		}
	}
	for _, id := range ids {
		if strings.Contains(id, "\x00") {
			return nil, fmt.Errorf("%s must not contain null bytes", name)
		}
		if len([]rune(id)) > maxIdentifierChars {
			return nil, fmt.Errorf("%s must not exceed %d characters", name, maxIdentifierChars)
		}
	}
	return ids, nil
}

// outputBudget 输出预算（对齐 _output_budget :1338-1348）。返回 -1 表示禁用截断。
func outputBudget(value, disable any, defaultMax int) (int, error) {
	// 类型校验：仅接受布尔（Python isinstance(disable, bool)）
	if _, ok := disable.(bool); !ok {
		return 0, fmt.Errorf("disable_output_truncation must be a boolean")
	}
	if disable == true {
		return -1, nil
	}
	if value == nil {
		return defaultMax, nil
	}
	parsed, err := positiveOf(value, "max_output_chars", false)
	if err != nil {
		return 0, err
	}
	if parsed < minOutputChars || parsed > maxOutputChars {
		return 0, fmt.Errorf("max_output_chars must be between 512 and 48000")
	}
	return parsed, nil
}

// ──────────────────────────── 基础校验助手 ────────────────────────────

// enumOf 枚举校验（对齐 _enum）。
func enumOf(value any, allowed []string, name string) (string, error) {
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be one of: %s", name, strings.Join(allowed, ", "))
	}
	for _, a := range allowed {
		if s == a {
			return s, nil
		}
	}
	return "", fmt.Errorf("%s must be one of: %s", name, strings.Join(allowed, ", "))
}

// positiveOf 正整数校验（对齐 _positive）。optional 时 nil 返回 0。
func positiveOf(value any, name string, optional bool) (int, error) {
	if value == nil && optional {
		return 0, nil
	}
	if value == nil {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	n, ok := value.(int)
	if !ok || n < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return n, nil
}

// nonempty 非空字符串（对齐 _nonempty）。
func nonempty(value any, name string) (string, error) {
	s, ok := value.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("%s must be a non-empty string", name)
	}
	return strings.TrimSpace(s), nil
}

// compact 压缩至限长（对齐 _compact :1377-1379；limit<4 时 Python 负切片语义：
// text[:limit-3] 取前 len+limit-3 个字符）。
func compact(value string, limit int) string {
	text := strings.Join(strings.Fields(value), " ")
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	end := limit - 3
	if end < 0 {
		end = len(runes) + end
		if end < 0 {
			end = 0
		}
	}
	return strings.TrimRight(string(runes[:end]), " ") + "..."
}

// appendNote 预算内追加注脚（对齐 _append_note :1382-1391）。
func appendNote(content, note string, budget int) string {
	if content == "" {
		if budget < 0 {
			return note
		}
		return compact(note, budget)
	}
	if budget < 0 {
		return fmt.Sprintf("%s\n\n%s", content, note)
	}
	remaining := budget - len([]rune(content)) - 2
	if remaining <= 3 {
		return content
	}
	rendered := note
	if len([]rune(note)) > remaining {
		rendered = compact(note, remaining)
	}
	return fmt.Sprintf("%s\n\n%s", content, rendered)
}

// candidateTokens 候选 token 估算（对齐 _candidate_tokens :1394-1396）。
func candidateTokens(records []SkillRecord) int {
	lines := make([]string, 0, len(records))
	for _, r := range records {
		lines = append(lines, fmt.Sprintf("- %s: %s", r.WorkerID, strings.Join(strings.Fields(r.Description), " ")))
	}
	rendered := strings.Join(lines, "\n")
	return (len([]rune(rendered)) + 3) / 4
}

// emptyMessage 空结果消息（对齐 _empty_message :1399-1404）。
func emptyMessage(operation string, category any) string {
	if operation != "search" {
		return "No entries."
	}
	if category != nil {
		return fmt.Sprintf("No matching Skills found in `%s`.", SanitizeModelText(strOf(category)))
	}
	return "No matching installed Skills found."
}

// compactJSON 紧凑 JSON（对齐 separators=(",",":")，键序保持 operation→cursor）。
func compactJSON(payload map[string]any) string {
	op, _ := payload["operation"].(string)
	cur, _ := payload["cursor"].(string)
	opJSON, _ := json.Marshal(op)
	curJSON, _ := json.Marshal(cur)
	return fmt.Sprintf(`{"operation":%s,"cursor":%s}`, opJSON, curJSON)
}

// orStr 首个非空字符串。
func orStr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// fnmatchReCache fnmatch 正则缓存
var fnmatchReCache = map[string]*regexp.Regexp{}

// fnmatchCase 大小写敏感 fnmatch（对齐 Python fnmatch.fnmatchcase：* 匹配含分隔符的任意序列，
// ? 匹配单字符；与 path.Match 的分隔符语义不同）。
func fnmatchCase(pattern, value string) bool {
	re, ok := fnmatchReCache[pattern]
	if !ok {
		var b strings.Builder
		b.WriteString("(?s)^")
		for _, r := range pattern {
			switch r {
			case '*':
				b.WriteString(".*")
			case '?':
				b.WriteString(".")
			default:
				b.WriteString(regexp.QuoteMeta(string(r)))
			}
		}
		b.WriteString("$")
		compiled, err := regexp.Compile(b.String())
		if err != nil {
			return false
		}
		fnmatchReCache[pattern] = compiled
		re = compiled
	}
	return re.MatchString(value)
}
