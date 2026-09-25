package graph_memory

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/registry"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestParseISO_有效日期 测试解析有效的 ISO 8601 日期
func TestParseISO_有效日期(t *testing.T) {
	ts, offset := ParseISO("2024-06-15T10:30:00Z")
	if ts < 0 {
		t.Fatalf("期望有效时间戳，得到 %d", ts)
	}
	if offset != 0 {
		t.Fatalf("期望 UTC 偏移为 0，得到 %d", offset)
	}
	// 2024-06-15T10:30:00Z → 1718447400
	expectedTs := int64(1718447400)
	if ts != expectedTs {
		t.Fatalf("期望时间戳 %d，得到 %d", expectedTs, ts)
	}
}

// TestParseISO_带时区 测试解析带时区偏移的日期
func TestParseISO_带时区(t *testing.T) {
	ts, offset := ParseISO("2024-06-15T10:30:00+08:00")
	if ts < 0 {
		t.Fatalf("期望有效时间戳，得到 %d", ts)
	}
	// +08:00 → offset = 8*4 = 32 (15分钟单位)
	if offset != 32 {
		t.Fatalf("期望偏移 32，得到 %d", offset)
	}
}

// TestParseISO_空输入 测试空字符串和无法匹配的输入
func TestParseISO_空输入(t *testing.T) {
	ts, offset := ParseISO("")
	if ts != -1 || offset != 0 {
		t.Fatalf("期望 (-1, 0)，得到 (%d, %d)", ts, offset)
	}

	ts, offset = ParseISO("not-a-date")
	if ts != -1 || offset != 0 {
		t.Fatalf("期望 (-1, 0)，得到 (%d, %d)", ts, offset)
	}
}

// TestDict2Relation_有效输入 测试从有效 dict 构建 Relation
func TestDict2Relation_有效输入(t *testing.T) {
	e1 := graph.NewEntity()
	e1.Name = "Alice"
	e2 := graph.NewEntity()
	e2.Name = "Bob"

	entities := []*graph.Entity{e1, e2}

	response := map[string]any{
		"source_id": 1,
		"target_id": 2,
		"name":      "knows",
		"fact":      "Alice knows Bob",
	}

	rel := Dict2Relation(response, entities, 0, "test-user")
	if rel == nil {
		t.Fatal("期望非 nil Relation")
	}
	if rel.Name != "knows" {
		t.Fatalf("期望 name=knows，得到 %s", rel.Name)
	}
	if rel.Content != "Alice knows Bob" {
		t.Fatalf("期望 content=Alice knows Bob，得到 %s", rel.Content)
	}
	if rel.ObjType != "Relation" {
		t.Fatalf("期望 obj_type=Relation，得到 %s", rel.ObjType)
	}
	if rel.LHSUUID() != e1.UUID {
		t.Fatalf("期望 LHS=%s，得到 %s", e1.UUID, rel.LHSUUID())
	}
	if rel.RHSUUID() != e2.UUID {
		t.Fatalf("期望 RHS=%s，得到 %s", e2.UUID, rel.RHSUUID())
	}
}

// TestDict2Relation_无效ID 测试无效的 source_id/target_id 返回 nil
func TestDict2Relation_无效ID(t *testing.T) {
	e1 := graph.NewEntity()
	entities := []*graph.Entity{e1}

	tests := []struct {
		name     string
		response map[string]any
	}{
		{"source_id 为 0", map[string]any{"source_id": 0, "target_id": 1}},
		{"target_id 超出范围", map[string]any{"source_id": 1, "target_id": 5}},
		{"source_id 为负数", map[string]any{"source_id": -1, "target_id": 1}},
		{"source_id 非数字", map[string]any{"source_id": "abc", "target_id": 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel := Dict2Relation(tt.response, entities, 0, "test-user")
			if rel != nil {
				t.Fatalf("期望 nil，得到非 nil Relation")
			}
		})
	}
}

// TestDict2Relation_自引用 测试 source_id == target_id 时 ObjType 为 EntityFact
func TestDict2Relation_自引用(t *testing.T) {
	e1 := graph.NewEntity()
	e1.Name = "Alice"
	entities := []*graph.Entity{e1}

	response := map[string]any{
		"source_id": 1,
		"target_id": 1,
		"name":      "lives_in",
		"fact":      "Alice lives in Beijing",
	}

	rel := Dict2Relation(response, entities, 0, "test-user")
	if rel == nil {
		t.Fatal("期望非 nil Relation")
	}
	if rel.ObjType != "EntityFact" {
		t.Fatalf("期望 obj_type=EntityFact，得到 %s", rel.ObjType)
	}
}

