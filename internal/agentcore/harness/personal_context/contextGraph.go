package personal_context

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	
	"strings"
	"unicode/utf8"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// 从发布的 Context Markdown 树读取图谱投影（查询时现算，无缓存）。
// Python: openjiuwen/harness/personal_context/context_graph.py（一比一移植）。

// ──────────────────────────── 常量 ────────────────────────────

// 图谱安全上限。Python: context_graph.py:17-20
const (
	maxGraphFiles     = 10_000
	maxGraphFileBytes = 2 * 1024 * 1024
	maxGraphPathChars = 1_024
	maxSliceDepth     = 10
)

// 搜索常量。Python: context_graph.py:27-28
const (
	maxSearchResults = 10
	maxSnippetChars  = 240
)

// BM25F 参数。Python: context_graph.py:31-33
const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// fieldWeights 字段权重。Python: context_graph.py:30 _FIELD_WEIGHTS
var fieldWeights = map[string]float64{"title": 4.0, "path": 3.0, "headings": 2.0, "body": 1.0}

// ──────────────────────────── 全局变量 ────────────────────────────

// graphMarkdownLinkPattern Markdown 内链（排除图片 ! 前缀）。
// Python: context_graph.py:14 _MARKDOWN_LINK —— RE2 无 lookbehind，改用捕获组
// 排除（匹配时检查前置字符），见 graphMarkdownLinks。
var graphMarkdownLinkPattern = regexp.MustCompile(`\[([^\]\r\n]*)\]\(([^)\r\n]+)\)`)

// searchTokenPattern 搜索分词：ascii 词 | CJK 段。
// Python: context_graph.py:21 _SEARCH_TOKEN
var searchTokenPattern = regexp.MustCompile(`[a-z0-9_]+|[\x{3400}-\x{4dbf}\x{4e00}-\x{9fff}]+`)

// headingPattern 1-3 级标题。
// Python: context_graph.py:22 _HEADING
var headingPattern = regexp.MustCompile(`(?m)^(#{1,3})\s+(.+?)\s*$`)

// collapsedWhitespacePattern 空白折叠。
// Python: context_graph.py:23 _COLLAPSED_WHITESPACE
var collapsedWhitespacePattern = regexp.MustCompile(`\s+`)

// ──────────────────────────── 结构体 ────────────────────────────

// GraphNode 图谱节点。
// Python: build_context_slice 内 nodes[node_id] 字典。
type GraphNode struct {
	// ID 节点 ID（page:<相对路径>）
	ID string `json:"id"`
	// Kind directory | document
	Kind string `json:"kind"`
	// Subkind directory.<深度> | document.0
	Subkind string `json:"subkind"`
	// Label 标题（H1 或文件名）
	Label string `json:"label"`
	// Path 相对路径
	Path string `json:"path"`
	// ServiceID 恒 nil（来源不进图谱）
	ServiceID *string `json:"service_id"`
	// HasChildren 是否有子节点
	HasChildren bool `json:"has_children"`
}

// GraphEdge 图谱边。
type GraphEdge struct {
	// Source 起点节点 ID
	Source string `json:"source"`
	// Target 终点节点 ID
	Target string `json:"target"`
	// Kind contains | references
	Kind string `json:"kind"`
}

// GraphSlice BFS 裁剪后的图谱切片。
type GraphSlice struct {
	// ContextReady 上下文是否就绪
	ContextReady bool `json:"context_ready"`
	// Nodes 节点列表（BFS 序）
	Nodes []GraphNode `json:"nodes"`
	// Edges 边列表（kind/source/target 排序）
	Edges []GraphEdge `json:"edges"`
}

// GraphPage 单页详情。
type GraphPage struct {
	// NodeID 节点 ID
	NodeID string `json:"node_id"`
	// Title 标题
	Title string `json:"title"`
	// Path 相对路径
	Path string `json:"path"`
	// Markdown 页面全文
	Markdown string `json:"markdown"`
}

