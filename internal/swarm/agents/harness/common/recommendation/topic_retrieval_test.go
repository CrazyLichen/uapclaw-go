// Package recommendation topic_retrieval 契约层单元测试。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/recommendation/topic_retrieval.py
package recommendation

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// retrievalTestBinding 构造合法的证据组件绑定。
func retrievalTestBinding(t *testing.T, componentID, providerID string) *LearnedComponentBinding {
	t.Helper()
	binding, err := NewLearnedComponentBinding(componentID, providerID, "v1", "cfg1")
	require.NoError(t, err)
	return binding
}

// retrievalTestCandidate 构造合法候选。
func retrievalTestCandidate(t *testing.T, freshnessClass string) *EvidenceCandidate {
	t.Helper()
	candidate, err := NewEvidenceCandidate("cand-1", "标题", "查询", freshnessClass, "global_new_chat", "broad_timely_practical")
	require.NoError(t, err)
	return candidate
}

// retrievalTestDocument 构造合法冻结文档。
func retrievalTestDocument(t *testing.T, id, content, sourceKind, publisher, publishedAt string) FrozenDocumentRevision {
	t.Helper()
	doc, err := NewFrozenDocumentRevision(id, "https://example.com/"+id, publisher, sourceKind,
		publishedAt, 1700000000, content, "2026-11-13", "v1", nil)
	require.NoError(t, err)
	return *doc
}

func TestVersionedDocumentIdentity(t *testing.T) {
	// 带版本 abs 页
	docID, version, ok := VersionedDocumentIdentity("https://arxiv.org/abs/2501.12345v3", "")
	assert.True(t, ok)
	assert.Equal(t, "arxiv:2501.12345", docID)
	assert.Equal(t, "v3", version)
	// pdf 与 html 形态
	_, _, ok = VersionedDocumentIdentity("https://arxiv.org/pdf/2501.12345v1.pdf", "")
	assert.True(t, ok)
	// 无版本 URL 不能绑定日期
	_, _, ok = VersionedDocumentIdentity("https://arxiv.org/abs/2501.12345", "")
	assert.False(t, ok)
	// 正文引用 arXiv:IDvN 后可绑定
	_, _, ok = VersionedDocumentIdentity("https://arxiv.org/abs/2501.12345", "cited as arXiv:2501.12345v2 recently")
	assert.True(t, ok)
	// 非 arXiv 域名
	_, _, ok = VersionedDocumentIdentity("https://example.org/abs/2501.12345v1", "")
	assert.False(t, ok)
}

func TestParseISOTimestamp(t *testing.T) {
	ts, ok := ParseISOTimestamp("2026-11-13T00:00:00Z")
	assert.True(t, ok)
	assert.Greater(t, ts, float64(1700000000))
	// 无时区回退 UTC（对齐 :486-487）
	_, ok = ParseISOTimestamp("2026-11-13T00:00:00")
	assert.True(t, ok)
	// 空串对齐 None
	_, ok = ParseISOTimestamp("")
	assert.False(t, ok)
	// 非法
	_, ok = ParseISOTimestamp("not-a-date")
	assert.False(t, ok)
}

func TestPublishedAtIsFresh(t *testing.T) {
	now := 1794528000.0 // 2026-11-13 00:00 UTC
	// evergreen 恒新鲜
	assert.True(t, PublishedAtIsFresh("", "evergreen_corpus", now))
	// 无日期不新鲜
	assert.False(t, PublishedAtIsFresh("", "current_event", now))
	// 新闻 72h 窗口
	assert.True(t, PublishedAtIsFresh("2026-11-12T00:00:00Z", NewsFreshnessClass, now))
	assert.False(t, PublishedAtIsFresh("2026-11-01T00:00:00Z", NewsFreshnessClass, now))
	// current_event 7 天
	assert.True(t, PublishedAtIsFresh("2026-11-08T00:00:00Z", "current_event", now))
	assert.False(t, PublishedAtIsFresh("2026-11-01T00:00:00Z", "current_event", now))
	// 其他 90 天
	assert.True(t, PublishedAtIsFresh("2026-09-01T00:00:00Z", "recent_development", now))
}

func TestPATrainingExampleValidation(t *testing.T) {
	// active：有 locator 无 starter
	active, err := NewPATrainingExample("snap-1", stringPtr("hello"), nil, "active", true, 100, "global_new_chat", "art-1")
	require.NoError(t, err)
	assert.Equal(t, "pa-training:active:art-1", active.ArtifactID())
	// active 缺 locator → 拒绝
	_, err = NewPATrainingExample("snap-1", nil, nil, "active", true, 100, "global_new_chat", "art-1")
	require.Error(t, err)
	var contractErr *TerminalRetrievalContractError
	assert.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "invalid_pa_training_example", contractErr.Reason)
	// passive：有 starter 无 locator
	passive, err := NewPATrainingExample("snap-1", nil, stringPtr("starter-1"), "passive", false, 100, "project:p1", "art-2")
	require.NoError(t, err)
	assert.Equal(t, "pa-training:passive:art-2", passive.ArtifactID())
	// 非法 scope
	_, err = NewPATrainingExample("snap-1", nil, stringPtr("s"), "passive", false, 100, "bogus", "art")
	assert.Error(t, err)
	// 负时间戳
	_, err = NewPATrainingExample("snap-1", nil, stringPtr("s"), "passive", false, -1, "global_new_chat", "art")
	assert.Error(t, err)
}

func TestPATrainingExamplePayloadRoundTrip(t *testing.T) {
	example, err := NewPATrainingExample("snap-1", stringPtr("hello"), nil, "active", true, 100.5, "global_new_chat", "art-1")
	require.NoError(t, err)
	payload := example.AsPayload()
	decoded, err := PATrainingExampleFromPayload(payload)
	require.NoError(t, err)
	assert.Equal(t, example.UserStateSnapshotID, decoded.UserStateSnapshotID)
	assert.Equal(t, "hello", *decoded.QueryTextLocator)
	assert.Equal(t, 100.5, decoded.EventTime)
	// 多余键拒绝
	bad := map[string]any{}
	for k, v := range payload {
		bad[k] = v
	}
	bad["extra"] = 1
	_, err = PATrainingExampleFromPayload(bad)
	assert.Error(t, err)
}

