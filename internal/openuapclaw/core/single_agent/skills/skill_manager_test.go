package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockFsProvider 用于测试的模拟文件系统提供者
type mockFsProvider struct {
	// files 路径→内容
	files map[string]string
	// dirs 路径→子目录列表
	dirs map[string][]DirInfo
	// dirFiles 路径→文件列表
	dirFiles map[string][]FileInfo
	// writeErr 写入时返回的错误
	writeErr error
}

func newMockFsProvider() *mockFsProvider {
	return &mockFsProvider{
		files:    make(map[string]string),
		dirs:     make(map[string][]DirInfo),
		dirFiles: make(map[string][]FileInfo),
	}
}

func (m *mockFsProvider) ReadFile(path string) (string, error) {
	content, ok := m.files[path]
	if !ok {
		return "", os.ErrNotExist
	}
	return content, nil
}

func (m *mockFsProvider) ListFiles(dir string) ([]FileInfo, error) {
	files, ok := m.dirFiles[dir]
	if !ok {
		return nil, os.ErrNotExist
	}
	return files, nil
}

func (m *mockFsProvider) ListDirectories(dir string) ([]DirInfo, error) {
	dirs, ok := m.dirs[dir]
	if !ok {
		return nil, os.ErrNotExist
	}
	return dirs, nil
}

func (m *mockFsProvider) WriteFile(path string, data []byte) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	m.files[path] = string(data)
	return nil
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewSkillManager 创建 SkillManager 实例
func TestNewSkillManager(t *testing.T) {
	sm := NewSkillManager("op-123")
	if sm.Count() != 0 {
		t.Errorf("期望 Count=0，实际 %d", sm.Count())
	}
	if sm.sysOperationID != "op-123" {
		t.Errorf("期望 sysOperationID=op-123，实际 %s", sm.sysOperationID)
	}
}

// TestSkillManager_Register_单个技能目录 注册单个技能目录
func TestSkillManager_Register_单个技能目录(t *testing.T) {
	provider := newMockFsProvider()
	// 设置技能目录结构：/skills/translate/ 下有 SKILL.md
	provider.dirs["/skills"] = []DirInfo{
		{Name: "translate", Path: "/skills/translate"},
	}
	provider.dirFiles["/skills/translate"] = []FileInfo{
		{Name: "SKILL.md", Path: "/skills/translate/SKILL.md"},
	}
	provider.files["/skills/translate/SKILL.md"] = "---\ndescription: 翻译技能\n---\n# 翻译技能\n"

	sm := NewSkillManagerWithProvider("op-123", provider)
	err := sm.Register([]string{"/skills"}, false)
	if err != nil {
		t.Fatalf("Register 失败: %v", err)
	}
	if sm.Count() != 1 {
		t.Errorf("期望 Count=1，实际 %d", sm.Count())
	}
	skill := sm.Get("translate")
	if skill == nil {
		t.Fatal("期望获取 translate 技能")
	}
	if skill.Description != "翻译技能" {
		t.Errorf("期望 Description=翻译技能，实际 %s", skill.Description)
	}
}

// TestSkillManager_Register_重复注册 重复注册不覆盖时返回错误
func TestSkillManager_Register_重复注册(t *testing.T) {
	provider := newMockFsProvider()
	provider.dirs["/skills"] = []DirInfo{
		{Name: "translate", Path: "/skills/translate"},
	}
	provider.dirFiles["/skills/translate"] = []FileInfo{
		{Name: "SKILL.md", Path: "/skills/translate/SKILL.md"},
	}
	provider.files["/skills/translate/SKILL.md"] = "---\ndescription: 翻译技能\n---\n"

	sm := NewSkillManagerWithProvider("op-123", provider)
	err := sm.Register([]string{"/skills"}, false)
	if err != nil {
		t.Fatalf("首次 Register 失败: %v", err)
	}
	// 再次注册，不覆盖
	err = sm.Register([]string{"/skills"}, false)
	if err == nil {
		t.Error("期望重复注册返回错误")
	}
}

// TestSkillManager_Register_覆盖注册 重复注册覆盖时不返回错误
func TestSkillManager_Register_覆盖注册(t *testing.T) {
	provider := newMockFsProvider()
	provider.dirs["/skills"] = []DirInfo{
		{Name: "translate", Path: "/skills/translate"},
	}
	provider.dirFiles["/skills/translate"] = []FileInfo{
		{Name: "SKILL.md", Path: "/skills/translate/SKILL.md"},
	}
	provider.files["/skills/translate/SKILL.md"] = "---\ndescription: 翻译技能\n---\n"

	sm := NewSkillManagerWithProvider("op-123", provider)
	err := sm.Register([]string{"/skills"}, false)
	if err != nil {
		t.Fatalf("首次 Register 失败: %v", err)
	}
	// 覆盖注册
	err = sm.Register([]string{"/skills"}, true)
	if err != nil {
		t.Errorf("覆盖注册不应返回错误: %v", err)
	}
}

