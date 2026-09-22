package en

import "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/registry"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册英文语言的多语言描述和格式化模板
//
// Python: entity_extraction/en.py register_language()
func init() {
	lang := "en"

	registry.SourceDescription[lang] = "\n<source_description>\n{source_description}\n</source_description>\n"
	registry.RefJSONObjectDef[lang] = "Definition for relevant JSON objects"
	registry.OutputFormat[lang] = "Output Definition (Final Output NEEDS to be JSON)"
	registry.DisplayEntity[lang] = "{i}. {name}:\n{content}"
	registry.MarkCurrentMsg[lang] = "<current_messages>\n{content}\n</current_messages>\n"
	registry.MarkHistoryMsg[lang] = "<history_messages>\n{history}\n</history_messages>\n"
	registry.RelationFormat[lang] = "{name} (<{lhs}>-[{name}]-<{rhs}>): {description}"
	registry.NoRelationGiven[lang] = "None"

	registry.EntityDefinitionDescription[lang] = ": Default entity type, pick this if no other option is suitable."
	registry.HumanEntityDescription[lang] = ": Represent a human, can either be the user or other people."
	registry.AIEntityDescription[lang] = ": Represent an AI assistant, can be a chatbot or other types of AI agents."
	registry.RelationDefinitionDescription[lang] = ": Default relation type."

	registry.MultilingualDescription[lang] = map[string]string{
		// Entity
		"{{[ent_def_name]}}":      "Name of extracted entity",
		"{{[ent_def_type]}}":      "Type ID of extracted entity, needs to be from the list of provided entity types",
		"{{[ent_ext_list]}}":      "List of extracted entities",
		"{{[ent_summary]}}":       "Important information regarding the entity, a short & concise summary within 250 words",
		"{{[ent_attributes]}}":    "Entity attributes",
		"{{[ent_info]}}":          "Extracted entity attributes and summary",
		"{{[ent_valid_since]}}":   "Date for when this entity starts to be valid, please use ISO format YYYY-MM-DDTHH:MM:SS[+HH:MM]",
		"{{[ent_valid_until]}}":   "Date for when this entity stops being valid, please use ISO format YYYY-MM-DDTHH:MM:SS[+HH:MM]",
		"{{[ent_dupe_name]}}":     "Name of existing entity",
		"{{[ent_dupe_id]}}":       "ID of existing entity",
		"{{[ent_dupe_id_list]}}":  "List of IDs for entities that may be duplicate of this existing entity",
		"{{[ent_dupe_list]}}":     "List of duplicate entities",
		// Relation
		"{{[rel_valid_since]}}":      "Date for when this fact / relation starts to be valid, please use ISO format YYYY-MM-DDTHH:MM:SS[+HH:MM]",
		"{{[rel_valid_until]}}":      "Date for when this fact / relation stops being valid, please use ISO format YYYY-MM-DDTHH:MM:SS[+HH:MM]",
		"{{[rel_fact]}}":             "Fact regarding the relation",
		"{{[rel_name]}}":             "Name of factual relation",
		"{{[rel_source_name]}}":      "Name of source entity",
		"{{[rel_source_id]}}":        "ID of source entity",
		"{{[rel_target_name]}}":      "Name of target entity",
		"{{[rel_target_id]}}":        "ID of target entity",
		"{{[rel_ext_list]}}":         "List of extracted relations",
		"{{[rel_filter_list]}}":      "List of IDs for facts that are likely relevant to the provided entity",
		"{{[rel_filter_reasoning]}}": "A brief reasoning for why certain facts are irrelevant, no need to be extensive (within 150 words)",
		"{{[rel_dupe_need_merge]}}":  "Whether merging is required, if no merge then other fields can be left empty",
		"{{[rel_dupe_reasoning]}}":   "Why do we need to merge the new relation with existing?",
		"{{[rel_dupe_content]}}":     "Updated fact regarding the relation",
		"{{[rel_dupe_id_list]}}":     "List of IDs for existing relations that should be merged within the new relation",
		// Datetime
		"{{[year]}}":      "year",
		"{{[month]}}":     "month",
		"{{[day]}}":       "day",
		"{{[hour]}}":      "hour",
		"{{[minute]}}":    "minute",
		"{{[second]}}":    "second",
		"{{[tz_name]}}":   "Timezone's name",
		"{{[tz_offset]}}": "Offset from UTC (use +HH:MM format)",
		"{{[tz_reason]}}": "Why this candidate",
		"{{[tz_list]}}":   "List of candidate timezones",
		// Misc.
		":": ":",
	}

	registry.RegisteredLanguage[lang] = struct{}{}
}