// TestDict2Relation_嵌套dict 测试单 key 嵌套 dict 的展开
func TestDict2Relation_嵌套dict(t *testing.T) {
	e1 := graph.NewEntity()
	e2 := graph.NewEntity()
	entities := []*graph.Entity{e1, e2}

	// 单 key 且值为 dict → 展开为内层
	response := map[string]any{
		"relation": map[string]any{
			"source_id": 1,
			"target_id": 2,
			"name":      "friend",
			"fact":      "A is friend of B",
		},
	}

	rel := Dict2Relation(response, entities, 0, "test-user")
	if rel == nil {
		t.Fatal("期望非 nil Relation")
	}
	if rel.Name != "friend" {
		t.Fatalf("期望 name=friend，得到 %s", rel.Name)
	}
}

// TestDict2Relation_缺省name 测试缺少 name 时默认为 RELATION，缺少 fact 时默认为 name
func TestDict2Relation_缺省name(t *testing.T) {
	e1 := graph.NewEntity()
	e2 := graph.NewEntity()
	entities := []*graph.Entity{e1, e2}

	response := map[string]any{
		"source_id": 1,
		"target_id": 2,
		"fact":      "some fact",
	}

	rel := Dict2Relation(response, entities, 0, "test-user")
	if rel == nil {
		t.Fatal("期望非 nil Relation")
	}
	if rel.Name != "RELATION" {
		t.Fatalf("期望 name=RELATION，得到 %s", rel.Name)
	}
}

// TestParseAllRelations_基本 测试基本的全部关系解析
func TestParseAllRelations_基本(t *testing.T) {
	entityTypes := []registry.EntityDef{
		{Name: "Person"},
		{Name: "Organization"},
	}

	entityDecls := []extraction.EntityDeclaration{
		{Name: "Alice", EntityTypeID: 0},
		{Name: "Bob", EntityTypeID: 0},
	}

	relations := []map[string]any{
		{
			"source_id": 1,
			"target_id": 2,
			"name":      "knows",
			"fact":      "Alice knows Bob",
			"content":   "Alice knows Bob",
		},
	}

	resultRels, resultEntities := ParseAllRelations(relations, entityDecls, entityTypes, 0, "test-user")

	if len(resultRels) != 1 {
		t.Fatalf("期望 1 个关系，得到 %d", len(resultRels))
	}
	if resultRels[0].Name != "knows" {
		t.Fatalf("期望 name=knows，得到 %s", resultRels[0].Name)
	}
	if len(resultEntities) != 2 {
		t.Fatalf("期望 2 个实体，得到 %d", len(resultEntities))
	}
}

// TestParseAllRelations_去重 测试关系内容去重
func TestParseAllRelations_去重(t *testing.T) {
	entityTypes := []registry.EntityDef{
		{Name: "Person"},
	}

	entityDecls := []extraction.EntityDeclaration{
		{Name: "Alice", EntityTypeID: 0},
		{Name: "Bob", EntityTypeID: 0},
	}

	relations := []map[string]any{
		{
			"source_id": 1,
			"target_id": 2,
			"name":      "knows",
			"fact":      "Alice knows Bob very well",
			"content":   "Alice knows Bob very well",
		},
		{
			"source_id": 1,
			"target_id": 2,
			"name":      "knows2",
			"fact":      "Alice knows Bob",
			"content":   "Alice knows Bob",
		},
	}

	resultRels, _ := ParseAllRelations(relations, entityDecls, entityTypes, 0, "test-user")

	// 第二条内容是第一条的子串，应被标记为空 content，Dict2Relation 会用 name 作为 content
	if len(resultRels) != 2 {
		t.Fatalf("期望 2 个关系，得到 %d", len(resultRels))
	}
}

