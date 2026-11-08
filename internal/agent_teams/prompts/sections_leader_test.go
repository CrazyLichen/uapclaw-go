package prompts

import (
	"strings"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBuildLeaderBootstrapSection_swarmflow开关 验证 {{collaboration_mechanism}} 槽位。
// Python: build_leader_bootstrap_section (sections.py L151-188)
func TestBuildLeaderBootstrapSection_swarmflow开关(t *testing.T) {
	withSwarm, err := BuildLeaderBootstrapSection(true, "cn")
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	without, err := BuildLeaderBootstrapSection(false, "cn")
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if withSwarm.Render("cn") == without.Render("cn") {
		t.Fatal("swarmflow 开闭应产生不同文本")
	}
	if strings.Contains(without.Render("cn"), "{{collaboration_mechanism}}") {
		t.Fatal("槽位应已被替换为空串")
	}
}

// TestBuildLeaderBootstrapSection_优先级 验证 P:11 与 section 名。
func TestBuildLeaderBootstrapSection_优先级(t *testing.T) {
	s, err := BuildLeaderBootstrapSection(false, "cn")
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if s.Name != SectionBootstrap {
		t.Fatalf("section 名应为 %s，实际 %s", SectionBootstrap, s.Name)
	}
	if s.Priority != 11 {
		t.Fatalf("priority 应为 11（硬编码），实际 %d", s.Priority)
	}
	body := s.Render("cn")
	if !strings.HasSuffix(body, "\n") {
		t.Fatal("bootstrap body 应以换行结尾")
	}
}

// TestBuildLeaderPrebuiltSection 验证 prebuilt 段。
// Python: build_leader_prebuilt_section (sections.py L222-252)
func TestBuildLeaderPrebuiltSection(t *testing.T) {
	s, err := BuildLeaderPrebuiltSection("cn")
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if s.Name != SectionPrebuilt {
		t.Fatalf("section 名应为 %s，实际 %s", SectionPrebuilt, s.Name)
	}
	if s.Priority != 11 {
		t.Fatalf("priority 应为 11，实际 %d", s.Priority)
	}
	if strings.Contains(s.Render("cn"), "collaboration_mechanism") {
		t.Fatal("prebuilt 无 collaboration_mechanism 槽（本模式没有 build_team 路径）")
	}

	en, err := BuildLeaderPrebuiltSection("en")
	if err != nil {
		t.Fatalf("en 返回错误: %v", err)
	}
	if strings.Contains(en.Render("en"), "团队已由系统建好") {
		t.Fatal("en 不应含中文")
	}
}

// TestBuildLeaderBoundGroupSection 验证 {{group}} 替换。
// Python: build_leader_bound_group_section (sections.py L190-220)
func TestBuildLeaderBoundGroupSection(t *testing.T) {
	s, err := BuildLeaderBoundGroupSection("my_group", "cn")
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if s.Name != SectionBoundGroup {
		t.Fatalf("section 名应为 %s，实际 %s", SectionBoundGroup, s.Name)
	}
	if s.Priority != 12 {
		t.Fatalf("priority 应为 12，实际 %d", s.Priority)
	}
	if !strings.Contains(s.Render("cn"), "my_group") {
		t.Fatal("应替换 {{group}}")
	}
	if strings.Contains(s.Render("cn"), "{{group}}") {
		t.Fatal("占位符应已被替换")
	}
}
