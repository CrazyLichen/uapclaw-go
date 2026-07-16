package skill_call

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

func TestNewSkillExperienceOperator(t *testing.T) {
	op := NewSkillExperienceOperator("my_skill")
	if op.OperatorID() != "skill_experience_my_skill" {
		t.Errorf("operatorID = %q, expected %q", op.OperatorID(), "skill_experience_my_skill")
	}
}

func TestSkillExperienceOperator_GetTunables(t *testing.T) {
	op := NewSkillExperienceOperator("my_skill")
	tunables := op.GetTunables()
	spec, ok := tunables[experiencesTarget]
	if !ok {
		t.Fatal("experiences tunable missing")
	}
	if spec.Kind != operator.TunableKindSkillExperience {
		t.Errorf("kind = %v, expected %v", spec.Kind, operator.TunableKindSkillExperience)
	}
	if spec.Path != "content" {
		t.Errorf("path = %q, expected %q", spec.Path, "content")
	}
}

func TestSkillExperienceOperator_SetParameter(t *testing.T) {
	t.Run("正确目标", func(t *testing.T) {
		var capturedValue any
		cb := operator.ParameterUpdatedCallback(func(_ string, value any) {
			capturedValue = value
		})
		op := NewSkillExperienceOperator("my_skill", WithSkillOnParameterUpdated(cb))
		op.SetParameter(experiencesTarget, []any{"record1"})
		if capturedValue == nil {
			t.Error("callback should have been called")
		}
	})

	t.Run("错误目标被忽略", func(t *testing.T) {
		called := false
		cb := operator.ParameterUpdatedCallback(func(string, any) { called = true })
		op := NewSkillExperienceOperator("my_skill", WithSkillOnParameterUpdated(cb))
		op.SetParameter("wrong_target", "value")
		if called {
			t.Error("callback should not be called for wrong target")
		}
	})

	t.Run("nil值被忽略", func(t *testing.T) {
		called := false
		cb := operator.ParameterUpdatedCallback(func(string, any) { called = true })
		op := NewSkillExperienceOperator("my_skill", WithSkillOnParameterUpdated(cb))
		op.SetParameter(experiencesTarget, nil)
		if called {
			t.Error("callback should not be called for nil value")
		}
	})

	t.Run("非列表值包装为列表", func(t *testing.T) {
		var capturedValue any
		cb := operator.ParameterUpdatedCallback(func(_ string, value any) {
			capturedValue = value
		})
		op := NewSkillExperienceOperator("my_skill", WithSkillOnParameterUpdated(cb))
		op.SetParameter(experiencesTarget, "single_item")
		items, ok := capturedValue.([]any)
		if !ok {
			t.Fatal("captured value should be []any")
		}
		if len(items) != 1 || items[0] != "single_item" {
			t.Errorf("items = %v, expected [single_item]", items)
		}
	})
}

