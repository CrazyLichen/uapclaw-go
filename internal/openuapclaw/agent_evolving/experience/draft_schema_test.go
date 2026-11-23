package experience

import (
	"strings"
	"testing"
)

// TestEvolutionSubject_ToPayload 测试 subject 稳定 payload 形态。
// Python: EvolutionSubject.to_payload (draft_schema.py:43-49)
func TestEvolutionSubject_ToPayload(t *testing.T) {
	s := EvolutionSubject{Kind: "skill", Name: "demo"}
	payload := s.ToPayload()
	if payload["kind"] != "skill" || payload["name"] != "demo" {
		t.Fatalf("payload 不含 kind/name: %v", payload)
	}
	if _, ok := payload["scope"]; ok {
		t.Fatalf("无 scope 时 payload 不应含 scope 键: %v", payload)
	}

	s2 := EvolutionSubject{Kind: "swarm-skill", Name: "demo", Scope: map[string]any{"team": "t1"}}
	payload2 := s2.ToPayload()
	scope, ok := payload2["scope"].(map[string]any)
	if !ok || scope["team"] != "t1" {
		t.Fatalf("scope 未正确承载: %v", payload2)
	}
}

// TestNormalizeSubject 测试 subject 信封归一化。
// Python: normalize_subject (draft_schema.py:64-94)
func TestNormalizeSubject(t *testing.T) {
	t.Run("非对象输入报错", func(t *testing.T) {
		_, err := NormalizeSubject("not-a-dict", nil)
		if err == nil || !strings.Contains(err.Error(), "subject must be an object") {
			t.Fatalf("期望 subject must be an object, got %v", err)
		}
	})

	t.Run("kind 缺失报错", func(t *testing.T) {
		_, err := NormalizeSubject(map[string]any{"name": "x"}, nil)
		if err == nil || !strings.Contains(err.Error(), "subject.kind is required") {
			t.Fatalf("期望 subject.kind is required, got %v", err)
		}
	})

	t.Run("legacy team-skill 别名归一为 swarm-skill", func(t *testing.T) {
		s, err := NormalizeSubject(map[string]any{"kind": "team-skill", "name": "x"}, nil)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if s.Kind != "swarm-skill" {
			t.Fatalf("期望归一为 swarm-skill, got %s", s.Kind)
		}
	})

	t.Run("kind 不在允许集报错并列出原始合法值", func(t *testing.T) {
		_, err := NormalizeSubject(map[string]any{"kind": "agent", "name": "x"}, nil)
		if err == nil || !strings.Contains(err.Error(), "subject.kind must be one of: skill, swarm-skill, team-skill") {
			t.Fatalf("期望 kind 合法集报错, got %v", err)
		}
	})

	t.Run("allowed_kinds 收窄", func(t *testing.T) {
		_, err := NormalizeSubject(map[string]any{"kind": "skill", "name": "x"}, map[string]bool{"swarm-skill": true})
		if err == nil || !strings.Contains(err.Error(), "subject.kind must be one of: swarm-skill") {
			t.Fatalf("期望 allowed 收窄报错, got %v", err)
		}
	})

	t.Run("name 缺失报错", func(t *testing.T) {
		_, err := NormalizeSubject(map[string]any{"kind": "skill", "name": "  "}, nil)
		if err == nil || !strings.Contains(err.Error(), "subject.name is required") {
			t.Fatalf("期望 subject.name is required, got %v", err)
		}
	})

	t.Run("scope 非对象报错", func(t *testing.T) {
		_, err := NormalizeSubject(map[string]any{"kind": "skill", "name": "x", "scope": "bad"}, nil)
		if err == nil || !strings.Contains(err.Error(), "subject.scope must be an object") {
			t.Fatalf("期望 scope 报错, got %v", err)
		}
	})

	t.Run("合法路径 name 去空白", func(t *testing.T) {
		s, err := NormalizeSubject(map[string]any{"kind": " skill ", "name": " x "}, nil)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if s.Kind != "skill" || s.Name != "x" {
			t.Fatalf("期望去空白, got kind=%s name=%s", s.Kind, s.Name)
		}
	})
}

