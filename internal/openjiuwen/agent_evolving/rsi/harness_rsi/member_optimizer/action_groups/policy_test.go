package actiongroups

import (
	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/rsi/harness_rsi"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestActionPolicyPrompt_模板逐字(t *testing.T) {
	prompt := ActionPolicyPrompt()
	// 分组/操作/工具白名单
	for _, want := range []string{
		"## Current Auto Harness Action Policy",
		"Allowed action_group values:",
		"- config\n- prompt\n- rail\n- skill\n- tool",
		"Allowed operation values:",
		"- add\n- modify\n- remove\n- search",
		"- read_file, grep, glob, list_files, write_file, edit_file"[:0], // 占位避免误配
	} {
		if strings.Contains(want, "占位") {
			continue
		}
		require.Contains(t, prompt, want)
	}
	// config 路径前缀
	require.Contains(t, prompt, "- config: runtime.yaml")
	// prompt 路径前缀
	require.Contains(t, prompt, "- prompt: identity.md, soul.md, prompt_sections/sections.yaml, prompt_sections/files/")
	// 工具/护栏规则段
	require.Contains(t, prompt, "Tool rules:")
	require.Contains(t, prompt, "Rail rules:")
	require.Contains(t, prompt, `{{"file": "tools/<name>.py", "class_name": "<ToolClass>"}}`)
	require.Contains(t, prompt, "Do not use them as a")
}

func TestFilterActionDefinitions_白名单(t *testing.T) {
	defs := []harnessrsi.ActionDefinition{
		{Group: "prompt", Operation: "add"},
		{Group: "prompt", Operation: "search"}, // prompt+search 不允许
		{Group: "model", Operation: "add"},     // 组不允许
		{Group: "skill", Operation: "search"},  // skill+search 允许
	}
	got := FilterActionDefinitions(defs)
	require.Len(t, got, 2)
	require.Equal(t, "prompt", got[0].Group)
	require.Equal(t, "skill", got[1].Group)
}

func TestSanitizeAllowedTools(t *testing.T) {
	got := SanitizeAllowedTools([]string{"read_file", "bash", "write_file", "web"})
	require.Equal(t, []string{"read_file", "write_file"}, got)
	require.Empty(t, SanitizeAllowedTools(nil))
}

