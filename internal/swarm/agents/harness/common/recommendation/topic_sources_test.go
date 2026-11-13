package recommendation

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// publishedAtForGround 供 grounding 用例的 now（对齐语料新鲜窗口）
func publishedAtForGround() float64 {
	publishedAt, _ := parsePublishedAt("2026-08-30T00:00:00Z")
	return publishedAt + 3600
}

func TestDefaultSourceClassifier_权威后缀与未知(t *testing.T) {
	cases := []struct {
		url  string
		kind string
		pub  string
	}{
		{"https://www.gov.cn/x.htm", "authoritative", "gov.cn"},
		{"https://edu.cn", "authoritative", "edu.cn"},
		{"https://www.who.int/a", "authoritative", "who.int"},
		{"https://un.org", "authoritative", "un.org"},
		{"https://example.com", "unknown", "example.com"},
		{"https://www.example.com", "unknown", "example.com"},
	}
	for _, c := range cases {
		got := DefaultSourceClassifier(c.url, "")
		if got.Kind != c.kind || got.Publisher != c.pub {
			t.Fatalf("%s → %v", c.url, got)
		}
	}
}

func TestDefaultSearchFetch_接线前显式失败(t *testing.T) {
	if _, _, err := DefaultSearch(context.Background(), "q", 5, 30); err == nil ||
		!strings.Contains(err.Error(), "run_free_search_structured") {
		t.Fatalf("err=%v", err)
	}
	if _, err := DefaultFetch(context.Background(), "https://a.com", 30); err == nil ||
		!strings.Contains(err.Error(), "10.3.31c") {
		t.Fatalf("err=%v", err)
	}
}

func TestParsePublishedAt_多格式与无效(t *testing.T) {
	if _, ok := parsePublishedAt("2026-08-30T00:00:00Z"); !ok {
		t.Fatal("RFC3339 应解析")
	}
	if _, ok := parsePublishedAt("2026-08-30T08:00:00+08:00"); !ok {
		t.Fatal("带时区应解析")
	}
	if _, ok := parsePublishedAt("not-a-date"); ok {
		t.Fatal("无效串应拒绝")
	}
	if _, ok := parsePublishedAt(42); ok {
		t.Fatal("非字符串应拒绝")
	}
	if _, ok := parsePublishedAt("  "); ok {
		t.Fatal("空白应拒绝")
	}
}

func TestDateIsFresh_分支(t *testing.T) {
	publishedAt, _ := parsePublishedAt("2026-08-30T00:00:00Z")
	now := publishedAt + 86400 // 发布 1 天后
	page := map[string]any{"published_at": "2026-08-30T00:00:00Z", "published_at_source": "meta"}
	// evergreen 恒真
	if !dateIsFresh(map[string]any{}, "evergreen_corpus", now) {
		t.Fatal("evergreen 应恒真")
	}
	if !dateIsFresh(page, "current_event", now) {
		t.Fatal("近期新闻应新鲜")
	}
	if dateIsFresh(map[string]any{}, "current_event", now) {
		t.Fatal("缺发布时间应拒绝")
	}
	if dateIsFresh(map[string]any{"published_at": "2026-08-30T00:00:00Z"}, "current_event", now) {
		t.Fatal("缺发布来源应拒绝")
	}
	if dateIsFresh(map[string]any{"published_at": "2030-01-01T00:00:00Z", "published_at_source": "meta"}, "current_event", now) {
		t.Fatal("未来时间应拒绝")
	}
}

func TestLexicallyAdmitted_锚点语义(t *testing.T) {
	if !lexicallyAdmitted(map[string]any{"identity_anchor": ""}, "any") {
		t.Fatal("无 identity 锚应放行")
	}
	if !lexicallyAdmitted(map[string]any{"identity_anchor": "cat", "context_anchors": []any{}}, "a cat here") {
		t.Fatal("identity 命中无上下文应放行")
	}
	if lexicallyAdmitted(map[string]any{"identity_anchor": "cat"}, "dog") {
		t.Fatal("identity 缺席应拒绝")
	}
	if !lexicallyAdmitted(map[string]any{"identity_anchor": "cat", "context_anchors": []any{"black", "white"}}, "a black cat") {
		t.Fatal("identity+任一上下文命中应放行")
	}
	if lexicallyAdmitted(map[string]any{"identity_anchor": "cat", "context_anchors": []any{"black", "white"}}, "a cat") {
		t.Fatal("上下文全缺席应拒绝")
	}
}

