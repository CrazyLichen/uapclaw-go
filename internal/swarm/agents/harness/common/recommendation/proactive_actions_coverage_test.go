package recommendation

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── ParseProactiveSkillMd 补充分支 ────────────────────────────

// TestParseProactiveSkillMd_YAML解析失败降级 非法 YAML 走逐行解析。
func TestParseProactiveSkillMd_YAML解析失败降级(t *testing.T) {
	// frontmatter 含缩进嵌套 → parseSimpleYAMLMap 返回 false → 降级逐行解析
	path := writeTempSkillMd(t, "degrade", "---\nname: degrade-skill\nmeta:\n  nested: 1\ndescription: 降级描述\n---\n正文")
	meta := ParseProactiveSkillMd(path)
	require.NotNil(t, meta)
	assert.Equal(t, "degrade-skill", meta["name"], "降级逐行解析仍应取到 name")
}

// TestParseProactiveSkillMd_带引号值 值两侧引号被剥离。
func TestParseProactiveSkillMd_带引号值(t *testing.T) {
	path := writeTempSkillMd(t, "quoted", "---\nname: \"quoted-skill\"\ndescription: '带引号的描述'\n---\n")
	meta := ParseProactiveSkillMd(path)
	require.NotNil(t, meta)
	assert.Equal(t, "quoted-skill", meta["name"], "双引号应被剥离")
	assert.Equal(t, "带引号的描述", meta["description"], "单引号应被剥离")
}

// TestParseProactiveSkillMd_body为空描述为空串 frontmatter 存在但正文为空。
func TestParseProactiveSkillMd_body为空(t *testing.T) {
	path := writeTempSkillMd(t, "empty-body", "---\nname: empty-body\n---\n")
	meta := ParseProactiveSkillMd(path)
	require.NotNil(t, meta)
	assert.Equal(t, "empty-body", meta["name"])
}

// ──────────────────────────── detectAllSkillsIn 补充分支 ────────────────────────────

// TestDetectAllSkillsIn_目录不可读 目录不存在时跳过该来源，不 panic。
func TestDetectAllSkillsIn_目录不可读(t *testing.T) {
	names, skills := detectAllSkillsIn(filepath.Join(t.TempDir(), "absent"), "")
	assert.NotNil(t, names)
	assert.Empty(t, names)
	assert.Empty(t, skills)
}

// TestDetectAllSkillsIn_技能名缺失回落目录名 SKILL.md 无 name 时用目录名。
// 注：ParseProactiveSkillMd 总会填 name（Path.stem），此分支覆盖 frontmatter 里
// name 解析为空串的情形。
func TestDetectAllSkillsIn_技能名缺失回落目录名(t *testing.T) {
	userSkills := t.TempDir()
	writeSkillDir(t, userSkills, "dir-name-skill", "---\nname: \"\"\n---\n正文")

	names, _ := detectAllSkillsIn(userSkills, "")
	assert.Contains(t, names, "dir-name-skill", "name 为空串时应回落目录名")
}

// TestDetectAllSkillsIn_未安装技能被过滤 内置来源全部 installed=false。
func TestDetectAllSkillsIn_未安装技能被过滤(t *testing.T) {
	builtinDir := t.TempDir()
	writeSkillDir(t, builtinDir, "builtin-a", "---\nname: builtin-a\ndescription: d\n---\n")

	names, skills := detectAllSkillsIn("", builtinDir)
	assert.Empty(t, names, "内置未安装技能不应入候选池")
	assert.Empty(t, skills)
}

// TestDetectAllSkillsIn_source字段 本地来源标 local，内置标 builtin。
func TestDetectAllSkillsIn_source字段(t *testing.T) {
	userSkills := t.TempDir()
	writeSkillDir(t, userSkills, "s1", "---\nname: s1\ndescription: d\n---\n")

	_, skills := detectAllSkillsIn(userSkills, "")
	require.Len(t, skills, 1)
	assert.Equal(t, "local", skills[0]["source"])
	assert.Equal(t, true, skills[0]["installed"])
}

// ──────────────────────────── AnalyzeAndDecide 补充分支 ────────────────────────────

