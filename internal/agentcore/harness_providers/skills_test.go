package harness_providers

import (
	"strings"
	"testing"
)

// TestNewSkillSource_构造校验 对齐 __post_init__（skills.py:31-40）。
func TestNewSkillSource_构造校验(t *testing.T) {
	if _, err := NewSkillSource("", "all", nil); err == nil || !strings.Contains(err.Error(), "non-empty") {
		t.Fatalf("空 dir 应报错: %v", err)
	}
	if _, err := NewSkillSource("/d", "merge", nil); err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("非法 mode 应报错: %v", err)
	}
	s, err := NewSkillSource("/d", "auto_list", []string{"a", "b"})
	if err != nil || s.Dir != "/d" || s.Mode != "auto_list" || len(s.EnabledSkills) != 2 {
		t.Fatalf("合法构造失败: %v %+v", err, s)
	}
}

// TestNormalizeSkills_三形态 对齐 normalize_skills（skills.py:43-59）。
func TestNormalizeSkills_三形态(t *testing.T) {
	if _, err := NormalizeSkills(nil, "merge"); err == nil {
		t.Fatal("非法 conflict 应报错")
	}
	src, _ := NewSkillSource("/d", "all", nil)
	out, err := NormalizeSkills([]any{src, "/x", map[string]any{"dir": "/y"}}, "skip")
	if err != nil || len(out) != 3 || out[0].Dir != "/d" || out[1].Dir != "/x" || out[2].Dir != "/y" {
		t.Fatalf("三形态归一失败: %v %+v", err, out)
	}
	if _, err := NormalizeSkills([]any{42}, "skip"); err == nil {
		t.Fatal("非法元素应报错")
	}
}