// TestNormalizeEvolveDraft 测试 evolve 草稿归一化。
// Python: normalize_evolve_draft (draft_schema.py:128-180)
func TestNormalizeEvolveDraft(t *testing.T) {
	subject := EvolutionSubject{Kind: "skill", Name: "demo"}

	t.Run("空列表报错", func(t *testing.T) {
		_, err := NormalizeEvolveDraft(subject, nil)
		if err == nil || !strings.Contains(err.Error(), "experiences must be a non-empty list") {
			t.Fatalf("期望非空列表报错, got %v", err)
		}
	})

	t.Run("非对象项报错", func(t *testing.T) {
		_, err := NormalizeEvolveDraft(subject, []any{"bad"})
		if err == nil || !strings.Contains(err.Error(), "experience at index 0 must be an object") {
			t.Fatalf("期望 index 报错, got %v", err)
		}
	})

	t.Run("content 缺失报错", func(t *testing.T) {
		_, err := NormalizeEvolveDraft(subject, []any{map[string]any{"summary": "s"}})
		if err == nil || !strings.Contains(err.Error(), "experience content is required") {
			t.Fatalf("期望 content 报错, got %v", err)
		}
	})

	t.Run("非法 target 报错", func(t *testing.T) {
		_, err := NormalizeEvolveDraft(subject, []any{map[string]any{"summary": "s", "content": "c", "target": "head"}})
		if err == nil || !strings.Contains(err.Error(), "invalid target: head") {
			t.Fatalf("期望 target 报错, got %v", err)
		}
	})

	t.Run("summary 缺失/超限报错", func(t *testing.T) {
		_, err := NormalizeEvolveDraft(subject, []any{map[string]any{"content": "c"}})
		if err == nil || !strings.Contains(err.Error(), "experience summary is required") {
			t.Fatalf("期望 summary 报错, got %v", err)
		}
		long := strings.Repeat("a", 161)
		_, err = NormalizeEvolveDraft(subject, []any{map[string]any{"summary": long, "content": "c"}})
		if err == nil || !strings.Contains(err.Error(), "at most 160 characters") {
			t.Fatalf("期望 summary 超限报错, got %v", err)
		}
	})

	t.Run("section 默认值与校验", func(t *testing.T) {
		d, err := NormalizeEvolveDraft(subject, []any{
			map[string]any{"summary": "s1", "content": "c1", "target": "description"},
			map[string]any{"summary": "s2", "content": "c2", "target": "body"},
			map[string]any{"summary": "s3", "content": "c3", "target": "script"},
			map[string]any{"summary": "s4", "content": "c4", "section": "Examples"},
		})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if d.Experiences[0]["section"] != "Instructions" {
			t.Fatalf("description 默认 section 期望 Instructions, got %v", d.Experiences[0]["section"])
		}
		if d.Experiences[1]["section"] != "Troubleshooting" {
			t.Fatalf("body 默认 section 期望 Troubleshooting, got %v", d.Experiences[1]["section"])
		}
		if d.Experiences[2]["section"] != "Scripts" {
			t.Fatalf("script 固定 section 期望 Scripts, got %v", d.Experiences[2]["section"])
		}
		if d.Experiences[3]["section"] != "Examples" {
			t.Fatalf("显式 section 期望保留, got %v", d.Experiences[3]["section"])
		}
	})

	t.Run("非法 section 报错", func(t *testing.T) {
		_, err := NormalizeEvolveDraft(subject, []any{map[string]any{"summary": "s", "content": "c", "section": "Nope"}})
		if err == nil || !strings.Contains(err.Error(), "invalid section: Nope") {
			t.Fatalf("期望 section 报错, got %v", err)
		}
	})

	t.Run("script_filename 校验", func(t *testing.T) {
		_, err := NormalizeEvolveDraft(subject, []any{map[string]any{"summary": "s", "content": "c", "script_filename": "/abs/run.sh"}})
		if err == nil || !strings.Contains(err.Error(), "script_filename must be a file name") {
			t.Fatalf("期望绝对路径报错, got %v", err)
		}
		_, err = NormalizeEvolveDraft(subject, []any{map[string]any{"summary": "s", "content": "c", "script_filename": ".."}})
		if err == nil || !strings.Contains(err.Error(), "script_filename must be a file name") {
			t.Fatalf("期望 .. 报错, got %v", err)
		}
		_, err = NormalizeEvolveDraft(subject, []any{map[string]any{"summary": "s", "content": "c", "script_filename": "a/b.sh"}})
		if err == nil || !strings.Contains(err.Error(), "script_filename must be a file name") {
			t.Fatalf("期望含目录报错, got %v", err)
		}
		d, err := NormalizeEvolveDraft(subject, []any{map[string]any{"summary": "s", "content": "c", "script_filename": "run.sh"}})
		if err != nil || d.Experiences[0]["script_filename"] != "run.sh" {
			t.Fatalf("期望合法文件名保留, got %v %v", d, err)
		}
	})

	t.Run("script_language/script_purpose/source_refs 保留", func(t *testing.T) {
		d, err := NormalizeEvolveDraft(subject, []any{map[string]any{
			"summary": "s", "content": "c", "script_language": "python", "script_purpose": "deploy",
			"source_refs": []any{"ref1", " ", "ref2"},
		}})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if d.Experiences[0]["script_language"] != "python" || d.Experiences[0]["script_purpose"] != "deploy" {
			t.Fatalf("script 元数据未保留: %v", d.Experiences[0])
		}
		refs, _ := d.Experiences[0]["source_refs"].([]string)
		if len(refs) != 2 || refs[0] != "ref1" || refs[1] != "ref2" {
			t.Fatalf("source_refs 期望去空白保留两项: %v", d.Experiences[0]["source_refs"])
		}
	})

	t.Run("source_refs 非列表报错", func(t *testing.T) {
		_, err := NormalizeEvolveDraft(subject, []any{map[string]any{"summary": "s", "content": "c", "source_refs": "bad"}})
		if err == nil || !strings.Contains(err.Error(), "source_refs must be a list") {
			t.Fatalf("期望 source_refs 报错, got %v", err)
		}
	})
}

