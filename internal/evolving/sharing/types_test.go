package sharing

import (
	"encoding/json"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// ──────────────────────────── 辅助 ────────────────────────────

// makeTestRecord 创建测试用 EvolutionRecord。
func makeTestRecord(score float64, source string) checkpointing.EvolutionRecord {
	patch, _ := checkpointing.NewEvolutionPatch("Troubleshooting", "append", "## Fix\n- check bounds", signal.EvolutionTargetBody)
	kws := []string{"IndexError", "bounds"}
	patch.Keywords = kws
	summary := "check loop upper bound"
	patch.Summary = &summary
	return *checkpointing.MakeEvolutionRecord(source, "ctx", *patch, score, nil, nil)
}

// roundTripDict 通过 JSON 序列化/反序列化确保 map 类型可被 Go 的 []any 断言。
// Go 中 []map[string]any 不能直接断言为 []any，但 JSON 往返后类型统一为 []any。
func roundTripDict(data map[string]any) map[string]any {
	raw, _ := json.Marshal(data)
	var result map[string]any
	json.Unmarshal(raw, &result)
	return result
}

// ──────────────────────────── SharingMeta 测试 ────────────────────────────

func TestNewSharingMeta_默认值(t *testing.T) {
	m := NewSharingMeta("python-debug")
	if m.SkillName != "python-debug" {
		t.Errorf("SkillName = %q, want %q", m.SkillName, "python-debug")
	}
	if m.UploadTrigger != "user_approval" {
		t.Errorf("UploadTrigger = %q, want %q", m.UploadTrigger, "user_approval")
	}
	if m.Confidence != 0.7 {
		t.Errorf("Confidence = %f, want %f", m.Confidence, 0.7)
	}
	if m.UploadAt == "" {
		t.Error("UploadAt should not be empty")
	}
}

func TestSharingMeta_ToDict_FromDict_往返(t *testing.T) {
	fb := "some feedback"
	uid := "user123"
	obid := "sb_abc123"
	original := &SharingMeta{
		SkillName:       "python-debug",
		SkillVersion:    "1.0",
		UploadTrigger:   "auto",
		UploadAt:        "2027-01-01T00:00:00Z",
		FeedbackExcerpt: &fb,
		SourceUserID:    &uid,
		Confidence:      0.9,
		OriginBundleID:  &obid,
	}
	dict := original.ToDict()
	restored := FromDictSharingMeta(dict)
	if restored.SkillName != original.SkillName {
		t.Errorf("SkillName = %q, want %q", restored.SkillName, original.SkillName)
	}
	if restored.SkillVersion != original.SkillVersion {
		t.Errorf("SkillVersion = %q, want %q", restored.SkillVersion, original.SkillVersion)
	}
	if restored.UploadTrigger != original.UploadTrigger {
		t.Errorf("UploadTrigger = %q, want %q", restored.UploadTrigger, original.UploadTrigger)
	}
	if restored.Confidence != original.Confidence {
		t.Errorf("Confidence = %f, want %f", restored.Confidence, original.Confidence)
	}
	if restored.FeedbackExcerpt == nil || *restored.FeedbackExcerpt != fb {
		t.Errorf("FeedbackExcerpt = %v, want %q", restored.FeedbackExcerpt, fb)
	}
	if restored.SourceUserID == nil || *restored.SourceUserID != uid {
		t.Errorf("SourceUserID = %v, want %q", restored.SourceUserID, uid)
	}
	if restored.OriginBundleID == nil || *restored.OriginBundleID != obid {
		t.Errorf("OriginBundleID = %v, want %q", restored.OriginBundleID, obid)
	}
}

func TestSharingMeta_ToDict_省略可选字段(t *testing.T) {
	m := NewSharingMeta("test")
	dict := m.ToDict()
	if _, ok := dict["feedback_excerpt"]; ok {
		t.Error("feedback_excerpt should be absent when nil")
	}
	if _, ok := dict["source_user_id"]; ok {
		t.Error("source_user_id should be absent when nil")
	}
}

// ──────────────────────────── SharedExperience 测试 ────────────────────────────

func TestSharedExperience_ToDict_FromDict_往返(t *testing.T) {
	record := makeTestRecord(0.8, "user_correction")
	meta := NewSharingMeta("python-debug")
	exp := SharedExperience{
		Record:      record,
		Keywords:    []string{"IndexError", "bounds"},
		Summary:     "check loop upper bound",
		SharingMeta: meta,
	}
	dict := roundTripDict(exp.ToDict())
	restored, err := FromDictSharedExperience(dict)
	if err != nil {
		t.Fatalf("FromDictSharedExperience error: %v", err)
	}
	if restored.Summary != exp.Summary {
		t.Errorf("Summary = %q, want %q", restored.Summary, exp.Summary)
	}
	if len(restored.Keywords) != len(exp.Keywords) {
		t.Errorf("Keywords length = %d, want %d", len(restored.Keywords), len(exp.Keywords))
	}
	if restored.SharingMeta == nil {
		t.Error("SharingMeta should not be nil")
	} else if restored.SharingMeta.SkillName != "python-debug" {
		t.Errorf("SharingMeta.SkillName = %q, want %q", restored.SharingMeta.SkillName, "python-debug")
	}
}

func TestSharedExperience_FromDict_NilSharingMeta(t *testing.T) {
	data := map[string]any{
		"record":       map[string]any{},
		"keywords":     []any{"a", "b"},
		"summary":      "test",
		"sharing_meta": nil,
	}
	exp, err := FromDictSharedExperience(data)
	if err != nil {
		t.Fatalf("FromDictSharedExperience error: %v", err)
	}
	if exp.SharingMeta != nil {
		t.Error("SharingMeta should be nil")
	}
}

// ──────────────────────────── SharedSkillBundle 测试 ────────────────────────────

func TestMakeSharedSkillBundle_关键词去重(t *testing.T) {
	experiences := []SharedExperience{
		{Keywords: []string{"IndexError", "bounds"}, Summary: "fix A"},
		{Keywords: []string{"bounds", "loop"}, Summary: "fix B"},
	}
	bundle := MakeSharedSkillBundle("python-debug", experiences, "", "")
	if len(bundle.KeywordsAggregate) != 3 {
		t.Errorf("KeywordsAggregate length = %d, want 3 (dedup)", len(bundle.KeywordsAggregate))
	}
	// 期望: ["IndexError", "bounds", "loop"]
	expected := []string{"IndexError", "bounds", "loop"}
	for i, kw := range expected {
		if i < len(bundle.KeywordsAggregate) && bundle.KeywordsAggregate[i] != kw {
			t.Errorf("KeywordsAggregate[%d] = %q, want %q", i, bundle.KeywordsAggregate[i], kw)
		}
	}
}

func TestMakeSharedSkillBundle_Summary拼接(t *testing.T) {
	experiences := []SharedExperience{
		{Keywords: []string{"a"}, Summary: "fix A"},
		{Keywords: []string{"b"}, Summary: "fix B"},
	}
	bundle := MakeSharedSkillBundle("test", experiences, "", "")
	want := "fix A; fix B"
	if bundle.SummaryAggregate != want {
		t.Errorf("SummaryAggregate = %q, want %q", bundle.SummaryAggregate, want)
	}
}

func TestMakeSharedSkillBundle_自定义Summary(t *testing.T) {
	experiences := []SharedExperience{
		{Keywords: []string{"a"}, Summary: "fix A"},
	}
	bundle := MakeSharedSkillBundle("test", experiences, "", "custom summary")
	if bundle.SummaryAggregate != "custom summary" {
		t.Errorf("SummaryAggregate = %q, want %q", bundle.SummaryAggregate, "custom summary")
	}
}

func TestSharedSkillBundle_ToDict_FromDict_往返(t *testing.T) {
	record := makeTestRecord(0.8, "user_correction")
	meta := NewSharingMeta("python-debug")
	exp := SharedExperience{
		Record:      record,
		Keywords:    []string{"IndexError"},
		Summary:     "fix",
		SharingMeta: meta,
	}
	bundle := &SharedSkillBundle{
		BundleID:          "sb_test001",
		SkillID:           "sk_test",
		SkillName:         "python-debug",
		SkillVersion:      "1.0",
		KeywordsAggregate: []string{"IndexError"},
		SummaryAggregate:  "fix",
		Experiences:       []SharedExperience{exp},
		CreatedAt:         "2027-01-01T00:00:00Z",
	}
	// 通过 JSON 往返确保类型正确（Go 中 []map[string]any 不能直接断言为 []any）
	dict := roundTripDict(bundle.ToDict())
	restored, err := FromDictSharedSkillBundle(dict)
	if err != nil {
		t.Fatalf("FromDictSharedSkillBundle error: %v", err)
	}
	if restored.BundleID != bundle.BundleID {
		t.Errorf("BundleID = %q, want %q", restored.BundleID, bundle.BundleID)
	}
	if restored.SkillID != bundle.SkillID {
		t.Errorf("SkillID = %q, want %q", restored.SkillID, bundle.SkillID)
	}
	if restored.SkillName != bundle.SkillName {
		t.Errorf("SkillName = %q, want %q", restored.SkillName, bundle.SkillName)
	}
	if len(restored.Experiences) != 1 {
		t.Errorf("Experiences length = %d, want 1", len(restored.Experiences))
	}
}

func TestSharedSkillBundle_FromDict_SkillContentHash回退(t *testing.T) {
	data := map[string]any{
		"skill_id":           "",
		"skill_content_hash": "hash123",
		"bundle_id":          "sb_test",
	}
	bundle, err := FromDictSharedSkillBundle(data)
	if err != nil {
		t.Fatalf("FromDictSharedSkillBundle error: %v", err)
	}
	if bundle.SkillID != "hash123" {
		t.Errorf("SkillID = %q, want %q (skill_content_hash fallback)", bundle.SkillID, "hash123")
	}
}

// ──────────────────────────── SkillPackageMeta 测试 ────────────────────────────

func TestSkillPackageMeta_ToDict_FromDict_往返(t *testing.T) {
	original := &SkillPackageMeta{
		SkillID:     "sk_test",
		SkillName:   "demo",
		Description: "test skill",
		UploadedAt:  "2027-01-01T00:00:00Z",
	}
	dict := original.ToDict()
	restored := FromDictSkillPackageMeta(dict)
	if restored.SkillID != original.SkillID {
		t.Errorf("SkillID = %q, want %q", restored.SkillID, original.SkillID)
	}
	if restored.SkillName != original.SkillName {
		t.Errorf("SkillName = %q, want %q", restored.SkillName, original.SkillName)
	}
}

// ──────────────────────────── SkillSearchResult 测试 ────────────────────────────

func TestSkillSearchResult_ToDict_FromDict_往返(t *testing.T) {
	original := &SkillSearchResult{
		SkillID:         "sk_test",
		SkillName:       "demo",
		Description:     "test",
		ExperienceCount: 5,
		Keywords:        []string{"a", "b"},
		Score:           0.85,
	}
	dict := original.ToDict()
	restored := FromDictSkillSearchResult(dict)
	if restored.SkillID != original.SkillID {
		t.Errorf("SkillID = %q, want %q", restored.SkillID, original.SkillID)
	}
	if restored.ExperienceCount != original.ExperienceCount {
		t.Errorf("ExperienceCount = %d, want %d", restored.ExperienceCount, original.ExperienceCount)
	}
	if restored.Score != original.Score {
		t.Errorf("Score = %f, want %f", restored.Score, original.Score)
	}
}

// ──────────────────────────── QueryKeywords 测试 ────────────────────────────

func TestQueryKeywords_ToDict_FromDict_往返(t *testing.T) {
	original := &QueryKeywords{
		Keywords:   []string{"ppt", "slide"},
		Intent:     "presentation",
		RawExcerpt: "need to fix slide layout",
	}
	dict := original.ToDict()
	restored := FromDictQueryKeywords(dict)
	if restored.Intent != original.Intent {
		t.Errorf("Intent = %q, want %q", restored.Intent, original.Intent)
	}
	if restored.RawExcerpt != original.RawExcerpt {
		t.Errorf("RawExcerpt = %q, want %q", restored.RawExcerpt, original.RawExcerpt)
	}
}

// ──────────────────────────── StagingResult 测试 ────────────────────────────

func TestEmptyStagingResult(t *testing.T) {
	sr := EmptyStagingResult()
	if sr.HasShareable() {
		t.Error("empty StagingResult should not have shareable")
	}
	if len(sr.StagedForShare) != 0 {
		t.Errorf("StagedForShare length = %d, want 0", len(sr.StagedForShare))
	}
	if len(sr.DroppedForShare) != 0 {
		t.Errorf("DroppedForShare length = %d, want 0", len(sr.DroppedForShare))
	}
}

func TestStagingResult_HasShareable(t *testing.T) {
	sr := &StagingResult{
		StagedForShare: []SharedExperience{{Summary: "test"}},
	}
	if !sr.HasShareable() {
		t.Error("StagingResult with items should have shareable")
	}
}

// ──────────────────────────── UploadResult 测试 ────────────────────────────

func TestUploadResult_MarshalJSON(t *testing.T) {
	r := UploadResult{OK: true, BundleID: "sb_123", Reason: "", Retryable: false}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if parsed["ok"] != true {
		t.Errorf("ok = %v, want true", parsed["ok"])
	}
	if parsed["bundle_id"] != "sb_123" {
		t.Errorf("bundle_id = %v, want sb_123", parsed["bundle_id"])
	}
}

// ──────────────────────────── 辅助函数测试 ────────────────────────────

func TestToStringSlice_Nil(t *testing.T) {
	result := toStringSlice(nil)
	if result != nil {
		t.Errorf("toStringSlice(nil) = %v, want nil", result)
	}
}

func TestToStringSlice_ValidSlice(t *testing.T) {
	result := toStringSlice([]any{"a", "b", "c"})
	if len(result) != 3 {
		t.Fatalf("length = %d, want 3", len(result))
	}
	if result[0] != "a" || result[1] != "b" || result[2] != "c" {
		t.Errorf("toStringSlice = %v, want [a b c]", result)
	}
}

func TestGetStr_DefaultValue(t *testing.T) {
	result := getStr(map[string]any{}, "missing", "default")
	if result != "default" {
		t.Errorf("getStr = %q, want %q", result, "default")
	}
}

func TestGetInt_Float64(t *testing.T) {
	result := getInt(map[string]any{"val": float64(42)}, "val", 0)
	if result != 42 {
		t.Errorf("getInt = %d, want 42", result)
	}
}

func TestGetFloat_Int(t *testing.T) {
	result := getFloat(map[string]any{"val": 3}, "val", 0.0)
	if result != 3.0 {
		t.Errorf("getFloat = %f, want 3.0", result)
	}
}