// TestDeclareEntities_基本 测试基本的实体声明转换
func TestDeclareEntities_基本(t *testing.T) {
	entityTypes := []registry.EntityDef{
		{Name: "Human"},
		{Name: "AI"},
	}

	decls := []extraction.EntityDeclaration{
		{Name: "Alice", EntityTypeID: 0},
		{Name: "ChatGPT", EntityTypeID: 1},
	}

	entities := DeclareEntities(decls, entityTypes, 0, "test-user")

	if len(entities) != 2 {
		t.Fatalf("期望 2 个实体，得到 %d", len(entities))
	}
	if entities[0].Name != "Alice" {
		t.Fatalf("期望 name=Alice，得到 %s", entities[0].Name)
	}
	if entities[0].ObjType != "Human" {
		t.Fatalf("期望 obj_type=Human，得到 %s", entities[0].ObjType)
	}
	if entities[1].ObjType != "AI" {
		t.Fatalf("期望 obj_type=AI，得到 %s", entities[1].ObjType)
	}
}

// TestDeclareEntities_类型ID越界 测试 EntityTypeID 超出范围时使用最大索引
func TestDeclareEntities_类型ID越界(t *testing.T) {
	entityTypes := []registry.EntityDef{
		{Name: "Human"},
		{Name: "AI"},
	}

	decls := []extraction.EntityDeclaration{
		{Name: "Unknown", EntityTypeID: 10}, // 超出范围，应使用最后一个
	}

	entities := DeclareEntities(decls, entityTypes, 0, "test-user")
	if entities[0].ObjType != "AI" {
		t.Fatalf("期望 obj_type=AI（最大索引），得到 %s", entities[0].ObjType)
	}
}

// TestDeclareEntities_空类型列表 测试空 entityTypes 不会 panic
func TestDeclareEntities_空类型列表(t *testing.T) {
	decls := []extraction.EntityDeclaration{
		{Name: "Alice", EntityTypeID: 0},
	}

	// 空类型列表不应 panic
	entities := DeclareEntities(decls, nil, 0, "test-user")
	if len(entities) != 1 {
		t.Fatalf("期望 1 个实体，得到 %d", len(entities))
	}
}

// TestResolveEntities_基本 测试基本的实体去重/合并解析
func TestResolveEntities_基本(t *testing.T) {
	// 准备已有实体
	existingEntity := graph.NewEntity()
	existingEntity.Name = "Alice"
	existingEntity.ObjType = "Human"

	// 准备候选实体
	candidates := []extraction.EntityDeclaration{
		{Name: "Alice_Smith", EntityTypeID: 0},
	}

	// 去重信息：已有实体 [0] 和候选 [1] 是重复的
	duplication := []map[string]any{
		{
			"id":            1,        // 1-based，指向 existing[0]
			"duplicate_ids": []any{2}, // 1-based，指向 candidates[0]（num_existing+1=2）
		},
	}

	result, mergePairs, toRemove := ResolveEntities(candidates, []*graph.Entity{existingEntity}, duplication)

	if len(result) != 1 {
		t.Fatalf("期望 1 个结果实体，得到 %d", len(result))
	}

	// 结果中第一个候选实体应被替换为已有实体
	if ent := result[0].Entity; ent != nil {
		if ent.UUID != existingEntity.UUID {
			t.Fatalf("期望结果实体 UUID 为 %s，得到 %v", existingEntity.UUID, ent.UUID)
		}
	} else {
		t.Fatal("期望结果为 Entity 类型")
	}

	// 待删除集合应为空（因为候选不在 mergeMap 中作为源）
	_ = mergePairs
	_ = toRemove
}

// TestResolveEntities_现有实体间合并 测试两个现有实体需要合并
func TestResolveEntities_现有实体间合并(t *testing.T) {
	existing1 := graph.NewEntity()
	existing1.Name = "Alice"
	existing1.ObjType = "Human"

	existing2 := graph.NewEntity()
	existing2.Name = "Alice_Smith"
	existing2.ObjType = "Human"

	candidates := []extraction.EntityDeclaration{}

	duplication := []map[string]any{
		{
			"id":            1,        // 1-based → existing[0] (Alice)
			"duplicate_ids": []any{2}, // 1-based → existing[1] (Alice_Smith)
		},
	}

	_, mergePairs, toRemove := ResolveEntities(candidates, []*graph.Entity{existing1, existing2}, duplication)

	if len(mergePairs) != 1 {
		t.Fatalf("期望 1 个合并对，得到 %d", len(mergePairs))
	}

	// Alice_Smith 应在待删除集合中
	if _, ok := toRemove[existing2.UUID]; !ok {
		t.Fatalf("期望 UUID %s 在待删除集合中", existing2.UUID)
	}
}

