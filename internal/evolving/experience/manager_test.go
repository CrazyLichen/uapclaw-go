package experience

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewExperienceManager 测试 ExperienceManager 创建
func TestNewExperienceManager(t *testing.T) {
	t.Run("不支持的 kind", func(t *testing.T) {
		_, err := NewExperienceManager(nil, nil, "bad_kind", "cn", nil, nil, nil)
		if err == nil {
			t.Errorf("期望返回错误")
		}
		if !strings.Contains(err.Error(), "不支持的体验管理器类型") {
			t.Errorf("错误消息 = %s, 应包含 '不支持的体验管理器类型'", err.Error())
		}
	})
	t.Run("skill kind", func(t *testing.T) {
		mgr, err := NewExperienceManager(nil, nil, "skill", "cn", nil, nil, nil)
		if err != nil {
			t.Errorf("NewExperienceManager 失败: %v", err)
		}
		if mgr.kind != "skill" {
			t.Errorf("kind = %s, 期望 skill", mgr.kind)
		}
		if mgr.language != "cn" {
			t.Errorf("language = %s, 期望 cn", mgr.language)
		}
	})
	t.Run("team-skill kind", func(t *testing.T) {
		mgr, err := NewExperienceManager(nil, nil, "team-skill", "en", nil, nil, nil)
		if err != nil {
			t.Errorf("NewExperienceManager 失败: %v", err)
		}
		if mgr.kind != "team-skill" {
			t.Errorf("kind = %s, 期望 team-skill", mgr.kind)
		}
	})
	t.Run("nil 参数初始化为空 map", func(t *testing.T) {
		mgr, err := NewExperienceManager(nil, nil, "skill", "cn", nil, nil, nil)
		if err != nil {
			t.Errorf("NewExperienceManager 失败: %v", err)
		}
		if len(mgr.skillOps) != 0 {
			t.Errorf("skillOps 应为空 map")
		}
		if len(mgr.pendingGovernance) != 0 {
			t.Errorf("pendingGovernance 应为空 map")
		}
	})
}

// TestExperienceManager_Properties 测试属性访问方法
func TestExperienceManager_Properties(t *testing.T) {
	mgr, _ := NewExperienceManager(nil, nil, "skill", "cn", nil, nil, nil)

	t.Run("PendingApprovalSnapshots", func(t *testing.T) {
		if mgr.PendingApprovalSnapshots() == nil {
			t.Errorf("应返回非 nil map")
		}
	})
	t.Run("PendingGovernance", func(t *testing.T) {
		if mgr.PendingGovernance() == nil {
			t.Errorf("应返回非 nil map")
		}
	})
	t.Run("SkillOps", func(t *testing.T) {
		if mgr.SkillOps() == nil {
			t.Errorf("应返回非 nil map")
		}
	})
}

// TestExperienceManager_BindPendingApprovalSnapshots 测试绑定暂存快照
func TestExperienceManager_BindPendingApprovalSnapshots(t *testing.T) {
	mgr, _ := NewExperienceManager(nil, nil, "skill", "cn", nil, nil, nil)

	t.Run("绑定 nil 重置为空 map", func(t *testing.T) {
		mgr.BindPendingApprovalSnapshots(nil)
		if len(mgr.pendingApprovalSnapshots) != 0 {
			t.Errorf("应重置为空 map")
		}
	})
	t.Run("绑定非 nil map", func(t *testing.T) {
		snapshots := map[string]*PendingChange{"cid": &checkpointing.PendingChange{ChangeID: "cid"}}
		mgr.BindPendingApprovalSnapshots(snapshots)
		if len(mgr.pendingApprovalSnapshots) != 1 {
			t.Errorf("应绑定 1 个条目")
		}
	})
}

// TestExperienceManager_RejectSimplify 测试丢弃治理操作
func TestExperienceManager_RejectSimplify(t *testing.T) {
	mgr, _ := NewExperienceManager(nil, nil, "skill", "cn", nil, nil, map[string]*PendingGovernance{
		"gov1": &PendingGovernance{Kind: "simplify", SkillName: "test"},
	})
	if len(mgr.pendingGovernance) != 1 {
		t.Errorf("初始应有 1 个治理操作")
	}
	mgr.RejectSimplify("gov1")
	if len(mgr.pendingGovernance) != 0 {
		t.Errorf("拒绝后应为空")
	}
}

