package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FsProvider 文件系统操作提供者接口。
//
// 【临时接口】仅用于当前 SkillManager 的文件读取和目录列举，
// 后续 9.32 SysOperation 实现后，提供 SysOperation 适配器替换此接口，
// 届时删除 FsProvider 及其 osFsProvider 默认实现。
type FsProvider interface {
	// ReadFile 读取文件内容，返回文本字符串。
	ReadFile(path string) (string, error)
	// ListFiles 列出目录下的文件（非递归）。
	ListFiles(dir string) ([]FileInfo, error)
	// ListDirectories 列出目录下的子目录（非递归）。
	ListDirectories(dir string) ([]DirInfo, error)
	// WriteFile 写入文件内容。
	WriteFile(path string, data []byte) error
}

// SkillManager 管理技能注册和检索。
//
// 维护一个技能注册表 registry（name → Skill），提供注册、注销、查询方法。
// 技能从包含 SKILL.md 文件的目录加载，SKILL.md 的 YAML front matter 中
// 提取 name 和 description 元数据。
//
// 对应 Python: SkillManager
type SkillManager struct {
	// registry 技能注册表，name → Skill
	registry map[string]*Skill
	// sysOperationID 系统操作 ID，用于后续对接 SysOperation
	sysOperationID string
	// description 当前描述缓存
	description string
	// fsProvider 文件系统操作提供者
	fsProvider FsProvider
}

// FileInfo 文件信息。
type FileInfo struct {
	// Name 文件名
	Name string
	// Path 文件路径
	Path string
}

// DirInfo 目录信息。
type DirInfo struct {
	// Name 目录名
	Name string
	// Path 目录路径
	Path string
}

// osFsProvider 基于 os 包的默认 FsProvider 实现。
//
// 【临时实现】后续 9.32 SysOperation 实现后删除，替换为 SysOperation 适配器。
type osFsProvider struct{}

// ──────────────────────────── 常量 ────────────────────────────

// SkillFileName 技能文件名（SKILL.md）。
//
// 对应 Python: SKILL_FILE_NAME = "SKILL.md"
const SkillFileName = "SKILL.md"

// yamlFrontMatterSeparator YAML front matter 分隔符
const yamlFrontMatterSeparator = "---"

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillManager 创建 SkillManager 实例。
//
// 对应 Python: SkillManager.__init__(sys_operation_id)
func NewSkillManager(sysOperationID string) *SkillManager {
	return &SkillManager{
		registry:       make(map[string]*Skill),
		sysOperationID: sysOperationID,
		fsProvider:     &osFsProvider{},
	}
}

// NewSkillManagerWithProvider 创建使用自定义 FsProvider 的 SkillManager 实例。
//
// 用于测试或后续 SysOperation 适配器注入。
func NewSkillManagerWithProvider(sysOperationID string, provider FsProvider) *SkillManager {
	return &SkillManager{
		registry:       make(map[string]*Skill),
		sysOperationID: sysOperationID,
		fsProvider:     provider,
	}
}

// SetSysOperationID 更新系统操作 ID。
//
// 对应 Python: SkillManager.set_sys_operation_id(sys_operation_id)
func (sm *SkillManager) SetSysOperationID(sysOperationID string) {
	sm.sysOperationID = sysOperationID
}

// SetFsProvider 设置文件系统操作提供者。
//
// 【临时方法】后续 9.32 SysOperation 适配器注入后可删除此方法。
func (sm *SkillManager) SetFsProvider(provider FsProvider) {
	sm.fsProvider = provider
}

// Register 注册技能元数据。
//
// 主注册入口：支持单个路径或多个路径。
// 对每个路径：
//  1. 尝试 ListDirectories → 如果成功，说明是目录
//     a. 先检查目录自身是否包含 SKILL.md → 直接注册
//     b. 否则遍历子目录逐一尝试注册
//  2. ListDirectories 失败 → 当作 SKILL.md 文件路径直接注册
//
// 对应 Python: SkillManager.register(skill_path, session_id, overwrite)
func (sm *SkillManager) Register(skillPaths []string, overwrite bool) error {
	var allErrs []error
	for _, path := range skillPaths {
		if err := sm.registerRoot(path, overwrite); err != nil {
			allErrs = append(allErrs, err)
		}
	}
	if len(allErrs) > 0 {
		return errors.Join(allErrs...)
	}
	return nil
}