// SearchResult 搜索结果条目。
type SearchResult struct {
	// NodeID 节点 ID
	NodeID string `json:"node_id"`
	// Title 标题
	Title string `json:"title"`
	// Path 相对路径
	Path string `json:"path"`
	// Snippet 命中摘要
	Snippet string `json:"snippet"`
}

// searchPage 单页索引记录。
// Python: context_graph.py:29 _SearchPage 五元组。
type searchPage struct {
	relative    string
	title       string
	text        string
	fieldCounts map[string]map[string]int
	fieldLengths map[string]int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildContextGraph 构建含引用边的 BFS 图谱切片。
// Python: context_graph.py:233 build_context_graph
func BuildContextGraph(contextRoot string, rootID string, depth int) (*GraphSlice, error) {
	return buildContextSlice(contextRoot, rootID, depth, true)
}

// BuildContextTree 构建文件树切片（不含 references 边）。
// Python: context_graph.py:244 build_context_tree
func BuildContextTree(contextRoot string, rootID string, depth int) (*GraphSlice, error) {
	return buildContextSlice(contextRoot, rootID, depth, false)
}

// ReadContextGraphPage 按 graph 节点 ID 读取一个已发布页面。
// Python: context_graph.py:358 read_context_graph_page
func ReadContextGraphPage(contextRoot string, nodeID string) (*GraphPage, error) {
	if nodeID == "" {
		return nil, graphError("PersonalContext graph page node ID is invalid", nil)
	}
	if strings.HasPrefix(nodeID, "source:") {
		return nil, graphError("PersonalContext source detail must be read with get_source", nil)
	}
	if !strings.HasPrefix(nodeID, "page:") {
		return nil, graphError("PersonalContext graph page node ID is invalid", nil)
	}
	relative := strings.TrimPrefix(nodeID, "page:")
	if relative == "" || strings.Contains(relative, "\\") {
		return nil, graphError("PersonalContext graph page node ID is invalid", nil)
	}
	if path.IsAbs(relative) || containsDotDotPart(relative) {
		return nil, graphError("PersonalContext graph page node ID is invalid", nil)
	}
	if path.Clean(relative) != relative || strings.ToLower(path.Ext(relative)) != ".md" {
		return nil, graphError("PersonalContext graph page node ID is invalid", nil)
	}
	description := filepath.Join(contextRoot, "description.md")
	if info, err := os.Lstat(contextRoot); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, graphError("PersonalContext Context is not ready", nil)
	}
	if info, err := os.Lstat(description); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, graphError("PersonalContext Context is not ready", nil)
	}
	descriptionText, err := readGraphText(contextRoot, description)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(descriptionText) == "" {
		return nil, graphError("PersonalContext Context is not ready", nil)
	}
	markdown, err := readGraphText(contextRoot, filepath.Join(contextRoot, filepath.FromSlash(relative)))
	if err != nil {
		return nil, err
	}
	title, _ := pageHeadingFields(relative, markdown)
	return &GraphPage{NodeID: nodeID, Title: title, Path: relative, Markdown: markdown}, nil
}

// SearchContextGraph BM25F 搜索已发布 Context Markdown。
// Python: context_graph.py:394 search_context_graph
func SearchContextGraph(contextRoot string, query string) ([]SearchResult, error) {
	pages, err := readContextPagesFromRoot(contextRoot)
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return []SearchResult{}, nil
	}
	queryTerms := tokenize(query)
	seen := map[string]struct{}{}
	var terms []string
	for _, term := range queryTerms {
		if _, exists := seen[term]; !exists {
			seen[term] = struct{}{}
			terms = append(terms, term)
		}
	}
	var candidates []searchPage
	relativeList := make([]string, 0, len(pages))
	for relative := range pages {
		relativeList = append(relativeList, relative)
	}
	sort.Strings(relativeList)
	for _, relative := range relativeList {
		candidates = append(candidates, makeSearchPage(relative, pages[relative].text))
	}
	ranked := rankBM25F(candidates, terms)
	if len(ranked) > maxSearchResults {
		ranked = ranked[:maxSearchResults]
	}
	results := make([]SearchResult, 0, len(ranked))
	for _, page := range ranked {
		results = append(results, SearchResult{
			NodeID:  "page:" + page.relative,
			Title:   page.title,
			Path:    page.relative,
			Snippet: snippetOf(page.text, query, terms),
		})
	}
	return results, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// graphError 构造 CONTEXT_PROACTIVE_FILE_EXECUTION_ERROR。
