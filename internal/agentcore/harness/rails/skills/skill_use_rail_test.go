package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	skillpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockSystemPromptBuilder 测试用 mock SystemPromptBuilder
type mockSystemPromptBuilder struct {
	language  string
	sections  map[string]saprompt.PromptSection
	removed   []string
}

func newMockSystemPromptBuilder(lang string) *mockSystemPromptBuilder {
	return &mockSystemPromptBuilder{
		language: lang,
		sections: make(map[string]saprompt.PromptSection),
	}
}

func (m *mockSystemPromptBuilder) AddSection(section saprompt.PromptSection) *saprompt.SystemPromptBuilder {
	m.sections[section.Name] = section
	return nil
}

func (m *mockSystemPromptBuilder) RemoveSection(name string) *saprompt.SystemPromptBuilder {
	delete(m.sections, name)
	m.removed = append(m.removed, name)
	return nil
}

func (m *mockSystemPromptBuilder) Language() string { return m.language }

func (m *mockSystemPromptBuilder) GetSection(name string) *saprompt.PromptSection {
	s, ok := m.sections[name]
	if !ok {
		return nil
	}
	return &s
}

func (m *mockSystemPromptBuilder) HasSection(name string) bool {
	_, ok := m.sections[name]
	return ok
}

// mockAbilityManager 测试用 mock AbilityManager
type mockAbilityManager struct {
	abilities map[string]schema.Ability
}

func newMockAbilityManager() *mockAbilityManager {
	return &mockAbilityManager{abilities: make(map[string]schema.Ability)}
}

func (m *mockAbilityManager) Add(ability schema.Ability) agentschema.AddAbilityResult {
	name := ability.AbilityName()
	m.abilities[name] = ability
	return agentschema.AddAbilityResult{Added: true}
}

func (m *mockAbilityManager) Remove(name string) schema.Ability {
	a := m.abilities[name]
	delete(m.abilities, name)
	return a
}

func (m *mockAbilityManager) Get(name string) schema.Ability { return m.abilities[name] }
func (m *mockAbilityManager) List() []schema.Ability {
	var result []schema.Ability
	for _, a := range m.abilities {
		result = append(result, a)
	}
	return result
}
func (m *mockAbilityManager) ListToolInfo(_ context.Context, _ []string, _ ...string) ([]schema.ToolInfoInterface, error) {
	return nil, nil
}
func (m *mockAbilityManager) Execute(_ context.Context, _ *agentinterfaces.AgentCallbackContext, _ []*llmschema.ToolCall, _ sessioninterfaces.SessionFacade, _ string) []agentschema.ExecuteResult {
	return nil
}
func (m *mockAbilityManager) SetContextEngine(_ ceinterface.ContextEngine) {}
func (m *mockAbilityManager) ReorderTools(_ []string)         {}
func (m *mockAbilityManager) AddMany(abilities []schema.Ability) []agentschema.AddAbilityResult {
	var results []agentschema.AddAbilityResult
	for _, a := range abilities {
		results = append(results, m.Add(a))
	}
	return results
}
func (m *mockAbilityManager) RemoveMany(names []string) []schema.Ability {
	var results []schema.Ability
	for _, n := range names {
		results = append(results, m.Remove(n))
	}
	return results
}

// mockBaseAgent 测试用 mock BaseAgent
type mockBaseAgent struct {
	card   *agentschema.AgentCard
	am     agentinterfaces.AbilityManagerInterface
	spb    saprompt.SystemPromptBuilderInterface
}

func newMockBaseAgent(spb saprompt.SystemPromptBuilderInterface, am agentinterfaces.AbilityManagerInterface) *mockBaseAgent {
	return &mockBaseAgent{
		card: &agentschema.AgentCard{BaseCard: schema.BaseCard{ID: "test-agent"}},
		am:   am,
		spb:  spb,
	}
}