// Unregister 注销技能。
//
// 对应 Python: SkillManager.unregister(name)
func (sm *SkillManager) Unregister(name string) {
	delete(sm.registry, name)
}

// Get 按名称获取技能。
//
// 对应 Python: SkillManager.get(name)
func (sm *SkillManager) Get(name string) *Skill {
	return sm.registry[name]
}

// GetAll 获取所有已注册技能。
//
// 对应 Python: SkillManager.get_all()
func (sm *SkillManager) GetAll() []*Skill {
	result := make([]*Skill, 0, len(sm.registry))
	for _, skill := range sm.registry {
		result = append(result, skill)
	}
	// 按名称排序，保证输出顺序稳定
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// GetNames 获取所有技能名称。
//
// 对应 Python: SkillManager.get_names()
func (sm *SkillManager) GetNames() []string {
	result := make([]string, 0, len(sm.registry))
	for name := range sm.registry {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// Has 检查技能是否存在。
//
// 对应 Python: SkillManager.has(name)
func (sm *SkillManager) Has(name string) bool {
	_, ok := sm.registry[name]
	return ok
}

// Clear 清空注册表。
//
// 对应 Python: SkillManager.clear()
func (sm *SkillManager) Clear() {
	sm.registry = make(map[string]*Skill)
}

// Count 返回已注册技能数量。
//
// 对应 Python: SkillManager.count()
func (sm *SkillManager) Count() int {
	return len(sm.registry)
}

// osFsProvider 实现 FsProvider 接口

// ReadFile 使用 os.ReadFile 读取文件内容。
func (p *osFsProvider) ReadFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ListFiles 使用 os.ReadDir 列出目录下的文件（非递归）。
func (p *osFsProvider) ListFiles(dir string) ([]FileInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []FileInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		files = append(files, FileInfo{
			Name: entry.Name(),
			Path: filepath.Join(dir, entry.Name()),
		})
	}
	return files, nil
}

// ListDirectories 使用 os.ReadDir 列出目录下的子目录（非递归）。
func (p *osFsProvider) ListDirectories(dir string) ([]DirInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var dirs []DirInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirs = append(dirs, DirInfo{
			Name: entry.Name(),
			Path: filepath.Join(dir, entry.Name()),
		})
	}
	return dirs, nil
}

// WriteFile 使用 os.WriteFile 写入文件内容（必要时创建父目录）。
func (p *osFsProvider) WriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadYAML 从文件读取 YAML front-matter。
//
// 如果文件以 "---" 开头，则按 YAML front-matter 格式解析：
// ---\nyaml_metadata\n---\nbody_content  ← YAML前置元数据与正文内容的分隔格式
// 返回 (yamlData, body, error)。
//
// 如果不以 "---" 开头，返回 (nil, text, nil)。
//
// 对应 Python: SkillManager._load_yaml(path, session_id)
func (sm *SkillManager) loadYAML(path string) (map[string]any, string, error) {
	text, err := sm.fsProvider.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("读取文件失败 %s: %w", path, err)
	}

	if strings.HasPrefix(text, yamlFrontMatterSeparator) {
		// 按 "---" 分割为三段：前空段 + YAML 块 + 正文
		parts := strings.SplitN(text, yamlFrontMatterSeparator, 3)
		if len(parts) < 3 {
			return nil, text, nil
		}
		yamlBlock := parts[1]
		body := strings.TrimLeft(parts[2], "\n\r")

		var yamlData map[string]any
		if err := yaml.Unmarshal([]byte(yamlBlock), &yamlData); err != nil {
			return nil, text, nil
		}
		return yamlData, body, nil
	}
	return nil, text, nil
}

// loadDescription 从 SKILL.md 的 YAML front-matter 提取 description 字段。
//
// 对应 Python: SkillManager._load_description(path, session_id)
func (sm *SkillManager) loadDescription(path string) (string, error) {
	sm.description = ""
	yamlData, _, err := sm.loadYAML(path)
	if err != nil {
		return "", err
	}
	if yamlData == nil {
		return "", errors.New("SKILL.md 文件不包含 YAML front matter")
	}
	descVal, ok := yamlData["description"]
	if !ok {
		return "", errors.New("SKILL.md 文件不包含 description 字段")
	}
	desc, ok := descVal.(string)
	if !ok {
		return "", fmt.Errorf("SKILL.md description 字段类型错误，期望 string，实际 %T", descVal)
	}
	sm.description = desc
	return desc, nil
}