func TestPassiveStarterExampleValidation(t *testing.T) {
	clicked := 101.0
	example, err := NewPassiveStarterExample("snap-1", "global_new_chat", "corpus-v1", "starter-1",
		"exp-1", "man-1", "rev-1", 100, &clicked, nil,
		map[string]string{"f": "v1"})
	require.NoError(t, err)
	assert.Equal(t, "passive-starter:exp-1:rev-1", example.ArtifactID())
	// 空 refs 拒绝
	_, err = NewPassiveStarterExample("snap-1", "global_new_chat", "corpus-v1", "starter-1",
		"exp-1", "man-1", "rev-1", 100, nil, nil, map[string]string{})
	assert.Error(t, err)
	// 负曝光时间拒绝
	_, err = NewPassiveStarterExample("snap-1", "global_new_chat", "corpus-v1", "starter-1",
		"exp-1", "man-1", "rev-1", -1, nil, nil, map[string]string{"f": "v1"})
	assert.Error(t, err)
	// ToTrainingExample
	training, err := example.ToTrainingExample(true, 200)
	require.NoError(t, err)
	assert.Equal(t, "passive", training.Domain)
	assert.Equal(t, "starter-1", *training.QuerySourceStarterID)
	assert.Nil(t, training.QueryTextLocator)
	// payload round-trip
	decoded, err := PassiveStarterExampleFromPayload(example.AsPayload())
	require.NoError(t, err)
	assert.Equal(t, 101.0, *decoded.ClickedAt)
	assert.Nil(t, decoded.NotInterestedAt)
}

func TestAdmitActiveExpression(t *testing.T) {
	newObservation := func(t *testing.T, inputOrigin, assistanceOrigin string) *ActiveExpressionObservation {
		t.Helper()
		o, err := NewActiveExpressionObservation("snap-1", "loc-1", 100, inputOrigin, assistanceOrigin, "global_new_chat")
		require.NoError(t, err)
		return o
	}
	// 自然手输 → 准入
	admission, err := AdmitActiveExpression(newObservation(t, "manual_free_text", "none"))
	require.NoError(t, err)
	assert.NotNil(t, admission.Example)
	assert.Empty(t, admission.ExclusionReason)
	// 非手输 → 排除
	admission, err = AdmitActiveExpression(newObservation(t, "voice", "none"))
	require.NoError(t, err)
	assert.Nil(t, admission.Example)
	assert.Equal(t, "ineligible_input_origin", admission.ExclusionReason)
	// 有协助 → 排除
	admission, err = AdmitActiveExpression(newObservation(t, "manual_free_text", "assistant"))
	require.NoError(t, err)
	assert.Nil(t, admission.Example)
	assert.Equal(t, "assisted_input_excluded", admission.ExclusionReason)
	// 观察 payload round-trip
	o := newObservation(t, "manual_free_text", "none")
	decoded, err := ActiveExpressionObservationFromPayload(o.AsPayload())
	require.NoError(t, err)
	assert.Equal(t, o.ArtifactID(), decoded.ArtifactID())
}

func TestPublicationDateProvenanceValidation(t *testing.T) {
	// 结构化搜索结果：三字段必填
	p, err := NewPublicationDateProvenance("structured_search_result", "date", "2026-11-13",
		"bing", "rcpt-1", "https://example.com/a", nil, "", "", "")
	require.NoError(t, err)
	// linked_document_metadata：非法页拒绝
	_, err = NewPublicationDateProvenance("linked_document_metadata", "date", "2026-11-13",
		"", "", "", map[string]any{"bogus": 1}, "", "", "")
	assert.Error(t, err)
	// page_explicit_metadata：带搜索字段拒绝
	_, err = NewPublicationDateProvenance("page_explicit_metadata", "date", "2026-11-13",
		"bing", "", "", nil, "", "", "")
	assert.Error(t, err)
	// listed_at 只能挂在 page_explicit_metadata
	_, err = NewPublicationDateProvenance("structured_search_result", "date", "2026-11-13",
		"bing", "r", "u", nil, "2026-11-12", "", "")
	assert.Error(t, err)
	// page_explicit_metadata + listed_at + api_url 合法
	_, err = NewPublicationDateProvenance("page_explicit_metadata", "date", "2026-11-13",
		"", "", "", nil, "2026-11-12", "", "https://api.example.com/1")
	require.NoError(t, err)
	// payload None 字段不落键
	payload := p.AsPayload()
	assert.NotContains(t, payload, "linked_page")
	assert.Equal(t, "bing", payload["search_provider_id"])
}

func TestFrozenDocumentRevisionValidation(t *testing.T) {
	// published_at 无法解析 → 拒绝
	_, err := NewFrozenDocumentRevision("doc-1", "https://example.com", "ex", "original",
		"bad-date", 100, "正文", "2026-11-13", "v1", nil)
	assert.Error(t, err)
	// published_at 为空但带溯源 → 拒绝
	prov, err := NewPublicationDateProvenance("structured_search_result", "date", "2026-11-13",
		"bing", "r", "https://example.com", nil, "", "", "")
	require.NoError(t, err)
	_, err = NewFrozenDocumentRevision("doc-1", "https://example.com", "ex", "original",
		"", 100, "正文", "2026-11-13", "v1", prov)
	assert.Error(t, err)
	// 溯源 URL 与文档 URL 不一致 → 拒绝
	prov2, err := NewPublicationDateProvenance("structured_search_result", "date", "2026-11-13",
		"bing", "r", "https://other.com", nil, "", "", "")
	require.NoError(t, err)
	_, err = NewFrozenDocumentRevision("doc-1", "https://example.com", "ex", "original",
		"2026-11-13T00:00:00Z", 100, "正文", "2026-11-13", "v1", prov2)
	assert.Error(t, err)
	// 合法
	doc, err := NewFrozenDocumentRevision("doc-1", "https://example.com", "ex", "original",
		"2026-11-13T00:00:00Z", 100, "正文", "2026-11-13", "v1", prov)
	require.NoError(t, err)
	payload := doc.AsPayload(true)
	assert.Contains(t, payload, "immutable_content")
	assert.NotContains(t, doc.AsPayload(false), "immutable_content")
	_ = payload
}