func (m *mockBaseAgent) Card() *agentschema.AgentCard                          { return m.card }
func (m *mockBaseAgent) AbilityManager() agentinterfaces.AbilityManagerInterface { return m.am }
func (m *mockBaseAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface { return m.spb }
func (m *mockBaseAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (m *mockBaseAgent) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (m *mockBaseAgent) Config() agentinterfaces.AgentConfig                    { return nil }
func (m *mockBaseAgent) CallbackManager() *agentinterfaces.AgentCallbackManager { return nil }
func (m *mockBaseAgent) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ cb.PerAgentCallbackFunc, _ ...cb.CallbackOption) error {
	return nil
}
func (m *mockBaseAgent) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...cb.CallbackOption) error {
	return nil
}
func (m *mockBaseAgent) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}
func (m *mockBaseAgent) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// createTempSkillDir 创建临时技能目录
func createTempSkillDir(t *testing.T, skills map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, desc := range skills {
		skillDir := filepath.Join(dir, name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatalf("创建技能目录失败: %v", err)
		}
		content := "---\ndescription: " + desc + "\n---\nSkill content"
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("写入 SKILL.md 失败: %v", err)
		}
	}
	return dir
}

// ──────────────────────────── 构造函数测试 ────────────────────────────

// TestNewSkillUseRail_正常 测试正常构造
func TestNewSkillUseRail_正常(t *testing.T) {
	r := NewSkillUseRail([]string{"/tmp/skills"})
	if r.skillMode != SkillModeAutoList {
		t.Errorf("默认 skillMode = %q, want %q", r.skillMode, SkillModeAutoList)
	}
	if !r.enableCache {
		t.Error("默认 enableCache 应为 true")
	}
	if !r.includeTools {
		t.Error("默认 includeTools 应为 true")
	}
}

// TestNewSkillUseRail_无效模式 测试无效 skillMode panic
func TestNewSkillUseRail_无效模式(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("期望 panic，但没有发生")
		}
		if !strings.Contains(fmt.Sprintf("%v", r), "Unsupported skill_mode") {
			t.Errorf("panic 消息不包含 'Unsupported skill_mode': %v", r)
		}
	}()
	NewSkillUseRail([]string{"/tmp/skills"}, WithSkillMode("invalid"))
}

// TestNewSkillUseRail_选项 测试选项生效
func TestNewSkillUseRail_选项(t *testing.T) {
	r := NewSkillUseRail([]string{"/tmp/skills"},
		WithSkillMode(SkillModeAll),
		WithEnableCache(false),
		WithIncludeTools(false),
		WithEnabledSkills([]string{"a", "b"}),
		WithDisabledSkills([]string{"c"}),
	)
	if r.skillMode != SkillModeAll {
		t.Errorf("skillMode = %q, want %q", r.skillMode, SkillModeAll)
	}
	if r.enableCache {
		t.Error("enableCache 应为 false")
	}
	if r.includeTools {
		t.Error("includeTools 应为 false")
	}
	if _, ok := r.enabledSkills["a"]; !ok {
		t.Error("enabledSkills 应包含 'a'")
	}
	if _, ok := r.disabledSkills["c"]; !ok {
		t.Error("disabledSkills 应包含 'c'")
	}
}

// ──────────────────────────── 辅助方法测试 ────────────────────────────

// TestNormalizeNameList_切片 测试切片输入
func TestNormalizeNameList_切片(t *testing.T) {
	result := normalizeNameList([]string{"a;b", "c"})
	if len(result) != 3 || result[0] != "a" || result[1] != "b" || result[2] != "c" {
		t.Errorf("normalizeNameList([]) = %v, want [a b c]", result)
	}
}

// TestNormalizeNameList_空值 测试空切片输入
func TestNormalizeNameList_空值(t *testing.T) {
	result := normalizeNameList(nil)
	if result != nil {
		t.Errorf("normalizeNameList(nil) = %v, want nil", result)
	}
}

// TestNormalizeNameList_空字符串 测试空字符串元素
func TestNormalizeNameList_空字符串(t *testing.T) {
	result := normalizeNameList([]string{""})
	if result != nil {
		t.Errorf("normalizeNameList(['']) = %v, want nil", result)
	}
}