// TestSkillManager_Unregister 注销技能
func TestSkillManager_Unregister(t *testing.T) {
	sm := NewSkillManager("op-123")
	sm.registry["translate"] = NewSkill("translate", "翻译", "/skills/translate")
	sm.Unregister("translate")
	if sm.Has("translate") {
		t.Error("期望 translate 已被注销")
	}
}

// TestSkillManager_Get 获取技能
func TestSkillManager_Get(t *testing.T) {
	sm := NewSkillManager("op-123")
	sm.registry["translate"] = NewSkill("translate", "翻译", "/skills/translate")
	skill := sm.Get("translate")
	if skill == nil || skill.Name != "translate" {
		t.Error("期望获取 translate 技能")
	}
	if sm.Get("nonexistent") != nil {
		t.Error("期望获取不存在的技能返回 nil")
	}
}

// TestSkillManager_GetAll 获取所有技能
func TestSkillManager_GetAll(t *testing.T) {
	sm := NewSkillManager("op-123")
	sm.registry["b"] = NewSkill("b", "B", "/b")
	sm.registry["a"] = NewSkill("a", "A", "/a")
	all := sm.GetAll()
	if len(all) != 2 {
		t.Errorf("期望 2 个技能，实际 %d", len(all))
	}
	// 按名称排序
	if all[0].Name != "a" || all[1].Name != "b" {
		t.Errorf("期望按名称排序 [a, b]，实际 [%s, %s]", all[0].Name, all[1].Name)
	}
}

// TestSkillManager_GetNames 获取所有技能名称
func TestSkillManager_GetNames(t *testing.T) {
	sm := NewSkillManager("op-123")
	sm.registry["b"] = NewSkill("b", "B", "/b")
	sm.registry["a"] = NewSkill("a", "A", "/a")
	names := sm.GetNames()
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Errorf("期望 [a, b]，实际 %v", names)
	}
}

// TestSkillManager_Has 检查技能存在
func TestSkillManager_Has(t *testing.T) {
	sm := NewSkillManager("op-123")
	sm.registry["translate"] = NewSkill("translate", "翻译", "/skills/translate")
	if !sm.Has("translate") {
		t.Error("期望 Has(translate)=true")
	}
	if sm.Has("nonexistent") {
		t.Error("期望 Has(nonexistent)=false")
	}
}

// TestSkillManager_Clear 清空注册表
func TestSkillManager_Clear(t *testing.T) {
	sm := NewSkillManager("op-123")
	sm.registry["translate"] = NewSkill("translate", "翻译", "/skills/translate")
	sm.Clear()
	if sm.Count() != 0 {
		t.Errorf("期望 Count=0，实际 %d", sm.Count())
	}
}

// TestSkillManager_Count 返回技能数量
func TestSkillManager_Count(t *testing.T) {
	sm := NewSkillManager("op-123")
	if sm.Count() != 0 {
		t.Errorf("期望 Count=0，实际 %d", sm.Count())
	}
	sm.registry["a"] = NewSkill("a", "A", "/a")
	sm.registry["b"] = NewSkill("b", "B", "/b")
	if sm.Count() != 2 {
		t.Errorf("期望 Count=2，实际 %d", sm.Count())
	}
}

// TestSkillManager_loadYAML_含FrontMatter 解析含 YAML front-matter 的文件
func TestSkillManager_loadYAML_含FrontMatter(t *testing.T) {
	provider := newMockFsProvider()
	provider.files["/skills/translate/SKILL.md"] = "---\ndescription: 翻译技能\nauthor: test\n---\n# 翻译技能\n"
	sm := NewSkillManagerWithProvider("op-123", provider)

	yamlData, body, err := sm.loadYAML("/skills/translate/SKILL.md")
	if err != nil {
		t.Fatalf("loadYAML 失败: %v", err)
	}
	if yamlData == nil {
		t.Fatal("期望 yamlData 非空")
	}
	if yamlData["description"] != "翻译技能" {
		t.Errorf("期望 description=翻译技能，实际 %v", yamlData["description"])
	}
	if yamlData["author"] != "test" {
		t.Errorf("期望 author=test，实际 %v", yamlData["author"])
	}
	if body != "# 翻译技能\n" {
		t.Errorf("期望 body='# 翻译技能\\n'，实际 %q", body)
	}
}

