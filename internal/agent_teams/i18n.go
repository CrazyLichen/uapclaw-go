package agent_teams

import (
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Language 支持的语言代码。
// 已迁移到 schema 包，此处保留类型别名以兼容现有调用方。
type Language = schema.Language

// ──────────────────────────── 枚举 ────────────────────────────

const (
	// LanguageCN 中文
	LanguageCN = schema.LanguageCN
	// LanguageEN 英文
	LanguageEN = schema.LanguageEN
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// STRINGS 双语字典。已迁移到 schema 包。
var STRINGS = schema.STRINGS

// ──────────────────────────── 导出函数 ────────────────────────────

// SetLanguage 设置全局语言。委托到 schema.SetLanguage()。
func SetLanguage(lang Language) error { return schema.SetLanguage(lang) }

// GetLanguage 获取当前全局语言。委托到 schema.GetLanguage()。
func GetLanguage() Language { return schema.GetLanguage() }

// T 解析本地化字符串。委托到 schema.T()。
func T(key string, kwargs ...map[string]any) (string, error) { return schema.T(key, kwargs...) }

// ──────────────────────────── 非导出函数 ────────────────────────────