func TestFrozenDocumentSnapshot(t *testing.T) {
	docA := retrievalTestDocument(t, "doc-1", "正文一", "original", "ex1", "2026-11-13T00:00:00Z")
	docB := retrievalTestDocument(t, "doc-2", "正文二", "reliable_report", "ex2", "2026-11-12T00:00:00Z")
	snapshot, err := NewFrozenDocumentSnapshot("2026-11-13", "v1", []FrozenDocumentRevision{docA, docB})
	require.NoError(t, err)
	assert.Len(t, snapshot.ByID(), 2)
	// 空文档集合
	_, err = NewFrozenDocumentSnapshot("2026-11-13", "v1", nil)
	assert.Error(t, err)
	// 重复 ID
	_, err = NewFrozenDocumentSnapshot("2026-11-13", "v1", []FrozenDocumentRevision{docA, docA})
	assert.Error(t, err)
	// 混快照
	_, err = NewFrozenDocumentSnapshot("2026-11-13", "v1", []FrozenDocumentRevision{docA,
		retrievalTestDocument(t, "doc-3", "正文三", "original", "ex3", "2026-11-13T00:00:00Z")})
	require.NoError(t, err)
}

func TestEvidenceCandidateValidation(t *testing.T) {
	_, err := NewEvidenceCandidate("c", "标题", "查询", "bogus_class", "global_new_chat", "cat")
	var contractErr *EvidenceContractError
	require.Error(t, err)
	assert.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "invalid_freshness_class", contractErr.Reason)
	// 非法 scope
	_, err = NewEvidenceCandidate("c", "标题", "查询", "current_event", "bogus", "cat")
	assert.Error(t, err)
	// news 类别合法
	_, err = NewEvidenceCandidate("c", "标题", "查询", NewsFreshnessClass, "global_new_chat", "cat")
	assert.NoError(t, err)
}

func TestHostExactOffsetCatalogCJK(t *testing.T) {
	// CJK 语义：中文句号断句；rune 索引不得截断多字节字符
	content := "人工智能正在改变世界。它也在改变工作方式！你准备好了吗？"
	catalog := hostExactOffsetCatalog(content)
	require.Len(t, catalog, 3)
	for _, entry := range catalog {
		text := entry["exact_text"].(string)
		start, end := entry["start"].(int), entry["end"].(int)
		runes := []rune(content)
		assert.Equal(t, text, string(runes[start:end]))
	}
	assert.Equal(t, "人工智能正在改变世界。", catalog[0]["exact_text"])
	// 超长无断句 → 硬截断
	long := string([]rune("汉")[:1]) + ""
	_ = long
	big := make([]rune, OffsetCatalogMaxChars+50)
	for i := range big {
		big[i] = '字'
	}
	catalog = hostExactOffsetCatalog(string(big))
	assert.Len(t, catalog, 2)
	assert.Equal(t, OffsetCatalogMaxChars, catalog[0]["end"].(int)-catalog[0]["start"].(int))
}

func TestEligibleDocumentRevisionIds(t *testing.T) {
	now := 1794528000.0
	// evergreen：来源三类别直通、含过期文档
	documents := []FrozenDocumentRevision{
		retrievalTestDocument(t, "d1", "内容", "authoritative", "p1", "2020-01-01T00:00:00Z"),
		retrievalTestDocument(t, "d2", "内容", "stable", "p2", "2020-01-01T00:00:00Z"),
		retrievalTestDocument(t, "d3", "内容", "reliable_report", "p3", "2020-01-01T00:00:00Z"),
	}
	candidate := retrievalTestCandidate(t, "evergreen_corpus")
	ids, err := EligibleDocumentRevisionIds(candidate, documents, now)
	require.NoError(t, err)
	assert.Equal(t, []string{"d1", "d2"}, ids)
	// current_event：主来源优先
	documents = []FrozenDocumentRevision{
		retrievalTestDocument(t, "d1", "内容", "authoritative", "p1", "2026-11-12T00:00:00Z"),
		retrievalTestDocument(t, "d2", "内容", "reliable_report", "p2", "2026-11-12T00:00:00Z"),
		retrievalTestDocument(t, "d3", "内容", "reliable_report", "p3", "2026-11-12T00:00:00Z"),
		retrievalTestDocument(t, "d4", "内容", "reliable_report", "p2", "2026-11-12T00:00:00Z"), // 同发布方
	}
	candidate = retrievalTestCandidate(t, "current_event")
	ids, err = EligibleDocumentRevisionIds(candidate, documents, now)
	require.NoError(t, err)
	assert.Equal(t, []string{"d1"}, ids)
	// 无主来源 → 双独立报道（最多 2 家）
	documents = documents[1:]
	ids, err = EligibleDocumentRevisionIds(candidate, documents, now)
	require.NoError(t, err)
	assert.Equal(t, []string{"d2", "d3"}, ids)
	// news：只认新闻来源
	newsDoc := retrievalTestDocument(t, "n1", "内容", NewsSourceKind, "p9", "2026-11-12T00:00:00Z")
	documents = append([]FrozenDocumentRevision{newsDoc}, documents...)
	newsCandidate := retrievalTestCandidate(t, NewsFreshnessClass)
	ids, err = EligibleDocumentRevisionIds(newsCandidate, documents, now)
	require.NoError(t, err)
	assert.Equal(t, []string{"n1"}, ids)
	// 非法 now → 输入契约失败
	_, err = EligibleDocumentRevisionIds(candidate, documents, -1)
	var contractErr *EvidenceContractError
	require.Error(t, err)
	assert.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "evidence_admission_input_failed", contractErr.Reason)
}

