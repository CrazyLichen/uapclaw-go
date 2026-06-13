package graph

import (
	"testing"
)

// TestNewBaseGraphObject_默认值 测试 BaseGraphObject 默认值
func TestNewBaseGraphObject_默认值(t *testing.T) {
	obj := NewBaseGraphObject()
	if len(obj.UUID) != 32 {
		t.Errorf("UUID 长度应为 32，实际为 %d", len(obj.UUID))
	}
	if obj.CreatedAt <= 0 {
		t.Error("CreatedAt 应大于 0")
	}
	if obj.UserID != "default_user" {
		t.Errorf("UserID 默认应为 default_user，实际为 %s", obj.UserID)
	}
	if obj.Language != "cn" {
		t.Errorf("Language 默认应为 cn，实际为 %s", obj.Language)
	}
}

// TestBaseGraphObject_EmbedTasks 测试 BaseGraphObject 的 EmbedTasks
func TestBaseGraphObject_EmbedTasks(t *testing.T) {
	obj := NewBaseGraphObject()
	obj.Content = "test content"
	tasks := obj.EmbedTasks()
	if len(tasks) != 1 {
		t.Fatalf("EmbedTasks 应返回 1 个任务，实际返回 %d", len(tasks))
	}
	if tasks[0].FieldName != "content_embedding" {
		t.Errorf("FieldName 应为 content_embedding，实际为 %s", tasks[0].FieldName)
	}
	if tasks[0].Text != "test content" {
		t.Errorf("Text 应为 test content，实际为 %s", tasks[0].Text)
	}
}

// TestBaseGraphObject_ToMap 测试 BaseGraphObject 的 ToMap
func TestBaseGraphObject_ToMap(t *testing.T) {
	obj := NewBaseGraphObject()
	obj.Content = "hello"
	obj.ContentEmbedding = []float64{0.1, 0.2}
	m := obj.ToMap()
	if m["uuid"] != obj.UUID {
		t.Error("ToMap uuid 字段不匹配")
	}
	if m["content"] != "hello" {
		t.Error("ToMap content 字段不匹配")
	}
	if m["content_embedding"] == nil {
		t.Error("ToMap content_embedding 不应为 nil")
	}
}

// TestEntity_EmbedTasks 测试 Entity 的 EmbedTasks
func TestEntity_EmbedTasks(t *testing.T) {
	e := NewEntity()
	e.Content = "entity content"
	e.Name = "entity name"
	tasks := e.EmbedTasks()
	if len(tasks) != 2 {
		t.Fatalf("Entity EmbedTasks 应返回 2 个任务，实际返回 %d", len(tasks))
	}
	if tasks[0].FieldName != "content_embedding" {
		t.Errorf("第1个任务 FieldName 应为 content_embedding，实际为 %s", tasks[0].FieldName)
	}
	if tasks[1].FieldName != "name_embedding" {
		t.Errorf("第2个任务 FieldName 应为 name_embedding，实际为 %s", tasks[1].FieldName)
	}
}

// TestEntity_ToMap_去重排序 测试 Entity ToMap 中 Relations/Episodes 去重排序
func TestEntity_ToMap_去重排序(t *testing.T) {
	e := NewEntity()
	e.Relations = []string{"c", "a", "b", "a"}
	e.Episodes = []string{"z", "x", "x"}
	m := e.ToMap()
	relations := m["relations"].([]string)
	if len(relations) != 3 {
		t.Fatalf("Relations 去重后应为 3 个，实际为 %d", len(relations))
	}
	if relations[0] != "a" || relations[1] != "b" || relations[2] != "c" {
		t.Errorf("Relations 排序不正确: %v", relations)
	}
	episodes := m["episodes"].([]string)
	if len(episodes) != 2 {
		t.Fatalf("Episodes 去重后应为 2 个，实际为 %d", len(episodes))
	}
}

// TestRelation_ToMap 测试 Relation 的 ToMap
func TestRelation_ToMap(t *testing.T) {
	r := NewRelation()
	r.LHS = "entity-uuid-1"
	r.RHS = "entity-uuid-2"
	r.ValidSince = 1000
	r.ValidUntil = 2000
	m := r.ToMap()
	if m["lhs"] != "entity-uuid-1" {
		t.Error("ToMap lhs 字段不匹配")
	}
	if m["rhs"] != "entity-uuid-2" {
		t.Error("ToMap rhs 字段不匹配")
	}
	if m["valid_since"] != int64(1000) {
		t.Error("ToMap valid_since 字段不匹配")
	}
}

// TestRelation_UpdateConnectedEntities 测试 UpdateConnectedEntities
func TestRelation_UpdateConnectedEntities(t *testing.T) {
	r := NewRelation()
	lhs := NewEntity()
	rhs := NewEntity()
	r.UpdateConnectedEntities(lhs, rhs)
	if len(lhs.Relations) != 1 || lhs.Relations[0] != r.UUID {
		t.Error("lhs.Relations 应包含关系的 UUID")
	}
	if len(rhs.Relations) != 1 || rhs.Relations[0] != r.UUID {
		t.Error("rhs.Relations 应包含关系的 UUID")
	}
	// 重复调用不应重复添加
	r.UpdateConnectedEntities(lhs, rhs)
	if len(lhs.Relations) != 1 {
		t.Error("重复调用不应重复添加关系 UUID")
	}
}

// TestEpisode_ToMap_去重排序 测试 Episode ToMap 中 Entities 去重排序
func TestEpisode_ToMap_去重排序(t *testing.T) {
	p := NewEpisode()
	p.Entities = []string{"b", "a", "b"}
	m := p.ToMap()
	entities := m["entities"].([]string)
	if len(entities) != 2 {
		t.Fatalf("Entities 去重后应为 2 个，实际为 %d", len(entities))
	}
	if entities[0] != "a" || entities[1] != "b" {
		t.Errorf("Entities 排序不正确: %v", entities)
	}
}

// TestUniqueSortedStrings 测试 uniqueSortedStrings
func TestUniqueSortedStrings(t *testing.T) {
	result := uniqueSortedStrings([]string{"c", "a", "b", "a", "c"})
	if len(result) != 3 {
		t.Fatalf("去重后应为 3 个，实际为 %d", len(result))
	}
	if result[0] != "a" || result[1] != "b" || result[2] != "c" {
		t.Errorf("排序不正确: %v", result)
	}
	// 空切片
	empty := uniqueSortedStrings(nil)
	if len(empty) != 0 {
		t.Errorf("空切片应返回空，实际为 %v", empty)
	}
}