// Python: context_graph.py:33 _graph_error
func graphError(message string, cause error) error {
	opts := []exception.ErrorOption{exception.WithMsg(message)}
	if cause != nil {
		opts = append(opts, exception.WithCause(cause))
	}
	return BuildError(StatusCodeContextProactiveFileExecutionError, opts...)
}

// checkGraphPath 路径必须位于受管根内且不穿越 symlink。
// Python: context_graph.py:37 _check_graph_path
func checkGraphPath(root string, target string) error {
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return graphError("PersonalContext graph path escaped its managed root", nil)
	}
	if len(filepath.ToSlash(relative)) > maxGraphPathChars {
		return graphError("PersonalContext graph path exceeds the safety limit", nil)
	}
	current := target
	for {
		info, statErr := os.Lstat(current)
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return graphError("PersonalContext graph path must not traverse a symlink", nil)
		}
		if current == root {
			return nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

// readGraphBytes 安全读取文件字节。
// Python: context_graph.py:53 _read_graph_bytes
func readGraphBytes(root string, target string) ([]byte, error) {
	if err := checkGraphPath(root, target); err != nil {
		return nil, err
	}
	info, statErr := os.Stat(target)
	if statErr != nil || !info.Mode().IsRegular() {
		return nil, graphError("PersonalContext graph file is missing", statErr)
	}
	if info.Size() > maxGraphFileBytes {
		return nil, graphError("PersonalContext graph file exceeds the safety limit", nil)
	}
	data, readErr := os.ReadFile(target)
	if readErr != nil {
		return nil, graphError("PersonalContext graph file could not be read", readErr)
	}
	return data, nil
}

// readGraphText 安全读取 UTF-8 文本。
// Python: context_graph.py:66 _read_graph_text
func readGraphText(root string, target string) (string, error) {
	data, err := readGraphBytes(root, target)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", graphError("PersonalContext graph file is not valid UTF-8", nil)
	}
	return string(data), nil
}

// graphMarkdownLinks 提取内链目标（排除图片 ![...](...)）——见 inlineLinks。
func graphMarkdownLinks(text string) [][]string {
	return inlineLinks(text)
}

// graphTarget 解析链接目标为页节点 ID（越界/非 .md/绝对路径返回空）。
// Python: context_graph.py:73 _graph_target
func graphTarget(contextRoot string, page string, rawTarget string, pageIDs map[string]string) string {
	target := strings.TrimSpace(rawTarget)
	if strings.HasPrefix(target, "<") {
		closing := strings.Index(target, ">")
		if closing < 0 {
			return ""
		}
		target = strings.TrimSpace(target[1:closing])
	} else {
		fields := strings.Fields(target)
		if len(fields) == 0 {
			return ""
		}
		target = strings.Trim(fields[0], "\"'")
	}
	if target == "" {
		return ""
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.Path == "" {
		return ""
	}
	pathValue := unquotePath(parsed.Path)
	if path.IsAbs(pathValue) || strings.ToLower(path.Ext(pathValue)) != ".md" {
		return ""
	}
	absTarget := filepath.Join(filepath.Dir(page), filepath.FromSlash(pathValue))
	absContext, _ := filepath.Abs(contextRoot)
	absTarget, _ = filepath.Abs(absTarget)
	relative, relErr := filepath.Rel(absContext, absTarget)
	if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ""
	}
	return pageIDs[filepath.ToSlash(relative)]
}

