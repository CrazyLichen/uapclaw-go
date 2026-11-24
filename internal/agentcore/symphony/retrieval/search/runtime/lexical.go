// Package runtime 提供检索运行时基础件。
//
// 对应 Python 代码：openjiuwen/symphony/retrieval/search/runtime/。
// 本文件对齐 lexical.py（704 行）：live 目录记录的确定性词法排序。
// RE2 与 Python re 差异：\w 在 Python 为 Unicode 语义，Go 侧用 [\p{L}\p{N}_] 等价类。
//
// 文件目录：
//
//	runtime/
//	└── lexical.go    # LexicalDocument/LexicalIndex/compile_matcher/BM25 排序
package runtime

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// wordRe 词切分正则（对齐 _WORD_RE：ASCII 词 | CJK 连续段 \u3400-\u9fff）
var wordRe = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9_+.#/-]*|[\x{3400}-\x{9fff}]+`)

// cjkRe CJK 连续段判定（对齐 re.fullmatch(r"[\u3400-\u9fff]+")）
var cjkRe = regexp.MustCompile(`^[\x{3400}-\x{9fff}]+$`)

// techSuffixRe a-z0-9 后跟 ++/# 判定（对齐 re.fullmatch(r"[a-z0-9]+(?:\+\+|#)")）
var techSuffixRe = regexp.MustCompile(`^[a-z0-9]+(\+\+|#)$`)

// identTermRe 身份词判定（对齐 re.fullmatch(r"[\w .+/#-]+")，Python \w=Unicode）
var identTermRe = regexp.MustCompile(`^[\p{L}\p{N}_ .+/#-]+$`)

// splitRe 简单 frontmatter 剥离用的分隔切分（对齐 re.split(r"[\s_./-]+")）
var splitRe = regexp.MustCompile(`[\s_./-]+`)

// subRe 排序查询正则替换（对齐 _ranking_query）
var subRe = regexp.MustCompile(`[|()\[\]{}^$*?\\]`)

// sepRe 表面词拆分（对齐 re.split(r"[_+.#/-]+")）
var sepRe = regexp.MustCompile(`[_+.#/-]+`)

// LexicalDocument 一条可检索记录（对齐 frozen dataclass）
type LexicalDocument struct {
	// Key 唯一键
	Key string
	// Name 名称
	Name string
	// Description 描述
	Description string
	// Body 正文（缺省空）
	Body string
	// Aliases 别名（缺省空）
	Aliases []string
	// Category 分类（缺省空）
	Category string
}

// LexicalHit 一次稳定排序命中
type LexicalHit struct {
	// Key 文档键
	Key string
	// Score 排序分
	Score float64
}

// documentFields 评分字段序（key/name/aliases/description/body/category）
type documentFields struct{ key, name, aliases, description, body, category string }

// bm25Stats 语料统计
type bm25Stats struct {
	averageLength float64
	postings      map[string]int
	size          int
	cjkSize       int
}

// LexicalIndex 一份不可变目录的可复用确定性索引
type LexicalIndex struct {
	documents   map[string]LexicalDocument
	searchText  map[string]string
	fieldTokens map[string][]map[string]bool
	frequencies map[string]map[string]int
	lengths     map[string]int
	bm25        bm25Stats
}

// NewLexicalIndex 构建索引（语料统计一次算齐）。
// 对齐 Python __init__（lexical.py:56-81）：字段权重 (5,5,5,4,1)、
// searchText 拼接序 = 前 2 字段 + aliases + 后 4 字段、BM25 统计。
func NewLexicalIndex(documents []LexicalDocument) *LexicalIndex {
	idx := &LexicalIndex{
		documents:   map[string]LexicalDocument{},
		searchText:  map[string]string{},
		fieldTokens: map[string][]map[string]bool{},
		frequencies: map[string]map[string]int{},
		lengths:     map[string]int{},
	}
	postings := map[string]int{}
	for _, document := range documents {
		idx.documents[document.Key] = document
		fields := documentFieldsOf(document)
		// 逐字段独立切分（对齐 Python tokens = tuple(_tokens(value) for value in fields)）
		tokens := [][]string{
			tokensOf(fields.key), tokensOf(fields.name), tokensOf(fields.aliases),
			tokensOf(fields.description), tokensOf(fields.body), tokensOf(fields.category),
		}
		parts := append([]string{fields.key, fields.name}, document.Aliases...)
		parts = append(parts, fields.description, fields.body, fields.category)
		idx.searchText[document.Key] = strings.Join(parts, "\n")
		fieldTokenSets := make([]map[string]bool, len(tokens))
		for i, tk := range tokens {
			set := map[string]bool{}
			for _, t := range tk {
				set[t] = true
			}
			fieldTokenSets[i] = set
		}
		idx.fieldTokens[document.Key] = fieldTokenSets
		// 字段权重 (5,5,5,4,1)，category 单独计分（对齐 :70）
		freq := map[string]int{}
		weights := [6]int{5, 5, 5, 4, 1, 0} // 第六位（category）不参与频率加权
		for i, tk := range tokens {
			counts := map[string]int{}
			for _, t := range tk {
				counts[t]++
			}
			for t, c := range counts {
				freq[t] += c * weights[i]
			}
		}
		idx.frequencies[document.Key] = freq
		total := 0
		for _, c := range freq {
			total += c
		}
		idx.lengths[document.Key] = total
		for t := range freq {
			postings[t]++
		}
	}
	avg := 0.0
	if len(idx.lengths) > 0 {
		sum := 0
		for _, l := range idx.lengths {
			sum += l
		}
		avg = float64(sum) / float64(len(idx.lengths))
	}
	cjkSize := 0
	for _, freq := range idx.frequencies {
		for t := range freq {
			if !isASCII(t) {
				cjkSize++
				break
			}
		}
	}
	idx.bm25 = bm25Stats{
		averageLength: avg,
		postings:      postings,
		size:          len(idx.documents),
		cjkSize:       cjkSize,
	}
	return idx
}

