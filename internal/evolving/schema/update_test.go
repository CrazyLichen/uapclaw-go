package schema

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

func TestUpdateKey(t *testing.T) {
	k := UpdateKey{"op1", "target1"}
	if k.OperatorID() != "op1" {
		t.Errorf("got %q, expected %q", k.OperatorID(), "op1")
	}
	if k.Target() != "target1" {
		t.Errorf("got %q, expected %q", k.Target(), "target1")
	}
}

func TestUpdateModeConstants(t *testing.T) {
	if UpdateModeReplace != "replace" {
		t.Errorf("UpdateModeReplace = %q, expected %q", UpdateModeReplace, "replace")
	}
	if UpdateModeAppend != "append" {
		t.Errorf("UpdateModeAppend = %q, expected %q", UpdateModeAppend, "append")
	}
	if UpdateModeMerge != "merge" {
		t.Errorf("UpdateModeMerge = %q, expected %q", UpdateModeMerge, "merge")
	}
}

func TestUpdateEffectConstants(t *testing.T) {
	if UpdateEffectState != "state" {
		t.Errorf("UpdateEffectState = %q, expected %q", UpdateEffectState, "state")
	}
	if UpdateEffectPendingChange != "pending_change" {
		t.Errorf("UpdateEffectPendingChange = %q, expected %q", UpdateEffectPendingChange, "pending_change")
	}
}

func TestUpdateValue(t *testing.T) {
	t.Run("NewUpdateValue默认值", func(t *testing.T) {
		uv := NewUpdateValue("hello")
		if uv.Mode != UpdateModeReplace {
			t.Errorf("default Mode = %v, expected %v", uv.Mode, UpdateModeReplace)
		}
		if uv.Effect != UpdateEffectState {
			t.Errorf("default Effect = %v, expected %v", uv.Effect, UpdateEffectState)
		}
		if uv.ChangeType != nil {
			t.Errorf("default ChangeType should be nil")
		}
	})

	t.Run("选项", func(t *testing.T) {
		uv := NewUpdateValue("hello",
			WithUpdateMode(UpdateModeAppend),
			WithUpdateEffect(UpdateEffectPendingChange),
			WithChangeType("test_type"),
			WithUpdateMetadata(map[string]any{"key": "val"}),
		)
		if uv.Mode != UpdateModeAppend {
			t.Errorf("mode = %v, expected %v", uv.Mode, UpdateModeAppend)
		}
		if uv.Effect != UpdateEffectPendingChange {
			t.Errorf("effect = %v, expected %v", uv.Effect, UpdateEffectPendingChange)
		}
		if uv.ChangeType == nil || *uv.ChangeType != "test_type" {
			t.Errorf("changeType = %v, expected %q", uv.ChangeType, "test_type")
		}
		if uv.Metadata["key"] != "val" {
			t.Errorf("metadata[key] = %v, expected %q", uv.Metadata["key"], "val")
		}
	})
}

func TestApplyResult_Ok(t *testing.T) {
	t.Run("已应用且无错误", func(t *testing.T) {
		r := ApplyResult{Applied: true, Errors: []string{}}
		if !r.Ok() {
			t.Error("expected Ok() = true")
		}
	})
	t.Run("未应用", func(t *testing.T) {
		r := ApplyResult{Applied: false, Errors: []string{}}
		if r.Ok() {
			t.Error("expected Ok() = false")
		}
	})
	t.Run("有错误", func(t *testing.T) {
		r := ApplyResult{Applied: true, Errors: []string{"some error"}}
		if r.Ok() {
			t.Error("expected Ok() = false")
		}
	})
}

func TestNormalizeUpdateValue(t *testing.T) {
	t.Run("已是UpdateValue直接返回", func(t *testing.T) {
		uv := UpdateValue{Payload: "test", Mode: UpdateModeAppend}
		result := NormalizeUpdateValue(uv, "")
		if result.Mode != UpdateModeAppend {
			t.Errorf("mode = %v, expected %v", result.Mode, UpdateModeAppend)
		}
	})
	t.Run("experiences目标", func(t *testing.T) {
		result := NormalizeUpdateValue("some_data", ExperiencesTarget)
		if result.Mode != UpdateModeAppend {
			t.Errorf("mode = %v, expected %v", result.Mode, UpdateModeAppend)
		}
		if result.Effect != UpdateEffectPendingChange {
			t.Errorf("effect = %v, expected %v", result.Effect, UpdateEffectPendingChange)
		}
		if result.ChangeType == nil || *result.ChangeType != SkillExperienceEntry {
			t.Errorf("changeType = %v, expected %v", result.ChangeType, SkillExperienceEntry)
		}
	})
	t.Run("其他目标默认replace+state", func(t *testing.T) {
		result := NormalizeUpdateValue("some_data", "other")
		if result.Mode != UpdateModeReplace {
			t.Errorf("mode = %v, expected %v", result.Mode, UpdateModeReplace)
		}
		if result.Effect != UpdateEffectState {
			t.Errorf("effect = %v, expected %v", result.Effect, UpdateEffectState)
		}
	})
}

func TestNormalizeUpdates(t *testing.T) {
	updates := map[UpdateKey]any{
		{"op1", "experiences"}:   "record1",
		{"op2", "system_prompt"}: "new_prompt",
	}
	result := NormalizeUpdates(updates)

	expResult := result[UpdateKey{"op1", "experiences"}]
	if expResult.Mode != UpdateModeAppend {
		t.Errorf("experiences mode = %v, expected %v", expResult.Mode, UpdateModeAppend)
	}

	promptResult := result[UpdateKey{"op2", "system_prompt"}]
	if promptResult.Mode != UpdateModeReplace {
		t.Errorf("prompt mode = %v, expected %v", promptResult.Mode, UpdateModeReplace)
	}
}

func TestNewApplyResult(t *testing.T) {
	r := NewApplyResult("op1", "target1", true, UpdateModeReplace, UpdateEffectState, "val")
	if r.OperatorID != "op1" {
		t.Errorf("operatorID = %q, expected %q", r.OperatorID, "op1")
	}
	if r.Target != "target1" {
		t.Errorf("target = %q, expected %q", r.Target, "target1")
	}
	if !r.Applied {
		t.Error("applied should be true")
	}
}

func TestApplyResultWithErrors(t *testing.T) {
	r := ApplyResultWithErrors("op1", "target1", UpdateModeAppend, UpdateEffectPendingChange, "val", "err1", "err2")
	if r.Applied {
		t.Error("applied should be false")
	}
	if len(r.Errors) != 2 {
		t.Errorf("errors count = %d, expected 2", len(r.Errors))
	}
}

func TestMetadataClone(t *testing.T) {
	t.Run("nil map", func(t *testing.T) {
		result := MetadataClone(nil)
		if result == nil {
			t.Error("result should not be nil")
		}
		if len(result) != 0 {
			t.Error("result should be empty")
		}
	})
	t.Run("非空map克隆", func(t *testing.T) {
		original := map[string]any{"key": "value"}
		cloned := MetadataClone(original)
		cloned["key"] = "changed"
		if original["key"] != "value" {
			t.Error("original should not be modified")
		}
	})
}
