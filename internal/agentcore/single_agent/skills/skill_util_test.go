package skills

import (
	"strings"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewSkillUtil 创建 SkillUtil 实例
func TestNewSkillUtil(t *testing.T) {
	su := NewSkillUtil("op-123")
	if su.SkillManager() == nil {
		t.Error("期望 SkillManager 非空")
	}
	if su.RemoteSkillUtil() == nil {
		t.Error("期望 RemoteSkillUtil 非空")
	}
}

// TestSkillUtil_SetSysOperationID 更新系统操作 ID
func TestSkillUtil_SetSysOperationID(t *testing.T) {
	su := NewSkillUtil("old-id")
	su.SetSysOperationID("new-id")
	if su.SkillManager().sysOperationID != "new-id" {
		t.Errorf("期望 SkillManager sysOperationID=new-id")
	}
	if su.RemoteSkillUtil().sysOperationID != "new-id" {
		t.Errorf("期望 RemoteSkillUtil sysOperationID=new-id")
	}
}

// TestSkillUtil_RegisterSkills 注册技能
func TestSkillUtil_RegisterSkills(t *testing.T) {
	provider := newMockFsProvider()
	provider.dirs["/skills/translate"] = []DirInfo{}
	provider.dirFiles["/skills/translate"] = []FileInfo{{Name: "SKILL.md", Path: "/skills/translate/SKILL.md"}}
	provider.files["/skills/translate/SKILL.md"] = "---\ndescription: 翻译技能\n---\n"

	su := NewSkillUtilWithProvider("op-123", provider)
	err := su.RegisterSkills([]string{"/skills/translate"}, false)
	if err != nil {
		t.Fatalf("RegisterSkills 失败: %v", err)
	}
	if !su.HasSkill() {
		t.Error("期望 HasSkill()=true")
	}
}

// TestSkillUtil_HasSkill_无技能 无技能时返回 false
func TestSkillUtil_HasSkill_无技能(t *testing.T) {
	su := NewSkillUtil("op-123")
	if su.HasSkill() {
		t.Error("期望 HasSkill()=false")
	}
}

// TestSkillUtil_GetSkillPrompt 生成技能提示词
func TestSkillUtil_GetSkillPrompt(t *testing.T) {
	provider := newMockFsProvider()
	provider.dirs["/skills/translate"] = []DirInfo{}
	provider.dirFiles["/skills/translate"] = []FileInfo{{Name: "SKILL.md", Path: "/skills/translate/SKILL.md"}}
	provider.files["/skills/translate/SKILL.md"] = "---\ndescription: 翻译技能\n---\n"

	su := NewSkillUtilWithProvider("op-123", provider)
	err := su.RegisterSkills([]string{"/skills/translate"}, false)
	if err != nil {
		t.Fatalf("RegisterSkills 失败: %v", err)
	}

	prompt := su.GetSkillPrompt()
	// 检查提示词包含关键部分
	if !strings.Contains(prompt, "You are an agent equipped with various skills") {
		t.Error("提示词缺少系统前缀")
	}
	if !strings.Contains(prompt, "Skill name: translate") {
		t.Error("提示词缺少技能名称")
	}
	if !strings.Contains(prompt, "Skill description: 翻译技能") {
		t.Error("提示词缺少技能描述")
	}
	if !strings.Contains(prompt, "read_file") {
		t.Error("提示词缺少 read_file 引用")
	}
	if !strings.Contains(prompt, "SKILL.md") {
		t.Error("提示词缺少 SKILL.md 引用")
	}
}

// TestSkillUtil_GetSkillPrompt_多技能 多技能时格式正确
func TestSkillUtil_GetSkillPrompt_多技能(t *testing.T) {
	provider := newMockFsProvider()
	provider.dirs["/skills/translate"] = []DirInfo{}
	provider.dirs["/skills/code-review"] = []DirInfo{}
	provider.dirFiles["/skills/translate"] = []FileInfo{{Name: "SKILL.md", Path: "/skills/translate/SKILL.md"}}
	provider.dirFiles["/skills/code-review"] = []FileInfo{{Name: "SKILL.md", Path: "/skills/code-review/SKILL.md"}}
	provider.files["/skills/translate/SKILL.md"] = "---\ndescription: 翻译\n---\n"
	provider.files["/skills/code-review/SKILL.md"] = "---\ndescription: 代码审查\n---\n"

	su := NewSkillUtilWithProvider("op-123", provider)
	err := su.RegisterSkills([]string{"/skills/translate", "/skills/code-review"}, false)
	if err != nil {
		t.Fatalf("RegisterSkills 失败: %v", err)
	}

	prompt := su.GetSkillPrompt()
	if !strings.Contains(prompt, "code-review") {
		t.Error("提示词缺少 code-review 技能")
	}
	if !strings.Contains(prompt, "translate") {
		t.Error("提示词缺少 translate 技能")
	}
}

// TestSkillUtil_GetSkillPrompt_无技能 无技能时仍返回系统前缀
func TestSkillUtil_GetSkillPrompt_无技能(t *testing.T) {
	su := NewSkillUtil("op-123")
	prompt := su.GetSkillPrompt()
	// 无技能时，模板中 skills 为空字符串
	if !strings.Contains(prompt, "You are an agent equipped with various skills") {
		t.Error("无技能时提示词仍应包含系统前缀")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