// Search 带匹配器过滤 + BM25/字段/短语三维评分排序。
// 对齐 Python search（lexical.py:83-133）。
func (x *LexicalIndex) Search(query string, keys []string, caseInsensitive bool, fixedStrings bool) ([]LexicalHit, error) {
	text := strings.TrimSpace(query)
	if text == "" {
		return nil, fmt.Errorf("query must be non-empty")
	}
	matcher, err := CompileMatcher(text, caseInsensitive, fixedStrings)
	if err != nil {
		return nil, err
	}
	scope := x.documents
	if keys != nil {
		scope = map[string]LexicalDocument{}
		for _, k := range keys {
			if d, ok := x.documents[k]; ok {
				scope[k] = d
			}
		}
	}
	var matched []LexicalDocument
	for _, d := range scope {
		if matcher(x.searchText[d.Key]) {
			matched = append(matched, d)
		}
	}
	if len(matched) == 0 {
		return nil, nil
	}
	queryTokens := tokensOf(rankingQuery(text))
	scored := make([]LexicalHit, 0, len(matched))
	for _, d := range matched {
		score := bm25Score(queryTokens, x.frequencies[d.Key], x.lengths[d.Key], x.bm25, nil)
		score += fieldScore(queryTokens, x.fieldTokens[d.Key])
		score += phraseScore(d, text, caseInsensitive)
		scored = append(scored, LexicalHit{d.Key, score})
	}
	sortHitsBy(scored, func(h LexicalHit) int {
		return identityTier(x.documents[h.Key], text, caseInsensitive, fixedStrings)
	})
	return scored, nil
}

// SearchTerms 自然语言词族排序。
// 对齐 Python search_terms（lexical.py:135-188）。
func (x *LexicalIndex) SearchTerms(query string, keys []string) ([]LexicalHit, error) {
	text := strings.TrimSpace(query)
	if text == "" {
		return nil, fmt.Errorf("query must be non-empty")
	}
	scope := x.documents
	if keys != nil {
		scope = map[string]LexicalDocument{}
		for _, k := range keys {
			if d, ok := x.documents[k]; ok {
				scope[k] = d
			}
		}
	}
	families := queryTermFamilies(text)
	seen := map[string]bool{}
	var queryTokens []string
	for _, family := range families {
		for _, t := range family.terms {
			if !seen[t] {
				seen[t] = true
				queryTokens = append(queryTokens, t)
			}
		}
	}
	queryTerms := map[string]bool{}
	for _, t := range queryTokens {
		queryTerms[t] = true
	}
	var matched []LexicalDocument
	for _, d := range scope {
		fields := x.fieldTokens[d.Key]
		hit := false
		// 文件夹描述不构成子代内容命中：body 字段（最后一个）不计（对齐 :156）
		for i := 0; i < len(fields)-1; i++ {
			for t := range fields[i] {
				if queryTerms[t] {
					hit = true
					break
				}
			}
			if hit {
				break
			}
		}
		if !hit && identityContainsQuery(d, text, queryTokens) {
			matched = append(matched, d)
		} else if hit {
			matched = append(matched, d)
		}
	}
	if len(matched) == 0 {
		return nil, nil
	}
	raws := make([]string, 0, len(families))
	for _, f := range families {
		raws = append(raws, f.raw)
	}
	scored := make([]LexicalHit, 0, len(matched))
	for _, d := range matched {
		score := bm25FamilyScore(families, x.frequencies[d.Key], x.lengths[d.Key], x.bm25)
		score += fieldFamilyScore(families, d, x.fieldTokens[d.Key])
		score += partialIdentityScore(d, raws)
		score += phraseScore(d, text, true)
		scored = append(scored, LexicalHit{d.Key, score})
	}
	sortHitsBy(scored, func(h LexicalHit) int { return termIdentityTier(x.documents[h.Key], text) })
	return scored, nil
}