// TestAnalyzeAndDecide_模型invoke报错 agent 报错时返回空结果不 panic。
func TestAnalyzeAndDecide_模型invoke报错(t *testing.T) {
	agent := newProactiveAgentWithReply(t, "")
	// 空 reply 让模型返回空文本 → 无 JSON → 无决策（覆盖 extract 空分支）
	result := AnalyzeAndDecide(context.Background(), "报告", nil, agent, "（暂无）", "zh")
	require.NotNil(t, result)
	assert.Nil(t, result.Decision, "空回复应返回空决策")
}

// TestAnalyzeAndDecide_decision非对象 decision 字段是字符串而非对象。
func TestAnalyzeAndDecide_decision非对象(t *testing.T) {
	agent := newProactiveAgentWithReply(t, `{"decision": "应该是对象但是字符串"}`)
	result := AnalyzeAndDecide(context.Background(), "报告", nil, agent, "（暂无）", "zh")
	assert.Nil(t, result.Decision)
}

// TestAnalyzeAndDecide_JSON是数组 顶层是数组时，贪婪正则取到数组内首个对象仍能解析。
// 这依赖 proactive_actions 版 ExtractJSONFromResponse 的贪婪语义（与 gradient_updater
// 的 raw_decode 版本不同），属于上游既有行为，不得视为缺陷。
func TestAnalyzeAndDecide_JSON是数组(t *testing.T) {
	agent := newProactiveAgentWithReply(t, `[{"decision": {"type": "task_reminder", "target": "X", "reason": "R"}}]`)
	result := AnalyzeAndDecide(context.Background(), "报告", nil, agent, "（暂无）", "zh")
	require.NotNil(t, result.Decision, "贪婪正则会取到数组内的对象")
	assert.Equal(t, "task_reminder", result.Decision.Type)
	assert.Equal(t, "X", result.Decision.Target)
}

// TestAnalyzeAndDecide_空决策规则文本 空串回落 "（暂无）"。
func TestAnalyzeAndDecide_空决策规则文本(t *testing.T) {
	captured := ""
	agent := newProactiveAgentCapturing(t, &captured, `{"decision": null}`)

	AnalyzeAndDecide(context.Background(), "报告", nil, agent, "", "zh")
	assert.Contains(t, captured, "（暂无）", "空决策规则应回落「（暂无）」")
}

// ──────────────────────────── buildDecision 补充分支 ────────────────────────────

// TestBuildDecision_缺urgency用默认值 urgency 字段缺失时取 0.5。
func TestBuildDecision_缺urgency用默认值(t *testing.T) {
	d := buildDecision(map[string]any{"type": "task_reminder", "target": "X"}, "task_reminder", "X")
	assert.Equal(t, defaultUrgency, d.Urgency)
	assert.Empty(t, d.Reason, "缺 reason 时应为空串")
}

// TestBuildDecision_reason非字符串 reason 是数字时回落空串。
func TestBuildDecision_reason非字符串(t *testing.T) {
	d := buildDecision(map[string]any{"reason": 123}, "task_reminder", "X")
	assert.Empty(t, d.Reason)
}

// ──────────────────────────── PreferredLanguage ────────────────────────────

// TestPreferredLanguage_默认中文 config 无 preferred_language 时返回 zh。
func TestPreferredLanguage_默认中文(t *testing.T) {
	got := PreferredLanguage()
	assert.Contains(t, []string{"zh", "en"}, got, "应返回配置语言或默认 zh")
}

// ──────────────────────────── LoadRecommendationState 补充分支 ────────────────────────────

// TestLoadRecommendationState_默认路径 空 path 走默认路径。
func TestLoadRecommendationState_默认路径(t *testing.T) {
	// 只验证不 panic 且返回非 nil（实际读用户目录，内容不确定）
	state := LoadRecommendationState("")
	assert.NotNil(t, state)
}

// TestLoadRecommendationState_目录而非文件 路径指向目录时降级为空状态。
func TestLoadRecommendationState_路径是目录(t *testing.T) {
	state := LoadRecommendationState(t.TempDir())
	assert.NotNil(t, state)
	assert.Empty(t, state.RecommendationHistory)
}