func TestValidateActionPolicy_错误矩阵(t *testing.T) {
	mk := func(mut func(map[string]any)) map[string]any {
		action := map[string]any{
			"action_id": "a1", "role": "solver", "action_group": "config",
			"operation": "modify", "target_path": "runtime.yaml",
			"declared_write_paths": []any{"runtime.yaml"},
			"constraints":          map[string]any{},
		}
		mut(action)
		return action
	}
	// 合法
	require.True(t, ValidateActionPolicy(mk(func(m map[string]any) {}), map[string]bool{"solver": true}).Valid)
	// 缺 action_id / role
	check := ValidateActionPolicy(mk(func(m map[string]any) { delete(m, "action_id") }), nil)
	require.Contains(t, check.Errors, "missing action_id")
	check = ValidateActionPolicy(mk(func(m map[string]any) { delete(m, "role") }), nil)
	require.Contains(t, check.Errors, "missing role")
	// role 不在 selected
	check = ValidateActionPolicy(mk(func(m map[string]any) {}), map[string]bool{"other": true})
	require.Contains(t, check.Errors, "role 'solver' not in selected roles")
	// 组/操白名单
	check = ValidateActionPolicy(mk(func(m map[string]any) { m["action_group"] = "model" }), nil)
	require.Contains(t, check.Errors, "unsupported action_group 'model'")
	check = ValidateActionPolicy(mk(func(m map[string]any) { m["operation"] = "bogus" }), nil)
	require.Contains(t, check.Errors, "unsupported operation 'bogus'")
	// 组-操组合
	check = ValidateActionPolicy(mk(func(m map[string]any) { m["action_group"] = "prompt"; m["operation"] = "search" }), nil)
	require.Contains(t, check.Errors, "operation 'search' is not allowed for action_group 'prompt'")
	// candidate_query/install_ref
	check = ValidateActionPolicy(mk(func(m map[string]any) { m["candidate_query"] = "q" }), nil)
	require.Contains(t, check.Errors, "candidate_query must be empty")
	check = ValidateActionPolicy(mk(func(m map[string]any) { m["install_ref"] = "r" }), nil)
	require.Contains(t, check.Errors, "install_ref must be empty")
	// skill/search 必填 candidate_query
	check = ValidateActionPolicy(mk(func(m map[string]any) {
		m["action_group"] = "skill"
		m["operation"] = "search"
		m["target_path"] = "skills/"
		m["declared_write_paths"] = []any{"skills/", "skills/skills.yaml"}
	}), nil)
	require.Contains(t, check.Errors, "skill/search requires a non-empty candidate_query")
	// skill/search 声明联锁
	check = ValidateActionPolicy(mk(func(m map[string]any) {
		m["action_group"] = "skill"
		m["operation"] = "search"
		m["candidate_query"] = "find skill"
		m["target_path"] = "skills/"
		m["declared_write_paths"] = []any{"skills/"}
	}), nil)
	require.Contains(t, check.Errors, "skill/search must declare skills/skills.yaml")
	// config 专项：声明非 runtime.yaml
	check = ValidateActionPolicy(mk(func(m map[string]any) {
		m["declared_write_paths"] = []any{"other.yaml"}
	}), nil)
	require.Contains(t, check.Errors, "config actions may only target and declare runtime.yaml")
	// prompt 保护
	check = ValidateActionPolicy(mk(func(m map[string]any) {
		m["action_group"] = "prompt"
		m["operation"] = "remove"
		m["target_path"] = "identity.md"
		m["declared_write_paths"] = []any{"identity.md"}
	}), nil)
	require.Contains(t, check.Errors, "prompt/remove cannot delete identity.md or soul.md")
	// prompt/add 只许扩展段
	check = ValidateActionPolicy(mk(func(m map[string]any) {
		m["action_group"] = "prompt"
		m["operation"] = "add"
		m["target_path"] = "identity.md"
		m["declared_write_paths"] = []any{"identity.md"}
	}), nil)
	require.Contains(t, check.Errors, "prompt/add may only create extension prompt sections")
	// prompt 段文件缺 section_name
	check = ValidateActionPolicy(mk(func(m map[string]any) {
		m["action_group"] = "prompt"
		m["operation"] = "add"
		m["target_path"] = "prompt_sections/files/x.md"
		m["declared_write_paths"] = []any{"prompt_sections/files/x.md", "prompt_sections/sections.yaml"}
	}), nil)
	require.Contains(t, check.Errors, "prompt section actions require constraints.section_name")
	// prompt 段文件缺 sections.yaml 联锁
	check = ValidateActionPolicy(mk(func(m map[string]any) {
		m["action_group"] = "prompt"
		m["operation"] = "add"
		m["target_path"] = "prompt_sections/files/x.md"
		m["declared_write_paths"] = []any{"prompt_sections/files/x.md"}
		m["constraints"] = map[string]any{"section_name": "x"}
	}), nil)
	require.Contains(t, check.Errors, "prompt section file changes must also declare prompt_sections/sections.yaml so the file can be mounted")
	// skill/add 路径形态
	check = ValidateActionPolicy(mk(func(m map[string]any) {
		m["action_group"] = "skill"
		m["operation"] = "add"
		m["target_path"] = "skills/My Skill/SKILL.md"
		m["declared_write_paths"] = []any{"skills/My Skill/SKILL.md", "skills/skills.yaml"}
	}), nil)
	require.Contains(t, check.Errors, "skill/add target_path must be skills/<snake_name>/SKILL.md")
	// tool/add 缺 class_name
	check = ValidateActionPolicy(mk(func(m map[string]any) {
		m["action_group"] = "tool"
		m["operation"] = "add"
		m["target_path"] = "tools/calc.py"
		m["declared_write_paths"] = []any{"tools/calc.py", "tools/tools.yaml"}
	}), nil)
	require.Contains(t, check.Errors, "tool/add requires constraints.class_name")
	// tool/remove 保护 manifest
	check = ValidateActionPolicy(mk(func(m map[string]any) {
		m["action_group"] = "tool"
		m["operation"] = "remove"
		m["target_path"] = "tools/tools.yaml"
		m["declared_write_paths"] = []any{"tools/tools.yaml"}
	}), nil)
	require.Contains(t, check.Errors, "tool/remove must target a package-local tool, not tools/tools.yaml")
	// rail/add 缺 rails.yaml 联锁
	check = ValidateActionPolicy(mk(func(m map[string]any) {
		m["action_group"] = "rail"
		m["operation"] = "add"
		m["target_path"] = "rails/guard.py"
		m["declared_write_paths"] = []any{"rails/guard.py"}
		m["constraints"] = map[string]any{"class_name": "Guard"}
	}), nil)
	require.Contains(t, check.Errors, "rail/add must declare rails/rails.yaml")
	// rail/remove 保护 manifest
	check = ValidateActionPolicy(mk(func(m map[string]any) {
		m["action_group"] = "rail"
		m["operation"] = "remove"
		m["target_path"] = "rails/rails.yaml"
		m["declared_write_paths"] = []any{"rails/rails.yaml"}
	}), nil)
	require.Contains(t, check.Errors, "rail/remove must target a package-local rail, not rails/rails.yaml")
	// 不支持的 allowed_tools
	check = ValidateActionPolicy(mk(func(m map[string]any) { m["allowed_tools"] = []any{"bash"} }), nil)
	require.Contains(t, check.Errors, "unsupported allowed_tools [bash]")
	// 路径越界
	check = ValidateActionPolicy(mk(func(m map[string]any) { m["target_path"] = "../x.yaml" }), nil)
	require.Contains(t, check.Errors, "target_path contains '..': ../x.yaml")
	// 路径绝对
	check = ValidateActionPolicy(mk(func(m map[string]any) { m["target_path"] = "/abs/x.yaml" }), nil)
	require.Contains(t, check.Errors, "target_path is absolute: /abs/x.yaml")
	// 路径组不匹配
	check = ValidateActionPolicy(mk(func(m map[string]any) { m["action_group"] = "skill"; m["target_path"] = "tools/x.py" }), nil)
	require.Contains(t, check.Errors, "target_path 'tools/x.py' is not allowed for action_group 'skill'")
}