// MissingTerms 查询中未出现在任何索引内容的词。
// 对齐 Python missing_terms（lexical.py:190-194）。
func (x *LexicalIndex) MissingTerms(query string) []string {
	families := queryTermFamilies(strings.TrimSpace(query))
	var missing []string
	for _, f := range families {
		found := false
		for _, t := range f.terms {
			if x.bm25.postings[t] > 0 {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, f.raw)
		}
	}
	return missing
}

// MatchSnippet 匹配字段被隐藏时的证据摘录。
// 对齐 Python match_snippet（lexical.py:196-231）。
func (x *LexicalIndex) MatchSnippet(key string, queries []string, visibleDescriptionChars int, maxChars int) string {
	document, ok := x.documents[key]
	if !ok || maxChars < 1 {
		return ""
	}
	matched := map[string]bool{}
	for _, query := range queries {
		// name/alias 身份直配（对齐 :211-214）
		for _, pair := range []struct {
			label string
			value string
		}{{"name", document.Name}} {
			if pair.value != "" && identityValueMatchesQuery(pair.value, document.Key, query) {
				return fmt.Sprintf("%s: %s", pair.label, excerpt(pair.value, 0, len(pair.value), maxChars))
			}
		}
		for _, alias := range document.Aliases {
			if alias != "" && identityValueMatchesQuery(alias, document.Key, query) {
				return fmt.Sprintf("alias: %s", excerpt(alias, 0, len(alias), maxChars))
			}
		}
		present := dedupe(tokensOf(query))
		for _, t := range informativeTerms(present, x.bm25) {
			matched[t] = true
		}
	}
	if len(matched) == 0 {
		return ""
	}
	description := strings.Join(strings.Fields(document.Description), " ")
	descTokenSet := map[string]bool{}
	for _, t := range tokensOf(description[:min(visibleDescriptionChars, len(description))]) {
		descTokenSet[t] = true
	}
	hiddenDescription := map[string]bool{}
	for t := range matched {
		if x.fieldTokens[key][3][t] && !descTokenSet[t] {
			hiddenDescription[t] = true
		}
	}
	if len(hiddenDescription) > 0 {
		return fmt.Sprintf("description: %s", matchedExcerpt(description, hiddenDescription, maxChars))
	}
	metadataTokens := map[string]bool{}
	for i := 0; i < 4; i++ {
		for t := range x.fieldTokens[key][i] {
			metadataTokens[t] = true
		}
	}
	bodyMatches := map[string]bool{}
	for t := range matched {
		if x.fieldTokens[key][4][t] && !metadataTokens[t] {
			bodyMatches[t] = true
		}
	}
	if len(bodyMatches) == 0 {
		return ""
	}
	body := withoutFrontMatter(document.Body)
	return fmt.Sprintf("body: %s", matchedExcerpt(body, bodyMatches, maxChars))
}

// CompileMatcher 有界字面量/正则匹配器。
// 对齐 Python compile_matcher（lexical.py:234-246）。
func CompileMatcher(query string, caseInsensitive bool, fixedStrings bool) (func(string) bool, error) {
	if fixedStrings {
		needle := query
		if caseInsensitive {
			needle = strings.ToLower(query)
		}
		return func(value string) bool {
			candidate := value
			if caseInsensitive {
				candidate = strings.ToLower(value)
			}
			return strings.Contains(candidate, needle)
		}, nil
	}
	expression, err := CompileSafePattern(query, caseInsensitive)
	if err != nil {
		return nil, err
	}
	return func(value string) bool { return expression.MatchString(value) }, nil
}

// CompileSafePattern 有界正则子集编译。
// 对齐 Python compile_safe_pattern（lexical.py:249-259）：长度 1-512、
// 危险重复拒绝、编译失败报 invalid search expression。
func CompileSafePattern(pattern string, caseInsensitive bool) (*regexp.Regexp, error) {
	text := pattern
	if len(text) == 0 || len(text) > 512 {
		return nil, fmt.Errorf("search expression must contain 1-512 characters")
	}
	if err := rejectUnsafeRepetition(text); err != nil {
		return nil, err
	}
	expr := text
	if caseInsensitive {
		expr = "(?i)" + text
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid search expression: %v", err)
	}
	return re, nil
}