// retrievalTestSnapshot 构造单文档快照。
func retrievalTestSnapshot(t *testing.T, content string) *FrozenDocumentSnapshot {
	t.Helper()
	doc := retrievalTestDocument(t, "doc-1", content, "original", "ex", "2026-11-12T00:00:00Z")
	snapshot, err := NewFrozenDocumentSnapshot("2026-11-13", "v1", []FrozenDocumentRevision{doc})
	require.NoError(t, err)
	return snapshot
}

func TestExtractCandidateEvidenceSuccess(t *testing.T) {
	content := "人工智能正在改变世界。它也在改变工作方式！"
	snapshot := retrievalTestSnapshot(t, content)
	candidate := retrievalTestCandidate(t, "current_event")
	binding := retrievalTestBinding(t, EvidenceComponentID, EvidenceAPIProviderID)
	// 抽取器返回宿主目录内的第一个片段
	catalog := hostExactOffsetCatalog(content)
	first := catalog[0]
	extractor := func(ctx context.Context, payload map[string]any) (any, error) {
		return map[string]any{
			"candidate_id": "cand-1",
			"spans": []any{
				map[string]any{"document_revision_id": "doc-1", "start": first["start"], "end": first["end"]},
			},
		}, nil
	}
	extracted, err := ExtractCandidateEvidence(candidate, snapshot, nil, "snap-1", "req-1", binding, extractor, 1794528000)
	require.NoError(t, err)
	require.Len(t, extracted.Spans, 1)
	assert.Equal(t, "人工智能正在改变世界。", extracted.Spans[0].ExactText)
	assert.Equal(t, "uncalibrated_api_proxy", extracted.ComponentReceipt.ScoreSemantics)
	assert.Equal(t, "success", extracted.ComponentReceipt.OutputStatus)
	assert.Equal(t, EvidenceContractVersionStr, extracted.ContractVersion)
	// AdmitCandidateEvidence：current_event 主来源 → single_primary_source
	bundle, err := AdmitCandidateEvidence(extracted, 1794528000)
	require.NoError(t, err)
	assert.Equal(t, "single_primary_source", bundle.SourceGateMode)
	assert.Len(t, bundle.Spans, 1)
}

func TestExtractCandidateEvidenceFailClosed(t *testing.T) {
	content := "人工智能正在改变世界。它也在改变工作方式！"
	snapshot := retrievalTestSnapshot(t, content)
	candidate := retrievalTestCandidate(t, "current_event")
	binding := retrievalTestBinding(t, EvidenceComponentID, EvidenceAPIProviderID)
	catalog := hostExactOffsetCatalog(content)
	first := catalog[0]

	call := func(extractor EvidenceExtractor) (*EvidenceContractError, error) {
		_, err := ExtractCandidateEvidence(candidate, snapshot, nil, "snap-1", "req-1", binding, extractor, 1794528000)
		var contractErr *EvidenceContractError
		if err == nil {
			return nil, nil
		}
		require.True(t, errors.As(err, &contractErr), "期望 EvidenceContractError，得到 %v", err)
		return contractErr, err
	}
	// 厂商异常 → invocation_failed 且带失败小票
	var contractErr *EvidenceContractError
	contractErr, _ = call(func(ctx context.Context, payload map[string]any) (any, error) {
		return nil, errors.New("boom")
	})
	assert.Equal(t, "evidence_provider_invocation_failed", contractErr.Reason)
	assert.NotNil(t, contractErr.ComponentReceipt)
	assert.Equal(t, "failed", contractErr.ComponentReceipt.OutputStatus)
	// 顶层带 snippet → 拒绝
	contractErr, _ = call(func(ctx context.Context, payload map[string]any) (any, error) {
		return map[string]any{"candidate_id": "cand-1", "spans": []any{}, "snippet": "x"}, nil
	})
	assert.Equal(t, "snippet_or_title_not_evidence", contractErr.Reason)
	// 候选不匹配
	contractErr, _ = call(func(ctx context.Context, payload map[string]any) (any, error) {
		return map[string]any{"candidate_id": "other", "spans": []any{}}, nil
	})
	assert.Equal(t, "candidate_mismatch", contractErr.Reason)
	// 空片段
	contractErr, _ = call(func(ctx context.Context, payload map[string]any) (any, error) {
		return map[string]any{"candidate_id": "cand-1", "spans": []any{}}, nil
	})
	assert.Equal(t, "no_candidate_support", contractErr.Reason)
	// 目录外偏移
	contractErr, _ = call(func(ctx context.Context, payload map[string]any) (any, error) {
		return map[string]any{"candidate_id": "cand-1", "spans": []any{
			map[string]any{"document_revision_id": "doc-1", "start": 0, "end": 1},
		}}, nil
	})
	assert.Equal(t, "invalid_evidence_offsets", contractErr.Reason)
	// 遗留前缀整段（2000 字符）拒绝
	legacyDoc := retrievalTestDocument(t, "doc-1", string(make([]rune, LegacyPrefixCharacterCount)), "original", "ex", "2026-11-12T00:00:00Z")
	legacySnapshot, err := NewFrozenDocumentSnapshot("2026-11-13", "v1", []FrozenDocumentRevision{legacyDoc})
	require.NoError(t, err)
	_, err = ExtractCandidateEvidence(candidate, legacySnapshot, nil, "snap-1", "req-1", binding,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return map[string]any{"candidate_id": "cand-1", "spans": []any{
				map[string]any{"document_revision_id": "doc-1", "start": 0, "end": LegacyPrefixCharacterCount},
			}}, nil
		}, 1794528000)
	require.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "legacy_prefix_not_evidence", contractErr.Reason)
	// 重复片段拒绝
	dupExtractor := func(ctx context.Context, payload map[string]any) (any, error) {
		return map[string]any{"candidate_id": "cand-1", "spans": []any{
			map[string]any{"document_revision_id": "doc-1", "start": first["start"], "end": first["end"]},
			map[string]any{"document_revision_id": "doc-1", "start": first["start"], "end": first["end"]},
		}}, nil
	}
	_, err = ExtractCandidateEvidence(candidate, snapshot, nil, "snap-1", "req-1", binding, dupExtractor, 1794528000)
	require.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "duplicate_evidence_span", contractErr.Reason)
	// 组件 ID 错误
	wrongBinding := retrievalTestBinding(t, PARetrieverComponentID, EvidenceAPIProviderID)
	_, err = ExtractCandidateEvidence(candidate, snapshot, nil, "snap-1", "req-1", wrongBinding, nil, 1794528000)
	require.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "evidence_input_contract_failed", contractErr.Reason)
}