func TestAdmittedSourceSet_三类准入(t *testing.T) {
	authoritative := map[string]any{"source_kind": "authoritative", "publisher": "a.gov"}
	stable := map[string]any{"source_kind": "stable", "publisher": "b.com"}
	// evergreen：authoritative/stable 取首个
	got := admittedSourceSet([]map[string]any{stable, authoritative}, "evergreen_corpus")
	if len(got) != 1 || !mapValueEqual(got[0], stable) {
		t.Fatalf("got=%v", got)
	}
	// current：authoritative/original 优先
	original := map[string]any{"source_kind": "original", "publisher": "c.com"}
	got = admittedSourceSet([]map[string]any{stable, authoritative, original}, "current_event")
	if len(got) != 1 || !mapValueEqual(got[0], authoritative) {
		t.Fatalf("got=%v", got)
	}
	// reliable_report：不同发布方最多 2 条
	r1 := map[string]any{"source_kind": "reliable_report", "publisher": "p1", "content_boundary": map[string]any{"text": "x"}}
	r2 := map[string]any{"source_kind": "reliable_report", "publisher": "p2", "content_boundary": map[string]any{"text": "y"}}
	r3 := map[string]any{"source_kind": "reliable_report", "publisher": "p3", "content_boundary": map[string]any{"text": "z"}}
	r1dup := map[string]any{"source_kind": "reliable_report", "publisher": "p1", "content_boundary": map[string]any{"text": "x"}}
	got = admittedSourceSet([]map[string]any{r1, r1dup, r2, r3}, "recent_development")
	if len(got) != 2 {
		t.Fatalf("reliable 应取 2：%d", len(got))
	}
	// 无可靠来源 → 空
	if got := admittedSourceSet([]map[string]any{stable}, "current_event"); len(got) != 0 {
		t.Fatalf("应空：%v", got)
	}
}

// sourceFetchOK 返回成功抓取页
func sourceFetchOK(url, content string) FetchFn {
	return func(ctx context.Context, target string, timeout int) (map[string]any, error) {
		return map[string]any{"status_code": 200.0, "url": target, "content": content,
			"fetch_reader": "direct"}, nil
	}
}

// groundedCandidate 构造一个可摆的个性化候选
func groundedCandidate(identity, contextAnchor, primaryQuery string) map[string]any {
	return map[string]any{
		"candidate_id":           "cand-1",
		"freshness_class":        "evergreen_corpus",
		"primary_query":          primaryQuery,
		"fallback_query":         "",
		"identity_anchor":        identity,
		"context_anchors":        []any{contextAnchor},
		"intent":                 "i",
		"title":                  "t",
		"source_memory_unit_ids": []any{"i1"},
		"source_locators":        []any{[]any{"s1", 1}},
	}
}

func TestGroundCandidate_证据束组装(t *testing.T) {
	classifier := func(targetURL, content string) SourceAssessment {
		return SourceAssessment{Kind: "stable", Publisher: "example.com"}
	}
	search := func(ctx context.Context, q string, maxResults, timeout int) (string, []map[string]string, error) {
		return "engine-x", []map[string]string{
			{"title": "t1", "url": "https://example.com/a", "snippet": "s1"},
			{"title": "bad", "url": "ftp://x", "snippet": ""},
		}, nil
	}
	got := GroundCandidate(context.Background(),
		groundedCandidate("cat", "black", "query a"),
		search, sourceFetchOK("https://example.com/a", "a black cat here"), classifier, publishedAtForGround())
	if got == nil {
		t.Fatal("应有结果")
	}
	bundle := got["evidence_bundle"].(map[string]any)
	if bundle["fetch_status"] != "success" || bundle["source_kind"] != "stable" {
		t.Fatalf("bundle keys=%v", bundle)
	}
	if bundle["retrieval_config_version"] != RetrievalConfigVersion {
		t.Fatal("config version 应写入")
	}
	sources := bundle["sources"].([]any)
	if len(sources) != 1 {
		t.Fatalf("sources=%d", len(sources))
	}
}

func TestGroundCandidate_词法拒绝后落空(t *testing.T) {
	classifier := func(targetURL, content string) SourceAssessment {
		return SourceAssessment{Kind: "stable", Publisher: "example.com"}
	}
	search := func(ctx context.Context, q string, maxResults, timeout int) (string, []map[string]string, error) {
		return "e", []map[string]string{{"title": "t", "url": "https://example.com/a"}}, nil
	}
	got := GroundCandidate(context.Background(),
		groundedCandidate("cat", "black", "query a"),
		search, sourceFetchOK("https://example.com/a", "no anchors here"), classifier, publishedAtForGround())
	if got != nil {
		t.Fatal("词法未命中应落空")
	}
}