// rejectUnsafeRepetition 危险重复检测状态机。
// 对齐 Python _reject_unsafe_repetition（lexical.py:262-338）。
func rejectUnsafeRepetition(pattern string) error {
	type frame struct {
		repeat, alternate, complex bool
	}
	frames := []frame{{}}
	escaped := false
	inClass := false
	previous := ""
	closedComplex := false
	repetitionCount := 0
	index := 0
	for index < len(pattern) {
		char := pattern[index]
		if escaped {
			if char >= '1' && char <= '9' {
				return fmt.Errorf("search expression backreferences are not supported")
			}
			escaped = false
			previous = "atom"
			index++
			continue
		}
		switch char {
		case '\\':
			escaped = true
			index++
			continue
		}
		if inClass {
			if char == ']' {
				inClass = false
				previous = "atom"
			}
			index++
			continue
		}
		switch char {
		case '[':
			inClass = true
			index++
			continue
		case '(':
			if index+1 < len(pattern) && pattern[index+1] == '?' {
				return fmt.Errorf("search expression extensions are not supported")
			}
			frames = append(frames, frame{})
			previous = "open"
			index++
			continue
		case ')':
			if len(frames) == 1 {
				return nil // Python break 出循环
			}
			top := frames[len(frames)-1]
			frames = frames[:len(frames)-1]
			closedComplex = top.repeat || top.alternate || top.complex
			frames[len(frames)-1].complex = frames[len(frames)-1].complex || closedComplex
			previous = "group"
			index++
			continue
		case '|':
			frames[len(frames)-1].alternate = true
			previous = "alternate"
			index++
			continue
		}
		isRepeat := char == '*' || char == '+' || char == '?'
		if char == '{' {
			closing := strings.IndexByte(pattern[index+1:], '}')
			isRepeat = closing != -1
			if isRepeat {
				closing = index + 1 + closing
				bounds := strings.SplitN(pattern[index+1:closing], ",", 2)
				for _, bound := range bounds {
					if bound != "" && (!allDigits(bound) || atoiSafe(bound) > 10000) {
						return fmt.Errorf("search expression repetition bound is too large")
					}
				}
				index = closing
			}
		}
		if isRepeat {
			repetitionCount++
			if previous == "" || previous == "open" || previous == "alternate" || previous == "repeat" ||
				(previous == "group" && closedComplex) {
				return fmt.Errorf("search expression contains unsafe repetition")
			}
			if repetitionCount > 1 {
				return fmt.Errorf("search expression contains multiple repetitions")
			}
			frames[len(frames)-1].repeat = true
			frames[len(frames)-1].complex = true
			previous = "repeat"
			index++
			continue
		}
		previous = "atom"
		closedComplex = false
		index++
	}
	if strings.Count(pattern, ".*") > 1 {
		return fmt.Errorf("search expression contains unsafe wildcard repetition")
	}
	return nil
}

// identityTier 身份层级（0=键等值 … 4=无）。
// 对齐 Python _identity_tier（lexical.py:341-369）。
func identityTier(document LexicalDocument, query string, caseInsensitive bool, fixedStrings bool) int {
	key := document.Key
	name := document.Name
	aliases := document.Aliases
	var terms []string
	if fixedStrings {
		terms = []string{query}
	} else {
		for _, part := range strings.Split(query, "|") {
			p := strings.TrimSpace(part)
			if p != "" && identTermRe.MatchString(p) {
				terms = append(terms, p)
			}
		}
	}
	if caseInsensitive {
		key = strings.ToLower(key)
		name = strings.ToLower(name)
		folded := make([]string, len(aliases))
		for i, a := range aliases {
			folded[i] = strings.ToLower(a)
		}
		aliases = folded
		for i, t := range terms {
			terms[i] = strings.ToLower(t)
		}
	}
	for _, t := range terms {
		if key == t {
			return 0
		}
	}
	for _, t := range terms {
		if name == t {
			return 1
		}
	}
	for _, alias := range aliases {
		for _, t := range terms {
			if alias == t {
				return 2
			}
		}
	}
	for _, value := range append([]string{key, name}, aliases...) {
		for _, t := range terms {
			if strings.HasPrefix(value, t) {
				return 3
			}
		}
	}
	return 4
}

// termIdentityTier 词族身份层级。
// 对齐 Python _term_identity_tier（lexical.py:372-391）。
func termIdentityTier(document LexicalDocument, query string) int {
	key := normalizeIdentity(document.Key)
	name := normalizeIdentity(document.Name)
	aliases := make([]string, 0, len(document.Aliases))
	for _, a := range document.Aliases {
		aliases = append(aliases, normalizeIdentity(a))
	}
	term := normalizeIdentity(query)
	if term == key {
		return 0
	}
	if term == name {
		return 1
	}
	for _, a := range aliases {
		if term == a {
			return 2
		}
	}
	identities := append([]string{key, name}, aliases...)
	if len(term) >= 2 {
		for _, v := range identities {
			if strings.Contains(v, term) {
				return 3
			}
		}
	}
	queryWords := map[string]bool{}
	for _, w := range strings.Split(term, " ") {
		queryWords[w] = true
	}
	// 单字母标识符（词法切分不保留）软匹配（对齐 :387-390）
	for _, identity := range identities {
		if len(identity) == 1 && queryWords[identity] {
			return 3
		}
	}
	return 4
}