// createSkillFromPath 从 SKILL.md 路径创建 Skill 对象。
//
// 目录名为技能名称，SKILL.md 所在目录为技能目录。
//
// 对应 Python: SkillManager._create_skill_from_path(path, session_id)
func (sm *SkillManager) createSkillFromPath(skillMDPath string) (*Skill, error) {
	description, err := sm.loadDescription(skillMDPath)
	if err != nil {
		return nil, err
	}
	skillDir := filepath.Dir(skillMDPath)
	skillName := filepath.Base(skillDir)
	return NewSkill(skillName, description, skillDir), nil
}

// findSkillMD 在文件列表中查找 SKILL.md（不区分大小写）。
//
// 对应 Python: SkillManager._find_skill_md(file_items)
func findSkillMD(files []FileInfo) (bool, string) {
	for _, f := range files {
		if strings.EqualFold(f.Name, "skill.md") {
			return true, f.Path
		}
	}
	return false, ""
}

// addToRegistry 向注册表添加技能。
//
// overwrite 为 false 时，如果技能已存在则返回 error。
//
// 对应 Python: SkillManager._add_to_registry(skill, overwrite)
func (sm *SkillManager) addToRegistry(skill *Skill, overwrite bool) error {
	if !overwrite {
		if _, exists := sm.registry[skill.Name]; exists {
			return fmt.Errorf("技能已存在: %s", skill.Name)
		}
	}
	sm.registry[skill.Name] = skill
	return nil
}

// registerSkillFromMD 从 SKILL.md 路径注册技能。
//
// 路径为空时 no-op。
//
// 对应 Python: SkillManager._register_skill_from_md(skill_md_path, session_id, overwrite)
func (sm *SkillManager) registerSkillFromMD(mdPath string, overwrite bool) error {
	if mdPath == "" {
		return nil
	}
	skill, err := sm.createSkillFromPath(mdPath)
	if err != nil {
		return err
	}
	if skill == nil {
		return nil
	}
	return sm.addToRegistry(skill, overwrite)
}

// tryRegisterDirAsSkill 尝试将目录注册为技能。
//
// 列出目录文件 → 查找 SKILL.md → 注册。
// 返回 (found, error)：found 表示是否找到 SKILL.md。
//
// 对应 Python: SkillManager._try_register_dir_as_skill(fs, dir_path, session_id, overwrite)
func (sm *SkillManager) tryRegisterDirAsSkill(dirPath string, overwrite bool) (bool, error) {
	files, err := sm.fsProvider.ListFiles(dirPath)
	if err != nil {
		return false, nil
	}
	if len(files) == 0 {
		return false, nil
	}
	found, skillMDPath := findSkillMD(files)
	if !found {
		return false, nil
	}
	if err := sm.registerSkillFromMD(skillMDPath, overwrite); err != nil {
		return true, err
	}
	return true, nil
}

// registerRoot 对单个根路径执行注册逻辑。
//
// 对应 Python: SkillManager.register() 中的 _register_root(root) 内部函数
func (sm *SkillManager) registerRoot(root string, overwrite bool) error {
	// 尝试列出子目录
	dirs, dirErr := sm.fsProvider.ListDirectories(root)
	if dirErr != nil {
		// root 不是目录 — 当作 SKILL.md 文件路径直接处理
		skill, err := sm.createSkillFromPath(root)
		if err != nil {
			return err
		}
		if skill != nil {
			return sm.addToRegistry(skill, overwrite)
		}
		return nil
	}

	// 先检查 root 自身是否是 skill 目录（直接包含 SKILL.md）
	found, err := sm.tryRegisterDirAsSkill(root, overwrite)
	if err != nil {
		return err
	}
	if found {
		return nil
	}

	// root 是父目录 — 遍历子目录注册多个 skill
	if len(dirs) == 0 {
		return nil
	}
	var allErrs []error
	for _, d := range dirs {
		if d.Path == "" || d.Name == "" {
			continue
		}
		if _, err := sm.tryRegisterDirAsSkill(d.Path, overwrite); err != nil {
			allErrs = append(allErrs, err)
		}
	}
	if len(allErrs) > 0 {
		return errors.Join(allErrs...)
	}
	return nil
}