// TestNormalizeNameSet 测试名称集合
func TestNormalizeNameSet(t *testing.T) {
	result := normalizeNameSet([]string{"a;a", "b"})
	if len(result) != 2 {
		t.Errorf("normalizeNameSet len = %d, want 2", len(result))
	}
	if _, ok := result["a"]; !ok {
		t.Error("normalizeNameSet 应包含 'a'")
	}
	if _, ok := result["b"]; !ok {
		t.Error("normalizeNameSet 应包含 'b'")
	}
}

// TestParseSkillDirs 测试解析分号/逗号分隔字符串
func TestParseSkillDirs(t *testing.T) {
	result := parseSkillDirs("/a;/b,/c")
	if len(result) != 3 || result[0] != "/a" || result[1] != "/b" || result[2] != "/c" {
		t.Errorf("parseSkillDirs = %v, want [/a /b /c]", result)
	}
}

// TestParseSkillDirs_空字符串 测试空字符串
func TestParseSkillDirs_空字符串(t *testing.T) {
	result := parseSkillDirs("")
	if result != nil {
		t.Errorf("parseSkillDirs('') = %v, want nil", result)
	}
}

// TestNormalizeSkillDirs 测试规范化目录列表
func TestNormalizeSkillDirs(t *testing.T) {
	result := normalizeSkillDirs([]string{"/tmp/a;/tmp/b"})
	if len(result) != 2 {
		t.Errorf("normalizeSkillDirs len = %d, want 2", len(result))
	}
}

// TestSkillMDPath 测试 skillMDPath
func TestSkillMDPath(t *testing.T) {
	skill := skillpkg.NewSkill("test", "desc", "/skills/test")
	result := skillMDPath(skill)
	if result != "/skills/test/SKILL.md" {
		t.Errorf("skillMDPath = %q, want '/skills/test/SKILL.md'", result)
	}
}

// ──────────────────────────── 过滤测试 ────────────────────────────

// TestFilterSkills_仅enabled 测试仅白名单
func TestFilterSkills_仅enabled(t *testing.T) {
	r := NewSkillUseRail([]string{"/tmp"},
		WithEnabledSkills([]string{"a"}),
		WithIncludeTools(false),
	)
	skills := []*skillpkg.Skill{
		skillpkg.NewSkill("a", "desc a", "/a"),
		skillpkg.NewSkill("b", "desc b", "/b"),
	}
	filtered := r.filterSkills(skills)
	if len(filtered) != 1 || filtered[0].Name != "a" {
		t.Errorf("filterSkills = %v, want only 'a'", filtered)
	}
}

// TestFilterSkills_仅disabled 测试仅黑名单
func TestFilterSkills_仅disabled(t *testing.T) {
	r := NewSkillUseRail([]string{"/tmp"},
		WithDisabledSkills([]string{"b"}),
		WithIncludeTools(false),
	)
	skills := []*skillpkg.Skill{
		skillpkg.NewSkill("a", "desc a", "/a"),
		skillpkg.NewSkill("b", "desc b", "/b"),
	}
	filtered := r.filterSkills(skills)
	if len(filtered) != 1 || filtered[0].Name != "a" {
		t.Errorf("filterSkills = %v, want only 'a'", filtered)
	}
}

// TestFilterSkills_同时设置 测试白名单+黑名单
func TestFilterSkills_同时设置(t *testing.T) {
	r := NewSkillUseRail([]string{"/tmp"},
		WithEnabledSkills([]string{"a", "b"}),
		WithDisabledSkills([]string{"b"}),
		WithIncludeTools(false),
	)
	skills := []*skillpkg.Skill{
		skillpkg.NewSkill("a", "desc a", "/a"),
		skillpkg.NewSkill("b", "desc b", "/b"),
		skillpkg.NewSkill("c", "desc c", "/c"),
	}
	filtered := r.filterSkills(skills)
	if len(filtered) != 1 || filtered[0].Name != "a" {
		t.Errorf("filterSkills = %v, want only 'a'", filtered)
	}
}

// TestFilterSkills_空skills 测试空技能列表
func TestFilterSkills_空skills(t *testing.T) {
	r := NewSkillUseRail([]string{"/tmp"}, WithIncludeTools(false))
	filtered := r.filterSkills(nil)
	if filtered != nil {
		t.Errorf("filterSkills(nil) = %v, want nil", filtered)
	}
}

