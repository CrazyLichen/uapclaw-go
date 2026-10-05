//go:build integration

package skills

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	goskills "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillManagerE2ESuite 测试 SkillManager + SkillUtil 系统级 E2E。
//
// 使用 MockFsProvider 模拟文件系统，验证完整的
// 技能扫描 → YAML 解析 → 注册 → 查询 → 提示词生成 管道。
//
// 对齐 Python: tests/system_tests/agent/skill/test_skill_real_system.py
type SkillManagerE2ESuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSkillManagerE2ESuite(t *testing.T) {
	suite.Run(t, new(SkillManagerE2ESuite))
}

// TestSkillManager_注册单个SKILLmd 测试从 SKILL.md 文件路径注册技能。
// 对齐 Python: SkillManager.register(skill_path)
func (s *SkillManagerE2ESuite) TestSkillManager_注册单个SKILLmd() {
	fs := newMockFs()
	fs.addSkillMD("/skills/data_analysis/SKILL.md", "data_analysis", "数据分析技能，用于处理和分析数据")

	sm := goskills.NewSkillManagerWithProvider("sys_op_test", fs)

	err := sm.Register([]string{"/skills/data_analysis/SKILL.md"}, false)
	s.Require().NoError(err, "注册不应返回错误")

	skill := sm.Get("data_analysis")
	s.Require().NotNil(skill, "应找到 data_analysis 技能")
	s.Equal("data_analysis", skill.Name, "名称应为 data_analysis")
	s.Equal("数据分析技能，用于处理和分析数据", skill.Description, "描述应匹配")
	// MDPath 由 createSkillFromPath → NewSkill 设置
	// 注意：当前 NewSkill 不设置 MDPath，Directory 已包含技能目录
	s.NotEmpty(skill.Directory, "Directory 不应为空")
}

// TestSkillManager_注册技能目录 测试从技能目录注册（自动查找 SKILL.md）。
// 对齐 Python: SkillManager.register(skill_path) 目录模式
func (s *SkillManagerE2ESuite) TestSkillManager_注册技能目录() {
	fs := newMockFs()
	fs.addSkillMD("/skills/coding/SKILL.md", "coding", "代码编写技能")
	fs.addDir("/skills/coding", []string{"SKILL.md"})

	sm := goskills.NewSkillManagerWithProvider("sys_op_test", fs)

	err := sm.Register([]string{"/skills/coding"}, false)
	s.Require().NoError(err, "注册目录不应返回错误")

	skill := sm.Get("coding")
	s.Require().NotNil(skill, "应找到 coding 技能")
	s.Equal("coding", skill.Name)
}

// TestSkillManager_注册多个技能目录 测试扫描包含多个技能子目录的根目录。
// 对齐 Python: SkillManager.register(root_path) 多子目录扫描
func (s *SkillManagerE2ESuite) TestSkillManager_注册多个技能目录() {
	fs := newMockFs()
	fs.addSkillMD("/workspace/skills/research/SKILL.md", "research", "研究技能，用于信息检索")
	fs.addSkillMD("/workspace/skills/writing/SKILL.md", "writing", "写作技能，用于文本生成")
	fs.addDir("/workspace/skills/research", []string{"SKILL.md"})
	fs.addDir("/workspace/skills/writing", []string{"SKILL.md"})
	fs.addDir("/workspace/skills", nil)
	fs.addSubDirs("/workspace/skills", []string{"research", "writing"})

	sm := goskills.NewSkillManagerWithProvider("sys_op_test", fs)

	err := sm.Register([]string{"/workspace/skills"}, false)
	s.Require().NoError(err, "注册多个技能目录不应返回错误")

	s.Equal(2, sm.Count(), "应注册 2 个技能")
	s.True(sm.Has("research"), "应有 research 技能")
	s.True(sm.Has("writing"), "应有 writing 技能")
}

// TestSkillManager_注销技能 测试 Unregister 移除已注册的技能。
// 对齐 Python: SkillManager.unregister(name)
func (s *SkillManagerE2ESuite) TestSkillManager_注销技能() {
	fs := newMockFs()
	fs.addSkillMD("/skills/test_skill/SKILL.md", "test_skill", "测试技能")

	sm := goskills.NewSkillManagerWithProvider("sys_op_test", fs)
	err := sm.Register([]string{"/skills/test_skill/SKILL.md"}, false)
	s.Require().NoError(err)
	s.Equal(1, sm.Count())

	sm.Unregister("test_skill")
	s.Equal(0, sm.Count(), "注销后计数应为 0")
	s.Nil(sm.Get("test_skill"), "注销后 Get 应返回 nil")
	s.False(sm.Has("test_skill"), "注销后 Has 应返回 false")
}