func TestGroundCandidate_未知分类落空(t *testing.T) {
	classifier := func(targetURL, content string) SourceAssessment {
		return SourceAssessment{Kind: "unknown", Publisher: "example.com"}
	}
	search := func(ctx context.Context, q string, maxResults, timeout int) (string, []map[string]string, error) {
		return "e", []map[string]string{{"title": "t", "url": "https://example.com/a"}}, nil
	}
	got := GroundCandidate(context.Background(),
		groundedCandidate("cat", "", "query a"),
		search, sourceFetchOK("https://example.com/a", "cat"), classifier, publishedAtForGround())
	if got != nil {
		t.Fatal("unknown 分类应落空")
	}
}

func TestGroundCandidate_非法保鲜等级与空主查询(t *testing.T) {
	if got := GroundCandidate(context.Background(),
		map[string]any{"freshness_class": "stale", "primary_query": "q"}, nil, nil,
		DefaultSourceClassifier, 1); got != nil {
		t.Fatal("非法保鲜应落空")
	}
	if got := GroundCandidate(context.Background(),
		map[string]any{"freshness_class": "current_event"}, nil, nil, DefaultSourceClassifier, 1); got != nil {
		t.Fatal("空主查询应落空")
	}
}

func TestGroundCandidate_回退查询仅当无选中时(t *testing.T) {
	classifier := func(targetURL, content string) SourceAssessment {
		return SourceAssessment{Kind: "stable", Publisher: "example.com"}
	}
	searched := []string{}
	search := func(ctx context.Context, q string, maxResults, timeout int) (string, []map[string]string, error) {
		searched = append(searched, q)
		return "e", []map[string]string{{"title": "t", "url": "https://example.com/a"}}, nil
	}
	candidate := groundedCandidate("cat", "", "query a")
	candidate["fallback_query"] = "query b"
	got := GroundCandidate(context.Background(), candidate, search,
		sourceFetchOK("https://example.com/a", "cat"), classifier, publishedAtForGround())
	if got == nil || len(searched) != 1 {
		t.Fatalf("主查询已选中则不跑回退：%v", searched)
	}
}

func TestGroundCandidate_抓取失败不计入(t *testing.T) {
	classifier := func(targetURL, content string) SourceAssessment {
		return SourceAssessment{Kind: "stable", Publisher: "example.com"}
	}
	search := func(ctx context.Context, q string, maxResults, timeout int) (string, []map[string]string, error) {
		return "e", []map[string]string{{"title": "t", "url": "https://example.com/a"}}, nil
	}
	fetch := func(ctx context.Context, target string, timeout int) (map[string]any, error) {
		return nil, errors.New("boom")
	}
	if got := GroundCandidate(context.Background(),
		groundedCandidate("cat", "", "query a"), search, fetch, classifier, publishedAtForGround()); got != nil {
		t.Fatal("抓取失败应落空")
	}
}

func TestGroundCandidate_非2xx与空内容落空(t *testing.T) {
	classifier := func(targetURL, content string) SourceAssessment {
		return SourceAssessment{Kind: "stable", Publisher: "example.com"}
	}
	search := func(ctx context.Context, q string, maxResults, timeout int) (string, []map[string]string, error) {
		return "e", []map[string]string{{"title": "t", "url": "https://example.com/a"}}, nil
	}
	fetch := func(ctx context.Context, target string, timeout int) (map[string]any, error) {
		return map[string]any{"status_code": 500.0, "url": target, "content": "x"}, nil
	}
	if got := GroundCandidate(context.Background(),
		groundedCandidate("cat", "", "query a"), search, fetch, classifier, publishedAtForGround()); got != nil {
		t.Fatal("非 2xx 应落空")
	}
}