// contextPage 单页：绝对路径 + 文本。
type contextPage struct {
	path string
	text string
}

// readContextPagesFromRoot 读取受管 Context 根下的全部 Markdown 页。
// Python: context_graph.py:107 _read_context_pages_from_root
func readContextPagesFromRoot(contextRoot string) (map[string]contextPage, error) {
	description := filepath.Join(contextRoot, "description.md")
	rootInfo, err := os.Lstat(contextRoot)
	if err != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return map[string]contextPage{}, nil
	}
	descInfo, err := os.Lstat(description)
	if err != nil || descInfo.Mode()&os.ModeSymlink != 0 || !descInfo.Mode().IsRegular() {
		return map[string]contextPage{}, nil
	}
	descriptionText, err := readGraphText(contextRoot, description)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(descriptionText) == "" {
		return map[string]contextPage{}, nil
	}
	pages := map[string]contextPage{}
	fileCount := 0
	walkErr := filepath.WalkDir(contextRoot, func(target string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := checkGraphPath(contextRoot, target); err != nil {
			return err
		}
		if d.IsDir() || strings.ToLower(filepath.Ext(target)) != ".md" {
			return nil
		}
		fileCount++
		if fileCount > maxGraphFiles {
			return graphError("PersonalContext Context file count exceeds the safety limit", nil)
		}
		relative, relErr := filepath.Rel(contextRoot, target)
		if relErr != nil {
			return relErr
		}
		relative = filepath.ToSlash(relative)
		text := descriptionText
		if target != description {
			text, err = readGraphText(contextRoot, target)
			if err != nil {
				return err
			}
		}
		pages[relative] = contextPage{path: target, text: text}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return pages, nil
}

// validateSliceRequest 校验切片请求。
// Python: context_graph.py:134 _validate_slice_request
func validateSliceRequest(rootID string, depth int) error {
	if depth < 1 || depth > maxSliceDepth {
		return graphError(fmt.Sprintf("PersonalContext graph depth must be between 1 and %d", maxSliceDepth), nil)
	}
	_ = rootID
	return nil
}

// buildContextSlice 构建 BFS 稳定切片。
// Python: context_graph.py:141 build_context_slice
func buildContextSlice(contextRoot string, rootID string, depth int, includeReferences bool) (*GraphSlice, error) {
	if err := validateSliceRequest(rootID, depth); err != nil {
		return nil, err
	}
	pages, err := readContextPagesFromRoot(contextRoot)
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return &GraphSlice{ContextReady: false, Nodes: []GraphNode{}, Edges: []GraphEdge{}}, nil
	}

	pageIDs := map[string]string{}
	nodes := map[string]GraphNode{}
	var sortedRelatives []string
	for relative := range pages {
		sortedRelatives = append(sortedRelatives, relative)
	}
	sort.Strings(sortedRelatives)
	for _, relative := range sortedRelatives {
		nodeID := "page:" + relative
		pageIDs[relative] = nodeID
		isDirectory := path.Base(relative) == "description.md"
		directoryDepth := len(strings.Split(path.Dir(relative), "/"))
		if path.Dir(relative) == "." {
			directoryDepth = 0
		}
		title, _ := pageHeadingFields(relative, pages[relative].text)
		kind := "document"
		subkind := "document.0"
		if isDirectory {
			kind = "directory"
			subkind = fmt.Sprintf("directory.%d", directoryDepth)
		}
		nodes[nodeID] = GraphNode{
			ID: nodeID, Kind: kind, Subkind: subkind, Label: title,
			Path: relative, ServiceID: nil,
		}
	}

	contains := map[string]map[string]struct{}{}
	addEdge := func(kind string, source string, target string) {
		if contains[kind] == nil {
			contains[kind] = map[string]struct{}{}
		}
		contains[kind][source+"\x00"+target] = struct{}{}
	}
	children := map[string][]string{}
	for _, relative := range sortedRelatives {
		nodeID := pageIDs[relative]
		if relative == "description.md" {
			continue
		}
		directory := path.Dir(relative)
		var parentDirectory string
		if path.Base(relative) == "description.md" {
			parentDirectory = path.Dir(directory)
			if parentDirectory == "." {
				parentDirectory = ""
			}
		} else {
			parentDirectory = directory
		}
		parentDescription := "description.md"
		if parentDirectory != "" {
			parentDescription = parentDirectory + "/description.md"
		}
		if parentID, exists := pageIDs[parentDescription]; exists {
			addEdge("contains", parentID, nodeID)
			children[parentID] = append(children[parentID], nodeID)
		}
	}
	for parentID := range children {
		childIDs := children[parentID]
		sort.Slice(childIDs, func(i, j int) bool {
			return nodes[childIDs[i]].Path < nodes[childIDs[j]].Path
		})
		node := nodes[parentID]
		node.HasChildren = len(childIDs) > 0
		nodes[parentID] = node
	}

	rootNodeID := pageIDs["description.md"]
	if rootID != "" {
		rootNodeID = rootID
	}
	rootNode, exists := nodes[rootNodeID]
	if !exists {
		return nil, graphError("PersonalContext graph root node does not exist", nil)
	}
	if rootNode.Kind != "directory" {
		return nil, graphError("PersonalContext graph root node must be a directory", nil)
	}

	currentLevel := []string{rootNodeID}
	if rootID != "" {
		currentLevel = append([]string{}, children[rootNodeID]...)
	}
	var selected []string
	selectedSet := map[string]struct{}{}
	for level := 0; level < depth; level++ {
		var nextLevel []string
		for _, nodeID := range currentLevel {
			if _, seen := selectedSet[nodeID]; seen {
				continue
			}
			selected = append(selected, nodeID)
			selectedSet[nodeID] = struct{}{}
			nextLevel = append(nextLevel, children[nodeID]...)
		}
		currentLevel = nextLevel
	}

	allowed := func(nodeID string) bool {
		if rootID == "" {
			return true
		}
		if nodeID == rootNodeID {
			return true
		}
		_, ok := selectedSet[nodeID]
		return ok
	}

	edges := []GraphEdge{}
	containsPairs := map[string]struct{}{}
	for pair := range contains["contains"] {
		containsPairs[pair] = struct{}{}
		parts := strings.SplitN(pair, "\x00", 2)
		if allowed(parts[0]) && allowed(parts[1]) {
			edges = append(edges, GraphEdge{Source: parts[0], Target: parts[1], Kind: "contains"})
		}
	}
	if includeReferences {
		for _, relative := range sortedRelatives {
			sourceID := pageIDs[relative]
			text := pages[relative].text
			for _, match := range inlineLinks(text) {
				targetID := graphTarget(contextRoot, pages[relative].path, match[1], pageIDs)
				if targetID == "" {
					continue
				}
				pair := sourceID + "\x00" + targetID
				if _, isContains := containsPairs[pair]; isContains {
					continue
				}
				if _, duplicated := contains["references"][pair]; duplicated {
					continue
				}
				if allowed(sourceID) && allowed(targetID) {
					addEdge("references", sourceID, targetID)
					edges = append(edges, GraphEdge{Source: sourceID, Target: targetID, Kind: "references"})
				}
			}
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Kind != edges[j].Kind {
			return edges[i].Kind < edges[j].Kind
		}
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		return edges[i].Target < edges[j].Target
	})
	return &GraphSlice{
		ContextReady: true,
		Nodes:        selectedNodes(nodes, selected),
		Edges:        edges,
	}, nil
}

