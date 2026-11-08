package skill

import (
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestSkillManager_EnabledFlag 验证技能启用标志的读写闭环与默认值。
func TestSkillManager_EnabledFlag(t *testing.T) {
	sm := NewSkillManager(t.TempDir())

	t.Run("未配置的技能默认为启用", func(t *testing.T) {
		if !sm.GetSkillEnabled("unknown-skill") {
			t.Fatal("未配置的技能应默认启用（向后兼容）")
		}
	})

	t.Run("禁用后读回为 false", func(t *testing.T) {
		sm.SetSkillEnabled("skill-a", false)
		if sm.GetSkillEnabled("skill-a") {
			t.Fatal("禁用后应读回 false")
		}
	})

	t.Run("重新启用后读回为 true", func(t *testing.T) {
		sm.SetSkillEnabled("skill-a", true)
		if !sm.GetSkillEnabled("skill-a") {
			t.Fatal("重新启用后应读回 true")
		}
	})

	t.Run("空技能名视为启用", func(t *testing.T) {
		if !sm.GetSkillEnabled("") {
			t.Fatal("空名应视为启用")
		}
	})
}

// TestSkillManager_ListDisabledSkills 验证禁用列表的内容与排序。
func TestSkillManager_ListDisabledSkills(t *testing.T) {
	sm := NewSkillManager(t.TempDir())

	t.Run("无禁用时返回空", func(t *testing.T) {
		if got := sm.ListDisabledSkills(); len(got) != 0 {
			t.Fatalf("应返回空，实际 %v", got)
		}
	})

	t.Run("禁用多个后按序返回", func(t *testing.T) {
		sm.SetSkillEnabled("zeta", false)
		sm.SetSkillEnabled("alpha", false)
		got := sm.ListDisabledSkills()
		if len(got) != 2 {
			t.Fatalf("应返回 2 个，实际 %v", got)
		}
		if got[0] != "alpha" || got[1] != "zeta" {
			t.Fatalf("应按字典序返回，实际 %v", got)
		}
	})

	t.Run("重新启用后从列表移除", func(t *testing.T) {
		sm.SetSkillEnabled("alpha", true)
		got := sm.ListDisabledSkills()
		if len(got) != 1 || got[0] != "zeta" {
			t.Fatalf("应只剩 zeta，实际 %v", got)
		}
	})
}

// TestSkillManager_ListExecutionDisabledSkills 验证执行禁用列表（只含已安装技能）。
func TestSkillManager_ListExecutionDisabledSkills(t *testing.T) {
	sm := NewSkillManager(t.TempDir())

	t.Run("无禁用时返回空", func(t *testing.T) {
		if got := sm.ListExecutionDisabledSkills(); len(got) != 0 {
			t.Fatalf("应返回空，实际 %v", got)
		}
	})

	t.Run("禁用未安装技能不进入执行禁用列表", func(t *testing.T) {
		sm.SetSkillEnabled("not-installed-skill", false)
		got := sm.ListExecutionDisabledSkills()
		for _, name := range got {
			if name == "not-installed-skill" {
				t.Fatalf("未安装的技能不应出现在执行禁用列表中：%v", got)
			}
		}
	})
}