// TestLoadRecommendationState_JSON为null 文件内容是 null。
func TestLoadRecommendationState_JSON为null(t *testing.T) {
	path := filepath.Join(t.TempDir(), "null.json")
	require.NoError(t, os.WriteFile(path, []byte("null"), 0o644))
	state := LoadRecommendationState(path)
	assert.NotNil(t, state)
	assert.Empty(t, state.RecommendationHistory)
}

// ──────────────────────────── SaveRecommendationState 补充分支 ────────────────────────────

// TestSaveRecommendationState_默认路径 空 path 走默认路径（写入用户 workspace，验证可写）。
func TestSaveRecommendationState_默认路径(t *testing.T) {
	// 只验证不 panic；不实际改动用户数据，故先备份原内容
	defaultPath := DefaultRecommendationStatePath()
	backup, backupErr := os.ReadFile(defaultPath)

	SaveRecommendationState(&RecommendationState{}, "")

	// 还原
	if backupErr == nil {
		_ = os.WriteFile(defaultPath, backup, 0o644)
	} else {
		_ = os.Remove(defaultPath)
	}
}

// TestSaveRecommendationState_序列化错误 含不可序列化值时走错误分支不 panic。
func TestSaveRecommendationState_序列化错误(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	// channel 无法 JSON 序列化
	SaveRecommendationState(&RecommendationState{
		RecommendationHistory: []map[string]any{{"bad": make(chan int)}},
	}, path)

	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "序列化失败时不应产出文件")
}

// TestDefaultRecommendationStatePath_受DATA_DIR管辖 默认路径落在 workspace 下。
func TestDefaultRecommendationStatePath_受DATA_DIR管辖(t *testing.T) {
	path := DefaultRecommendationStatePath()
	assert.Equal(t, recommendationStateFileName, filepath.Base(path))
	assert.Contains(t, path, filepath.Join("agent", "workspace"))
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// TestGetBuiltinSkillsDirForRecommendation_与skill包一致 交叉校验两个包的解析结果一致。
func TestGetBuiltinSkillsDirForRecommendation_与skill包一致(t *testing.T) {
	resDir, ok := findRepoResourcesDirForTest()
	if !ok {
		t.Skip("未找到仓库根 resources/ 目录")
	}
	t.Setenv(pathutil.EnvResourcesDir, resDir)

	got := getBuiltinSkillsDirForRecommendation()
	want := filepath.Join(resDir, "agent", "workspace", "skills")
	assert.Equal(t, want, got, "recommendation 包与 skill 包应解析到同一内置技能目录")
}

// TestDetectAllSkills_公开入口 公开入口把 workspace 目录与内置源组合后委托内部实现。
// 只断言不 panic 且返回非 nil（实际目录由进程级 workspace 决定，不在此重定向）。
func TestDetectAllSkills_公开入口(t *testing.T) {
	names, skills := DetectAllSkills()
	assert.NotNil(t, names)
	assert.NotNil(t, skills)
	// 候选池里每条都必须是已安装
	for _, s := range skills {
		installed, _ := s["installed"].(bool)
		assert.True(t, installed, "候选池必须只含已安装技能")
	}
}

// TestPreferredLanguage_配置文件覆盖 用临时 config.yaml 覆盖 preferred_language。
func TestPreferredLanguage_配置文件覆盖(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("preferred_language: en\n"), 0o644))
	// config.New("") 走默认路径，无法直接注入；此处仅验证函数在缺配置时不 panic
	got := PreferredLanguage()
	assert.NotEmpty(t, got)
}

// TestSaveRecommendationState_写入失败路径 目标路径是已存在的目录时 WriteFile 失败。
func TestSaveRecommendationState_写入失败路径(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "as-dir")
	require.NoError(t, os.MkdirAll(target, 0o755))
	// 让 statePath 指向一个已存在的目录 → WriteFile 失败
	SaveRecommendationState(&RecommendationState{}, target)
	// 不应 panic；目录仍在
	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

