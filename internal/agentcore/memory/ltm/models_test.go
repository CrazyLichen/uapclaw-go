package ltm

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
)

// TestAddMemResult_零值 测试 AddMemResult 零值初始化。
func TestAddMemResult_零值(t *testing.T) {
	result := &AddMemResult{}
	if len(result.Variables) != 0 {
		t.Errorf("Variables 应为空切片")
	}
	if len(result.UserProfile) != 0 {
		t.Errorf("UserProfile 应为空切片")
	}
	if len(result.Summary) != 0 {
		t.Errorf("Summary 应为空切片")
	}
}

// TestMemInfo_字段赋值 测试 MemInfo 字段赋值。
func TestMemInfo_字段赋值(t *testing.T) {
	mi := &MemInfo{
		MemID:   "test-id",
		Content: "测试内容",
		Type:    mem_model.MemoryTypeUserProfile,
	}
	if mi.MemID != "test-id" {
		t.Errorf("MemID = %q, want %q", mi.MemID, "test-id")
	}
	if mi.Type != mem_model.MemoryTypeUserProfile {
		t.Errorf("Type = %v, want UserProfile", mi.Type)
	}
}

// TestMemResult_字段赋值 测试 MemResult 字段赋值。
func TestMemResult_字段赋值(t *testing.T) {
	mr := &MemResult{
		MemInfo: &MemInfo{MemID: "m1", Content: "c1"},
		Score:   0.85,
	}
	if mr.MemInfo.MemID != "m1" {
		t.Errorf("MemInfo.MemID = %q, want %q", mr.MemInfo.MemID, "m1")
	}
	if mr.Score != 0.85 {
		t.Errorf("Score = %f, want 0.85", mr.Score)
	}
}