func TestGroundCandidates_批量共享缓存与过滤nil(t *testing.T) {
	classifier := func(targetURL, content string) SourceAssessment {
		return SourceAssessment{Kind: "stable", Publisher: "example.com"}
	}
	search := func(ctx context.Context, q string, maxResults, timeout int) (string, []map[string]string, error) {
		return "e", []map[string]string{{"title": "t", "url": "https://example.com/a"}}, nil
	}
	c1 := groundedCandidate("cat", "", "query a")
	c2 := groundedCandidate("", "", "query b") // 无 identity 锚 → 词法放行
	fetchCalls := 0
	fetch := func(ctx context.Context, target string, timeout int) (map[string]any, error) {
		fetchCalls++
		return sourceFetchOK(target, "cat content")(ctx, target, timeout)
	}
	got := GroundCandidates(context.Background(), []map[string]any{c1, c2}, search, fetch, classifier, publishedAtForGround())
	if len(got) != 2 {
		t.Fatalf("两候选都应产出：%d", len(got))
	}
	_ = fetchCalls
}

func TestLoadFixedStarterCorpus_真实语料加载(t *testing.T) {
	starters, err := LoadFixedStarterCorpus(CorpusVersion)
	if err != nil {
		t.Fatalf("真实语料应可加载：%v", err)
	}
	if len(starters) < 100 || len(starters) > 150 {
		t.Fatalf("数量=%d", len(starters))
	}
	if starters[0].StarterID == "" || starters[0].Origin != "project_authored" {
		t.Fatalf("首行=%+v", starters[0])
	}
}

func TestLoadFixedStarterCorpus_版本不一致拒绝(t *testing.T) {
	if _, err := LoadFixedStarterCorpus("other-version"); err == nil ||
		!strings.Contains(err.Error(), "version mismatch") {
		t.Fatalf("err=%v", err)
	}
}

func TestParseStarter_逐字段校验(t *testing.T) {
	base := map[string]any{
		"starter_id": "s1", "text": "t", "topic": "生活", "hook_type": "surprise",
		"freshness_class": "evergreen_corpus", "created_at": "2026-01-01",
		"expires_at": nil, "corpus_version": CorpusVersion, "origin": "project_authored",
	}
	if _, err := parseStarter(base, 1); err != nil {
		t.Fatalf("合法行应通过：%v", err)
	}
	// 字段缺失
	bad := map[string]any{}
	for k, v := range base {
		if k != "text" {
			bad[k] = v
		}
	}
	if _, err := parseStarter(bad, 2); err == nil || !strings.Contains(err.Error(), "schema does not match") {
		t.Fatalf("err=%v", err)
	}
	// 非法 topic
	bad2 := cloneMap(base)
	bad2["topic"] = "其他"
	if _, err := parseStarter(bad2, 3); err == nil || !strings.Contains(err.Error(), "topic bucket") {
		t.Fatalf("err=%v", err)
	}
	// 非法 hook_type
	bad3 := cloneMap(base)
	bad3["hook_type"] = "other"
	if _, err := parseStarter(bad3, 4); err == nil || !strings.Contains(err.Error(), "hook_type") {
		t.Fatalf("err=%v", err)
	}
	// 非法 freshness
	bad4 := cloneMap(base)
	bad4["freshness_class"] = "stale"
	if _, err := parseStarter(bad4, 5); err == nil || !strings.Contains(err.Error(), "freshness_class") {
		t.Fatalf("err=%v", err)
	}
	// expires_at 非 str
	bad5 := cloneMap(base)
	bad5["expires_at"] = 3
	if _, err := parseStarter(bad5, 6); err == nil || !strings.Contains(err.Error(), "expires_at") {
		t.Fatalf("err=%v", err)
	}
	// 非法 origin
	bad6 := cloneMap(base)
	bad6["origin"] = "imported"
	if _, err := parseStarter(bad6, 7); err == nil || !strings.Contains(err.Error(), "project_authored") {
		t.Fatalf("err=%v", err)
	}
	// 空 text
	bad7 := cloneMap(base)
	bad7["text"] = " "
	if _, err := parseStarter(bad7, 8); err == nil || !strings.Contains(err.Error(), "non-empty string") {
		t.Fatalf("err=%v", err)
	}
}

func mapValueEqual(a, b map[string]any) bool {
	return a["source_kind"] == b["source_kind"] && a["publisher"] == b["publisher"]
}