func TestAdmitCandidateEvidenceGates(t *testing.T) {
	now := 1794528000.0
	fresh := "2026-11-12T00:00:00Z"
	stale := "2020-01-01T00:00:00Z"
	build := func(t *testing.T, publishedAt, sourceKind string) ExtractedCandidateEvidence {
		t.Helper()
		doc := retrievalTestDocument(t, "doc-1", "内容一。", sourceKind, "ex", publishedAt)
		snapshot, err := NewFrozenDocumentSnapshot("2026-11-13", "v1", []FrozenDocumentRevision{doc})
		require.NoError(t, err)
		candidate := retrievalTestCandidate(t, "current_event")
		binding := retrievalTestBinding(t, EvidenceComponentID, EvidenceAPIProviderID)
		catalog := hostExactOffsetCatalog("内容一。")
		extracted, err := ExtractCandidateEvidence(candidate, snapshot, nil, "snap-1", "req-1", binding,
			func(ctx context.Context, payload map[string]any) (any, error) {
				return map[string]any{"candidate_id": "cand-1", "spans": []any{
					map[string]any{"document_revision_id": "doc-1", "start": catalog[0]["start"], "end": catalog[0]["end"]},
				}}, nil
			}, now)
		require.NoError(t, err)
		return *extracted
	}
	// 过期片段 → 新鲜度闸失败
	extracted := build(t, stale, "original")
	_, err := AdmitCandidateEvidence(&extracted, now)
	var contractErr *EvidenceContractError
	require.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "freshness_gate_failed", contractErr.Reason)
	// 非主来源 → 来源闸失败（current_event 无 authoritative/original，且单 report 不够两条）
	extracted = build(t, fresh, "reliable_report")
	_, err = AdmitCandidateEvidence(&extracted, now)
	require.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "source_gate_failed", contractErr.Reason)
}

func TestRetrieveFixedStarters(t *testing.T) {
	record, err := NewFixedStarterRecord("s1", "标题", "正文", "话题", "question", "evergreen_corpus",
		"corpus", "broad_timely_practical", []string{"global_new_chat"}, "corpus-v1")
	require.NoError(t, err)
	corpus, err := NewFrozenStarterCorpus("corpus-v1", []FixedStarterRecord{*record})
	require.NoError(t, err)
	binding := retrievalTestBinding(t, PARetrieverComponentID, PAAPIFallbackProviderID)
	interest, err := NewRankedStarterInterest("i1", "兴趣一", 1)
	require.NoError(t, err)

	okExtractor := func(ctx context.Context, payload map[string]any) (any, error) {
		return map[string]any{"hits": []any{
			map[string]any{"starter_id": "s1", "interest_summary_id": "i1"},
		}}, nil
	}
	batch, err := RetrieveFixedStarters("snap-1", "global_new_chat", []*RankedStarterInterest{interest},
		corpus, "corpus-v1", binding, FixedStarterRetriever(okExtractor), "req-1", 3, "")
	require.NoError(t, err)
	require.Len(t, batch.Hits, 1)
	assert.Equal(t, 1, batch.Hits[0].Rank)
	assert.Equal(t, "success", batch.ComponentReceipt.OutputStatus)
	assert.Empty(t, batch.IgnoredProviderFields)

	// 命中数超 top_k
	_, err = RetrieveFixedStarters("snap-1", "global_new_chat", []*RankedStarterInterest{interest},
		corpus, "corpus-v1", binding, FixedStarterRetriever(okExtractor), "req-1", 0, "")
	require.Error(t, err) // top_k<1 输入契约
	bigOK := func(ctx context.Context, payload map[string]any) (any, error) {
		return map[string]any{"hits": []any{
			map[string]any{"starter_id": "s1", "interest_summary_id": "i1"},
			map[string]any{"starter_id": "s1", "interest_summary_id": "i1"},
		}}, nil
	}
	_, err = RetrieveFixedStarters("snap-1", "global_new_chat", []*RankedStarterInterest{interest},
		corpus, "corpus-v1", binding, FixedStarterRetriever(bigOK), "req-1", 1, "")
	var contractErr *TerminalRetrievalContractError
	require.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "retrieval_output_exceeds_top_k", contractErr.Reason)

	// 冷启动兴趣为空时声称兴趣 → 拒绝
	_, err = RetrieveFixedStarters("snap-1", "global_new_chat", nil,
		corpus, "corpus-v1", binding, FixedStarterRetriever(okExtractor), "req-1", 3, "")
	require.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "cold_hit_must_not_claim_interest", contractErr.Reason)

	// 忽略字段进 fallback
	withExtra := func(ctx context.Context, payload map[string]any) (any, error) {
		return map[string]any{"hits": []any{
			map[string]any{"starter_id": "s1", "interest_summary_id": "i1", "title": "被忽略的标题"},
		}}, nil
	}
	batch, err = RetrieveFixedStarters("snap-1", "global_new_chat", []*RankedStarterInterest{interest},
		corpus, "corpus-v1", binding, FixedStarterRetriever(withExtra), "req-1", 3, "")
	require.NoError(t, err)
	assert.Equal(t, "fallback", batch.ComponentReceipt.OutputStatus)
	assert.NotEmpty(t, batch.IgnoredProviderFields)

	// 语料文本不出现在请求中的厂商字段白名单外的字段 → 拒绝
	badField := func(ctx context.Context, payload map[string]any) (any, error) {
		return map[string]any{"hits": []any{
			map[string]any{"starter_id": "s1", "interest_summary_id": "i1", "url": "https://x"},
		}}, nil
	}
	_, err = RetrieveFixedStarters("snap-1", "global_new_chat", []*RankedStarterInterest{interest},
		corpus, "corpus-v1", binding, FixedStarterRetriever(badField), "req-1", 3, "")
	require.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "retrieval_output_contract_failed", contractErr.Reason)

	// 语料版本不一致
	_, err = RetrieveFixedStarters("snap-1", "global_new_chat", []*RankedStarterInterest{interest},
		corpus, "corpus-v2", binding, FixedStarterRetriever(okExtractor), "req-1", 3, "")
	assert.Error(t, err)

	// scope 无可用语料（"project: other" 带空格本身即非法 scope，见 retrievalValidScope）
	_, err = RetrieveFixedStarters("snap-1", "global_new_chat", nil,
		corpus, "corpus-v1", binding, FixedStarterRetriever(okExtractor), "req-1", 3, "")
	require.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "cold_hit_must_not_claim_interest", contractErr.Reason)
	_, err = RetrieveFixedStarters("snap-1", "project:other", []*RankedStarterInterest{interest},
		corpus, "corpus-v1", binding, FixedStarterRetriever(okExtractor), "req-1", 3, "")
	require.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "no_scope_eligible_starters", contractErr.Reason)
	// 带空格的 project scope 本身非法 → 输入契约
	_, err = RetrieveFixedStarters("snap-1", "project: other", []*RankedStarterInterest{interest},
		corpus, "corpus-v1", binding, FixedStarterRetriever(okExtractor), "req-1", 3, "")
	require.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "retrieval_input_contract_failed", contractErr.Reason)

	// 厂商失败 fail-closed
	_, err = RetrieveFixedStarters("snap-1", "global_new_chat", []*RankedStarterInterest{interest},
		corpus, "corpus-v1", binding, FixedStarterRetriever(func(ctx context.Context, payload map[string]any) (any, error) {
			return nil, errors.New("boom")
		}), "req-1", 3, "")
	require.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "provider_invocation_failed", contractErr.Reason)
	assert.Equal(t, "failed", contractErr.ComponentReceipt.OutputStatus)
}

