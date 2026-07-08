package skills

import (
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewSkill 创建 Skill 实例
func TestNewSkill(t *testing.T) {
	s := NewSkill("translate", "翻译技能", "/skills/translate")
	if s.Name != "translate" {
		t.Errorf("期望 Name=translate，实际 %s", s.Name)
	}
	if s.Description != "翻译技能" {
		t.Errorf("期望 Description=翻译技能，实际 %s", s.Description)
	}
	if s.Directory != "/skills/translate" {
		t.Errorf("期望 Directory=/skills/translate，实际 %s", s.Directory)
	}
}

// TestSkill_AsDict_含目录 转字典（含目录）
func TestSkill_AsDict_含目录(t *testing.T) {
	s := NewSkill("translate", "翻译技能", "/skills/translate")
	d := s.AsDict(true)
	if d["name"] != "translate" {
		t.Errorf("期望 name=translate，实际 %v", d["name"])
	}
	if d["description"] != "翻译技能" {
		t.Errorf("期望 description=翻译技能，实际 %v", d["description"])
	}
	if d["directory"] != "/skills/translate" {
		t.Errorf("期望 directory=/skills/translate，实际 %v", d["directory"])
	}
}

// TestSkill_AsDict_不含目录 转字典（不含目录）
func TestSkill_AsDict_不含目录(t *testing.T) {
	s := NewSkill("translate", "翻译技能", "/skills/translate")
	d := s.AsDict(false)
	if _, ok := d["directory"]; ok {
		t.Error("期望不含 directory 字段")
	}
}

// TestSkill_String 多行可读格式
func TestSkill_String(t *testing.T) {
	s := NewSkill("translate", "翻译技能", "/skills/translate")
	str := s.String()
	if str != "Skill: translate\nDescription: 翻译技能\nDirectory: /skills/translate" {
		t.Errorf("String() 输出不符合预期: %s", str)
	}
}

// TestSkill_GoString_短描述 单行紧凑格式（短描述不截断）
func TestSkill_GoString_短描述(t *testing.T) {
	s := NewSkill("translate", "翻译", "/skills/translate")
	gs := s.GoString()
	expected := "[Skill: translate / Description: 翻译 / Directory: /skills/translate]"
	if gs != expected {
		t.Errorf("GoString() 输出不符合预期: %s", gs)
	}
}

// TestSkill_GoString_长描述 单行紧凑格式（长描述截断至30字符）
func TestSkill_GoString_长描述(t *testing.T) {
	longDesc := "这是一个非常非常非常非常非常非常非常非常非常非常非常非常长的描述"
	s := NewSkill("translate", longDesc, "/skills/translate")
	gs := s.GoString()
	// 截断后描述长度为 30 + "..."
	if len(s.Description) > descriptionTruncateLen {
		if !contains(gs, "...") {
			t.Error("期望包含截断标记 ...")
		}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestTruncateDescription_短描述 短描述不截断
func TestTruncateDescription_短描述(t *testing.T) {
	desc := "翻译技能"
	got := truncateDescription(desc)
	if got != desc {
		t.Errorf("短描述不应截断: got %q, want %q", got, desc)
	}
}

// TestTruncateDescription_长描述 长描述截断至30字符
func TestTruncateDescription_长描述(t *testing.T) {
	desc := "这是一个非常非常非常非常非常非常非常非常非常非常非常非常长的描述"
	got := truncateDescription(desc)
	if len(got) != descriptionTruncateLen+3 { // 30 + len("...")
		t.Errorf("长描述应截断至 %d+3 字符: got len=%d, content=%q", descriptionTruncateLen, len(got), got)
	}
	if got[:descriptionTruncateLen] != desc[:descriptionTruncateLen] {
		t.Errorf("截断前缀不匹配: got %q, want %q", got[:descriptionTruncateLen], desc[:descriptionTruncateLen])
	}
	if got[descriptionTruncateLen:] != "..." {
		t.Errorf("截断后缀应为 ...: got %q", got[descriptionTruncateLen:])
	}
}

// TestTruncateDescription_恰好30字符 恰好30字符不截断
func TestTruncateDescription_恰好30字符(t *testing.T) {
	desc := "123456789012345678901234567890" // 30 chars
	got := truncateDescription(desc)
	if got != desc {
		t.Errorf("恰好30字符不应截断: got %q, want %q", got, desc)
	}
}

// contains 检查字符串是否包含子串
func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