// normalizeIdentity 身份归一化（分隔符切分 + 空段过滤）。
// 对齐 Python _normalize_identity（lexical.py:394-395）。
func normalizeIdentity(value string) string {
	parts := splitRe.Split(strings.ToLower(value), -1)
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

// identityValueMatchesQuery 身份值与查询互含判定。
// 对齐 Python _identity_value_matches_query（lexical.py:398-406）。
func identityValueMatchesQuery(value, key, query string) bool {
	identity := normalizeIdentity(value)
	canonical := normalizeIdentity(key)
	requested := normalizeIdentity(query)
	if identity == "" || identity == canonical || requested == "" {
		return false
	}
	if identity == requested || (len(identity) >= 2 && strings.Contains(requested, identity)) {
		return true
	}
	return len(requested) >= 2 && strings.Contains(identity, requested)
}

// identityContainsQuery 身份字段含查询判定。
// 对齐 Python _identity_contains_query（lexical.py:409-424）。
func identityContainsQuery(document LexicalDocument, query string, queryTokens []string) bool {
	identities := []string{normalizeIdentity(document.Key), normalizeIdentity(document.Name)}
	for _, a := range document.Aliases {
		identities = append(identities, normalizeIdentity(a))
	}
	normalized := normalizeIdentity(query)
	for _, v := range identities {
		if normalized == v {
			return true
		}
	}
	if len(normalized) >= 2 {
		for _, v := range identities {
			if strings.Contains(v, normalized) {
				return true
			}
		}
	}
	queryWords := map[string]bool{}
	for _, w := range strings.Split(normalized, " ") {
		queryWords[w] = true
	}
	for _, identity := range identities {
		if len(identity) == 1 && queryWords[identity] {
			return true
		}
	}
	identityTokens := map[string]bool{}
	for _, v := range append([]string{document.Key, document.Name}, document.Aliases...) {
		for _, t := range tokensOf(v) {
			identityTokens[t] = true
		}
	}
	for _, term := range queryTokens {
		if len(term) >= 4 {
			for token := range identityTokens {
				if strings.Contains(token, term) {
					return true
				}
			}
		}
	}
	return false
}

// partialIdentityScore ≥4 字符查询词在身份 token 内的子串得分。
// 对齐 Python _partial_identity_score（lexical.py:427-434）。
func partialIdentityScore(document LexicalDocument, queryTokens []string) float64 {
	identityTokens := map[string]bool{}
	for _, v := range append([]string{document.Key, document.Name}, document.Aliases...) {
		for _, t := range tokensOf(v) {
			identityTokens[t] = true
		}
	}
	partial := map[string]bool{}
	for _, term := range queryTokens {
		if len(term) < 4 {
			continue
		}
		for token := range identityTokens {
			if term != token && strings.Contains(token, term) {
				partial[term] = true
			}
		}
	}
	unique := len(dedupe(queryTokens))
	if unique == 0 {
		unique = 1
	}
	return 6.0 * float64(len(partial)) / float64(unique)
}

// informativeTerms 低频信息词选取。
// 对齐 Python _informative_terms（lexical.py:437-444）。
func informativeTerms(terms []string, stats bm25Stats) []string {
	if len(terms) == 0 {
		return nil
	}
	ordered := dedupe(terms)
	threshold := int(float64(stats.size) * 0.6)
	if threshold < 1 {
		threshold = 1
	}
	var informative []string
	for _, t := range ordered {
		if stats.postings[t] <= threshold {
			informative = append(informative, t)
		}
	}
	if len(informative) == 0 {
		// 取 postings 最小者（tie 取先出现）
		best := ordered[0]
		bestCount := stats.postings[best]
		if bestCount == 0 {
			bestCount = stats.size + 1
		}
		for _, t := range ordered[1:] {
			c := stats.postings[t]
			if c == 0 {
				c = stats.size + 1
			}
			if c < bestCount {
				best, bestCount = t, c
			}
		}
		informative = []string{best}
	}
	sort.SliceStable(informative, func(i, j int) bool {
		ci, cj := stats.postings[informative[i]], stats.postings[informative[j]]
		if ci != cj {
			return ci < cj
		}
		return indexOf(ordered, informative[i]) < indexOf(ordered, informative[j])
	})
	return informative
}

// withoutFrontMatter 剥离 frontmatter。
// 对齐 Python _without_front_matter（lexical.py:447-455）。
func withoutFrontMatter(body string) string {
	lines := strings.SplitAfter(body, "\n") // keepends 语义
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return body
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.Join(lines[i+1:], "")
		}
	}
	return body
}