// TestNormalizeEvolveDraft_ApprovalPersistenceViews 测试审批/持久化双视图。
// Python: EvolveDraft.approval_view / persistence_view (draft_schema.py:150-163)
func TestNormalizeEvolveDraft_ApprovalPersistenceViews(t *testing.T) {
	subject := EvolutionSubject{Kind: "skill", Name: "demo"}
	d, err := NormalizeEvolveDraft(subject, []any{map[string]any{
		"summary": "s", "content": "c", "source_refs": []any{"ref1"},
	}})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	approval := d.ApprovalView()
	if _, ok := approval["experiences"].([]map[string]any)[0]["source_refs"]; !ok {
		t.Fatalf("审批视图应含 source_refs")
	}
	persist := d.PersistenceView()
	if _, ok := persist["experiences"].([]map[string]any)[0]["source_refs"]; ok {
		t.Fatalf("持久化视图应剔除 source_refs")
	}
}

// TestNormalizeSimplifyDraft 测试精简草稿归一化。
// Python: normalize_simplify_draft (draft_schema.py:182-230)
func TestNormalizeSimplifyDraft(t *testing.T) {
	subject := EvolutionSubject{Kind: "skill", Name: "demo"}

	t.Run("空列表报错", func(t *testing.T) {
		_, err := NormalizeSimplifyDraft(subject, nil)
		if err == nil || !strings.Contains(err.Error(), "actions must be a non-empty list") {
			t.Fatalf("期望非空列表报错, got %v", err)
		}
	})

	t.Run("非法 action 报错", func(t *testing.T) {
		_, err := NormalizeSimplifyDraft(subject, []any{map[string]any{"action": "DROP", "record_id": "r1"}})
		if err == nil || !strings.Contains(err.Error(), "invalid simplify action: DROP") {
			t.Fatalf("期望 action 报错, got %v", err)
		}
	})

	t.Run("action 小写归一大写", func(t *testing.T) {
		d, err := NormalizeSimplifyDraft(subject, []any{map[string]any{"action": "delete", "record_id": "r1"}})
		if err != nil || d.Actions[0]["action"] != "DELETE" {
			t.Fatalf("期望归一为 DELETE, got %v %v", d, err)
		}
	})

	t.Run("record_id 缺失报错", func(t *testing.T) {
		_, err := NormalizeSimplifyDraft(subject, []any{map[string]any{"action": "DELETE"}})
		if err == nil || !strings.Contains(err.Error(), "record_id is required") {
			t.Fatalf("期望 record_id 报错, got %v", err)
		}
	})

	t.Run("REFINE 缺 new_content 报错", func(t *testing.T) {
		_, err := NormalizeSimplifyDraft(subject, []any{map[string]any{"action": "REFINE", "record_id": "r1"}})
		if err == nil || !strings.Contains(err.Error(), "REFINE requires new_content") {
			t.Fatalf("期望 REFINE 报错, got %v", err)
		}
	})

	t.Run("MERGE 缺 new_content/merge_remove_ids 报错", func(t *testing.T) {
		_, err := NormalizeSimplifyDraft(subject, []any{map[string]any{"action": "MERGE", "record_id": "r1"}})
		if err == nil || !strings.Contains(err.Error(), "MERGE requires new_content") {
			t.Fatalf("期望 MERGE new_content 报错, got %v", err)
		}
		_, err = NormalizeSimplifyDraft(subject, []any{map[string]any{"action": "MERGE", "record_id": "r1", "new_content": "c"}})
		if err == nil || !strings.Contains(err.Error(), "MERGE requires merge_remove_ids") {
			t.Fatalf("期望 MERGE merge_remove_ids 报错, got %v", err)
		}
	})

	t.Run("merge_remove_ids 校验", func(t *testing.T) {
		_, err := NormalizeSimplifyDraft(subject, []any{map[string]any{
			"action": "MERGE", "record_id": "r1", "new_content": "c", "merge_remove_ids": []any{"r1"}}})
		if err == nil || !strings.Contains(err.Error(), "merge_remove_ids must not contain record_id") {
			t.Fatalf("期望自包含报错, got %v", err)
		}
		_, err = NormalizeSimplifyDraft(subject, []any{map[string]any{
			"action": "MERGE", "record_id": "r1", "new_content": "c", "merge_remove_ids": []any{"r2", "r2"}}})
		if err == nil || !strings.Contains(err.Error(), "duplicate merge_remove_id") {
			t.Fatalf("期望重复报错, got %v", err)
		}
		d, err := NormalizeSimplifyDraft(subject, []any{map[string]any{
			"action": "MERGE", "record_id": "r1", "new_content": "c", "merge_remove_ids": []any{"r2"}}})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		ids, _ := d.Actions[0]["merge_remove_ids"].([]string)
		if len(ids) != 1 || ids[0] != "r2" {
			t.Fatalf("merge_remove_ids 未保留: %v", d.Actions[0])
		}
	})
}