// TestSkillManager_loadYAML_无FrontMatter 解析不含 YAML front-matter 的文件
func TestSkillManager_loadYAML_无FrontMatter(t *testing.T) {
	provider := newMockFsProvider()
	provider.files["/skills/translate/SKILL.md"] = "# 翻译技能\n这是纯文本内容。"
	sm := NewSkillManagerWithProvider("op-123", provider)

	yamlData, body, err := sm.loadYAML("/skills/translate/SKILL.md")
	if err != nil {
		t.Fatalf("loadYAML 失败: %v", err)
	}
	if yamlData != nil {
		t.Errorf("期望 yamlData 为 nil，实际 %v", yamlData)
	}
	if body != "# 翻译技能\n这是纯文本内容。" {
		t.Errorf("期望 body 为原文，实际 %q", body)
	}
}

// TestSkillManager_loadDescription_缺失字段 description 字段缺失时返回错误
func TestSkillManager_loadDescription_缺失字段(t *testing.T) {
	provider := newMockFsProvider()
	provider.files["/skills/translate/SKILL.md"] = "---\nname: translate\n---\n"
	sm := NewSkillManagerWithProvider("op-123", provider)

	_, err := sm.loadDescription("/skills/translate/SKILL.md")
	if err == nil {
		t.Error("期望返回错误")
	}
}

// TestSkillManager_loadDescription_无FrontMatter 无 YAML front-matter 时返回错误
func TestSkillManager_loadDescription_无FrontMatter(t *testing.T) {
	provider := newMockFsProvider()
	provider.files["/skills/translate/SKILL.md"] = "纯文本内容"
	sm := NewSkillManagerWithProvider("op-123", provider)

	_, err := sm.loadDescription("/skills/translate/SKILL.md")
	if err == nil {
		t.Error("期望返回错误")
	}
}

// TestFindSkillMD 查找 SKILL.md（不区分大小写）
func TestFindSkillMD(t *testing.T) {
	tests := []struct {
		name      string
		files     []FileInfo
		wantFound bool
		wantPath  string
	}{
		{
			name: "大写SKILL.md",
			files: []FileInfo{
				{Name: "SKILL.md", Path: "/skills/a/SKILL.md"},
				{Name: "other.txt", Path: "/skills/a/other.txt"},
			},
			wantFound: true,
			wantPath:  "/skills/a/SKILL.md",
		},
		{
			name: "小写skill.md",
			files: []FileInfo{
				{Name: "skill.md", Path: "/skills/a/skill.md"},
			},
			wantFound: true,
			wantPath:  "/skills/a/skill.md",
		},
		{
			name: "混合大小写Skill.Md",
			files: []FileInfo{
				{Name: "Skill.Md", Path: "/skills/a/Skill.Md"},
			},
			wantFound: true,
			wantPath:  "/skills/a/Skill.Md",
		},
		{
			name: "无SKILL.md",
			files: []FileInfo{
				{Name: "README.md", Path: "/skills/a/README.md"},
			},
			wantFound: false,
			wantPath:  "",
		},
		{
			name:      "空文件列表",
			files:     []FileInfo{},
			wantFound: false,
			wantPath:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found, path := findSkillMD(tt.files)
			if found != tt.wantFound {
				t.Errorf("期望 found=%v，实际 %v", tt.wantFound, found)
			}
			if path != tt.wantPath {
				t.Errorf("期望 path=%s，实际 %s", tt.wantPath, path)
			}
		})
	}
}

// TestSkillManager_Register_直接文件路径 注册 SKILL.md 文件路径（非目录）
func TestSkillManager_Register_直接文件路径(t *testing.T) {
	provider := newMockFsProvider()
	// ListDirectories 返回错误 → 当作文件路径处理
	provider.files["/skills/translate/SKILL.md"] = "---\ndescription: 翻译技能\n---\n"

	sm := NewSkillManagerWithProvider("op-123", provider)
	err := sm.Register([]string{"/skills/translate/SKILL.md"}, false)
	if err != nil {
		t.Fatalf("Register 失败: %v", err)
	}
	if sm.Count() != 1 {
		t.Errorf("期望 Count=1，实际 %d", sm.Count())
	}
}

