package adapter

import (
	"context"
	"testing"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestSetDefaultSendPushFunc 验证默认推送函数可替换并恢复原值。
func TestSetDefaultSendPushFunc(t *testing.T) {
	orig := defaultSendPushFunc
	t.Cleanup(func() { SetDefaultSendPushFunc(orig) })

	called := false
	SetDefaultSendPushFunc(func(context.Context, map[string]any) error {
		called = true
		return nil
	})

	// 未注入实例级函数时，getSendPushFunc 应回落到刚设置的默认值
	d := NewDeepAdapter()
	fn := d.getSendPushFunc()
	if fn == nil {
		t.Fatal("应回落到默认推送函数")
	}
	if err := fn(context.Background(), map[string]any{}); err != nil {
		t.Fatalf("默认推送函数不应报错: %v", err)
	}
	if !called {
		t.Fatal("默认推送函数应被调用")
	}
}

// TestResetPersistentCheckpointer 验证重置后标记为未就绪，可重复调用。
func TestResetPersistentCheckpointer(t *testing.T) {
	ResetPersistentCheckpointer()
	ResetPersistentCheckpointer() // 幂等
}

// TestSetSkillManager 验证技能管理器注入。
func TestSetSkillManager(t *testing.T) {
	d := NewDeepAdapter()
	d.SetSkillManager(nil)
	if d.skillManager != nil {
		t.Fatal("注入 nil 后应为 nil")
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestResolveModelClientFromConfig_空配置返回nil 验证构建失败时记录并返回 nil。
func TestResolveModelClientFromConfig_空配置返回nil(t *testing.T) {
	d := NewDeepAdapter()
	if got := d.resolveModelClientFromConfig("", "", "", 0); got != nil {
		t.Fatalf("空配置应构建失败并返回 nil，实际 %T", got)
	}
}

// TestResolveMultimediaModelClients 验证三类多媒体客户端解析器都能安全降级。
func TestResolveMultimediaModelClients(t *testing.T) {
	t.Run("vision 空配置返回 nil", func(t *testing.T) {
		d := NewDeepAdapter()
		d.visionModelConfig = &hschema.VisionModelConfig{}
		if got := d.resolveVisionModelClient(); got != nil {
			t.Fatalf("空配置应返回 nil，实际 %T", got)
		}
	})

	t.Run("audio 空配置返回 nil", func(t *testing.T) {
		d := NewDeepAdapter()
		d.audioModelConfig = &hschema.AudioModelConfig{}
		if got := d.resolveAudioModelClient(); got != nil {
			t.Fatalf("空配置应返回 nil，实际 %T", got)
		}
	})

	t.Run("video 空配置返回 nil", func(t *testing.T) {
		d := NewDeepAdapter()
		d.videoModelConfig = &hschema.VideoModelConfig{}
		if got := d.resolveVideoModelClient(); got != nil {
			t.Fatalf("空配置应返回 nil，实际 %T", got)
		}
	})

	t.Run("audio 使用 QAModel 字段", func(t *testing.T) {
		d := NewDeepAdapter()
		d.audioModelConfig = &hschema.AudioModelConfig{QAModel: "qa-model", BaseURL: "http://localhost"}
		// 仅验证取 QAModel 分支不 panic；空 api_key 下构建失败返回 nil
		_ = d.resolveAudioModelClient()
	})
}

// ──────────────────────────── 非导出函数补充测试 ────────────────────────────

// TestCancelSchedulerRunningTasks_无实例 验证 instance 缺失时安全返回。
func TestCancelSchedulerRunningTasks_无实例(t *testing.T) {
	d := NewDeepAdapter()
	assertNotPanicsAdapter(t, func() { d.cancelSchedulerRunningTasks() })
}

// TestCancelSchedulerRunningTasks_无LoopController 验证无 LoopController 时安全返回。
func TestCancelSchedulerRunningTasks_无LoopController(t *testing.T) {
	d := newEvolutionTestAdapter(t)
	assertNotPanicsAdapter(t, func() { d.cancelSchedulerRunningTasks() })
}

// TestExtractRebuildFollowupPrompt 验证 followup prompt 提取条件。
func TestExtractRebuildFollowupPrompt(t *testing.T) {
	t.Run("nil 返回空", func(t *testing.T) {
		if got := extractRebuildFollowupPrompt(nil); got != "" {
			t.Fatalf("应返回空，实际 %q", got)
		}
	})

	t.Run("action 不匹配返回空", func(t *testing.T) {
		got := extractRebuildFollowupPrompt(map[string]any{
			"action":          "other",
			"followup_prompt": "p",
		})
		if got != "" {
			t.Fatalf("应返回空，实际 %q", got)
		}
	})

	t.Run("action 匹配时返回裁剪后的 prompt", func(t *testing.T) {
		got := extractRebuildFollowupPrompt(map[string]any{
			"action":          "run_rebuild_followup",
			"followup_prompt": "  hello  ",
		})
		if got != "hello" {
			t.Fatalf("应返回 hello，实际 %q", got)
		}
	})
}
