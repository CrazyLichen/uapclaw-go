package en

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/registry"
)

// TestRegisterLanguage_英文注册 包初始化把英文模板写入全局注册表。
func TestRegisterLanguage_英文注册(t *testing.T) {
	_, registered := registry.RegisteredLanguage["en"]
	assert.True(t, registered, "en 必须在已注册语言集合中")

	assert.NotEmpty(t, registry.SourceDescription["en"])
	assert.NotEmpty(t, registry.RefJSONObjectDef["en"])
	assert.NotEmpty(t, registry.OutputFormat["en"])
	assert.NotEmpty(t, registry.DisplayEntity["en"])
	assert.NotEmpty(t, registry.MarkCurrentMsg["en"])
	assert.NotEmpty(t, registry.MarkHistoryMsg["en"])
	assert.NotEmpty(t, registry.RelationFormat["en"])
	assert.Equal(t, "None", registry.NoRelationGiven["en"])

	assert.NotEmpty(t, registry.EntityDefinitionDescription["en"])
	assert.NotEmpty(t, registry.HumanEntityDescription["en"])
	assert.NotEmpty(t, registry.AIEntityDescription["en"])
	assert.NotEmpty(t, registry.RelationDefinitionDescription["en"])

	descriptions := registry.MultilingualDescription["en"]
	assert.NotEmpty(t, descriptions)
	assert.Equal(t, "Name of extracted entity", descriptions["{{[ent_def_name]}}"])
	assert.Equal(t, "year", descriptions["{{[year]}}"])
}