// selectedNodes 按 BFS 顺序组装节点列表。
func selectedNodes(nodes map[string]GraphNode, order []string) []GraphNode {
	result := make([]GraphNode, 0, len(order))
	for _, nodeID := range order {
		result = append(result, nodes[nodeID])
	}
	return result
}

// containsDotDotPart 路径是否含 ".." 段。
func containsDotDotPart(relative string) bool {
	for _, part := range strings.Split(relative, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

// inlineLinks 提取非图片 Markdown 链接。
// Python _MARKDOWN_LINK 的 (?<!!) 等价：匹配后检查 link 起点前一字符非 '!'。
func inlineLinks(text string) [][]string {
	var result [][]string
	offset := 0
	for {
		rest := text[offset:]
		loc := graphMarkdownLinkPattern.FindStringSubmatchIndex(rest)
		if loc == nil {
			break
		}
		if loc[0] == 0 || rest[loc[0]-1] != '!' {
			result = append(result, []string{rest[loc[2]:loc[3]], rest[loc[4]:loc[5]]})
		}
		offset += loc[1]
		if loc[1] == loc[0] {
			offset++
		}
	}
	return result
}

// pageHeadingFields 解析 H1 标题与 2-3 级子标题串。
// Python: context_graph.py:267 _page_heading_fields
func pageHeadingFields(relative string, text string) (string, string) {
	h1 := ""
	var subheadings []string
	for _, match := range headingPattern.FindAllStringSubmatch(text, -1) {
		heading := strings.TrimSpace(strings.TrimRight(match[2], "#"))
		switch len(match[1]) {
		case 1:
			if h1 == "" {
				h1 = heading
			}
		case 2, 3:
			subheadings = append(subheadings, heading)
		}
	}
	stem := path.Base(relative)
	stem = strings.TrimSuffix(stem, path.Ext(stem))
	if h1 == "" {
		h1 = stem
	}
	return h1, strings.Join(subheadings, " ")
}

// makeSearchPage 构造字段分词索引。
// Python: context_graph.py:283 _search_page
func makeSearchPage(relative string, text string) searchPage {
	title, subheadings := pageHeadingFields(relative, text)
	stem := path.Base(relative)
	stem = strings.TrimSuffix(stem, path.Ext(stem))
	page := searchPage{
		relative:     relative,
		title:        title,
		text:         text,
		fieldCounts:  map[string]map[string]int{},
		fieldLengths: map[string]int{},
	}
	counts := func(value string) (map[string]int, int) {
		counter := map[string]int{}
		total := 0
		for _, token := range tokenize(value) {
			counter[token]++
			total++
		}
		return counter, total
	}
	page.fieldCounts["title"], page.fieldLengths["title"] = counts(stem + " " + title)
	page.fieldCounts["path"], page.fieldLengths["path"] = counts(strings.ReplaceAll(relative, "/", " "))
	page.fieldCounts["headings"], page.fieldLengths["headings"] = counts(subheadings)
	page.fieldCounts["body"], page.fieldLengths["body"] = counts(text)
	return page
}

// tokenize 搜索分词：ascii 词 + CJK 单字与二元组（输入先 ToLower 对齐 casefold）。
// Python: context_graph.py:255 _tokenize
func tokenize(text string) []string {
	lowered := strings.ToLower(text)
	var tokens []string
	for _, match := range searchTokenPattern.FindAllString(lowered, -1) {
		if !isCJKToken(match) {
			tokens = append(tokens, match)
			continue
		}
		runes := []rune(match)
		for index := 0; index < len(runes); index++ {
			tokens = append(tokens, string(runes[index]))
			if index+1 < len(runes) {
				tokens = append(tokens, string(runes[index])+string(runes[index+1]))
			}
		}
	}
	return tokens
}

// isCJKToken 是否为 CJK 连续段（非 ascii 词）。
func isCJKToken(token string) bool {
	return !strings.ContainsFunc(token, func(r rune) bool { return r < 0x3400 })
}

// averageFieldLengths 各字段平均长度。
// Python: context_graph.py:300 _average_field_lengths
func averageFieldLengths(pages []searchPage) map[string]float64 {
	result := map[string]float64{}
	for name := range fieldWeights {
		total := 0
		for _, page := range pages {
			if value, exists := page.fieldLengths[name]; exists {
				total += value
			}
		}
		average := float64(total) / float64(len(pages))
		if average < 1.0 {
			average = 1.0
		}
		result[name] = average
	}
	return result
}

// rankBM25F BM25F 排序（分数>0 保留；同分按路径大小写折叠排序）。
// Python: context_graph.py:304 _rank_bm25f
func rankBM25F(pages []searchPage, queryTerms []string) []searchPage {
	if len(pages) == 0 || len(queryTerms) == 0 {
		return nil
	}
	averages := averageFieldLengths(pages)
	documentFrequencies := map[string]int{}
	for _, term := range queryTerms {
		count := 0
		for _, page := range pages {
			for name := range fieldWeights {
				if page.fieldCounts[name][term] > 0 {
					count++
					break
				}
			}
		}
		documentFrequencies[term] = count
	}
	type scored struct {
		score float64
		page  searchPage
	}
	var scoredPages []scored
	for _, page := range pages {
		score := 0.0
		for _, term := range queryTerms {
			df := documentFrequencies[term]
			if df == 0 {
				continue
			}
			weighted := 0.0
			for name, weight := range fieldWeights {
				count := page.fieldCounts[name][term]
				if count == 0 {
					continue
				}
				lengthRatio := float64(page.fieldLengths[name]) / averages[name]
				weighted += weight * float64(count) / (1.0 - bm25B + bm25B*lengthRatio)
			}
			if weighted == 0 {
				continue
			}
			idf := math.Log(1.0 + (float64(len(pages)-df)+0.5)/(float64(df)+0.5))
			score += idf * (weighted * (bm25K1 + 1.0)) / (bm25K1 + weighted)
		}
		if score > 0 {
			scoredPages = append(scoredPages, scored{score: score, page: page})
		}
	}
	sort.SliceStable(scoredPages, func(i, j int) bool {
		if scoredPages[i].score != scoredPages[j].score {
			return scoredPages[i].score > scoredPages[j].score
		}
		return strings.ToLower(scoredPages[i].page.relative) < strings.ToLower(scoredPages[j].page.relative)
	})
	result := make([]searchPage, 0, len(scoredPages))
	for _, item := range scoredPages {
		result = append(result, item.page)
	}
	return result
}

// snippetOf 命中摘要（优先 query 命中位置，前后扩 80 字符内 240 上限）。
// Python: context_graph.py:340 _snippet
func snippetOf(text string, query string, queryTerms []string) string {
	collapsed := collapsedWhitespacePattern.ReplaceAllString(strings.TrimSpace(text), " ")
	if len([]rune(collapsed)) <= maxSnippetChars {
		return collapsed
	}
	folded := strings.ToLower(collapsed)
	index := strings.Index(folded, strings.ToLower(query))
	if index < 0 {
		index = -1
		for _, term := range queryTerms {
			if position := strings.Index(folded, term); position >= 0 && (index < 0 || position < index) {
				index = position
			}
		}
		if index < 0 {
			index = 0
		}
	}
	runes := []rune(collapsed)
	index = len([]rune(collapsed[:index]))
	start := index - 80
	if start < 0 {
		start = 0
	}
	end := start + maxSnippetChars
	if end > len(runes) {
		end = len(runes)
		if end-start < maxSnippetChars {
			adjusted := end - maxSnippetChars
			if adjusted > 0 {
				start = adjusted
			}
		}
	}
	prefix := ""
	if start > 0 {
		prefix = "…"
	}
	suffix := ""
	if end < len(runes) {
		suffix = "…"
	}
	return prefix + string(runes[start:end]) + suffix
}

// minInt 取小。
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