// excerpt 上下文摘录（… 包边、空白折叠、限长）。
// 对齐 Python _excerpt（lexical.py:458-467）。
func excerpt(body string, start, end, maxChars int) string {
	half := (maxChars - (end - start)) / 2
	if half < 1 {
		half = 1
	}
	left := start - half
	if left < 0 {
		left = 0
	}
	right := end + half
	if right > len(body) {
		right = len(body)
	}
	text := strings.Join(strings.Fields(body[left:right]), " ")
	if left > 0 {
		text = "…" + text
	}
	if right < len(body) {
		text = text + "…"
	}
	// Python text[:max_chars] 按字符截断（"…" 占 1 字符），Go 需按 rune
	runes := []rune(text)
	if len(runes) > maxChars {
		text = string(runes[:maxChars])
	}
	return text
}

// matchedExcerpt 覆盖最多的命中摘录。
// 对齐 Python _matched_excerpt（lexical.py:470-480）。
func matchedExcerpt(value string, terms map[string]bool, maxChars int) string {
	best := ""
	bestCoverage := -1
	for _, loc := range wordRe.FindAllStringIndex(value, -1) {
		occurrence := value[loc[0]:loc[1]]
		overlap := false
		for _, t := range tokensOf(occurrence) {
			if terms[t] {
				overlap = true
				break
			}
		}
		if !overlap {
			continue
		}
		candidate := excerpt(value, loc[0], loc[1], maxChars)
		coverage := 0
		for _, t := range tokensOf(candidate) {
			if terms[t] {
				coverage++
			}
		}
		if coverage > bestCoverage {
			best, bestCoverage = candidate, coverage
		}
		if coverage == len(terms) {
			break
		}
	}
	if best == "" {
		end := len(value)
		if end > maxChars {
			end = maxChars
		}
		best = excerpt(value, 0, end, maxChars)
	}
	return best
}

// documentFieldsOf 字段序构造。
// 对齐 Python _document_fields（lexical.py:483-491）。
func documentFieldsOf(document LexicalDocument) documentFields {
	return documentFields{
		key:         document.Key,
		name:        document.Name,
		aliases:     strings.Join(document.Aliases, "\n"),
		description: document.Description,
		body:        withoutFrontMatter(document.Body),
		category:    document.Category,
	}
}

// rankingQuery 剥离正则元字符（对齐 lexical.py:494-495）。
func rankingQuery(query string) string {
	return subRe.ReplaceAllString(query, " ")
}

// tokensOf 词形展开（表面词 + 单复数 + 动词形）。
// 对齐 Python _tokens（lexical.py:498-508）。
func tokensOf(values ...string) []string {
	var tokens []string
	for _, value := range values {
		for _, part := range surfaceTokens(value) {
			tokens = append(tokens, part)
			if !isASCII(part) {
				continue // Python :502-503 非 ASCII 跳过形态展开
			}
			singular := singularOf(part)
			if singular != part {
				tokens = append(tokens, singular)
			}
			for _, form := range verbForms(part) {
				if form != part && form != singular {
					tokens = append(tokens, form)
				}
			}
		}
	}
	return tokens
}

// surfaceTerms 去重保序表面词（对齐 lexical.py:511-512）。
func surfaceTerms(value string) []string {
	return dedupe(surfaceTokens(value))
}

// surfaceTokens 一次切分（BM25 需重复计数）。
// 对齐 Python _surface_tokens（lexical.py:515-531）。
func surfaceTokens(value string) []string {
	var terms []string
	for _, raw := range wordRe.FindAllString(value, -1) {
		folded := strings.Trim(strings.ToLower(raw), "._-/")
		if folded == "" {
			continue
		}
		if cjkRe.MatchString(folded) {
			terms = append(terms, folded)
			// 重叠二元组（对齐 :525）
			for i := 0; i+1 < len([]rune(folded))+0 || i < len([]rune(folded))-1; i++ {
				runes := []rune(folded)
				if i >= len(runes)-1 {
					break
				}
				terms = append(terms, string(runes[i:i+2]))
			}
			continue
		}
		if techSuffixRe.MatchString(folded) {
			terms = append(terms, folded)
			continue
		}
		for _, part := range sepRe.Split(folded, -1) {
			if len(part) > 1 || allDigits(part) {
				terms = append(terms, part)
			}
		}
	}
	return terms
}

// termFamily 一个查询词族（raw + 展开词集）
type termFamily struct {
	raw   string
	terms []string
}

// queryTermFamilies 查询词族（CJK 整段一族、ASCII 逐表面词一族）。
// 对齐 Python _query_term_families（lexical.py:534-543）。
func queryTermFamilies(value string) []termFamily {
	var families []termFamily
	seen := map[string]bool{}
	add := func(f termFamily) {
		if !seen[f.raw] {
			seen[f.raw] = true
			families = append(families, f)
		}
	}
	for _, raw := range wordRe.FindAllString(value, -1) {
		terms := surfaceTerms(raw)
		if !isASCII(raw) {
			add(termFamily{raw, terms})
			continue
		}
		for _, term := range terms {
			expanded := dedupe(tokensOf(term))
			add(termFamily{term, expanded})
		}
	}
	return families
}