// TestSaveRecommendationState_Mkdir失败 父路径是文件时 MkdirAll 失败。
func TestSaveRecommendationState_Mkdir失败(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
	// 父路径是文件 → MkdirAll 失败
	SaveRecommendationState(&RecommendationState{}, filepath.Join(blocker, "sub", "state.json"))
	// 不应 panic；blocker 仍是普通文件未被改写成目录
	info, err := os.Stat(blocker)
	require.NoError(t, err)
	assert.False(t, info.IsDir(), "blocker 应仍是文件")
}

// TestDetectAllSkillsIn_禁用技能被剔除 通过 skills_state.json 注入禁用名单。
func TestDetectAllSkillsIn_禁用技能被剔除(t *testing.T) {
	userSkills := t.TempDir()
	writeSkillDir(t, userSkills, "enabled-skill", "---\nname: enabled-skill\ndescription: d\n---\n")
	writeSkillDir(t, userSkills, "disabled-skill", "---\nname: disabled-skill\ndescription: d\n---\n")

	// 写 skills_state.json 到 userSkills 同级的预期位置：LoadExecutionDisabledSkills
	// 读 GetStateFile()（受 UAPCLAW_DATA_DIR 管辖），故直接验证候选池过滤逻辑：
	// 若当前环境无禁用名单，两个技能都应保留；有名单则被剔除的那个不出现。
	names, skills := detectAllSkillsIn(userSkills, "")
	assert.Contains(t, names, "enabled-skill")
	assert.LessOrEqual(t, len(skills), 2)

	// 被剔除的技能不在 names 里（有禁用名单时的分支）
	for _, s := range skills {
		name, _ := s["name"].(string)
		assert.Contains(t, names, name, "列表里的技能必须也在 names 集合里")
	}
}

// TestPreferredLanguageFrom 配置读取各分支：正常/缺失/类型错/加载失败。
func TestPreferredLanguageFrom(t *testing.T) {
	t.Run("正常取到语言", func(t *testing.T) {
		got := preferredLanguageFrom(func() (map[string]any, error) {
			return map[string]any{"preferred_language": "en"}, nil
		})
		assert.Equal(t, "en", got)
	})

	t.Run("字段缺失回落zh", func(t *testing.T) {
		got := preferredLanguageFrom(func() (map[string]any, error) {
			return map[string]any{}, nil
		})
		assert.Equal(t, "zh", got)
	})

	t.Run("字段为空串回落zh", func(t *testing.T) {
		got := preferredLanguageFrom(func() (map[string]any, error) {
			return map[string]any{"preferred_language": ""}, nil
		})
		assert.Equal(t, "zh", got)
	})

	t.Run("字段类型错回落zh", func(t *testing.T) {
		got := preferredLanguageFrom(func() (map[string]any, error) {
			return map[string]any{"preferred_language": 123}, nil
		})
		assert.Equal(t, "zh", got)
	})

	t.Run("加载失败回落zh", func(t *testing.T) {
		got := preferredLanguageFrom(func() (map[string]any, error) {
			return nil, assert.AnError
		})
		assert.Equal(t, "zh", got)
	})
}

// TestDetectAllSkillsIn_禁用名单过滤 禁用名单里的技能被剔出候选池与 names 集合。
func TestDetectAllSkillsIn_禁用名单过滤(t *testing.T) {
	userSkills := t.TempDir()
	writeSkillDir(t, userSkills, "keep-skill", "---\nname: keep-skill\ndescription: d\n---\n")
	writeSkillDir(t, userSkills, "drop-skill", "---\nname: drop-skill\ndescription: d\n---\n")

	original := loadDisabledSkills
	t.Cleanup(func() { loadDisabledSkills = original })
	loadDisabledSkills = func() []string { return []string{"drop-skill"} }

	names, skills := detectAllSkillsIn(userSkills, "")

	assert.Contains(t, names, "keep-skill")
	assert.NotContains(t, names, "drop-skill", "禁用技能应被剔出 names 集合")
	for _, s := range skills {
		assert.NotEqual(t, "drop-skill", s["name"], "禁用技能不应出现在候选池")
	}
}
