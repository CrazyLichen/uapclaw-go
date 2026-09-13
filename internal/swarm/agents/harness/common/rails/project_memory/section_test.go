package project_memory

import (
	"strings"
	"testing"
)

func TestBuildProjectMemorySection_非空内容(t *testing.T) {
	section := BuildProjectMemorySection("Hello memory", 120)
	if section == nil {
		t.Fatal("expected non-nil section")
	}
	if section.Name != SectionName {
		t.Fatalf("expected name=%s, got %s", SectionName, section.Name)
	}
	if section.Priority != 120 {
		t.Fatalf("expected priority=120, got %d", section.Priority)
	}
	cn, ok := section.Content["cn"]
	if !ok || !strings.Contains(cn, "项目记忆") {
		t.Fatalf("cn content should contain 项目记忆, got %q", cn)
	}
	en, ok := section.Content["en"]
	if !ok || !strings.Contains(en, "Project Memory") {
		t.Fatalf("en content should contain Project Memory, got %q", en)
	}
	// 双语都应包含原始内容
	if !strings.Contains(cn, "Hello memory") {
		t.Fatalf("cn content should contain body text")
	}
	if !strings.Contains(en, "Hello memory") {
		t.Fatalf("en content should contain body text")
	}
}

func TestBuildProjectMemorySection_空内容(t *testing.T) {
	section := BuildProjectMemorySection("", 120)
	if section != nil {
		t.Fatal("expected nil for empty content")
	}
	section = BuildProjectMemorySection("   ", 120)
	if section != nil {
		t.Fatal("expected nil for whitespace content")
	}
}

func TestBuildProjectMemorySection_自定义优先级(t *testing.T) {
	section := BuildProjectMemorySection("test", 85)
	if section == nil {
		t.Fatal("expected non-nil section")
	}
	if section.Priority != 85 {
		t.Fatalf("expected priority=85, got %d", section.Priority)
	}
}
