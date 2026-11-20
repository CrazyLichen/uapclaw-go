package vector_fields

import "testing"

// ──────────────────────────── 结构体 ────────────────────────────

// milvusStageDictProvider 抽取两类阶段字典方法，便于表驱动断言。
type milvusStageDictProvider interface {
	// ToConstructDict 返回构建阶段配置
	ToConstructDict() map[string]any
	// ToSearchDict 返回搜索阶段配置
	ToSearchDict() map[string]any
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestMilvusFields_StageDicts 验证各索引类型的构建/搜索阶段字典输出。
func TestMilvusFields_StageDicts(t *testing.T) {
	cases := []struct {
		name  string
		field milvusStageDictProvider
	}{
		{"AUTO", NewMilvusAUTO("embedding")},
		{"FLAT", NewMilvusFLAT("embedding")},
		{"HNSW", NewMilvusHNSW("embedding", 30, 360, 2.0)},
		{"IVF", NewMilvusIVF("embedding", 16384, 16)},
		{"SCANN", NewMilvusSCANN("embedding", 16384, 16, true, 100)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			construct := c.field.ToConstructDict()
			search := c.field.ToSearchDict()
			if construct == nil {
				t.Fatal("ToConstructDict 不应返回 nil")
			}
			if search == nil {
				t.Fatal("ToSearchDict 不应返回 nil")
			}
			// 与 ToDict 的等价性：同一 stage 应产生相同内容
			if len(construct) != len(ToDict(c.field, StageConstruct)) {
				t.Fatalf("ToConstructDict 与 ToDict(construct) 长度不一致: %v vs %v",
					construct, ToDict(c.field, StageConstruct))
			}
			if len(search) != len(ToDict(c.field, StageSearch)) {
				t.Fatalf("ToSearchDict 与 ToDict(search) 长度不一致: %v vs %v",
					search, ToDict(c.field, StageSearch))
			}
		})
	}
}

// TestMilvusHNSW_Validate 验证 HNSW 参数边界。
func TestMilvusHNSW_Validate(t *testing.T) {
	tests := []struct {
		name    string
		m       int
		efCons  int
		efFac   float64
		wantErr bool
	}{
		{"合法参数", 30, 360, 2.0, false},
		{"M 下界 2 合法", 2, 360, 2.0, false},
		{"M 上界 2048 合法", 2048, 360, 2.0, false},
		{"M 过小", 1, 360, 2.0, true},
		{"M 过大", 2049, 360, 2.0, true},
		{"EfConstruction 过小", 30, 0, 2.0, true},
		{"EfSearchFactor 为 0", 30, 360, 0, true},
		{"EfSearchFactor 为负", 30, 360, -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewMilvusHNSW("embedding", tt.m, tt.efCons, tt.efFac).Validate()
			if tt.wantErr && err == nil {
				t.Fatal("期望报错，实际通过")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("期望通过，实际报错: %v", err)
			}
		})
	}
}

// TestMilvusIVF_Validate 验证 IVF 参数边界。
func TestMilvusIVF_Validate(t *testing.T) {
	tests := []struct {
		name    string
		nlist   int
		nprobe  int
		wantErr bool
	}{
		{"合法参数", 16384, 16, false},
		{"nlist 过小", 0, 16, true},
		{"nprobe 过小", 16384, 0, true},
		{"nprobe 大于 nlist", 10, 20, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewMilvusIVF("embedding", tt.nlist, tt.nprobe).Validate()
			if tt.wantErr && err == nil {
				t.Fatal("期望报错，实际通过")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("期望通过，实际报错: %v", err)
			}
		})
	}
}

// TestMilvusSCANN_Validate 验证 SCANN 参数边界。
func TestMilvusSCANN_Validate(t *testing.T) {
	tests := []struct {
		name     string
		nlist    int
		nprobe   int
		reorderK int
		wantErr  bool
	}{
		{"合法参数", 16384, 16, 100, false},
		{"nlist 过小", 0, 16, 100, true},
		{"nprobe 过小", 16384, 0, 100, true},
		{"nprobe 大于 nlist", 10, 20, 100, true},
		{"reorderK 为负", 16384, 16, -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewMilvusSCANN("embedding", tt.nlist, tt.nprobe, true, tt.reorderK).Validate()
			if tt.wantErr && err == nil {
				t.Fatal("期望报错，实际通过")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("期望通过，实际报错: %v", err)
			}
		})
	}
}