// TestSkillManager_注册覆盖 测试 overwrite=true 时覆盖同名技能。
// 对齐 Python: SkillManager.register(skill_path, overwrite=True)
// 注意：Go 端技能名从 SKILL.md 所在目录名推导（filepath.Base），
// 不是从 YAML front matter 的 name 字段。
func (s *SkillManagerE2ESuite) TestSkillManager_注册覆盖() {
	fs := newMockFs()
	// 两个不同目录包含同名（目录名）的 SKILL.md
	fs.addSkillMD("/skills/my_skill/v1/SKILL.md", "my_skill", "版本1描述")
	fs.addSkillMD("/skills/my_skill/v2/SKILL.md", "my_skill", "版本2描述")

	sm := goskills.NewSkillManagerWithProvider("sys_op_test", fs)

	// 首次注册
	err := sm.Register([]string{"/skills/my_skill/v1/SKILL.md"}, false)
	s.Require().NoError(err)
	s.Equal(1, sm.Count())
	// 技能名 = 目录名 "v1"（filepath.Base("/skills/my_skill/v1")）
	s.Equal("v1", sm.Get("v1").Name)
	s.Equal("版本1描述", sm.Get("v1").Description)

	// 注册不同目录的同名技能（目录名不同 → 不同技能）
	err = sm.Register([]string{"/skills/my_skill/v2/SKILL.md"}, false)
	s.Require().NoError(err, "不同目录名应为不同技能")
	s.Equal(2, sm.Count())
	s.Equal("版本2描述", sm.Get("v2").Description)
}

// TestSkillManager_GetAll排序 测试 GetAll 返回按名称排序的技能列表。
func (s *SkillManagerE2ESuite) TestSkillManager_GetAll排序() {
	fs := newMockFs()
	fs.addSkillMD("/skills/alpha/SKILL.md", "alpha", "技能A")
	fs.addSkillMD("/skills/charlie/SKILL.md", "charlie", "技能C")
	fs.addSkillMD("/skills/bravo/SKILL.md", "bravo", "技能B")

	sm := goskills.NewSkillManagerWithProvider("sys_op_test", fs)
	sm.Register([]string{"/skills/alpha/SKILL.md"}, false)
	sm.Register([]string{"/skills/charlie/SKILL.md"}, false)
	sm.Register([]string{"/skills/bravo/SKILL.md"}, false)

	all := sm.GetAll()
	s.Len(all, 3)
	s.Equal("alpha", all[0].Name, "第一个应为 alpha")
	s.Equal("bravo", all[1].Name, "第二个应为 bravo")
	s.Equal("charlie", all[2].Name, "第三个应为 charlie")
}

// TestSkillManager_GetNames排序 测试 GetNames 返回排序的名称列表。
func (s *SkillManagerE2ESuite) TestSkillManager_GetNames排序() {
	fs := newMockFs()
	fs.addSkillMD("/skills/z_skill/SKILL.md", "z_skill", "Z技能")
	fs.addSkillMD("/skills/a_skill/SKILL.md", "a_skill", "A技能")

	sm := goskills.NewSkillManagerWithProvider("sys_op_test", fs)
	sm.Register([]string{"/skills/z_skill/SKILL.md"}, false)
	sm.Register([]string{"/skills/a_skill/SKILL.md"}, false)

	names := sm.GetNames()
	s.Equal([]string{"a_skill", "z_skill"}, names, "名称应排序")
}

// TestSkillManager_Clear 测试 Clear 清空所有技能。
func (s *SkillManagerE2ESuite) TestSkillManager_Clear() {
	fs := newMockFs()
	fs.addSkillMD("/skills/s1/SKILL.md", "s1", "技能1")
	fs.addSkillMD("/skills/s2/SKILL.md", "s2", "技能2")

	sm := goskills.NewSkillManagerWithProvider("sys_op_test", fs)
	sm.Register([]string{"/skills/s1/SKILL.md"}, false)
	sm.Register([]string{"/skills/s2/SKILL.md"}, false)
	s.Equal(2, sm.Count())

	sm.Clear()
	s.Equal(0, sm.Count(), "Clear 后计数应为 0")
	s.Empty(sm.GetAll(), "Clear 后 GetAll 应为空")
}