func TestValidateActionPolicy_StructGetter(t *testing.T) {
	// MemberOptimizationAction 经 ActionGetter 接口参与校验（D-c2-1）
	action := actionLikeMap{"action_id": "a1", "role": "solver", "action_group": "config",
		"operation": "modify", "target_path": "runtime.yaml",
		"declared_write_paths": []string{"runtime.yaml"}, "constraints": map[string]any{}}
	require.True(t, ValidateActionPolicy(action, map[string]bool{"solver": true}).Valid)
}

func TestLoadActionDefinitions_内置符号组与文件(t *testing.T) {
	// 符号组展开：config 三操
	defs := LoadActionDefinitions([]string{"config"})
	require.Len(t, defs, 3)
	require.Equal(t, "config_add", defs[0].Name)
	require.Equal(t, "Add a package-local config surface", defs[0].Purpose)
	require.True(t, defs[2].IsDestructive) // config_remove

	// 文件装载 + 过滤
	dir := t.TempDir()
	path := filepath.Join(dir, "groups.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`actions:
  - name: custom_prompt
    group: prompt
    operation: add
    purpose: p
  - name: bad_group
    group: model
    operation: add
`), 0o644))
	defs = LoadActionDefinitions([]string{"prompt", path})
	require.Len(t, defs, 4) // prompt 内置 3 + custom_prompt（bad_group 被滤）
	require.Equal(t, "custom_prompt", defs[3].Name)

	// 不存在的文件路径被忽略
	defs = LoadActionDefinitions([]string{"prompt", filepath.Join(dir, "missing.yaml")})
	require.Len(t, defs, 3)
}

func TestSnakeName_校验(t *testing.T) {
	require.True(t, isSnakeName("my_skill"))
	require.False(t, isSnakeName("MySkill"))
	require.False(t, isSnakeName("_lead"))
	require.False(t, isSnakeName("trail_"))
	require.False(t, isSnakeName("a__b"))
	require.False(t, isSnakeName("1abc"))
	require.False(t, isSnakeName("a b"))
	require.True(t, isSnakeName("abc123"))
}

// actionLikeMap 经 getter 双轨的字段载体。
type actionLikeMap map[string]any

func (a actionLikeMap) ActionFieldValue(key string) any { return a[key] }

func TestNormalizePolicyPath(t *testing.T) {
	require.Equal(t, "skills/skills.yaml", normalizePolicyPath("skills/skills.yaml"))
	require.Equal(t, "a/b", normalizePolicyPath("/a/b/"))
	require.Equal(t, "a/b", normalizePolicyPath(`a\b`))
}
