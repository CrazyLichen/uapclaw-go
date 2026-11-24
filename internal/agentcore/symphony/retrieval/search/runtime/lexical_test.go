package runtime

import (
	"strings"
	"testing"
)

// TestSurfaceTokens 切分与 CJK 二元组
func TestSurfaceTokens(t *testing.T) {
	// ASCII 词 + 分隔符拆分 + 单字符非数字剔除
	got := surfaceTokens("data_processing.v2")
	if got[0] != "data" || got[1] != "processing" || got[2] != "v2" {
		t.Fatalf("切分不符: %v", got)
	}
	// 重复保留（BM25 频次需要）
	got = surfaceTokens("go go go")
	if len(got) != 3 {
		t.Fatalf("重复应保留: %v", got)
	}
	// CJK 整段 + 二元组
	got = surfaceTokens("数据库")
	if got[0] != "数据库" || got[1] != "数据" || got[2] != "据库" {
		t.Fatalf("CJK 切分不符: %v", got)
	}
	// c++ / c# 保留
	got = surfaceTokens("C++ c#")
	if got[0] != "c++" || got[1] != "c#" {
		t.Fatalf("技术后缀不符: %v", got)
	}
	// 单字符非数字剔除（"a" 剔除、"2" 保留）
	got = surfaceTokens("a 2")
	if len(got) != 1 || got[0] != "2" {
		t.Fatalf("单字符语义不符: %v", got)
	}
}

// TestTokensOf 单复数与动词形态展开
func TestTokensOf(t *testing.T) {
	// 复数 → 单数
	got := tokensOf("categories")
	joined := strings.Join(got, ",")
	if !strings.Contains(joined, "category") {
		t.Fatalf("ies→y 不符: %v", got)
	}
	// boxes → box（xes→-2）
	got = tokensOf("boxes")
	if !contains(got, "box") {
		t.Fatalf("xes→-2 不符: %v", got)
	}
	// running → run + runn + runne? Python: stems=[run], no double, +e → run/rune
	got = tokensOf("running")
	joined = strings.Join(got, ",")
	if !strings.Contains(joined, "run") {
		t.Fatalf("ing→stem 不符: %v", got)
	}
	// 非 ASCII 不做形态展开
	got = tokensOf("数据库")
	if len(got) != 3 { // 整段+两二元组，无展开
		t.Fatalf("CJK 不应展开: %v", got)
	}
}

func contains(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}

// TestSingularOf 单数化分支
func TestSingularOf(t *testing.T) {
	cases := map[string]string{
		"categories": "category",
		"boxes":      "box",  // xes→-2
		"buses":      "bu",   // ses? zes→-2: "buses" ends "ses"? no zes; ends "ses" not in list... ends "es"; check: ches/shes/xes/zes/sses → "buses" ends "ses"→不在名单 → ends "s" → "buse"
		"classes":    "clas", // sses→-2
		"series":     "series",
		"species":    "species",
		"users":      "user",
		"ss":         "ss",
	}
	for in, _ := range cases {
		_ = in
	}
	if singularOf("categories") != "category" {
		t.Fatalf("ies 不符: %s", singularOf("categories"))
	}
	if singularOf("boxes") != "box" {
		t.Fatalf("xes 不符: %s", singularOf("boxes"))
	}
	if singularOf("classes") != "class" { // Python sses → token[:-2]
		t.Fatalf("sses 不符: %s", singularOf("classes"))
	}
	if singularOf("series") != "series" {
		t.Fatalf("series 不符: %s", singularOf("series"))
	}
	if singularOf("users") != "user" {
		t.Fatalf("s 不符: %s", singularOf("users"))
	}
	if singularOf("cat") != "cat" {
		t.Fatalf("短词不变: %s", singularOf("cat"))
	}
}

// TestVerbForms 动词形态
func TestVerbForms(t *testing.T) {
	// Python stems=[runn]+double(run) → (runn, run, runne, rune)
	vf := verbForms("running")
	if len(vf) != 4 || vf[0] != "runn" || vf[1] != "run" || vf[2] != "runne" || vf[3] != "rune" {
		t.Fatalf("ing 形态不符: %v", vf)
	}
	if verbForms("applied")[0] != "apply" { // 占位断言见下方 ied 分支
		_ = verbForms("stopped")
	}
	// stopped：stem="stopp"，双写→stems=[stopp,stop]，+e→stoppe/stop
	vf2 := verbForms("stopped")
	if vf2[0] != "stopp" || !contains(vf2, "stop") {
		t.Fatalf("ed 双写处理: %v", vf2)
	}
	// applied → apply（ied→y）
	if verbForms("applied")[0] != "apply" {
		t.Fatalf("ied 不符: %v", verbForms("applied"))
	}
	// 短词无形态
	if len(verbForms("run")) != 0 {
		t.Fatal("短词应无形态")
	}
}