func TestFixedStarterRecordAndCorpus(t *testing.T) {
	// global scope 类别闸
	_, err := NewFixedStarterRecord("s1", "标题", "正文", "话题", "question", "evergreen_corpus",
		"corpus", "wrong_category", []string{"global_new_chat"}, "corpus-v1")
	var contractErr *TerminalRetrievalContractError
	require.Error(t, err)
	assert.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "wrong_category", contractErr.Reason)
	// scope 重复
	_, err = NewFixedStarterRecord("s1", "标题", "正文", "话题", "question", "evergreen_corpus",
		"corpus", "broad_timely_practical", []string{"global_new_chat", "global_new_chat"}, "corpus-v1")
	assert.Error(t, err)
	// 语料重复 ID
	r1, err := NewFixedStarterRecord("s1", "标题", "正文", "话题", "question", "evergreen_corpus",
		"corpus", "broad_timely_practical", []string{"global_new_chat"}, "corpus-v1")
	require.NoError(t, err)
	_, err = NewFrozenStarterCorpus("corpus-v1", []FixedStarterRecord{*r1, *r1})
	require.Error(t, err)
	contractErr = nil
	assert.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "duplicate_corpus_id", contractErr.Reason)
	// 跨版本语料
	r2, err := NewFixedStarterRecord("s2", "标题", "正文", "话题", "question", "evergreen_corpus",
		"corpus", "broad_timely_practical", []string{"global_new_chat"}, "corpus-v2")
	require.NoError(t, err)
	_, err = NewFrozenStarterCorpus("corpus-v1", []FixedStarterRecord{*r1, *r2})
	assert.Error(t, err)
}

func TestEvidenceHeldOutValidation(t *testing.T) {
	doc := retrievalTestDocument(t, "doc-1", "内容一。内容二。", "original", "ex", "2026-11-12T00:00:00Z")
	snapshot, err := NewFrozenDocumentSnapshot("2026-11-13", "v1", []FrozenDocumentRevision{doc})
	require.NoError(t, err)
	candidate := retrievalTestCandidate(t, "current_event")
	// 合法金标偏移
	offset, err := NewEvidenceOffset("doc-1", 0, 4)
	require.NoError(t, err)
	_, err = NewEvidenceHeldOutCase("case-1", "fx-1", candidate, snapshot, []EvidenceOffset{*offset}, true)
	require.NoError(t, err)
	// 偏移超出文档（rune 语义）
	bigOffset, err := NewEvidenceOffset("doc-1", 0, 100)
	require.NoError(t, err)
	_, err = NewEvidenceHeldOutCase("case-1", "fx-1", candidate, snapshot, []EvidenceOffset{*bigOffset}, true)
	var contractErr *EvidenceContractError
	require.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "invalid_heldout_gold_offset", contractErr.Reason)
	// 预测：负时延拒绝
	_, err = NewEvidenceHeldOutPrediction("case-1", nil, true, -1, 0)
	assert.Error(t, err)
	_, err = NewEvidenceHeldOutPrediction("case-1", nil, true, 10, -0.5)
	assert.Error(t, err)
	ok, err := NewEvidenceHeldOutPrediction("case-1", []EvidenceOffset{*offset}, true, 10, 0.5)
	require.NoError(t, err)
	assert.Equal(t, "case-1", ok.CaseID)
}