// singularOf 英文单数化。
// 对齐 Python _singular（lexical.py:546-555）。
func singularOf(token string) string {
	if len(token) <= 3 ||
		strings.HasSuffix(token, "ss") || strings.HasSuffix(token, "us") ||
		strings.HasSuffix(token, "is") || strings.HasSuffix(token, "series") ||
		strings.HasSuffix(token, "species") {
		return token
	}
	if strings.HasSuffix(token, "ies") {
		return token[:len(token)-3] + "y"
	}
	for _, suffix := range []string{"ches", "shes", "xes", "zes", "sses"} {
		if strings.HasSuffix(token, suffix) {
			return token[:len(token)-2]
		}
	}
	if strings.HasSuffix(token, "s") {
		return token[:len(token)-1]
	}
	return token
}

// verbForms 英文动词形态展开。
// 对齐 Python _verb_forms（lexical.py:558-572）。
func verbForms(token string) []string {
	if len(token) <= 5 {
		return nil
	}
	var stems []string
	switch {
	case strings.HasSuffix(token, "ing"):
		stems = []string{token[:len(token)-3]}
	case strings.HasSuffix(token, "ied"):
		return []string{token[:len(token)-3] + "y"}
	case strings.HasSuffix(token, "ed"):
		stems = []string{token[:len(token)-2]}
	default:
		return nil
	}
	stem := stems[0]
	if len(stem) > 2 && stem[len(stem)-1] == stem[len(stem)-2] && !strings.HasSuffix(stem, "ss") {
		stems = append(stems, stem[:len(stem)-1])
	}
	out := dedupe(stems)
	for _, s := range dedupe(stems) {
		out = append(out, s+"e")
	}
	return dedupe(out)
}

// bm25Score BM25 主分（最强信息项加分）。
// 对齐 Python _bm25_score（lexical.py:575-589）。
func bm25Score(queryTokens []string, frequencies map[string]int, length int, stats bm25Stats, documentCount *int) float64 {
	score := 0.0
	strongest := 0.0
	for _, token := range dedupe(queryTokens) {
		if frequencies[token] == 0 {
			continue
		}
		contribution, information := bm25Contribution(token, frequencies, length, stats, documentCount)
		score += contribution
		if information > strongest {
			strongest = information
		}
	}
	return score + strongest
}

// bm25FamilyScore 词族 BM25 分（raw 直配优先、CJK 覆盖率折算）。
// 对齐 Python _bm25_family_score（lexical.py:592-622）。
func bm25FamilyScore(families []termFamily, frequencies map[string]int, length int, stats bm25Stats) float64 {
	score := 0.0
	strongest := 0.0
	for _, family := range families {
		var candidates []string
		if frequencies[family.raw] > 0 {
			candidates = []string{family.raw}
		} else {
			candidates = family.terms
		}
		type contrib struct{ contribution, information float64 }
		var contributions []contrib
		for _, token := range candidates {
			if frequencies[token] == 0 {
				continue
			}
			population := &stats.size
			if !isASCII(token) {
				population = &stats.cjkSize
			}
			c, i := bm25Contribution(token, frequencies, length, stats, population)
			contributions = append(contributions, contrib{c, i})
		}
		if len(contributions) == 0 {
			continue
		}
		best := contributions[0]
		for _, c := range contributions[1:] {
			if c.contribution > best.contribution {
				best = c
			}
		}
		if !isASCII(family.raw) && frequencies[family.raw] == 0 {
			covered := 0
			for _, t := range family.terms[min(1, len(family.terms)):] {
				if frequencies[t] > 0 {
					covered++
				}
			}
			denom := len(family.terms) - 1
			if denom < 1 {
				denom = 1
			}
			coverage := float64(covered) / float64(denom)
			best.contribution *= coverage
			best.information *= coverage
		}
		score += best.contribution
		if best.information > strongest {
			strongest = best.information
		}
	}
	return score + strongest
}

// bm25Contribution 单词贡献与信息量。
// 对齐 Python _bm25_contribution（lexical.py:625-640）。
func bm25Contribution(token string, frequencies map[string]int, length int, stats bm25Stats, documentCount *int) (float64, float64) {
	frequency := frequencies[token]
	documentFrequency := stats.postings[token]
	population := stats.size
	if documentCount != nil {
		population = *documentCount
	}
	inverseFrequency := math.Log(1 + (float64(population-documentFrequency)+0.5)/(float64(documentFrequency)+0.5))
	denominator := float64(frequency) + 1.5*(0.28+0.72*float64(length)/math.Max(stats.averageLength, 1e-9))
	information := inverseFrequency * inverseFrequency
	return information * (float64(frequency) * 2.5) / denominator, information
}