// TestParseRelationMerging_需要合并 测试关系合并响应解析
func TestParseRelationMerging_需要合并(t *testing.T) {
	relation := graph.NewRelation()
	relation.Content = "old content"
	relation.Name = "test_rel"
	relation.ValidSince = -1
	relation.ValidUntil = -1

	existingRels := []map[string]any{
		{"uuid": "uuid-1"},
		{"uuid": "uuid-2"},
		{"uuid": "uuid-3"},
	}

	response := map[string]any{
		"need_merging":     true,
		"combined_content": "merged content",
		"duplicate_ids":    []any{2, 3},
		"valid_since":      "2024-01-01T00:00:00Z",
		"valid_until":      "2024-12-31T23:59:59Z",
	}

	toRemove := ParseRelationMerging(response, relation, existingRels)

	if relation.Content != "merged content" {
		t.Fatalf("期望 content=merged content，得到 %s", relation.Content)
	}
	if relation.ValidSince < 0 {
		t.Fatal("期望 valid_since 被更新")
	}
	if relation.ValidUntil < 0 {
		t.Fatal("期望 valid_until 被更新")
	}
	if len(toRemove) != 2 {
		t.Fatalf("期望 2 个待删除 UUID，得到 %d", len(toRemove))
	}
	if _, ok := toRemove["uuid-1"]; ok {
		t.Fatal("uuid-1 不应在待删除集合中（ID=2 → uuid-2）")
	}
	if _, ok := toRemove["uuid-2"]; !ok {
		t.Fatal("uuid-2 应在待删除集合中")
	}
	if _, ok := toRemove["uuid-3"]; !ok {
		t.Fatal("uuid-3 应在待删除集合中")
	}
}

// TestParseRelationMerging_不需要合并 测试不需要合并时返回空集合
func TestParseRelationMerging_不需要合并(t *testing.T) {
	relation := graph.NewRelation()
	relation.Content = "original"

	existingRels := []map[string]any{
		{"uuid": "uuid-1"},
	}

	response := map[string]any{
		"need_merging":     false,
		"combined_content": "merged content",
	}

	toRemove := ParseRelationMerging(response, relation, existingRels)

	if relation.Content != "original" {
		t.Fatalf("内容不应被修改，得到 %s", relation.Content)
	}
	if len(toRemove) != 0 {
		t.Fatalf("期望 0 个待删除 UUID，得到 %d", len(toRemove))
	}
}

// TestParseRelationMerging_空内容不合并 测试 need_merging=true 但 combined_content 为空时不合并
func TestParseRelationMerging_空内容不合并(t *testing.T) {
	relation := graph.NewRelation()
	relation.Content = "original"

	existingRels := []map[string]any{
		{"uuid": "uuid-1"},
	}

	response := map[string]any{
		"need_merging":     true,
		"combined_content": "",
	}

	toRemove := ParseRelationMerging(response, relation, existingRels)

	if relation.Content != "original" {
		t.Fatalf("内容不应被修改，得到 %s", relation.Content)
	}
	if len(toRemove) != 0 {
		t.Fatalf("期望 0 个待删除 UUID，得到 %d", len(toRemove))
	}
}

// TestToInt 测试 toInt 辅助函数
func TestToInt(t *testing.T) {
	tests := []struct {
		input  any
		want   int
		wantOK bool
	}{
		{42, 42, true},
		{int64(42), 42, true},
		{float64(42), 42, true},
		{"42", 42, true},
		{"abc", 0, false},
		{nil, 0, false},
	}

	for _, tt := range tests {
		got, ok := toInt(tt.input)
		if ok != tt.wantOK || got != tt.want {
			t.Errorf("toInt(%v) = (%d, %v)，期望 (%d, %v)", tt.input, got, ok, tt.want, tt.wantOK)
		}
	}
}

// TestToBool 测试 toBool 辅助函数
func TestToBool(t *testing.T) {
	tests := []struct {
		input any
		want  bool
	}{
		{true, true},
		{false, false},
		{float64(1), true},
		{float64(0), false},
		{1, true},
		{0, false},
		{"true", true},
		{"false", false},
		{"TRUE", true},
		{nil, false},
	}

	for _, tt := range tests {
		got := toBool(tt.input)
		if got != tt.want {
			t.Errorf("toBool(%v) = %v，期望 %v", tt.input, got, tt.want)
		}
	}
}