// ──────────────────────────── 技能加载测试 ────────────────────────────

// TestLoadYAML_有FrontMatter 测试有 YAML front matter 的文件
func TestLoadYAML_有FrontMatter(t *testing.T) {
	dir := t.TempDir()
	content := "---\ndescription: 测试技能\n---\n正文内容"
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewSkillUseRail([]string{dir}, WithIncludeTools(false))
	yamlData, body, err := r.loadYAML(path)
	if err != nil {
		t.Fatalf("loadYAML 错误: %v", err)
	}
	if yamlData == nil {
		t.Fatal("yamlData 不应为 nil")
	}
	if desc, _ := yamlData["description"].(string); desc != "测试技能" {
		t.Errorf("description = %q, want '测试技能'", desc)
	}
	if body != "正文内容" {
		t.Errorf("body = %q, want '正文内容'", body)
	}
}

// TestLoadYAML_无FrontMatter 测试无 YAML front matter 的文件
func TestLoadYAML_无FrontMatter(t *testing.T) {
	dir := t.TempDir()
	content := "纯文本内容"
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewSkillUseRail([]string{dir}, WithIncludeTools(false))
	yamlData, body, err := r.loadYAML(path)
	if err != nil {
		t.Fatalf("loadYAML 错误: %v", err)
	}
	if yamlData != nil {
		t.Error("yamlData 应为 nil")
	}
	if body != "纯文本内容" {
		t.Errorf("body = %q, want '纯文本内容'", body)
	}
}

// TestLoadDescription 测试 loadDescription
func TestLoadDescription(t *testing.T) {
	dir := t.TempDir()
	content := "---\ndescription: 我的技能\n---\n内容"
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewSkillUseRail([]string{dir}, WithIncludeTools(false))
	desc, err := r.loadDescription(path)
	if err != nil {
		t.Fatalf("loadDescription 错误: %v", err)
	}
	if desc != "我的技能" {
		t.Errorf("description = %q, want '我的技能'", desc)
	}
}

// TestLoadDescription_缺少字段 测试缺少 description 字段
func TestLoadDescription_缺少字段(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: test\n---\n内容"
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewSkillUseRail([]string{dir}, WithIncludeTools(false))
	_, err := r.loadDescription(path)
	if err == nil {
		t.Error("期望错误，但返回 nil")
	}
}

// TestRefreshSkillsIncrementally 测试增量加载
func TestRefreshSkillsIncrementally(t *testing.T) {
	dir := createTempSkillDir(t, map[string]string{
		"skill_a": "技能 A",
		"skill_b": "技能 B",
	})

	r := NewSkillUseRail([]string{dir}, WithIncludeTools(false))
	if err := r.refreshSkillsIncrementally(); err != nil {
		t.Fatalf("refreshSkillsIncrementally 错误: %v", err)
	}

	if len(r.skillCache) != 2 {
		t.Errorf("skillCache len = %d, want 2", len(r.skillCache))
	}
	if len(r.skillOrder) != 2 {
		t.Errorf("skillOrder len = %d, want 2", len(r.skillOrder))
	}
}

// TestRefreshSkillsIncrementally_缓存未变化 测试 mtime 不变时缓存命中
func TestRefreshSkillsIncrementally_缓存未变化(t *testing.T) {
	dir := createTempSkillDir(t, map[string]string{
		"skill_a": "技能 A",
	})

	r := NewSkillUseRail([]string{dir}, WithIncludeTools(false))
	if err := r.refreshSkillsIncrementally(); err != nil {
		t.Fatalf("第一次 refresh 错误: %v", err)
	}

	// 修改缓存中的技能内容，第二次加载如果缓存命中应该保持旧值
	r.skillCache[filepath.Join(dir, "skill_a")].Description = "旧描述"

	if err := r.refreshSkillsIncrementally(); err != nil {
		t.Fatalf("第二次 refresh 错误: %v", err)
	}

	// 由于 mtime 未变，应该命中缓存
	if r.skillCache[filepath.Join(dir, "skill_a")].Description != "旧描述" {
		t.Error("缓存未命中：mtime 不变时应使用缓存")
	}
}