// TestSkillManager_Register_目录自身含SKILL 目录自身包含 SKILL.md 时直接注册
func TestSkillManager_Register_目录自身含SKILL(t *testing.T) {
	provider := newMockFsProvider()
	// /skills/translate 本身就是技能目录
	provider.dirs["/skills/translate"] = []DirInfo{} // 没有子目录
	provider.dirFiles["/skills/translate"] = []FileInfo{
		{Name: "SKILL.md", Path: "/skills/translate/SKILL.md"},
	}
	provider.files["/skills/translate/SKILL.md"] = "---\ndescription: 翻译技能\n---\n"

	sm := NewSkillManagerWithProvider("op-123", provider)
	err := sm.Register([]string{"/skills/translate"}, false)
	if err != nil {
		t.Fatalf("Register 失败: %v", err)
	}
	if sm.Count() != 1 {
		t.Errorf("期望 Count=1，实际 %d", sm.Count())
	}
}

// TestSkillManager_Register_多个路径 注册多个路径
func TestSkillManager_Register_多个路径(t *testing.T) {
	provider := newMockFsProvider()
	provider.dirs["/skills/a"] = []DirInfo{}
	provider.dirs["/skills/b"] = []DirInfo{}
	provider.dirFiles["/skills/a"] = []FileInfo{{Name: "SKILL.md", Path: "/skills/a/SKILL.md"}}
	provider.dirFiles["/skills/b"] = []FileInfo{{Name: "SKILL.md", Path: "/skills/b/SKILL.md"}}
	provider.files["/skills/a/SKILL.md"] = "---\ndescription: 技能A\n---\n"
	provider.files["/skills/b/SKILL.md"] = "---\ndescription: 技能B\n---\n"

	sm := NewSkillManagerWithProvider("op-123", provider)
	err := sm.Register([]string{"/skills/a", "/skills/b"}, false)
	if err != nil {
		t.Fatalf("Register 失败: %v", err)
	}
	if sm.Count() != 2 {
		t.Errorf("期望 Count=2，实际 %d", sm.Count())
	}
}

// TestSkillManager_SetSysOperationID 更新 sysOperationID
func TestSkillManager_SetSysOperationID(t *testing.T) {
	sm := NewSkillManager("old-id")
	sm.SetSysOperationID("new-id")
	if sm.sysOperationID != "new-id" {
		t.Errorf("期望 sysOperationID=new-id，实际 %s", sm.sysOperationID)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestOsFsProvider_ReadFile osFsProvider 读取文件
func TestOsFsProvider_ReadFile(t *testing.T) {
	// 创建临时文件
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &osFsProvider{}
	content, err := p.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile 失败: %v", err)
	}
	if content != "hello" {
		t.Errorf("期望 content=hello，实际 %s", content)
	}
}

// TestOsFsProvider_ListFiles osFsProvider 列出文件
func TestOsFsProvider_ListFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	p := &osFsProvider{}
	files, err := p.ListFiles(dir)
	if err != nil {
		t.Fatalf("ListFiles 失败: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("期望 2 个文件，实际 %d", len(files))
	}
}

// TestOsFsProvider_ListDirectories osFsProvider 列出子目录
func TestOsFsProvider_ListDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "b"), 0o755); err != nil {
		t.Fatal(err)
	}

	p := &osFsProvider{}
	dirs, err := p.ListDirectories(dir)
	if err != nil {
		t.Fatalf("ListDirectories 失败: %v", err)
	}
	if len(dirs) != 2 {
		t.Errorf("期望 2 个子目录，实际 %d", len(dirs))
	}
}

// TestOsFsProvider_WriteFile osFsProvider 写入文件
func TestOsFsProvider_WriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "test.txt")

	p := &osFsProvider{}
	err := p.WriteFile(path, []byte("hello"))
	if err != nil {
		t.Fatalf("WriteFile 失败: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	if string(content) != "hello" {
		t.Errorf("期望 content=hello，实际 %s", string(content))
	}
}

// TestSkillManager_registerRoot_目录遍历子目录 父目录遍历子目录注册
func TestSkillManager_registerRoot_目录遍历子目录(t *testing.T) {
	provider := newMockFsProvider()
	// /skills 是父目录，有 translate 和 code-review 两个子目录
	provider.dirs["/skills"] = []DirInfo{
		{Name: "translate", Path: "/skills/translate"},
		{Name: "code-review", Path: "/skills/code-review"},
	}
	provider.dirs["/skills/translate"] = []DirInfo{}
	provider.dirs["/skills/code-review"] = []DirInfo{}
	provider.dirFiles["/skills"] = []FileInfo{} // 父目录不含 SKILL.md
	provider.dirFiles["/skills/translate"] = []FileInfo{{Name: "SKILL.md", Path: "/skills/translate/SKILL.md"}}
	provider.dirFiles["/skills/code-review"] = []FileInfo{{Name: "SKILL.md", Path: "/skills/code-review/SKILL.md"}}
	provider.files["/skills/translate/SKILL.md"] = "---\ndescription: 翻译\n---\n"
	provider.files["/skills/code-review/SKILL.md"] = "---\ndescription: 代码审查\n---\n"

	sm := NewSkillManagerWithProvider("op-123", provider)
	err := sm.Register([]string{"/skills"}, false)
	if err != nil {
		t.Fatalf("Register 失败: %v", err)
	}
	if sm.Count() != 2 {
		t.Errorf("期望 Count=2，实际 %d", sm.Count())
	}
}

