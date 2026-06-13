package graph

import "testing"

// TestWeightedRankConfig_Name 测试加权排序名称
func TestWeightedRankConfig_Name(t *testing.T) {
	w := NewWeightedRankConfig()
	if w.Name() != "weighted" {
		t.Errorf("Name 应为 weighted，实际为 %s", w.Name())
	}
}

// TestWeightedRankConfig_HigherIsBetter 测试加权排序方向
func TestWeightedRankConfig_HigherIsBetter(t *testing.T) {
	w := NewWeightedRankConfig()
	if w.HigherIsBetter() {
		t.Error("WeightedRankConfig HigherIsBetter 应为 false")
	}
}

// TestWeightedRankConfig_IsActive_默认 测试默认通道活跃状态
func TestWeightedRankConfig_IsActive_默认(t *testing.T) {
	w := NewWeightedRankConfig()
	active := w.IsActive()
	if active != [3]int{1, 1, 1} {
		t.Errorf("默认3通道应全部活跃，实际为 %v", active)
	}
}

// TestWeightedRankConfig_IsActive_部分通道 测试部分通道关闭
func TestWeightedRankConfig_IsActive_部分通道(t *testing.T) {
	w := &WeightedRankConfig{NameDense: 0, ContentDense: 0.6, ContentSparse: 0.4}
	active := w.IsActive()
	if active != [3]int{0, 1, 1} {
		t.Errorf("应只有2通道活跃，实际为 %v", active)
	}
}

// TestWeightedRankConfig_Args_归一化 测试权重归一化
func TestWeightedRankConfig_Args_归一化(t *testing.T) {
	w := NewWeightedRankConfig() // 0.15, 0.60, 0.25
	pos, _ := w.Args()
	if len(pos) != 3 {
		t.Fatalf("应有 3 个位置参数，实际为 %d", len(pos))
	}
	// 归一化后总和应为 1.0
	total := 0.0
	for _, v := range pos {
		total += v.(float64)
	}
	if total < 0.99 || total > 1.01 {
		t.Errorf("归一化后总和应为 1.0，实际为 %v", total)
	}
}

// TestWeightedRankConfig_Args_过滤零值 测试零值权重过滤
func TestWeightedRankConfig_Args_过滤零值(t *testing.T) {
	w := &WeightedRankConfig{NameDense: 0, ContentDense: 0.6, ContentSparse: 0.4}
	pos, _ := w.Args()
	if len(pos) != 2 {
		t.Fatalf("过滤零值后应有 2 个参数，实际为 %d", len(pos))
	}
}

// TestRRFRankConfig_Name 测试RRF排序名称
func TestRRFRankConfig_Name(t *testing.T) {
	r := NewRRFRankConfig()
	if r.Name() != "rrf" {
		t.Errorf("Name 应为 rrf，实际为 %s", r.Name())
	}
}

// TestRRFRankConfig_HigherIsBetter 测试RRF排序方向
func TestRRFRankConfig_HigherIsBetter(t *testing.T) {
	r := NewRRFRankConfig()
	if !r.HigherIsBetter() {
		t.Error("RRFRankConfig HigherIsBetter 应为 true")
	}
}

// TestRRFRankConfig_IsActive_默认 测试默认通道活跃状态
func TestRRFRankConfig_IsActive_默认(t *testing.T) {
	r := NewRRFRankConfig()
	active := r.IsActive()
	if active != [3]int{1, 1, 1} {
		t.Errorf("默认3通道应全部活跃，实际为 %v", active)
	}
}

// TestRRFRankConfig_Args 测试RRF参数
func TestRRFRankConfig_Args(t *testing.T) {
	r := NewRRFRankConfig()
	pos, kw := r.Args()
	if len(pos) != 1 {
		t.Fatalf("应有 1 个位置参数，实际为 %d", len(pos))
	}
	if pos[0].(int) != 40 {
		t.Errorf("K 应为 40，实际为 %v", pos[0])
	}
	if len(kw) != 0 {
		t.Errorf("关键字参数应为空，实际为 %v", kw)
	}
}

// TestRankerRegistry_RegisterAndGet 测试注册和获取
func TestRankerRegistry_RegisterAndGet(t *testing.T) {
	reg := NewRankerRegistry()
	rankers := map[string]any{
		"weighted": func() string { return "weighted_ranker" },
		"rrf":      func() string { return "rrf_ranker" },
	}
	reg.Register("milvus", rankers)
	_, ok := reg.GetRanker("milvus", "weighted")
	if !ok {
		t.Error("应能获取已注册的排序器")
	}
	_, ok = reg.GetRanker("milvus", "nonexistent")
	if ok {
		t.Error("未注册的策略应返回 false")
	}
	_, ok = reg.GetRanker("unknown", "weighted")
	if ok {
		t.Error("未注册的后端应返回 false")
	}
}

// TestRRFRankConfig_IsActive_部分通道 测试部分通道关闭
func TestRRFRankConfig_IsActive_部分通道(t *testing.T) {
	r := &RRFRankConfig{K: 40, NameDense: false, ContentDense: true, ContentSparse: true}
	active := r.IsActive()
	if active != [3]int{0, 1, 1} {
		t.Errorf("应只有2通道活跃，实际为 %v", active)
	}
}