// TestCompileSafePattern 边界与危险模式
func TestCompileSafePattern(t *testing.T) {
	// 空表达式
	if _, err := CompileSafePattern("", false); err == nil {
		t.Fatal("空应报错")
	}
	// 超长
	if _, err := CompileSafePattern(strings.Repeat("a", 513), false); err == nil {
		t.Fatal("超长应报错")
	}
	// 反向引用
	if _, err := CompileSafePattern(`(a)\1`, false); err == nil {
		t.Fatal("反向引用应报错")
	}
	// 扩展语法
	if _, err := CompileSafePattern(`(?i)abc`, false); err == nil {
		t.Fatal("扩展语法应报错")
	}
	// 重复次数上限
	if _, err := CompileSafePattern(`a{20000}`, false); err == nil {
		t.Fatal("过大上界应报错")
	}
	if _, err := CompileSafePattern(`a{100}`, false); err != nil {
		t.Fatalf("合法上界应通过: %v", err)
	}
	// 多次重复
	if _, err := CompileSafePattern(`a*b*c`, false); err == nil {
		t.Fatal("多次重复应报错")
	}
	if _, err := CompileSafePattern(`a*b`, false); err != nil {
		t.Fatalf("单次重复应通过: %v", err)
	}
	// 重复作用于空/组开头/分支后
	if _, err := CompileSafePattern(`(*a)`, false); err == nil {
		t.Fatal("组内空重复应报错")
	}
	if _, err := CompileSafePattern(`a|*b`, false); err == nil {
		t.Fatal("分支后重复应报错")
	}
	// 多个 .* 通配
	if _, err := CompileSafePattern(`.*a.*`, false); err == nil {
		t.Fatal("多 .* 应报错")
	}
	if _, err := CompileSafePattern(`.*a`, false); err != nil {
		t.Fatalf("单 .* 应通过: %v", err)
	}
}