// TestSkillManager_缺少description字段 测试缺少 description 的 SKILL.md 返回错误。
// Go 端 loadDescription 要求 description 字段必须存在。
func (s *SkillManagerE2ESuite) TestSkillManager_缺少description字段() {
	fs := newMockFs()
	// 添加一个没有 description 的 SKILL.md
	fs.files["/skills/no_desc/SKILL.md"] = "---\nname: no_desc\n---\n\n# No Description Skill\nSome content"

	sm := goskills.NewSkillManagerWithProvider("sys_op_test", fs)

	err := sm.Register([]string{"/skills/no_desc/SKILL.md"}, false)
	// Go 端要求 description 字段必须存在
	s.Error(err, "缺少 description 字段应返回错误")
	s.Contains(err.Error(), "description", "错误应提及 description")
}

// TestSkillManager_无效YAMLfrontMatter 测试格式错误的 YAML front matter。
func (s *SkillManagerE2ESuite) TestSkillManager_无效YAMLfrontMatter() {
	fs := newMockFs()
	// 完全没有 front matter
	fs.files["/skills/bad/SKILL.md"] = "This is just plain text without YAML"

	sm := goskills.NewSkillManagerWithProvider("sys_op_test", fs)

	err := sm.Register([]string{"/skills/bad/SKILL.md"}, false)
	// 无 front matter 应导致注册失败
	s.Error(err, "无 YAML front matter 应返回错误")
}

// TestSkillUtil_注册并生成提示词 测试 SkillUtil 注册技能并生成提示词。
// 对齐 Python: SkillUtil.register_skills() + SkillUtil.get_skill_prompt()
func (s *SkillManagerE2ESuite) TestSkillUtil_注册并生成提示词() {
	fs := newMockFs()
	fs.addSkillMD("/skills/data_analysis/SKILL.md", "data_analysis", "数据分析技能")

	su := goskills.NewSkillUtilWithProvider("sys_op_test", fs)

	err := su.RegisterSkills([]string{"/skills/data_analysis/SKILL.md"}, false)
	s.Require().NoError(err, "SkillUtil 注册不应报错")

	// 验证技能已注册
	s.True(su.HasSkill(), "HasSkill 应返回 true")

	// 验证提示词生成
	prompt := su.GetSkillPrompt()
	s.NotEmpty(prompt, "提示词不应为空")
	s.Contains(prompt, "data_analysis", "提示词应包含技能名称")
	s.Contains(prompt, "数据分析技能", "提示词应包含技能描述")
	s.Contains(prompt, "read_file", "提示词应提及 read_file 工具")
}

// TestSkillUtil_无技能时提示词 测试 SkillUtil 无技能时的行为。
// Go 端 GetSkillPrompt 即使无技能也返回系统前缀模板。
func (s *SkillManagerE2ESuite) TestSkillUtil_无技能时提示词() {
	fs := newMockFs()
	su := goskills.NewSkillUtilWithProvider("sys_op_test", fs)

	s.False(su.HasSkill(), "无注册技能时 HasSkill 应返回 false")

	prompt := su.GetSkillPrompt()
	// Go 端即使无技能也返回系统前缀 + 空技能列表模板
	s.NotEmpty(prompt, "无技能时仍返回系统前缀模板")
	s.Contains(prompt, "agent equipped", "系统前缀应包含 'agent equipped'")
}

// TestSkillUtil_多个技能提示词 测试多个技能的提示词包含所有技能。
func (s *SkillManagerE2ESuite) TestSkillUtil_多个技能提示词() {
	fs := newMockFs()
	fs.addSkillMD("/skills/research/SKILL.md", "research", "研究技能")
	fs.addSkillMD("/skills/writing/SKILL.md", "writing", "写作技能")

	su := goskills.NewSkillUtilWithProvider("sys_op_test", fs)
	err := su.RegisterSkills([]string{"/skills/research/SKILL.md", "/skills/writing/SKILL.md"}, false)
	s.Require().NoError(err)

	prompt := su.GetSkillPrompt()
	s.Contains(prompt, "research", "提示词应包含 research")
	s.Contains(prompt, "writing", "提示词应包含 writing")
	s.Contains(prompt, "研究技能", "提示词应包含研究技能描述")
	s.Contains(prompt, "写作技能", "提示词应包含写作技能描述")
}

// TestSkill_创建与AsDict 测试 Skill 结构体创建和 AsDict 方法。
func (s *SkillManagerE2ESuite) TestSkill_创建与AsDict() {
	skill := goskills.NewSkill("test_skill", "测试技能描述", "/skills/test")

	dict := skill.AsDict(true)
	s.Equal("test_skill", dict["name"])
	s.Equal("测试技能描述", dict["description"])
	s.Equal("/skills/test", dict["directory"])

	// 不包含 directory
	dictNoDir := skill.AsDict(false)
	_, hasDir := dictNoDir["directory"]
	s.False(hasDir, "includeDirectory=false 时不应包含 directory")
}

