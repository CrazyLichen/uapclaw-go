package cn

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/registry"
)

// TestRegisterLanguage_中文注册 包初始化把中文模板写入全局注册表。
func TestRegisterLanguage_中文注册(t *testing.T) {
	_, registered := registry.RegisteredLanguage["cn"]
	assert.True(t, registered, "cn 必须在已注册语言集合中")

	assert.NotEmpty(t, registry.SourceDescription["cn"])
	assert.NotEmpty(t, registry.RefJSONObjectDef["cn"])
	assert.NotEmpty(t, registry.OutputFormat["cn"])
	assert.NotEmpty(t, registry.DisplayEntity["cn"])
	assert.NotEmpty(t, registry.MarkCurrentMsg["cn"])
	assert.NotEmpty(t, registry.MarkHistoryMsg["cn"])
	assert.NotEmpty(t, registry.RelationFormat["cn"])
	assert.NotEmpty(t, registry.NoRelationGiven["cn"])

	assert.NotEmpty(t, registry.EntityDefinitionDescription["cn"])
	assert.NotEmpty(t, registry.HumanEntityDescription["cn"])
	assert.NotEmpty(t, registry.AIEntityDescription["cn"])
	assert.NotEmpty(t, registry.RelationDefinitionDescription["cn"])

	descriptions := registry.MultilingualDescription["cn"]
	assert.NotEmpty(t, descriptions)
	assert.Equal(t, "新提取实体的名字", descriptions["{{[ent_def_name]}}"])
	assert.Equal(t, "年", descriptions["{{[year]}}"])
	assert.Equal(t, "：", descriptions[":"])
}