func TestSkillExperienceOperator_PreviewUpdate(t *testing.T) {
	t.Run("append+pending_change成功", func(t *testing.T) {
		op := NewSkillExperienceOperator("my_skill")
		update := schema.NewUpdateValue([]any{"record1", "record2"},
			schema.WithUpdateMode(schema.UpdateModeAppend),
			schema.WithUpdateEffect(schema.UpdateEffectPendingChange),
		)
		result := op.PreviewUpdate(experiencesTarget, update)
		if !result.Applied {
			t.Error("should be applied")
		}
		if result.OperatorID != "skill_experience_my_skill" {
			t.Errorf("operatorID = %q, expected %q", result.OperatorID, "skill_experience_my_skill")
		}
		if len(result.Records) != 2 {
			t.Errorf("records count = %d, expected 2", len(result.Records))
		}
		if result.LifecycleStage == nil || *result.LifecycleStage != schema.LocalApplyCompleted {
			t.Errorf("lifecycleStage = %v, expected %q", result.LifecycleStage, schema.LocalApplyCompleted)
		}
		if result.Metadata["skill_name"] != "my_skill" {
			t.Errorf("metadata skill_name = %v, expected %q", result.Metadata["skill_name"], "my_skill")
		}
	})

	t.Run("merge+pending_change成功", func(t *testing.T) {
		op := NewSkillExperienceOperator("my_skill")
		update := schema.NewUpdateValue([]any{"record1"},
			schema.WithUpdateMode(schema.UpdateModeMerge),
			schema.WithUpdateEffect(schema.UpdateEffectPendingChange),
		)
		result := op.PreviewUpdate(experiencesTarget, update)
		if !result.Applied {
			t.Error("should be applied for merge+pending_change")
		}
	})

	t.Run("不支持的目标", func(t *testing.T) {
		op := NewSkillExperienceOperator("my_skill")
		update := schema.NewUpdateValue("data",
			schema.WithUpdateMode(schema.UpdateModeAppend),
			schema.WithUpdateEffect(schema.UpdateEffectPendingChange),
		)
		result := op.PreviewUpdate("wrong_target", update)
		if result.Applied {
			t.Error("should not be applied for wrong target")
		}
		if len(result.Errors) == 0 {
			t.Error("should have errors")
		}
	})

	t.Run("不支持的mode", func(t *testing.T) {
		op := NewSkillExperienceOperator("my_skill")
		update := schema.NewUpdateValue("data",
			schema.WithUpdateMode(schema.UpdateModeReplace),
			schema.WithUpdateEffect(schema.UpdateEffectPendingChange),
		)
		result := op.PreviewUpdate(experiencesTarget, update)
		if result.Applied {
			t.Error("should not be applied for replace mode")
		}
	})

	t.Run("不支持的effect", func(t *testing.T) {
		op := NewSkillExperienceOperator("my_skill")
		update := schema.NewUpdateValue("data",
			schema.WithUpdateMode(schema.UpdateModeAppend),
			schema.WithUpdateEffect(schema.UpdateEffectState),
		)
		result := op.PreviewUpdate(experiencesTarget, update)
		if result.Applied {
			t.Error("should not be applied for state effect")
		}
	})

	t.Run("空records未应用", func(t *testing.T) {
		op := NewSkillExperienceOperator("my_skill")
		update := schema.NewUpdateValue([]any{},
			schema.WithUpdateMode(schema.UpdateModeAppend),
			schema.WithUpdateEffect(schema.UpdateEffectPendingChange),
		)
		result := op.PreviewUpdate(experiencesTarget, update)
		if result.Applied {
			t.Error("should not be applied for empty records")
		}
	})

	t.Run("metadata合并", func(t *testing.T) {
		op := NewSkillExperienceOperator("my_skill")
		update := schema.NewUpdateValue([]any{"r1"},
			schema.WithUpdateMode(schema.UpdateModeAppend),
			schema.WithUpdateEffect(schema.UpdateEffectPendingChange),
			schema.WithUpdateMetadata(map[string]any{"custom_key": "custom_value"}),
		)
		result := op.PreviewUpdate(experiencesTarget, update)
		if result.Metadata["custom_key"] != "custom_value" {
			t.Error("custom metadata should be preserved")
		}
		if result.Metadata["skill_name"] != "my_skill" {
			t.Error("skill_name should be added")
		}
	})
}

func TestSkillExperienceOperator_GetState(t *testing.T) {
	op := NewSkillExperienceOperator("my_skill")
	state := op.GetState()
	if len(state) != 0 {
		t.Errorf("state should be empty, got %v", state)
	}
}

func TestSkillExperienceOperator_LoadState(t *testing.T) {
	op := NewSkillExperienceOperator("my_skill")
	// LoadState 是空操作，不应 panic
	op.LoadState(map[string]any{"key": "value"})
}

func TestSkillExperienceOperator_ApplyUpdate(t *testing.T) {
	// ApplyUpdate 应路由到 PreviewUpdate
	op := NewSkillExperienceOperator("my_skill")
	update := schema.NewUpdateValue([]any{"record1"},
		schema.WithUpdateMode(schema.UpdateModeAppend),
		schema.WithUpdateEffect(schema.UpdateEffectPendingChange),
	)
	result := op.ApplyUpdate(experiencesTarget, update)
	if !result.Applied {
		t.Error("ApplyUpdate should route to PreviewUpdate and be applied")
	}
	if result.LifecycleStage == nil || *result.LifecycleStage != schema.LocalApplyCompleted {
		t.Error("should have local_apply_completed lifecycle stage")
	}
}

func TestSkillCallOperatorAlias(t *testing.T) {
	// 验证类型别名
	var op SkillCallOperator = *NewSkillExperienceOperator("test")
	if op.OperatorID() != "skill_experience_test" {
		t.Errorf("alias type should work, got %q", op.OperatorID())
	}
}