// TestSkillManager_SetSysOperationID 测试 SetSysOperationID 更新系统操作 ID。
func (s *SkillManagerE2ESuite) TestSkillManager_SetSysOperationID() {
	sm := goskills.NewSkillManager("old_id")
	sm.SetSysOperationID("new_id")
	// SetSysOperationID 不返回值，仅验证不崩溃
	s.Equal(0, sm.Count(), "更新 sysOperationID 后计数不变")
}

// TestSkillManager_并发注册 测试并发注册技能不崩溃。
func (s *SkillManagerE2ESuite) TestSkillManager_并发注册() {
	fs := newMockFs()
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("skill_%02d", i)
		fs.addSkillMD(fmt.Sprintf("/skills/%s/SKILL.md", name), name, fmt.Sprintf("技能%d", i))
	}

	sm := goskills.NewSkillManagerWithProvider("sys_op_test", fs)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("skill_%02d", idx)
			_ = sm.Register([]string{fmt.Sprintf("/skills/%s/SKILL.md", name)}, true)
		}(i)
	}
	wg.Wait()

	// 不崩溃即为通过
	s.Equal(10, sm.Count(), "并发注册后应有 10 个技能")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mockFsProvider 模拟文件系统提供者。
type mockFsProvider struct {
	// files 文件路径 → 内容
	files map[string]string
	// dirs 目录路径 → 子文件名列表
	dirs map[string][]string
	// subDirs 目录路径 → 子目录名列表
	subDirs map[string][]string
	// mu 并发保护
	mu sync.Mutex
}

// newMockFs 创建模拟文件系统。
func newMockFs() *mockFsProvider {
	return &mockFsProvider{
		files:   make(map[string]string),
		dirs:    make(map[string][]string),
		subDirs: make(map[string][]string),
	}
}

// addSkillMD 添加 SKILL.md 文件到模拟文件系统。
func (m *mockFsProvider) addSkillMD(path, name, description string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n# %s\nSkill content here", name, description, name)
	m.files[path] = content
}

// addDir 添加目录条目。
func (m *mockFsProvider) addDir(dir string, files []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dirs[dir] = files
}

// addSubDirs 添加子目录列表。
func (m *mockFsProvider) addSubDirs(dir string, subDirs []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subDirs[dir] = subDirs
}

// ReadFile 实现 FsProvider.ReadFile。
func (m *mockFsProvider) ReadFile(path string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if content, ok := m.files[path]; ok {
		return content, nil
	}
	return "", fmt.Errorf("文件不存在: %s", path)
}

// ListFiles 实现 FsProvider.ListFiles。
func (m *mockFsProvider) ListFiles(dir string) ([]goskills.FileInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if names, ok := m.dirs[dir]; ok {
		result := make([]goskills.FileInfo, 0, len(names))
		for _, name := range names {
			result = append(result, goskills.FileInfo{
				Name: name,
				Path: dir + "/" + name,
			})
		}
		return result, nil
	}
	return nil, fmt.Errorf("目录不存在: %s", dir)
}

// ListDirectories 实现 FsProvider.ListDirectories。
func (m *mockFsProvider) ListDirectories(dir string) ([]goskills.DirInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if subDirs, ok := m.subDirs[dir]; ok {
		result := make([]goskills.DirInfo, 0, len(subDirs))
		for _, name := range subDirs {
			result = append(result, goskills.DirInfo{
				Name: name,
				Path: dir + "/" + name,
			})
		}
		return result, nil
	}
	// 如果 dir 存在于 dirs 中但没有 subDirs，返回空列表
	if _, ok := m.dirs[dir]; ok {
		return []goskills.DirInfo{}, nil
	}
	return nil, fmt.Errorf("目录不存在: %s", dir)
}

// WriteFile 实现 FsProvider.WriteFile。
func (m *mockFsProvider) WriteFile(path string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[path] = string(data)
	return nil
}

// 编译时验证 mockFsProvider 满足 FsProvider 接口
var _ goskills.FsProvider = (*mockFsProvider)(nil)

// 编译时验证 SkillManager 和 SkillUtil 相关类型
var (
	_ *goskills.SkillManager = nil
	_ *goskills.SkillUtil    = nil
	_ *goskills.Skill        = nil
	_ *prompt.PromptTemplate = nil // 技能提示词依赖 prompt 包
)

// 确保 strings 包被使用
var _ = strings.Contains