// TestRefreshSkillsIncrementally_缓存失效 测试 mtime 变化时重新加载
func TestRefreshSkillsIncrementally_缓存失效(t *testing.T) {
	dir := createTempSkillDir(t, map[string]string{
		"skill_a": "技能 A",
	})

	r := NewSkillUseRail([]string{dir}, WithIncludeTools(false))
	if err := r.refreshSkillsIncrementally(); err != nil {
		t.Fatalf("第一次 refresh 错误: %v", err)
	}

	// 修改文件触发 mtime 变化
	time.Sleep(10 * time.Millisecond)
	skillMD := filepath.Join(dir, "skill_a", "SKILL.md")
	newContent := "---\ndescription: 新技能 A\n---\n内容"
	if err := os.WriteFile(skillMD, []byte(newContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := r.refreshSkillsIncrementally(); err != nil {
		t.Fatalf("第二次 refresh 错误: %v", err)
	}

	// mtime 变化应重新加载
	key := ""
	for k := range r.skillCache {
		if strings.Contains(k, "skill_a") {
			key = k
			break
		}
	}
	if key == "" {
		t.Fatal("未找到 skill_a 缓存")
	}
	if r.skillCache[key].Description != "新技能 A" {
		t.Errorf("缓存失效后应重新加载：description = %q, want '新技能 A'", r.skillCache[key].Description)
	}
}

// TestCollectSkillsInOrder 测试按序收集去重
func TestCollectSkillsInOrder(t *testing.T) {
	dir := createTempSkillDir(t, map[string]string{
		"alpha": "技能 A",
		"beta":  "技能 B",
	})

	r := NewSkillUseRail([]string{dir}, WithIncludeTools(false))
	if err := r.refreshSkillsIncrementally(); err != nil {
		t.Fatalf("refresh 错误: %v", err)
	}

	collected := r.collectSkillsInOrder()
	if len(collected) != 2 {
		t.Errorf("collected len = %d, want 2", len(collected))
	}
}

// TestPrepareSkills 测试完整 prepareSkills 流程
func TestPrepareSkills(t *testing.T) {
	dir := createTempSkillDir(t, map[string]string{
		"alpha": "技能 A",
		"beta":  "技能 B",
	})

	r := NewSkillUseRail([]string{dir}, WithIncludeTools(false),
		WithDisabledSkills([]string{"beta"}),
	)
	if err := r.prepareSkills(); err != nil {
		t.Fatalf("prepareSkills 错误: %v", err)
	}

	if len(r.skills) != 1 {
		t.Errorf("skills len = %d, want 1（beta 被禁用）", len(r.skills))
	}
	if r.skills[0].Name != "alpha" {
		t.Errorf("skills[0].Name = %q, want 'alpha'", r.skills[0].Name)
	}
}

// TestPrepareSkills_禁用缓存 测试 enableCache=false 时每次都重新加载（缓存仍会被填充）
func TestPrepareSkills_禁用缓存(t *testing.T) {
	dir := createTempSkillDir(t, map[string]string{
		"alpha": "技能 A",
	})

	r := NewSkillUseRail([]string{dir}, WithIncludeTools(false), WithEnableCache(false))
	if err := r.prepareSkills(); err != nil {
		t.Fatalf("第一次 prepareSkills 错误: %v", err)
	}
	// enableCache=false 时，prepareSkills 先清空缓存再重新加载，
	// 加载完成后 skillCache 仍会被 refreshSkillsIncrementally 填充
	if len(r.skills) != 1 {
		t.Errorf("skills len = %d, want 1", len(r.skills))
	}

	// 验证每次调用都重新加载：先手动修改缓存内容，再次调用后应被覆盖
	r.skillCache[filepath.Join(dir, "alpha")].Description = "旧描述"
	if err := r.prepareSkills(); err != nil {
		t.Fatalf("第二次 prepareSkills 错误: %v", err)
	}
	key := ""
	for k := range r.skillCache {
		if strings.Contains(k, "alpha") {
			key = k
			break
		}
	}
	if key != "" && r.skillCache[key].Description != "技能 A" {
		t.Errorf("enableCache=false 时应每次重新加载，description = %q, want '技能 A'", r.skillCache[key].Description)
	}
}

// ──────────────────────────── 提示词注入测试 ────────────────────────────

// TestGetSkillDescription_无演化经验 测试无演化经验文本
func TestGetSkillDescription_无演化经验(t *testing.T) {
	r := NewSkillUseRail([]string{"/tmp"}, WithIncludeTools(false))
	skill := skillpkg.NewSkill("test", "原始描述", "/test")
	desc := r.getSkillDescription(skill)
	if desc != "原始描述" {
		t.Errorf("getSkillDescription = %q, want '原始描述'", desc)
	}
}

// TestGetSkillDescription_有演化经验 测试有演化经验文本
func TestGetSkillDescription_有演化经验(t *testing.T) {
	r := NewSkillUseRail([]string{"/tmp"}, WithIncludeTools(false))
	r.evolutionTexts["test"] = "经验文本"
	skill := skillpkg.NewSkill("test", "原始描述", "/test")
	desc := r.getSkillDescription(skill)
	if !strings.Contains(desc, "演进经验") {
		t.Errorf("getSkillDescription 应包含 '演进经验': %q", desc)
	}
	if !strings.Contains(desc, "经验文本") {
		t.Errorf("getSkillDescription 应包含 '经验文本': %q", desc)
	}
}

// TestBuildSkillsSection_all模式 测试 all 模式
func TestBuildSkillsSection_all模式(t *testing.T) {
	r := NewSkillUseRail([]string{"/tmp"}, WithSkillMode(SkillModeAll), WithIncludeTools(false))
	r.systemPromptBuilder = newMockSystemPromptBuilder("cn")
	r.skills = []*skillpkg.Skill{
		skillpkg.NewSkill("a", "技能 A", "/a"),
		skillpkg.NewSkill("b", "技能 B", "/b"),
	}

	section := r.buildSkillsSection()
	if section == nil {
		t.Fatal("section 不应为 nil")
	}
	if section.Name != "skills" {
		t.Errorf("section.Name = %q, want 'skills'", section.Name)
	}
	if !strings.Contains(section.Content["cn"], "技能 A") {
		t.Error("all 模式应包含技能描述")
	}
}

// TestBuildSkillsSection_autoList模式 测试 auto_list 模式
func TestBuildSkillsSection_autoList模式(t *testing.T) {
	r := NewSkillUseRail([]string{"/tmp"}, WithSkillMode(SkillModeAutoList), WithIncludeTools(false))
	r.systemPromptBuilder = newMockSystemPromptBuilder("cn")

	section := r.buildSkillsSection()
	if section == nil {
		t.Fatal("section 不应为 nil")
	}
	if !strings.Contains(section.Content["cn"], "list_skill") {
		t.Error("auto_list 模式应包含 list_skill")
	}
}

// TestBuildSkillsSection_空skills 测试 all 模式空技能
func TestBuildSkillsSection_空skills(t *testing.T) {
	r := NewSkillUseRail([]string{"/tmp"}, WithSkillMode(SkillModeAll), WithIncludeTools(false))
	r.systemPromptBuilder = newMockSystemPromptBuilder("cn")
	r.skills = nil

	section := r.buildSkillsSection()
	if section == nil {
		t.Fatal("section 不应为 nil")
	}
	if !strings.Contains(section.Content["cn"], "没有选择任何技能") {
		t.Error("all 模式空技能应回退到 no_skill 提示")
	}
}

// TestBeforeModelCall_注入section 测试 BeforeModelCall 注入 section
func TestBeforeModelCall_注入section(t *testing.T) {
	spb := newMockSystemPromptBuilder("cn")
	r := NewSkillUseRail([]string{"/tmp"}, WithSkillMode(SkillModeAll), WithIncludeTools(false))
	r.systemPromptBuilder = spb
	r.skills = []*skillpkg.Skill{
		skillpkg.NewSkill("a", "技能 A", "/a"),
	}

	err := r.BeforeModelCall(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeforeModelCall 错误: %v", err)
	}
	if !spb.HasSection("skills") {
		t.Error("BeforeModelCall 应注入 skills section")
	}
}

// TestBeforeModelCall_空builder 测试 systemPromptBuilder 为 nil 时空操作
func TestBeforeModelCall_空builder(t *testing.T) {
	r := NewSkillUseRail([]string{"/tmp"}, WithSkillMode(SkillModeAll), WithIncludeTools(false))
	r.systemPromptBuilder = nil

	err := r.BeforeModelCall(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeforeModelCall 错误: %v", err)
	}
}

// ──────────────────────────── Init/Uninit 测试 ────────────────────────────

// TestInit_工具注册 测试 Init 注册工具
func TestInit_工具注册(t *testing.T) {
	spb := newMockSystemPromptBuilder("cn")
	am := newMockAbilityManager()
	agent := newMockBaseAgent(spb, am)

	r := NewSkillUseRail([]string{"/tmp"}, WithSkillMode(SkillModeAll), WithIncludeTools(true))
	err := r.Init(agent)
	if err != nil {
		t.Fatalf("Init 错误: %v", err)
	}
	if len(r.ownedToolIDs) == 0 {
		t.Error("Init 应注册工具到 ResourceMgr")
	}
}

// TestInit_autoList注册ListSkillTool 测试 auto_list 模式注册 ListSkillTool
func TestInit_autoList注册ListSkillTool(t *testing.T) {
	spb := newMockSystemPromptBuilder("cn")
	am := newMockAbilityManager()
	agent := newMockBaseAgent(spb, am)

	r := NewSkillUseRail([]string{"/tmp"}, WithSkillMode(SkillModeAutoList), WithIncludeTools(false))
	err := r.Init(agent)
	if err != nil {
		t.Fatalf("Init 错误: %v", err)
	}
	// auto_list 模式应注册 SkillTool + ListSkillTool = 2 个工具
	if len(r.ownedToolIDs) < 2 {
		t.Errorf("auto_list 模式至少应注册 2 个工具，实际 %d", len(r.ownedToolIDs))
	}
}

// TestUninit_工具注销 测试 Uninit 注销工具
func TestUninit_工具注销(t *testing.T) {
	spb := newMockSystemPromptBuilder("cn")
	am := newMockAbilityManager()
	agent := newMockBaseAgent(spb, am)

	r := NewSkillUseRail([]string{"/tmp"}, WithSkillMode(SkillModeAll), WithIncludeTools(false))
	_ = r.Init(agent)
	_ = r.Uninit(agent)

	if len(r.ownedToolNames) != 0 {
		t.Error("Uninit 应清空 ownedToolNames")
	}
	if len(r.ownedToolIDs) != 0 {
		t.Error("Uninit 应清空 ownedToolIDs")
	}
}

// ──────────────────────────── SkillsMeta / ClearSkills 测试 ────────────────────────────

// TestSkillsMeta 测试 SkillsMeta 返回副本
func TestSkillsMeta(t *testing.T) {
	r := NewSkillUseRail([]string{"/tmp"}, WithIncludeTools(false))
	r.skills = []*skillpkg.Skill{
		skillpkg.NewSkill("a", "desc", "/a"),
	}

	meta := r.SkillsMeta()
	if len(meta) != 1 {
		t.Errorf("SkillsMeta len = %d, want 1", len(meta))
	}
	// 修改副本不应影响原列表
	meta[0].Name = "changed"
	if r.skills[0].Name != "a" {
		t.Error("SkillsMeta 应返回副本，不应影响原列表")
	}
}

// TestClearSkills 测试 ClearSkills
func TestClearSkills(t *testing.T) {
	r := NewSkillUseRail([]string{"/tmp"}, WithIncludeTools(false))
	r.skillCache["key"] = skillpkg.NewSkill("a", "desc", "/a")
	r.skills = []*skillpkg.Skill{skillpkg.NewSkill("a", "desc", "/a")}

	r.ClearSkills()
	if len(r.skillCache) != 0 {
		t.Error("ClearSkills 应清空 skillCache")
	}
	if len(r.skills) != 0 {
		t.Error("ClearSkills 应清空 skills")
	}
}

// ──────────────────────────── LoadSkillsFromDir 测试 ────────────────────────────

// TestLoadSkillsFromDir 测试静态加载
func TestLoadSkillsFromDir(t *testing.T) {
	dir := createTempSkillDir(t, map[string]string{
		"alpha": "技能 A",
		"beta":  "技能 B",
	})

	skills, err := LoadSkillsFromDir(context.Background(), []string{dir})
	if err != nil {
		t.Fatalf("LoadSkillsFromDir 错误: %v", err)
	}
	if len(skills) != 2 {
		t.Errorf("LoadSkillsFromDir len = %d, want 2", len(skills))
	}
}

// TestLoadSkillsFromDir_不存在的目录 测试目录不存在
func TestLoadSkillsFromDir_不存在的目录(t *testing.T) {
	skills, err := LoadSkillsFromDir(context.Background(), []string{"/nonexistent_dir_12345"})
	if err != nil {
		t.Fatalf("LoadSkillsFromDir 不应返回错误: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("不存在的目录应返回空列表，实际 len=%d", len(skills))
	}
}

// TestLoadSkillsFromDir_空目录 测试空 skillsDir
func TestLoadSkillsFromDir_空目录(t *testing.T) {
	_, err := LoadSkillsFromDir(context.Background(), []string{})
	if err == nil {
		t.Error("空 skillsDir 应返回错误")
	}
}

// ──────────────────────────── Priority 测试 ────────────────────────────

// TestPriority 测试优先级
func TestPriority(t *testing.T) {
	r := NewSkillUseRail([]string{"/tmp"})
	if r.Priority() != skillUseRailPriority {
		t.Errorf("Priority() = %d, want %d", r.Priority(), skillUseRailPriority)
	}
}

// ──────────────────────────── BeforeInvoke/AfterInvoke 测试 ────────────────────────────

// TestBeforeInvoke 测试 BeforeInvoke
func TestBeforeInvoke(t *testing.T) {
	dir := createTempSkillDir(t, map[string]string{
		"alpha": "技能 A",
	})

	r := NewSkillUseRail([]string{dir}, WithIncludeTools(false))
	err := r.BeforeInvoke(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeforeInvoke 错误: %v", err)
	}
	// BeforeInvoke 应触发 refreshSkillPrompt → prepareSkills
	if len(r.skills) != 1 {
		t.Errorf("BeforeInvoke 后 skills len = %d, want 1", len(r.skills))
	}
}

// TestAfterInvoke 测试 AfterInvoke
func TestAfterInvoke(t *testing.T) {
	r := NewSkillUseRail([]string{"/tmp"}, WithIncludeTools(false))
	err := r.AfterInvoke(context.Background(), nil)
	if err != nil {
		t.Fatalf("AfterInvoke 错误: %v", err)
	}
}

// ──────────────────────────── ReloadSkills 测试 ────────────────────────────

// TestReloadSkills 测试 ReloadSkills
func TestReloadSkills(t *testing.T) {
	dir := createTempSkillDir(t, map[string]string{
		"alpha": "技能 A",
	})

	r := NewSkillUseRail([]string{dir}, WithIncludeTools(false))
	if err := r.ReloadSkills(context.Background()); err != nil {
		t.Fatalf("ReloadSkills 错误: %v", err)
	}
	if len(r.skills) != 1 {
		t.Errorf("ReloadSkills 后 skills len = %d, want 1", len(r.skills))
	}
}

// ──────────────────────────── ValidSkillModes 测试 ────────────────────────────

// TestValidSkillModes 测试有效模式集合
func TestValidSkillModes(t *testing.T) {
	if _, ok := ValidSkillModes[SkillModeAll]; !ok {
		t.Error("ValidSkillModes 应包含 'all'")
	}
	if _, ok := ValidSkillModes[SkillModeAutoList]; !ok {
		t.Error("ValidSkillModes 应包含 'auto_list'")
	}
	if _, ok := ValidSkillModes["invalid"]; ok {
		t.Error("ValidSkillModes 不应包含 'invalid'")
	}
}