func TestValidLinkedDatePageFullPath(t *testing.T) {
	// 构造合法 arXiv 链接日期页
	bodyURL := "https://arxiv.org/abs/2501.12345v3"
	dateURL := "https://arxiv.org/html/2501.12345v3"
	// date 页正文引用 arXiv:2501.12345v3，使其与 body 身份一致
	page := map[string]any{
		"date_page_url":      dateURL,
		"body_url":           bodyURL,
		"document_id":        "arxiv:2501.12345",
		"version":            "v3",
		"date_page_excerpt":  "updated listing arXiv:2501.12345v3",
		"operation_id":       "op-1",
		"request_id":         "req-1",
		"http_status_code":   200.0,
	}
	// date 页 URL 必须带版本且与 body 身份一致——这里是同一篇 v3
	assert.True(t, ValidLinkedDatePage(page, bodyURL))
	// 日期页与 body 同 URL → 拒绝
	same := map[string]any{}
	for k, v := range page {
		same[k] = v
	}
	same["date_page_url"] = bodyURL
	assert.False(t, ValidLinkedDatePage(same, bodyURL))
	// http 状态码非 2xx → 拒绝
	bad := map[string]any{}
	for k, v := range page {
		bad[k] = v
	}
	bad["http_status_code"] = 404.0
	assert.False(t, ValidLinkedDatePage(bad, bodyURL))
	// 非 dict → 拒绝
	assert.False(t, ValidLinkedDatePage("not-a-dict", bodyURL))
	// linked_document_metadata 溯源链路
	prov, err := NewPublicationDateProvenance("linked_document_metadata", "date", "2026-11-13",
		"", "", "", page, "", "", "")
	assert.NoError(t, err)
	payload := prov.AsPayload()
	assert.Contains(t, payload, "linked_page")
}

func TestActiveExpressionExampleRoundTrip(t *testing.T) {
	example, err := NewActiveExpressionExample("snap-1", "loc-1", 100, "manual_free_text", "none", "global_new_chat")
	require.NoError(t, err)
	// ToTrainingExample
	training, err := example.ToTrainingExample(true, 200)
	require.NoError(t, err)
	assert.Equal(t, "active", training.Domain)
	assert.Equal(t, "loc-1", *training.QueryTextLocator)
	assert.Nil(t, training.QuerySourceStarterID)
	// payload round-trip
	decoded, err := ActiveExpressionExampleFromPayload(example.AsPayload())
	require.NoError(t, err)
	assert.Equal(t, example.ArtifactID(), decoded.ArtifactID())
	// 多余键拒绝
	bad := map[string]any{}
	for k, v := range example.AsPayload() {
		bad[k] = v
	}
	bad["extra"] = 1
	_, err = ActiveExpressionExampleFromPayload(bad)
	assert.Error(t, err)
}

func TestEvidenceBundleAndHelpers(t *testing.T) {
	// Ratio
	assert.Equal(t, 0.0, Ratio(1, 0, 0.0))
	assert.Equal(t, 0.5, Ratio(1, 2, 0.0))
	// FreshForCandidate
	span := &ExactEvidenceSpan{PublishedAt: "2026-11-12T00:00:00Z"}
	assert.True(t, FreshForCandidate(span, "current_event", 1794528000))
	// CandidateID 方法
	candidate := retrievalTestCandidate(t, "current_event")
	assert.Equal(t, "cand-1", (&ExtractedCandidateEvidence{Candidate: candidate}).CandidateID())
	// retrievalFiniteTimestamp int 分支
	assert.True(t, retrievalFiniteTimestamp(5))
	assert.False(t, retrievalFiniteTimestamp(-5))
	assert.False(t, retrievalFiniteTimestamp("x"))
	assert.False(t, retrievalFiniteTimestamp(true))
	// optionalStringFromEmpty / copyAnyMap / mapValueOrEmpty
	assert.Nil(t, optionalStringFromEmpty(""))
	assert.Equal(t, "x", optionalStringFromEmpty("x"))
	m := copyAnyMap(map[string]any{"a": 1})
	m["a"] = 2
	assert.Equal(t, 1, copyAnyMap(map[string]any{"a": 1})["a"])
	assert.Equal(t, "", mapValueOrEmpty(nil, "k"))
	// derefStringOrEmpty
	assert.Equal(t, "", derefStringOrEmpty(nil))
	// EvidenceBundle AsPayload
	bundle := &EvidenceBundle{
		CandidateID: "c", ContextScope: "global_new_chat", Category: "cat",
		FreshnessClass: "current_event", SnapshotDate: "d", SnapshotVersion: "v",
		Spans: []ExactEvidenceSpan{{CandidateID: "c", Start: 0, End: 1}},
		SourceGateMode: "single_primary_source",
	}
	payload := bundle.AsPayload()
	assert.Equal(t, "single_primary_source", payload["source_gate_mode"])
}

