package cn

import "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/registry"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册中文语言的多语言描述和格式化模板
//
// Python: entity_extraction/cn.py register_language()
func init() {
	lang := "cn"

	registry.SourceDescription[lang] = "\n<数据源描述>\n{source_description}\n</数据源描述>\n"
	registry.RefJSONObjectDef[lang] = "相关JSON Object定义"
	registry.OutputFormat[lang] = "输出定义（最终输出需要为JSON）"
	registry.DisplayEntity[lang] = "{i}. {name}：\n{content}"
	registry.MarkCurrentMsg[lang] = "<当前信息>\n{content}\n</当前信息>\n"
	registry.MarkHistoryMsg[lang] = "<历史信息>\n{history}\n</历史信息>\n"
	registry.RelationFormat[lang] = "{name}（<{lhs}>-[{name}]-<{rhs}>）：{description}"
	registry.NoRelationGiven[lang] = "无"

	registry.EntityDefinitionDescription[lang] = "：默认实体类型。若该实体不属于其他提供的类型，请选此类。"
	registry.HumanEntityDescription[lang] = "：代表人类的实体类型，可以是用户也可以是其他人。"
	registry.AIEntityDescription[lang] = "：代表AI的实体类型，可能是聊天助手也可能是其他智能体。"
	registry.RelationDefinitionDescription[lang] = "：默认实体联系类型。"

	registry.MultilingualDescription[lang] = map[string]string{
		// Entity
		"{{[ent_def_name]}}":     "新提取实体的名字",
		"{{[ent_def_type]}}":     "新提取实体的类型id，需要在提供的实体类型中",
		"{{[ent_ext_list]}}":     "新提取的实体列表",
		"{{[ent_summary]}}":      "实体相关的重要信息，500字以内的简要摘要",
		"{{[ent_attributes]}}":   "实体的属性值",
		"{{[ent_info]}}":         "提取的实体属性与摘要",
		"{{[ent_valid_since]}}":  "实体的生效日期，请使用ISO格式YYYY-MM-DDTHH:MM:SS[+HH:MM]",
		"{{[ent_valid_until]}}":  "实体的中止日期，请使用ISO格式YYYY-MM-DDTHH:MM:SS[+HH:MM]",
		"{{[ent_dupe_name]}}":    "现有实体的名字",
		"{{[ent_dupe_id]}}":      "现有实体的ID",
		"{{[ent_dupe_id_list]}}": "与现有实体重复的实体ID列表",
		"{{[ent_dupe_list]}}":    "重复实体列表",
		// Relation
		"{{[rel_valid_since]}}":      "事实/关系的生效日期，请使用ISO格式YYYY-MM-DDTHH:MM:SS[+HH:MM]",
		"{{[rel_valid_until]}}":      "事实/关系的中止日期，请使用ISO格式YYYY-MM-DDTHH:MM:SS[+HH:MM]",
		"{{[rel_fact]}}":             "关于实体联系的事实",
		"{{[rel_name]}}":             "该实体联系的名称",
		"{{[rel_source_name]}}":      "主体的实体名称",
		"{{[rel_source_id]}}":        "主体的实体ID",
		"{{[rel_target_name]}}":      "客体的实体名称",
		"{{[rel_target_id]}}":        "客体的实体ID",
		"{{[rel_ext_list]}}":         "新提取的关系列表",
		"{{[rel_filter_list]}}":      "与给定实体可能相关的事实ID列表",
		"{{[rel_filter_reasoning]}}": "关于为何特定事实与给定实体无关，提供简单推理过程, 无需过于详尽（100字内）",
		"{{[rel_dupe_need_merge]}}":  "是否需要融合，若不需要后面所有内容可以留空",
		"{{[rel_dupe_reasoning]}}":   "为何需要将新增关系与现有关系融合？",
		"{{[rel_dupe_content]}}":     "更新后的实体联系事实",
		"{{[rel_dupe_id_list]}}":     "需要与新增关系融合的已有关系ID列表",
		// Datetime
		"{{[year]}}":      "年",
		"{{[month]}}":     "月",
		"{{[day]}}":       "日",
		"{{[hour]}}":      "小时",
		"{{[minute]}}":    "分钟",
		"{{[second]}}":    "秒",
		"{{[tz_name]}}":   "时区名",
		"{{[tz_offset]}}": "相对于UTC标准时的时差（用+HH:MM格式）",
		"{{[tz_reason]}}": "为什么可能是这个时区",
		"{{[tz_list]}}":   "可能的时区列表",
		// Misc.
		":": "：",
	}

	registry.RegisteredLanguage[lang] = struct{}{}
}