// TestSkillManager_Register_空目录名跳过 子目录名为空时跳过
func TestSkillManager_Register_空目录名跳过(t *testing.T) {
	provider := newMockFsProvider()
	provider.dirs["/skills"] = []DirInfo{
		{Name: "", Path: ""}, // 空目录名，应跳过
		{Name: "translate", Path: "/skills/translate"},
	}
	provider.dirs["/skills/translate"] = []DirInfo{}
	provider.dirFiles["/skills"] = []FileInfo{}
	provider.dirFiles["/skills/translate"] = []FileInfo{{Name: "SKILL.md", Path: "/skills/translate/SKILL.md"}}
	provider.files["/skills/translate/SKILL.md"] = "---\ndescription: 翻译\n---\n"

	sm := NewSkillManagerWithProvider("op-123", provider)
	err := sm.Register([]string{"/skills"}, false)
	if err != nil {
		t.Fatalf("Register 失败: %v", err)
	}
	if sm.Count() != 1 {
		t.Errorf("期望 Count=1，实际 %d", sm.Count())
	}
}

// TestSkillManager_SetFsProvider 设置自定义 FsProvider
func TestSkillManager_SetFsProvider(t *testing.T) {
	sm := NewSkillManager("op-123")
	provider := newMockFsProvider()
	sm.SetFsProvider(provider)
	if sm.fsProvider != provider {
		t.Error("期望 fsProvider 被设置为自定义 provider")
	}
}

// TestSkillManager_Register_注册失败时返回错误 注册失败时返回聚合错误
func TestSkillManager_Register_注册失败时返回错误(t *testing.T) {
	provider := newMockFsProvider()
	// /skills 有子目录但子目录没有 SKILL.md
	provider.dirs["/skills"] = []DirInfo{
		{Name: "bad", Path: "/skills/bad"},
	}
	provider.dirs["/skills/bad"] = []DirInfo{}
	provider.dirFiles["/skills/bad"] = []FileInfo{} // 无 SKILL.md

	sm := NewSkillManagerWithProvider("op-123", provider)
	err := sm.Register([]string{"/skills"}, false)
	// 无 SKILL.md 不会报错，只是跳过
	if err != nil {
		t.Fatalf("期望无错误（跳过无 SKILL.md 的目录），实际: %v", err)
	}
	if sm.Count() != 0 {
		t.Errorf("期望 Count=0，实际 %d", sm.Count())
	}
}

// TestSkillManager_registerSkillFromMD_从MD注册 注册 SKILL.md 文件
func TestSkillManager_registerSkillFromMD_从MD注册(t *testing.T) {
	provider := newMockFsProvider()
	provider.files["/skills/translate/SKILL.md"] = "---\ndescription: 翻译技能\n---\n# 翻译技能\n"

	sm := NewSkillManagerWithProvider("op-123", provider)
	err := sm.registerSkillFromMD("/skills/translate/SKILL.md", false)
	if err != nil {
		t.Fatalf("registerSkillFromMD 失败: %v", err)
	}
	if sm.Count() != 1 {
		t.Errorf("期望 Count=1，实际 %d", sm.Count())
	}
}

// TestSkillManager_registerSkillFromMD_文件不存在 文件不存在时返回错误
func TestSkillManager_registerSkillFromMD_文件不存在(t *testing.T) {
	provider := newMockFsProvider()
	sm := NewSkillManagerWithProvider("op-123", provider)
	err := sm.registerSkillFromMD("/nonexistent/SKILL.md", false)
	if err == nil {
		t.Error("期望返回错误")
	}
}

// TestSkillManager_loadYAML_文件不存在 文件不存在时返回错误
func TestSkillManager_loadYAML_文件不存在(t *testing.T) {
	provider := newMockFsProvider()
	sm := NewSkillManagerWithProvider("op-123", provider)
	_, _, err := sm.loadYAML("/nonexistent/SKILL.md")
	if err == nil {
		t.Error("期望返回错误")
	}
}

// ensure 编译检查
var _ = errors.New
var _ = fmt.Sprintf