// TestValidateSimplifyRecordRefs 测试持久化 record 引用校验。
// Python: validate_simplify_record_refs (draft_schema.py:233-243)
func TestValidateSimplifyRecordRefs(t *testing.T) {
	subject := EvolutionSubject{Kind: "skill", Name: "demo"}
	d, err := NormalizeSimplifyDraft(subject, []any{
		map[string]any{"action": "DELETE", "record_id": "r1"},
		map[string]any{"action": "MERGE", "record_id": "r2", "new_content": "c", "merge_remove_ids": []any{"r3"}},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if err := ValidateSimplifyRecordRefs(d, map[string]bool{"r1": true, "r2": true, "r3": true}); err != nil {
		t.Fatalf("全存在时不该报错: %v", err)
	}
	if err := ValidateSimplifyRecordRefs(d, map[string]bool{"r2": true, "r3": true}); err == nil ||
		!strings.Contains(err.Error(), "record not found: r1") {
		t.Fatalf("期望 record not found: r1, got %v", err)
	}
	if err := ValidateSimplifyRecordRefs(d, map[string]bool{"r1": true, "r2": true}); err == nil ||
		!strings.Contains(err.Error(), "record not found: r3") {
		t.Fatalf("期望 record not found: r3, got %v", err)
	}
}
