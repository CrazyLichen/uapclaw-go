// 本文件对齐 openjiuwen/harness_providers/skills.py。
// 把 portable 技能包整目录复制进各 CLI 的发现目录。
package harness_providers

import (
	"fmt"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillSource 一个 manifest 技能目录或含技能目录的技能库。
// Python: SkillSource (skills.py:23-40, frozen dataclass)
type SkillSource struct {
	// Dir 技能包或技能库根目录
	Dir string
	// Mode 取值 all 或 auto_list
	Mode string
	// EnabledSkills auto_list 模式下的技能名白名单（nil 表示全选）
	EnabledSkills []string
}

// NewSkillSource 构造并校验 SkillSource。
// Python: __post_init__（skills.py:31-40）
func NewSkillSource(dir string, mode string, enabledSkills []string) (SkillSource, error) {
	if dir == "" {
		return SkillSource{}, fmt.Errorf("skill dir must be a non-empty path")
	}
	if mode != "all" && mode != "auto_list" {
		return SkillSource{}, fmt.Errorf("skill mode must be all or auto_list")
	}
	// Python: enabled_skills 为 None 表示全选；传数组时元素必须全是字符串
	// （Go 侧 []string 类型系统已保证元素为字符串，保留空参校验语义）
	return SkillSource{Dir: dir, Mode: mode, EnabledSkills: enabledSkills}, nil
}

// ──────────────────────────── 常量 ────────────────────────────

// scanDirs provider 名 → 发现目录（项目相对路径）。
// Python: _SCAN_DIRS (skills.py:19)
var scanDirs = map[string]string{
	"claudecode": ".claude/skills",
	"codex":      ".agents/skills",
	"dsh":        ".dsh/skills",
}

// ──────────────────────────── 全局变量 ────────────────────────────

// copyLock 进程内复制互斥锁。
// Python: _COPY_LOCK (skills.py:20)
var copyLock sync.Mutex

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeSkills 冻结 provider 配置，构造期不访问源文件。
// Python: normalize_skills (skills.py:43-59)
func NormalizeSkills(values []any, conflict string) ([]SkillSource, error) {
	if conflict != "skip" && conflict != "replace" && conflict != "append" {
		return nil, fmt.Errorf("skill_conflict must be skip, replace or append")
	}
	result := make([]SkillSource, 0, len(values))
	for _, value := range values {
		switch v := value.(type) {
		case SkillSource:
			result = append(result, v)
		case string:
			result = append(result, SkillSource{Dir: v, Mode: "all"})
		case map[string]any:
			// Python: SkillSource(**dict(value))——走 __post_init__ 校验
			dir, _ := v["dir"].(string)
			mode, _ := v["mode"].(string)
			if mode == "" {
				mode = "all"
			}
			var enabled []string
			if raw, ok := v["enabled_skills"].([]any); ok {
				for _, item := range raw {
					s, _ := item.(string)
					enabled = append(enabled, s)
				}
			}
			src, err := NewSkillSource(dir, mode, enabled)
			if err != nil {
				return nil, err
			}
			result = append(result, src)
		default:
			return nil, fmt.Errorf("skill source must be a directory or manifest skill declaration")
		}
	}
	return result, nil
}