// fieldScore 字段加权重叠分。
// 对齐 Python _field_score（lexical.py:643-661）。
func fieldScore(queryTokens []string, fieldTokens []map[string]bool) float64 {
	terms := map[string]bool{}
	for _, t := range queryTokens {
		terms[t] = true
	}
	if len(terms) == 0 {
		return 0.0
	}
	weights := []float64{5.0, 5.0, 5.0, 3.4, 0.35, 0.1}
	score := 0.0
	for i, weight := range weights {
		if i >= len(fieldTokens) {
			break
		}
		overlap := 0
		for t := range fieldTokens[i] {
			if terms[t] {
				overlap++
			}
		}
		if overlap > 0 {
			score += weight * float64(overlap) / float64(len(terms))
		}
	}
	identityTerms := map[string]bool{}
	for i := 0; i < 3 && i < len(fieldTokens); i++ {
		for t := range fieldTokens[i] {
			identityTerms[t] = true
		}
	}
	identityOverlap := 0
	for t := range identityTerms {
		if terms[t] {
			identityOverlap++
		}
	}
	if identityOverlap > 0 {
		denom := len(identityTerms)
		if denom < 1 {
			denom = 1
		}
		score += 16.0 * float64(identityOverlap) / float64(denom)
	}
	return score
}

// fieldFamilyScore 词族字段分。
// 对齐 Python _field_family_score（lexical.py:664-680）。
func fieldFamilyScore(families []termFamily, document LexicalDocument, fieldTokens []map[string]bool) float64 {
	if len(families) == 0 {
		return 0.0
	}
	weights := []float64{5.0, 5.0, 5.0, 3.4, 0.35, 0.1}
	score := 0.0
	for i, weight := range weights {
		if i >= len(fieldTokens) {
			break
		}
		matched := 0
		for _, family := range families {
			for _, term := range family.terms {
				if fieldTokens[i][term] {
					matched++
					break
				}
			}
		}
		score += weight * float64(matched) / float64(len(families))
	}
	identityTerms := map[string]bool{}
	for i := 0; i < 3 && i < len(fieldTokens); i++ {
		for t := range fieldTokens[i] {
			identityTerms[t] = true
		}
	}
	identityMatches := 0
	for _, family := range families {
		for _, term := range family.terms {
			if identityTerms[term] {
				identityMatches++
				break
			}
		}
	}
	identityParts := append([]string{document.Key, document.Name}, document.Aliases...)
	identitySurface := surfaceTerms(strings.Join(identityParts, "\n"))
	denom := len(identitySurface)
	if len(families) > denom {
		denom = len(families)
	}
	if denom < 1 {
		denom = 1
	}
	score += 16.0 * float64(identityMatches) / float64(denom)
	return score
}

// phraseScore 短语直配分（字段分级 12/5/0.5/0.1）。
// 对齐 Python _phrase_score（lexical.py:683-701）。
func phraseScore(document LexicalDocument, query string, caseInsensitive bool) float64 {
	var alternatives []string
	for _, part := range strings.Split(query, "|") {
		p := strings.TrimSpace(part)
		if p != "" {
			alternatives = append(alternatives, p)
		}
	}
	if len(alternatives) == 0 {
		return 0.0
	}
	fields := documentFieldsOf(document)
	fieldStrings := []string{fields.key, fields.name, fields.aliases, fields.description, fields.body, fields.category}
	if caseInsensitive {
		for i := range alternatives {
			alternatives[i] = strings.ToLower(alternatives[i])
		}
		for i := range fieldStrings {
			fieldStrings[i] = strings.ToLower(fieldStrings[i])
		}
	}
	score := 0.0
	for _, alternative := range alternatives {
		switch {
		case strings.Contains(fieldStrings[0], alternative) ||
			strings.Contains(fieldStrings[1], alternative) ||
			strings.Contains(fieldStrings[2], alternative):
			score += 12.0
		case strings.Contains(fieldStrings[3], alternative):
			score += 5.0
		case strings.Contains(fieldStrings[4], alternative):
			score += 0.5
		case strings.Contains(fieldStrings[5], alternative):
			score += 0.1
		}
	}
	return score
}

// sortHitsBy 身份层级升序 → 分数降序 → 键 casefold 升序 → 键升序。
// 对齐 Python 排序键（lexical.py:118-133/178-188）。
func sortHitsBy(hits []LexicalHit, tier func(LexicalHit) int) {
	sort.SliceStable(hits, func(i, j int) bool {
		ti, tj := tier(hits[i]), tier(hits[j])
		if ti != tj {
			return ti < tj
		}
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		li, lj := strings.ToLower(hits[i].Key), strings.ToLower(hits[j].Key)
		if li != lj {
			return li < lj
		}
		return hits[i].Key < hits[j].Key
	})
}

// isASCII 全 ASCII 判定（对齐 Python str.isascii）
func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return len(s) > 0 || true
}

// dedupe 去重保序
func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// indexOf 首次出现下标
func indexOf(list []string, target string) int {
	for i, s := range list {
		if s == target {
			return i
		}
	}
	return -1
}

// allDigits 全数字判定
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// atoiSafe 安全 atoi（失败 0）
func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
