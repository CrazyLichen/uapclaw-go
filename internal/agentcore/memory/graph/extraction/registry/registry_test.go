package registry

import (
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestDefaultEntityDefAttr_默认值 测试默认实体属性模板
func TestDefaultEntityDefAttr_默认值(t *testing.T) {
	if DefaultEntityDefAttr == nil {
		t.Fatal("DefaultEntityDefAttr 不应为 nil")
	}
	if DefaultEntityDefAttr.Content != "" {
		t.Errorf("DefaultEntityDefAttr.Content = %q, want empty string", DefaultEntityDefAttr.Content)
	}
}

// TestDefaultEntity_基础校验 测试默认实体类型定义
func TestDefaultEntity_基础校验(t *testing.T) {
	if DefaultEntity == nil {
		t.Fatal("DefaultEntity 不应为 nil")
	}
	if DefaultEntity.Name != "Entity" {
		t.Errorf("DefaultEntity.Name = %q, want %q", DefaultEntity.Name, "Entity")
	}
	if DefaultEntity.Attributes == nil {
		t.Error("DefaultEntity.Attributes 不应为 nil")
	}
}

// TestHumanEntity_基础校验 测试人类实体类型定义
func TestHumanEntity_基础校验(t *testing.T) {
	if HumanEntity == nil {
		t.Fatal("HumanEntity 不应为 nil")
	}
	if HumanEntity.Name != "Human" {
		t.Errorf("HumanEntity.Name = %q, want %q", HumanEntity.Name, "Human")
	}
	if HumanEntity.Attributes == nil {
		t.Error("HumanEntity.Attributes 不应为 nil")
	}
}

// TestAIEntity_基础校验 测试 AI 实体类型定义
func TestAIEntity_基础校验(t *testing.T) {
	if AIEntity == nil {
		t.Fatal("AIEntity 不应为 nil")
	}
	if AIEntity.Name != "AI" {
		t.Errorf("AIEntity.Name = %q, want %q", AIEntity.Name, "AI")
	}
	if AIEntity.Attributes == nil {
		t.Error("AIEntity.Attributes 不应为 nil")
	}
}

// TestDefaultRelation_基础校验 测试默认关系类型定义
func TestDefaultRelation_基础校验(t *testing.T) {
	if DefaultRelation == nil {
		t.Fatal("DefaultRelation 不应为 nil")
	}
	if DefaultRelation.Name != "Relation" {
		t.Errorf("DefaultRelation.Name = %q, want %q", DefaultRelation.Name, "Relation")
	}
	if DefaultRelation.LHS == nil {
		t.Error("DefaultRelation.LHS 不应为 nil")
	}
	if DefaultRelation.RHS == nil {
		t.Error("DefaultRelation.RHS 不应为 nil")
	}
}

// TestEntityDefAttr_字段序列化 测试 EntityDefAttr JSON 序列化
func TestEntityDefAttr_字段序列化(t *testing.T) {
	attr := &EntityDefAttr{Content: "测试摘要"}
	if attr.Content != "测试摘要" {
		t.Errorf("EntityDefAttr.Content = %q, want %q", attr.Content, "测试摘要")
	}
}

// TestRelationDef_字段校验 测试 RelationDef 字段
func TestRelationDef_字段校验(t *testing.T) {
	rdef := &RelationDef{
		Name:        "WorksAt",
		Description: map[string]string{"cn": "在某处工作", "en": "works at"},
		LHS:         HumanEntity,
		RHS:         DefaultEntity,
	}
	if rdef.Name != "WorksAt" {
		t.Errorf("RelationDef.Name = %q, want %q", rdef.Name, "WorksAt")
	}
	if rdef.LHS != HumanEntity {
		t.Error("RelationDef.LHS 不匹配 HumanEntity")
	}
	if rdef.RHS != DefaultEntity {
		t.Error("RelationDef.RHS 不匹配 DefaultEntity")
	}
}

// TestSchemaInfoHeader_值 测试常量值
func TestSchemaInfoHeader_值(t *testing.T) {
	if SchemaInfoHeader != "\n\n---\n" {
		t.Errorf("SchemaInfoHeader = %q, want %q", SchemaInfoHeader, "\n\n---\n")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