// TestCompileMatcher 字面量与正则、大小写
func TestCompileMatcher(t *testing.T) {
	m, err := CompileMatcher("HELLO", true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !m("say hello world") {
		t.Fatal("大小写不敏感字面量应命中")
	}
	m, _ = CompileMatcher("HELLO", false, true)
	if m("hello") {
		t.Fatal("大小写敏感不应命中")
	}
	m, _ = CompileMatcher("h.llo", false, false)
	if !m("hello") {
		t.Fatal("正则应命中")
	}
	if m("hllo") {
		t.Fatal("正则不应误命中")
	}
	// 非法正则
	if _, err := CompileMatcher("a**", false, false); err == nil {
		t.Fatal("危险重复应报错")
	}
}

// TestLexicalIndexSearch 基础检索与排序
func TestLexicalIndexSearch(t *testing.T) {
	docs := []LexicalDocument{
		{Key: "skill-a", Name: "Skill A", Description: "processes data files"},
		{Key: "skill-b", Name: "Data Tool", Description: "data processing toolkit"},
		{Key: "unrelated", Name: "Other", Description: "does something else"},
	}
	idx := NewLexicalIndex(docs)
	// 空查询报错
	if _, err := idx.Search("", nil, true, false); err == nil {
		t.Fatal("空查询应报错")
	}
	hits, err := idx.Search("data", nil, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("应命中")
	}
	// unrelated 不命中
	for _, h := range hits {
		if h.Key == "unrelated" {
			t.Fatal("unrelated 不应命中")
		}
	}
	// 键等值身份层级优先：skill-a 键含 data？键 skill-a 不等 data；Data Tool name 含 data
	// 身份层级：Data Tool 无等值；直接断言前两命中包含两文档即可（层级排序稳定性）
	if len(hits) != 2 {
		t.Fatalf("命中数不符: %d", len(hits))
	}
	// scope 过滤
	hits2, _ := idx.Search("data", []string{"skill-b"}, true, false)
	if len(hits2) != 1 || hits2[0].Key != "skill-b" {
		t.Fatalf("scope 过滤不符: %v", hits2)
	}
	// 无命中
	hits3, _ := idx.Search("zzzz", nil, true, false)
	if len(hits3) != 0 {
		t.Fatalf("无命中应空: %v", hits3)
	}
}

// TestLexicalIndexSearch_身份层级 键/名/别名等值优先
func TestLexicalIndexSearch_身份层级(t *testing.T) {
	docs := []LexicalDocument{
		{Key: "other-key", Name: "deploy helper", Description: "deploy stuff with deploy"},
		{Key: "deploy", Name: "其他名字", Description: "deploy"},
	}
	idx := NewLexicalIndex(docs)
	hits, _ := idx.Search("deploy", nil, true, false)
	if len(hits) < 2 {
		t.Fatalf("命中数: %d", len(hits))
	}
	// 键等值（tier 0）应排最前
	if hits[0].Key != "deploy" {
		t.Fatalf("键等值应置顶: %v", hits)
	}
}

// TestLexicalIndexSearchTerms 词族检索
func TestLexicalIndexSearchTerms(t *testing.T) {
	docs := []LexicalDocument{
		{Key: "python-tool", Name: "Python Tool", Description: "python scripting utilities"},
		{Key: "java-tool", Name: "Java Tool", Description: "java utilities"},
	}
	idx := NewLexicalIndex(docs)
	if _, err := idx.SearchTerms("", nil); err == nil {
		t.Fatal("空查询应报错")
	}
	hits, err := idx.SearchTerms("python scripting", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Key != "python-tool" {
		t.Fatalf("词族命中不符: %v", hits)
	}
	// 单查询词命中
	hits, _ = idx.SearchTerms("python", nil)
	if len(hits) != 1 {
		t.Fatalf("单词命中不符: %v", hits)
	}
}

// TestMissingTerms 缺词
func TestMissingTerms(t *testing.T) {
	docs := []LexicalDocument{{Key: "a", Name: "alpha beta", Description: "has alpha"}}
	idx := NewLexicalIndex(docs)
	missing := idx.MissingTerms("alpha missingword")
	if len(missing) != 1 || missing[0] != "missingword" {
		t.Fatalf("缺词不符: %v", missing)
	}
	if len(idx.MissingTerms("alpha beta")) != 0 {
		t.Fatal("全命中应无缺词")
	}
}

// TestMatchSnippet 摘录路径
func TestMatchSnippet(t *testing.T) {
	docs := []LexicalDocument{
		{Key: "k", Name: "My Tool", Description: "visible desc", Body: "---\nname: x\n---\nbody with hidden zebra content"},
	}
	idx := NewLexicalIndex(docs)
	if idx.MatchSnippet("nope", []string{"x"}, 180, 96) != "" {
		t.Fatal("未知键应空")
	}
	if idx.MatchSnippet("k", []string{"x"}, 180, 0) != "" {
		t.Fatal("maxChars<1 应空")
	}
	// name 直配
	s := idx.MatchSnippet("k", []string{"My Tool"}, 180, 96)
	if !strings.HasPrefix(s, "name: ") {
		t.Fatalf("name 直配不符: %q", s)
	}
	// body 隐藏词（zebra 不在 name/desc）
	s = idx.MatchSnippet("k", []string{"zebra"}, 180, 96)
	if !strings.HasPrefix(s, "body: ") {
		t.Fatalf("body 摘录不符: %q", s)
	}
	// 无匹配词
	if idx.MatchSnippet("k", []string{"qqqqqq"}, 180, 96) != "" {
		t.Fatal("无匹配应空")
	}
}

// TestBM25Score 评分确定性
func TestBM25Score(t *testing.T) {
	stats := bm25Stats{averageLength: 5, postings: map[string]int{"data": 2}, size: 2, cjkSize: 0}
	freq := map[string]int{"data": 3}
	score := bm25Score([]string{"data"}, freq, 6, stats, nil)
	if score <= 0 {
		t.Fatalf("评分应为正: %v", score)
	}
	// 不在语料的词零贡献
	zero := bm25Score([]string{"absent"}, freq, 6, stats, nil)
	if zero != 0 {
		t.Fatalf("缺失词应零分: %v", zero)
	}
}

// TestCJKSearch 中文检索
func TestCJKSearch(t *testing.T) {
	docs := []LexicalDocument{
		{Key: "db", Name: "数据库工具", Description: "连接数据库"},
		{Key: "web", Name: "Web 工具", Description: "网页开发"},
	}
	idx := NewLexicalIndex(docs)
	hits, err := idx.Search("数据", nil, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Key != "db" {
		t.Fatalf("中文检索不符: %v", hits)
	}
	// 词族路径
	hits, _ = idx.SearchTerms("数据库", nil)
	if len(hits) == 0 || hits[0].Key != "db" {
		t.Fatalf("中文词族不符: %v", hits)
	}
}

// TestIdentityTier 层级判定
func TestIdentityTier(t *testing.T) {
	doc := LexicalDocument{Key: "my-key", Name: "My Key", Aliases: []string{"mk"}}
	// 键等值 tier0
	if identityTier(doc, "my-key", true, false) != 0 {
		t.Fatal("键等值应 tier0")
	}
	// 名等值 tier1
	if identityTier(doc, "My Key", true, false) != 1 {
		t.Fatal("名等值应 tier1")
	}
	// 别名 tier2
	if identityTier(doc, "mk", true, false) != 2 {
		t.Fatal("别名应 tier2")
	}
	// 前缀 tier3
	if identityTier(doc, "my-k", true, false) != 3 {
		t.Fatal("前缀应 tier3")
	}
	// 无 tier4
	if identityTier(doc, "zzz", true, false) != 4 {
		t.Fatal("无匹配应 tier4")
	}
}

// TestNormalizeIdentity 归一化
func TestNormalizeIdentity(t *testing.T) {
	if normalizeIdentity("My_Cool.Tool-x  Y") != "my cool tool x y" {
		t.Fatalf("归一化不符: %q", normalizeIdentity("My_Cool.Tool-x  Y"))
	}
}

// TestTermIdentityTier 词族层级
func TestTermIdentityTier(t *testing.T) {
	doc := LexicalDocument{Key: "data-tool", Name: "Data Tool"}
	if termIdentityTier(doc, "data tool") != 0 {
		t.Fatalf("归一等值应 tier0: %d", termIdentityTier(doc, "data tool"))
	}
	if termIdentityTier(doc, "DATA_TOOL") != 0 {
		t.Fatalf("分隔符无关等值应 tier0: %d", termIdentityTier(doc, "DATA_TOOL"))
	}
	if termIdentityTier(doc, "data") != 3 {
		t.Fatalf("子串应 tier3: %d", termIdentityTier(doc, "data"))
	}
	if termIdentityTier(doc, "zzz") != 4 {
		t.Fatal("无匹配应 tier4")
	}
}

// TestExcerpt 摘录边界
func TestExcerpt(t *testing.T) {
	body := strings.Repeat("word ", 100)
	got := excerpt(body, 50, 54, 20)
	// Python [:max_chars] 按字符计，Go 测试用 rune 计
	if gotRunes := len([]rune(got)); gotRunes > 20 {
		t.Fatalf("限长不符: %d", gotRunes)
	}
	// Python 同款：[:max_chars] 截断会丢掉尾部省略号，只保留前缀
	if !strings.HasPrefix(got, "…") {
		t.Fatalf("前缀不符: %q", got)
	}
	// 中段无包边
	got = excerpt("hello world", 0, 11, 96)
	if strings.Contains(got, "…") {
		t.Fatalf("全量摘录不应包边: %q", got)
	}
}

// TestWithoutFrontMatter frontmatter 剥离
func TestWithoutFrontMatter(t *testing.T) {
	if got := withoutFrontMatter("---\na: 1\n---\nbody text"); got != "body text" {
		t.Fatalf("剥离不符: %q", got)
	}
	if got := withoutFrontMatter("no frontmatter"); got != "no frontmatter" {
		t.Fatalf("无 frontmatter 应原样: %q", got)
	}
	if got := withoutFrontMatter("---\nunclosed"); got != "---\nunclosed" {
		t.Fatalf("未闭合应原样: %q", got)
	}
}

// TestInformativeTerms 信息词选取
func TestInformativeTerms(t *testing.T) {
	stats := bm25Stats{size: 10, postings: map[string]int{"common": 9, "rare": 1}}
	got := informativeTerms([]string{"common", "rare"}, stats)
	if len(got) != 1 || got[0] != "rare" {
		t.Fatalf("应只留低频词: %v", got)
	}
	// 全超阈值 → 取 postings 最小
	got = informativeTerms([]string{"common"}, stats)
	if len(got) != 1 || got[0] != "common" {
		t.Fatalf("全超阈值应保留最小: %v", got)
	}
}