// TestExperienceManager_getRebuildTemplate 测试获取重建提示词模板
func TestExperienceManager_getRebuildTemplate(t *testing.T) {
	t.Run("skill cn", func(t *testing.T) {
		mgr, _ := NewExperienceManager(nil, nil, "skill", "cn", nil, nil, nil)
		template := mgr.getRebuildTemplate()
		if !strings.Contains(template, "%g") {
			t.Errorf("skill cn template 应包含 %%g")
		}
	})
	t.Run("skill en", func(t *testing.T) {
		mgr, _ := NewExperienceManager(nil, nil, "skill", "en", nil, nil, nil)
		template := mgr.getRebuildTemplate()
		if !strings.Contains(template, "%g") {
			t.Errorf("skill en template 应包含 %%g")
		}
	})
	t.Run("team-skill cn", func(t *testing.T) {
		mgr, _ := NewExperienceManager(nil, nil, "team-skill", "cn", nil, nil, nil)
		template := mgr.getRebuildTemplate()
		if !strings.Contains(template, "%g") {
			t.Errorf("team-skill cn template 应包含 %%g")
		}
	})
	t.Run("未知语言回退到 en", func(t *testing.T) {
		mgr, _ := NewExperienceManager(nil, nil, "skill", "fr", nil, nil, nil)
		template := mgr.getRebuildTemplate()
		if !strings.Contains(template, "skill-creator") {
			t.Errorf("未知语言应回退到 en, 应包含 skill-creator")
		}
	})
}

// TestExperienceManager_getDefaultRebuildIntent 测试获取默认重建意图
func TestExperienceManager_getDefaultRebuildIntent(t *testing.T) {
	t.Run("skill cn", func(t *testing.T) {
		mgr, _ := NewExperienceManager(nil, nil, "skill", "cn", nil, nil, nil)
		intent := mgr.getDefaultRebuildIntent()
		if !strings.Contains(intent, "技能") {
			t.Errorf("skill cn intent 应包含 '技能'")
		}
	})
	t.Run("skill en", func(t *testing.T) {
		mgr, _ := NewExperienceManager(nil, nil, "skill", "en", nil, nil, nil)
		intent := mgr.getDefaultRebuildIntent()
		if !strings.Contains(intent, "skill") {
			t.Errorf("skill en intent 应包含 'skill'")
		}
	})
}

// TestToApplyResult 测试 PendingCommitResult 到 ExperienceApplyResult 转换
func TestToApplyResult(t *testing.T) {
	result := toApplyResult("test_skill", PendingCommitResult{AppliedCount: 3, PendingCount: 1})
	if result.SkillName != "test_skill" {
		t.Errorf("SkillName = %s, 期望 test_skill", result.SkillName)
	}
	if result.AppliedCount != 3 {
		t.Errorf("AppliedCount = %d, 期望 3", result.AppliedCount)
	}
	if result.PendingCount != 1 {
		t.Errorf("PendingCount = %d, 期望 1", result.PendingCount)
	}
}

// TestSetApplyUpdatesFn 测试包级注入函数设置
func TestSetApplyUpdatesFn(t *testing.T) {
	origFn := applyUpdatesFn
	defer func() { applyUpdatesFn = origFn }()

	t.Run("设置和调用", func(t *testing.T) {
		called := false
		SetApplyUpdatesFn(func(_ map[string]operator.Operator, _ map[schema.UpdateKey]any) []schema.ApplyResult {
			called = true
			return nil
		})
		result := evolvingExecuteUpdates(nil, nil)
		if !called {
			t.Errorf("注入函数未被调用")
		}
		if result != nil {
			t.Errorf("期望 nil 结果")
		}
	})
}

// TestDefaultExecuteUpdates 测试默认更新执行逻辑
func TestDefaultExecuteUpdates(t *testing.T) {
	t.Run("UpdateValue 全部 applied", func(t *testing.T) {
		updates := map[schema.UpdateKey]schema.UpdateValue{
			schema.UpdateKey{"op1", schema.ExperiencesTarget}: schema.UpdateValue{},
		}
		results := defaultExecuteUpdates(nil, updates)
		if len(results) != 1 {
			t.Errorf("镀度 = %d, 期望 1", len(results))
		}
	})
}

// TestUpdatesToAnyMap 测试 updatesToAnyMap 转换
func TestUpdatesToAnyMap(t *testing.T) {
	t.Run("非空 map", func(t *testing.T) {
		updates := map[schema.UpdateKey]schema.UpdateValue{
			schema.UpdateKey{"op1", schema.ExperiencesTarget}: schema.UpdateValue{},
		}
		result := updatesToAnyMap(updates)
		if len(result) != 1 {
			t.Errorf("updatesToAnyMap 镀度 = %d, 期望 1", len(result))
		}
	})
	t.Run("空 map", func(t *testing.T) {
		result := updatesToAnyMap(nil)
		if len(result) != 0 {
			t.Errorf("updatesToAnyMap(nil) 镀度 = %d, 期望 0", len(result))
		}
	})
}

// TestPendingGovernance 测试 PendingGovernance 结构体
func TestPendingGovernance(t *testing.T) {
	t.Run("simplify 类型", func(t *testing.T) {
		gov := &PendingGovernance{
			Kind:      "simplify",
			SkillName: "test_skill",
			Actions:   []map[string]any{{"action": "DELETE", "record_id": "r1"}},
		}
		if gov.Kind != "simplify" {
			t.Errorf("Kind = %s, 期望 simplify", gov.Kind)
		}
		if gov.SkillName != "test_skill" {
			t.Errorf("SkillName = %s, 期望 test_skill", gov.SkillName)
		}
		if len(gov.Actions) != 1 {
			t.Errorf("Actions 镀度 = %d, 期望 1", len(gov.Actions))
		}
	})
}
