package skills

import (
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Skill 表示一个技能的元数据。
//
// 每个技能对应磁盘上一个包含 SKILL.md 文件的目录。
// SKILL.md 文件使用 YAML front matter 格式，其中 description 字段被解析为技能描述。
//
// 对应 Python: Skill(BaseModel)
type Skill struct {
	// Name 技能名称（通常为 SKILL.md 所在目录的目录名）
	Name string
	// Description 技能描述（从 SKILL.md YAML front matter 的 description 字段提取）
	Description string
	// Directory 技能所在目录路径
	Directory string
}

// ──────────────────────────── 常量 ────────────────────────────

// descriptionTruncateLen __repr__ 中 description 截断长度
const descriptionTruncateLen = 30

// ──────────────────────────── 全局变量 ────────────────────────────

// ensure 检查截断函数与 GoString 逻辑一致（编译期检查）。
var _ = truncateDescription

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkill 创建 Skill 实例。
func NewSkill(name, description, directory string) *Skill {
	return &Skill{
		Name:        name,
		Description: description,
		Directory:   directory,
	}
}

// AsDict 将 Skill 转为字典。
//
// includeDirectory 为 true 时包含 directory 字段，为 false 时省略。
//
// 对应 Python: Skill.asdict(include_directory=True)
func (s *Skill) AsDict(includeDirectory bool) map[string]any {
	result := map[string]any{
		"name":        s.Name,
		"description": s.Description,
	}
	if includeDirectory {
		result["directory"] = s.Directory
	}
	return result
}

// String 返回 Skill 的多行可读格式。
//
// 对应 Python: Skill.__str__()
func (s *Skill) String() string {
	return fmt.Sprintf("Skill: %s\nDescription: %s\nDirectory: %s", s.Name, s.Description, s.Directory)
}

// GoString 返回 Skill 的单行紧凑格式，description 截断至 30 字符。
//
// 对应 Python: Skill.__repr__()
func (s *Skill) GoString() string {
	desc := s.Description
	if len(desc) > descriptionTruncateLen {
		desc = desc[:descriptionTruncateLen] + "..."
	}
	return fmt.Sprintf("[Skill: %s / Description: %s / Directory: %s]", s.Name, desc, s.Directory)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// truncateDescription 截断描述字符串，用于 GoString。
func truncateDescription(desc string) string {
	if len(desc) > descriptionTruncateLen {
		return desc[:descriptionTruncateLen] + "..."
	}
	return desc
}