func TestExtractCandidateEvidenceReceiptAndOutputShapes(t *testing.T) {
	content := "内容甲。内容乙。"
	snapshot := retrievalTestSnapshot(t, content)
	candidate := retrievalTestCandidate(t, "current_event")
	binding := retrievalTestBinding(t, EvidenceComponentID, EvidenceAPIProviderID)
	catalog := hostExactOffsetCatalog(content)
	first := catalog[0]

	// 非 dict 响应 → output contract
	_, err := ExtractCandidateEvidence(candidate, snapshot, nil, "snap-1", "req-1", binding,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return "not-a-dict", nil
		}, 1794528000)
	var contractErr *EvidenceContractError
	require.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "evidence_output_contract_failed", contractErr.Reason)
	// 片段未知文档
	_, err = ExtractCandidateEvidence(candidate, snapshot, nil, "snap-1", "req-1", binding,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return map[string]any{"candidate_id": "cand-1", "spans": []any{
				map[string]any{"document_revision_id": "ghost", "start": first["start"], "end": first["end"]},
			}}, nil
		}, 1794528000)
	require.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "unknown_document_revision", contractErr.Reason)
	// offset 非整数
	_, err = ExtractCandidateEvidence(candidate, snapshot, nil, "snap-1", "req-1", binding,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return map[string]any{"candidate_id": "cand-1", "spans": []any{
				map[string]any{"document_revision_id": "doc-1", "start": 0.5, "end": first["end"]},
			}}, nil
		}, 1794528000)
	require.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "invalid_evidence_offsets", contractErr.Reason)
	// start==end
	_, err = ExtractCandidateEvidence(candidate, snapshot, nil, "snap-1", "req-1", binding,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return map[string]any{"candidate_id": "cand-1", "spans": []any{
				map[string]any{"document_revision_id": "doc-1", "start": first["start"], "end": first["start"]},
			}}, nil
		}, 1794528000)
	require.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "empty_evidence_span", contractErr.Reason)
	// 片段级 snippet 字段
	_, err = ExtractCandidateEvidence(candidate, snapshot, nil, "snap-1", "req-1", binding,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return map[string]any{"candidate_id": "cand-1", "spans": []any{
				map[string]any{"document_revision_id": "doc-1", "start": first["start"], "end": first["end"], "snippet": "x"},
			}}, nil
		}, 1794528000)
	require.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "snippet_or_title_not_evidence", contractErr.Reason)
	// 未知 EvidenceContractError 从抽取器抛出 → invocation_failed
	_, err = ExtractCandidateEvidence(candidate, snapshot, nil, "snap-1", "req-1", binding,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return nil, NewEvidenceContractError("other_reason", "", nil)
		}, 1794528000)
	require.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "evidence_provider_invocation_failed", contractErr.Reason)
	// allowed_document_revision_ids 未知文档 → 输入契约失败
	_, err = ExtractCandidateEvidence(candidate, snapshot, []string{"ghost"}, "snap-1", "req-1", binding,
		func(ctx context.Context, payload map[string]any) (any, error) { return nil, nil },
		1794528000)
	require.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "evidence_input_contract_failed", contractErr.Reason)
	// 空白 request_id
	_, err = ExtractCandidateEvidence(candidate, snapshot, nil, "snap-1", "  ", binding,
		func(ctx context.Context, payload map[string]any) (any, error) { return nil, nil },
		1794528000)
	require.True(t, errors.As(err, &contractErr))
	assert.Equal(t, "evidence_input_contract_failed", contractErr.Reason)
}

func TestAdmitCandidateEvidenceEvergreenAndNews(t *testing.T) {
	now := 1794528000.0
	runAdmit := func(t *testing.T, freshnessClass, sourceKind, publishedAt string) (*EvidenceBundle, *EvidenceContractError) {
		t.Helper()
		doc := retrievalTestDocument(t, "doc-1", "内容一。", sourceKind, "ex", publishedAt)
		snapshot, err := NewFrozenDocumentSnapshot("2026-11-13", "v1", []FrozenDocumentRevision{doc})
		require.NoError(t, err)
		candidate := retrievalTestCandidate(t, freshnessClass)
		binding := retrievalTestBinding(t, EvidenceComponentID, EvidenceAPIProviderID)
		catalog := hostExactOffsetCatalog("内容一。")
		extracted, err := ExtractCandidateEvidence(candidate, snapshot, nil, "snap-1", "req-1", binding,
			func(ctx context.Context, payload map[string]any) (any, error) {
				return map[string]any{"candidate_id": "cand-1", "spans": []any{
					map[string]any{"document_revision_id": "doc-1", "start": catalog[0]["start"], "end": catalog[0]["end"]},
				}}, nil
			}, now)
		require.NoError(t, err)
		bundle, err := AdmitCandidateEvidence(extracted, now)
		var contractErr *EvidenceContractError
		if err != nil {
			require.True(t, errors.As(err, &contractErr))
			return nil, contractErr
		}
		return bundle, nil
	}
	// evergreen + stable → single_stable_source（evergreen 无视发布日期）
	bundle, contractErr := runAdmit(t, "evergreen_corpus", "stable", "2020-01-01T00:00:00Z")
	assert.Nil(t, contractErr)
	assert.Equal(t, "single_stable_source", bundle.SourceGateMode)
	// evergreen + 非稳定来源 → source_gate_failed
	_, contractErr = runAdmit(t, "evergreen_corpus", "reliable_report", "2020-01-01T00:00:00Z")
	require.NotNil(t, contractErr)
	assert.Equal(t, "source_gate_failed", contractErr.Reason)
	// news + 新闻来源 → single_news_report
	bundle, contractErr = runAdmit(t, NewsFreshnessClass, NewsSourceKind, "2026-11-12T00:00:00Z")
	assert.Nil(t, contractErr)
	assert.Equal(t, "single_news_report", bundle.SourceGateMode)
	// news + 非新闻来源 → source_gate_failed
	_, contractErr = runAdmit(t, NewsFreshnessClass, "original", "2026-11-12T00:00:00Z")
	require.NotNil(t, contractErr)
	assert.Equal(t, "source_gate_failed", contractErr.Reason)
	// 非法 now
	extracted := &ExtractedCandidateEvidence{Candidate: retrievalTestCandidate(t, "current_event")}
	_, err := AdmitCandidateEvidence(extracted, -1)
	var admissionErr *EvidenceContractError
	require.True(t, errors.As(err, &admissionErr))
	assert.Equal(t, "evidence_admission_input_failed", admissionErr.Reason)
}

// recordingRetriever 实现 RecordValidation 的假检索器，验证 provider_validation 记录。
type recordingRetriever struct {
	outcomes []string
}

func (r *recordingRetriever) RecordValidation(response any, outcome string) { r.outcomes = append(r.outcomes, outcome) }
func (r *recordingRetriever) Invoke(ctx context.Context, payload map[string]any) (any, error) {
	return map[string]any{"hits": []any{}}, nil
}

func TestRecordProviderValidationOn(t *testing.T) {
	recorder := &recordingRetriever{}
	recordProviderValidationOn(recorder, nil, "business_validated")
	assert.Equal(t, []string{"business_validated"}, recorder.outcomes)
	// 无 recorder 的普通闭包 → no-op 不 panic
	recordProviderValidationOn(struct{ x int }{}, nil, "business_validated")
}