func cloneMap(src map[string]any) map[string]any {
	dst := map[string]any{}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// corpusRankFixture 一个带锚点的语料排序输入
func corpusRankFixture(corpus []FixedStarter) []CorpusRankedInterest {
	anchor := corpus[0].Text[:minInt(2, len([]rune(corpus[0].Text)))]
	return []CorpusRankedInterest{{
		InterestSummaryID: "i1",
		InterestSummary:   "s",
		Rank:              1,
		AllowedAnchors:    []string{anchor},
		SourceLocators:    []SourceLocator{{SessionID: "s1", Ordinal: 1}},
	}}
}

// fakeRankingAdapter 可编程 I3 适配器
type fakeRankingAdapter struct {
	response any
	err      error
}

func (f *fakeRankingAdapter) RankFixedStarters(ctx context.Context, request FixedStarterRankingRequest) (any, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}

func TestRetrieveFixedStartersSources_暖启动命中物化(t *testing.T) {
	corpus, err := LoadFixedStarterCorpus(CorpusVersion)
	if err != nil {
		t.Fatal(err)
	}
	interests := corpusRankFixture(corpus)
	anchor := interests[0].AllowedAnchors[0]
	resp := map[string]any{"hits": []any{
		map[string]any{"starter_id": corpus[0].StarterID, "interest_summary_id": "i1"},
	}}
	hits, err := RetrieveFixedStartersSources(context.Background(), interests, CorpusVersion,
		&fakeRankingAdapter{response: resp}, RetrievalTopK)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(hits) != 1 || hits[0].StarterID != corpus[0].StarterID || hits[0].Rank != 1 {
		t.Fatalf("hits=%+v", hits)
	}
	if hits[0].MatchedAnchor != anchor || hits[0].Text != corpus[0].Text {
		t.Fatalf("hit=%+v", hits[0])
	}
	if len(hits[0].SourceLocators) != 1 {
		t.Fatal("定位子应继承")
	}
}

func TestRetrieveFixedStartersSources_冷启动空兴趣(t *testing.T) {
	corpus, err := LoadFixedStarterCorpus(CorpusVersion)
	if err != nil {
		t.Fatal(err)
	}
	resp := map[string]any{"hits": []any{
		map[string]any{"starter_id": corpus[0].StarterID, "interest_summary_id": nil},
	}}
	hits, err := RetrieveFixedStartersSources(context.Background(), nil, CorpusVersion,
		&fakeRankingAdapter{response: resp}, 4)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if hits[0].InterestSummaryID != nil || hits[0].MatchedAnchor != nil {
		t.Fatalf("hit=%+v", hits[0])
	}
}

func TestRetrieveFixedStartersSources_契约违约族(t *testing.T) {
	corpus, err := LoadFixedStarterCorpus(CorpusVersion)
	if err != nil {
		t.Fatal(err)
	}
	interests := corpusRankFixture(corpus)
	// top_k 越界
	if _, err := RetrieveFixedStartersSources(context.Background(), interests, CorpusVersion,
		&fakeRankingAdapter{}, 0); err == nil || !strings.Contains(err.Error(), "top_k") {
		t.Fatalf("err=%v", err)
	}
	// 兴趣缺定位子
	noLoc := []CorpusRankedInterest{{InterestSummaryID: "i1", InterestSummary: "s", Rank: 1,
		AllowedAnchors: []string{"a"}, SourceLocators: nil}}
	if _, err := RetrieveFixedStartersSources(context.Background(), noLoc, CorpusVersion,
		&fakeRankingAdapter{}, RetrievalTopK); err == nil || !strings.Contains(err.Error(), "source locators") {
		t.Fatalf("err=%v", err)
	}
	// 响应缺 hits
	if _, err := RetrieveFixedStartersSources(context.Background(), interests, CorpusVersion,
		&fakeRankingAdapter{response: map[string]any{}}, RetrievalTopK); err == nil ||
		!strings.Contains(err.Error(), "retrieval_contract_failed") {
		t.Fatalf("err=%v", err)
	}
	// 未知 starter_id
	resp := map[string]any{"hits": []any{map[string]any{"starter_id": "ghost", "interest_summary_id": nil}}}
	if _, err := RetrieveFixedStartersSources(context.Background(), nil, CorpusVersion,
		&fakeRankingAdapter{response: resp}, RetrievalTopK); err == nil ||
		!strings.Contains(err.Error(), "not in the requested corpus") {
		t.Fatalf("err=%v", err)
	}
	// 冷启动缺位但带了兴趣
	resp2 := map[string]any{"hits": []any{map[string]any{"starter_id": corpus[0].StarterID, "interest_summary_id": nil}}}
	if _, err := RetrieveFixedStartersSources(context.Background(), interests, CorpusVersion,
		&fakeRankingAdapter{response: resp2}, RetrievalTopK); err == nil ||
		!strings.Contains(err.Error(), "warm request hit") {
		t.Fatalf("err=%v", err)
	}
	// 暖启动引用未排序兴趣
	resp3 := map[string]any{"hits": []any{map[string]any{"starter_id": corpus[0].StarterID, "interest_summary_id": "ghost-i"}}}
	if _, err := RetrieveFixedStartersSources(context.Background(), interests, CorpusVersion,
		&fakeRankingAdapter{response: resp3}, RetrievalTopK); err == nil ||
		!strings.Contains(err.Error(), "not in the ranked input") {
		t.Fatalf("err=%v", err)
	}
	// 暖启动命中不含允许锚点
	resp4 := map[string]any{"hits": []any{map[string]any{"starter_id": corpus[0].StarterID, "interest_summary_id": "i1"}}}
	noAnchor := []CorpusRankedInterest{{InterestSummaryID: "i1", InterestSummary: "s", Rank: 1,
		AllowedAnchors: []string{"绝不匹配的锚点"}, SourceLocators: []SourceLocator{{SessionID: "s", Ordinal: 1}}}}
	if _, err := RetrieveFixedStartersSources(context.Background(), noAnchor, CorpusVersion,
		&fakeRankingAdapter{response: resp4}, RetrievalTopK); err == nil ||
		!strings.Contains(err.Error(), "allowed interest anchor") {
		t.Fatalf("err=%v", err)
	}
	// 重复 starter_id
	resp5 := map[string]any{"hits": []any{
		map[string]any{"starter_id": corpus[0].StarterID, "interest_summary_id": nil},
		map[string]any{"starter_id": corpus[0].StarterID, "interest_summary_id": nil},
	}}
	if _, err := RetrieveFixedStartersSources(context.Background(), nil, CorpusVersion,
		&fakeRankingAdapter{response: resp5}, RetrievalTopK); err == nil ||
		!strings.Contains(err.Error(), "must not repeat") {
		t.Fatalf("err=%v", err)
	}
}

func TestArkFixedStarterRankingAdapter_载荷与提示词(t *testing.T) {
	var gotPrompt string
	var gotStage string
	adapter := NewArkFixedStarterRankingAdapter(
		func(ctx context.Context, stage, systemPrompt, payload string) (any, error) {
			gotStage, gotPrompt = stage, systemPrompt
			return map[string]any{}, nil
		})
	corpus, err := LoadFixedStarterCorpus(CorpusVersion)
	if err != nil {
		t.Fatal(err)
	}
	req := FixedStarterRankingRequest{
		RankedInterests: corpusRankFixture(corpus), Corpus: corpus[:2],
		CorpusVersion: CorpusVersion, TopK: 4,
	}
	if _, err := adapter.RankFixedStarters(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if gotStage != "fixed_starter_retriever" || gotPrompt != i3SystemPrompt {
		t.Fatalf("stage=%q prompt 匹配=%v", gotStage, gotPrompt == i3SystemPrompt)
	}
}

func TestHelpersSources(t *testing.T) {
	if truncateRunes("你好世界", 2) != "你好" {
		t.Fatal("rune 截断错误")
	}
	if sortStringsCopy([]string{"b", "a"})[0] != "a" {
		t.Fatal("排序错误")
	}
	if nilValueIfEmpty("") != nil || nilValueIfEmpty("x") != "x" {
		t.Fatal("空串转换错误")
	}
}

func TestSourceRecord_发布时间字段写入(t *testing.T) {
	page := map[string]any{
		"url": "https://example.com/a", "content": strings.Repeat("字", 2500),
		"fetch_reader": "reader-x", "published_at": "2026-08-30T00:00:00Z",
		"published_at_source": "meta",
	}
	record := sourceRecord(page, SourceAssessment{Kind: "stable", Publisher: "example.com"}, "engine-x", 100.0)
	boundary := record["content_boundary"].(map[string]any)
	if intOf(boundary["end"]) != EvidenceTextLimit {
		t.Fatalf("截断边界=%v", boundary["end"])
	}
	if record["published_at"] != "2026-08-30T00:00:00Z" || record["published_at_source"] != "meta" {
		t.Fatalf("record=%v", record)
	}
	if record["fetch_reader"] != "reader-x" || record["search_engine"] != "engine-x" {
		t.Fatalf("record=%v", record)
	}
}

func TestSourceRecord_无发布时间(t *testing.T) {
	page := map[string]any{"url": "https://example.com/a", "content": "x"}
	record := sourceRecord(page, SourceAssessment{Kind: "stable", Publisher: "p"}, "e", 1)
	if _, ok := record["published_at"]; ok {
		t.Fatal("无发布时间不应写字段")
	}
}
