package common

import (
	"testing"
)

// TestGenerateIdxName 测试向量索引名称生成
func TestGenerateIdxName(t *testing.T) {
	result := GenerateIdxName("user1", "scope1", "summary")
	expected := "uid_user1_gid_scope1_mtype_summary"
	if result != expected {
		t.Errorf("GenerateIdxName() = %q, want %q", result, expected)
	}
}

// TestGenerateIdxName_空参数 测试空参数
func TestGenerateIdxName_空参数(t *testing.T) {
	result := GenerateIdxName("", "", "")
	expected := "uid__gid__mtype_"
	if result != expected {
		t.Errorf("GenerateIdxName() = %q, want %q", result, expected)
	}
}

// TestParseMemtypeFromIdxName 测试从索引名解析记忆类型
func TestParseMemtypeFromIdxName(t *testing.T) {
	tests := []struct {
		name     string
		idxName  string
		expected string
	}{
		{"正常索引名", "uid_user1_gid_scope1_mtype_summary", "summary"},
		{"user_profile类型", "uid_u1_gid_s1_mtype_user_profile", "profile"},
		{"无下划线", "nounderscore", "nounderscore"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseMemtypeFromIdxName(tt.idxName)
			if result != tt.expected {
				t.Errorf("ParseMemtypeFromIdxName(%q) = %q, want %q", tt.idxName, result, tt.expected)
			}
		})
	}
}

// TestParseMemoryHitInfos 测试命中结果解析
func TestParseMemoryHitInfos(t *testing.T) {
	hits := []HitInfo{
		{ID: "id1", Score: 0.9},
		{ID: "id2", Score: 0.7},
	}
	ids, scores, err := ParseMemoryHitInfos(hits)
	if err != nil {
		t.Fatalf("ParseMemoryHitInfos() error = %v", err)
	}
	if len(ids) != 2 || ids[0] != "id1" || ids[1] != "id2" {
		t.Errorf("ids = %v, want [id1 id2]", ids)
	}
	if scores["id1"] != 0.9 || scores["id2"] != 0.7 {
		t.Errorf("scores = %v, want {id1:0.9 id2:0.7}", scores)
	}
}

// TestParseMemoryHitInfos_空列表 测试空列表
func TestParseMemoryHitInfos_空列表(t *testing.T) {
	ids, scores, err := ParseMemoryHitInfos([]HitInfo{})
	if err != nil {
		t.Fatalf("ParseMemoryHitInfos() error = %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("ids = %v, want empty", ids)
	}
	if len(scores) != 0 {
		t.Errorf("scores = %v, want empty", scores)
	}
}

// TestParseMemoryHitInfos_nil 测试 nil 输入
func TestParseMemoryHitInfos_nil(t *testing.T) {
	ids, _, err := ParseMemoryHitInfos(nil)
	if err != nil {
		t.Fatalf("ParseMemoryHitInfos() error = %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("ids = %v, want empty", ids)
	}
}
