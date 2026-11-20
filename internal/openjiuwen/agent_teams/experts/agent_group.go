// Python 源：openjiuwen/agent_teams/experts/agent_group.py（523 行 1:1）
//
// AgentGroup 包格式：加载器与展示卡片。包规则只在这里（自 jiuwenswarm 迁入）：
//
//   - manifest.json 声明 package_type="agent_group"、name 等于包目录名、agents
//     花名册必须含 leader；花名册名都是合法 Team member_name；
//   - 每个成员是 agents/<member>/ 内一个 AgentTemplate 包；成员身份取目录名，
//     永不取其 manifest name；
//   - 加载时改写每个成员模板：agent_card.id 变为成员名、组 instruction 追加为
//     agent_group_instruction 提示词段、组共享 skills 合并、leader 的 AGENT.md
//     在其 persona 目录未覆盖时显式挂载。
//
// Python 的异步变体（aload_* / read_agent_group_card 的 to_thread）沿 D-15b
// 惯例同步化（Go 调用方自行 goroutine 化）。

package experts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/resources"
	hschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// ExpertsLogComponent experts 组件日志。
const ExpertsLogComponent = logger.ComponentAgentServer

// LeaderMember 每个 AgentGroup 必含的花名册名；永不可作 teammate spawn。
// Python: LEADER_MEMBER (agent_group.py:57)
const LeaderMember = "leader"

// rosterNamePattern 花名册名即预定义团队成员名（路由键与路径段）。与
// spawn_teammate 的 ^[a-z][a-z0-9-]*$ 不同，它还允许 _：Hub 发布包使用
// narrative_designer 这类名，其提示词按名引用成员。
// Python: _ROSTER_NAME_PATTERN (agent_group.py:62)
var rosterNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// ──────────────────────────── 结构体 ────────────────────────────

// LoadedAgentGroupPackage 一次校验通过的 AgentGroup 加载，单次调用链内可复用。
// Python: LoadedAgentGroupPackage (agent_group.py:69-90)
type LoadedAgentGroupPackage struct {
	// PackageDir 包目录（已解析绝对路径）
	PackageDir string
	// Manifest manifest 映射
	Manifest map[string]any
	// Templates 按成员名索引的模板 spec（含 leader）
	Templates map[string]hschema.AgentTemplateSpec
}

// Instruction manifest.instruction（trim 后）。
func (p *LoadedAgentGroupPackage) Instruction() string {
	v, _ := p.Manifest["instruction"].(string)
	return strings.TrimSpace(v)
}

// Capabilities 显式执行能力，否则取稳定的包 tag 身份。
func (p *LoadedAgentGroupPackage) Capabilities() []string {
	return GroupCapabilities(p.Manifest)
}

// AgentGroupMemberCard AgentGroup 卡片的一个花名册条目。
// Python: AgentGroupMemberCard (agent_group.py:104-124)
type AgentGroupMemberCard struct {
	// Name 成员名 = agents/<name>/ 目录（即 Team member_name）
	Name string
	// Group 所属组名
	Group string
	// Card 成员 manifest 的卡片；其 name 保持 manifest 值
	Card resources.AgentTemplateCard
}

// Ref 全局身份 (group, name)。Python: ref property (:115-117)
func (m *AgentGroupMemberCard) Ref() ExpertRef {
	return ExpertRef{Name: m.Name, Group: m.Group}
}

// IsLeader 是否组的 leader。Python: is_leader property (:119-121)
func (m *AgentGroupMemberCard) IsLeader() bool {
	return m.Name == LeaderMember
}

// DisplayLabel 按 language 展示名：卡片展示名 → manifest 名 → 成员名。
func (m *AgentGroupMemberCard) DisplayLabel(language string) string {
	if label := m.Card.DisplayLabel(language); label != "" {
		return label
	}
	return m.Name
}

// AgentGroupCard 一个 AgentGroup 包的展示信息（仅 manifest）。
// Python: AgentGroupCard (agent_group.py:126-153)
type AgentGroupCard struct {
	Name               string
	Description        string
	DisplayName        resources.LocalizedText
	DisplayDescription resources.LocalizedText
	Instruction        string
	Capabilities       []string
	Category           string
	Tags               []resources.LocalizedText
	Version            string
	Avatar             string
	// Members 花名册序（含 leader）。
	Members []AgentGroupMemberCard
}

// DisplayLabel 按 language 展示名，回落 name。
func (c *AgentGroupCard) DisplayLabel(language string) string {
	if label := resources.LocalizedTextOf(c.DisplayName, language); label != "" {
		return label
	}
	return c.Name
}

// DisplayText 按 language 展示描述，回落 description。
func (c *AgentGroupCard) DisplayText(language string) string {
	if text := resources.LocalizedTextOf(c.DisplayDescription, language); text != "" {
		return text
	}
	return c.Description
}

// Member 按成员名取花名册条目；缺失得 nil。
func (c *AgentGroupCard) Member(name string) *AgentGroupMemberCard {
	for i := range c.Members {
		if c.Members[i].Name == name {
			return &c.Members[i]
		}
	}
	return nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GroupCapabilities 用显式执行能力，否则取稳定的包 tag 身份。
// dict 项取 id/zh/en；保序去重。
// Python: group_capabilities (agent_group.py:66-81)
func GroupCapabilities(manifest map[string]any) []string {
	raw, present := manifest["capabilities"]
	if !present {
		raw = manifest["tags"]
	}
	items, _ := raw.([]any)
	names := []string{}
	seen := map[string]bool{}
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			item = orAny(orAny(m["id"], m["zh"]), m["en"])
		}
		name := ""
		if s, ok := item.(string); ok {
			name = strings.TrimSpace(s)
		}
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// ReadAgentGroupCard 读 AgentGroup 卡片：组 manifest + 成员 manifests。
// 只建卡片——persona、工具、技能内容不加载。
// Python: load_agent_group_card (agent_group.py:161-178)（async 包装 :156-158
// 沿 D-15b 同步化）
func ReadAgentGroupCard(packageDir string) (*AgentGroupCard, error) {
	root := expandUser(packageDir)
	manifest, err := resources.ReadManifestMapping(filepath.Join(root, "manifest.json"))
	if err != nil {
		return nil, err
	}
	if manifest["package_type"] != "agent_group" {
		return nil, fmt.Errorf("agent_group manifest package_type must be 'agent_group'")
	}
	names, err := agentNames(manifest)
	if err != nil {
		return nil, err
	}
	memberCards := make([]resources.AgentTemplateCard, 0, len(names))
	for _, name := range names {
		card, err := resources.LoadAgentTemplateCard(filepath.Join(root, "agents", name, "manifest.json"))
		if err != nil {
			return nil, err
		}
		memberCards = append(memberCards, card)
	}
	return groupCard(root, manifest, names, memberCards), nil
}

// LoadAgentGroupPackage 加载一个校验通过的 AgentGroup 为按成员有序的模板 spec。
// Python: load_agent_group_package (agent_group.py:207-209)
func LoadAgentGroupPackage(path string) (map[string]hschema.AgentTemplateSpec, error) {
	pkg, err := LoadAgentGroupPackageBundle(path)
	if err != nil {
		return nil, err
	}
	return pkg.Templates, nil
}

// LoadAgentGroupPackageBundle 加载并校验一个 AgentGroup 包（一次）。
// Python: load_agent_group_package_bundle (agent_group.py:217-283)
func LoadAgentGroupPackageBundle(path string) (*LoadedAgentGroupPackage, error) {
	packageDir, err := filepath.Abs(expandUser(path))
	if err != nil {
		return nil, fmt.Errorf("agent_group package directory not found: %s", path)
	}
	if stat, statErr := os.Stat(packageDir); statErr != nil || !stat.IsDir() {
		// Python: resolve(strict=True) 的 OSError → ValueError（:220-222）
		if _, err := os.Lstat(packageDir); err != nil {
			return nil, fmt.Errorf("agent_group package directory not found: %s", path)
		}
		return nil, fmt.Errorf("agent_group package path is not a directory: %s", path)
	}

	manifestPath, err := packageFile(filepath.Join(packageDir, "manifest.json"), packageDir, "agent_group manifest")
	if err != nil {
		return nil, err
	}
	payload, err := readMapping(manifestPath, "agent_group manifest")
	if err != nil {
		return nil, err
	}
	if payload["package_type"] != "agent_group" {
		return nil, fmt.Errorf("agent_group manifest package_type must be 'agent_group'")
	}
	if name, _ := payload["name"].(string); name != filepath.Base(packageDir) {
		return nil, fmt.Errorf("agent_group manifest name must match directory: %q != %q", payload["name"], filepath.Base(packageDir))
	}

	instruction, _ := payload["instruction"].(string)
	if payload["instruction"] != nil && typeof(payload["instruction"]) != "string" {
		return nil, fmt.Errorf("agent_group manifest instruction must be a string")
	}
	instruction = strings.TrimSpace(instruction)
	sharedSkills, err := sharedSkills(packageDir, payload)
	if err != nil {
		return nil, err
	}

	templates := map[string]hschema.AgentTemplateSpec{}
	names, err := agentNames(payload)
	if err != nil {
		return nil, err
	}
	for _, agentName := range names {
		template, err := loadMemberTemplate(packageDir, agentName)
		if err != nil {
			return nil, err
		}
		if agentName == LeaderMember && template.Runtime != nil {
			return nil, fmt.Errorf("AgentGroup leader does not support external runtime")
		}
		promptSections := append([]hschema.PromptSectionSpec{}, template.PromptSections...)
		if instruction != "" {
			for _, section := range promptSections {
				if section.Name == "agent_group_instruction" {
					return nil, fmt.Errorf("AgentTemplate %q reserves prompt section name 'agent_group_instruction'", agentName)
				}
			}
			promptSections = append(promptSections, hschema.PromptSectionSpec{
				Name:     "agent_group_instruction",
				Content:  map[string]string{"cn": instruction, "en": instruction},
				Priority: 20,
			})
		}

		skills := append([]hschema.SkillSpec{}, template.Skills...)
		skillDirs := map[string]bool{}
		for _, skill := range skills {
			if abs, err := filepath.Abs(skill.Dir); err == nil {
				skillDirs[abs] = true
			}
		}
		for _, skill := range sharedSkills {
			sharedDir, err := filepath.Abs(skill.Dir)
			if err != nil {
				continue
			}
			if !skillDirs[sharedDir] {
				skills = append(skills, skill)
				skillDirs[sharedDir] = true
			}
		}
		template.PromptSections = promptSections
		template.Skills = skills
		templates[agentName] = template
	}
	return &LoadedAgentGroupPackage{
		PackageDir: packageDir,
		Manifest:   payload,
		Templates:  templates,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// groupCard 组装卡片（load_agent_group_card 的装配半区）。
// Python: _group_card (agent_group.py:181-204)
func groupCard(root string, manifest map[string]any, names []string, memberCards []resources.AgentTemplateCard) *AgentGroupCard {
	groupName, _ := manifest["name"].(string)
	if groupName == "" {
		groupName = filepath.Base(root)
	}
	var rawTags []any
	if tags, ok := manifest["tags"].([]any); ok {
		rawTags = tags
	}
	card := &AgentGroupCard{
		Name:               groupName,
		Description:        resources.ManifestText(manifest["description"]),
		DisplayName:        resources.NormalizeLocalized(manifest["display_name"]),
		DisplayDescription: resources.NormalizeLocalized(manifest["display_description"]),
		Instruction:        resources.ManifestText(manifest["instruction"]),
		Capabilities:       GroupCapabilities(manifest),
		Category:           stringOr(manifest["category"]),
		Version:            resources.ManifestText(manifest["version"]),
		Avatar:             stringOr(manifest["avatar"]),
	}
	for _, tag := range rawTags {
		card.Tags = append(card.Tags, resources.NormalizeLocalized(tag))
	}
	for i, name := range names {
		card.Members = append(card.Members, AgentGroupMemberCard{Name: name, Group: groupName, Card: memberCards[i]})
	}
	return card
}

// readMapping JSON 读一个映射文件（错误全部转 ValueError 语义）。
// Python: _read_mapping (agent_group.py:286-295)
func readMapping(path string, label string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s missing: %s", label, path)
		}
		return nil, fmt.Errorf("%s is not valid JSON: %s", label, path)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %s", label, path)
	}
	if payload == nil {
		return nil, fmt.Errorf("%s must contain a JSON object: %s", label, path)
	}
	return payload, nil
}

// safeComponent 校验路径安全组件名。
// Python: _safe_component (agent_group.py:298-308)
func safeComponent(value any, label string) (string, error) {
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("invalid %s: expected a string", label)
	}
	name := strings.TrimSpace(s)
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("invalid %s: %q", label, value)
	}
	if strings.ContainsAny(name, "/\\") {
		return "", fmt.Errorf("invalid %s (path separator): %q", label, name)
	}
	if filepath.IsAbs(name) || isWindowsAbs(name) {
		return "", fmt.Errorf("invalid %s (absolute path): %q", label, name)
	}
	return name, nil
}

// childDir 解析 root 下受控子目录（防逃逸）。
// Python: _child_dir (agent_group.py:311-317)
func childDir(root string, name string, label string) (string, error) {
	candidate, err := filepath.Abs(filepath.Join(root, name))
	if err != nil {
		return "", fmt.Errorf("%s escapes AgentGroup package: %s", label, name)
	}
	if !within(candidate, root) {
		return "", fmt.Errorf("%s escapes AgentGroup package: %s", label, name)
	}
	if stat, err := os.Stat(candidate); err != nil || !stat.IsDir() {
		return "", fmt.Errorf("%s is not a directory: %s", label, candidate)
	}
	return candidate, nil
}

// packageFile 解析并校验 root 内的文件。
// Python: _package_file (agent_group.py:320-327)
func packageFile(path string, root string, label string) (string, error) {
	resolved, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("%s missing: %s", label, path)
	}
	if !within(resolved, root) {
		return "", fmt.Errorf("%s must be a file inside %s: %s", label, root, path)
	}
	if stat, err := os.Stat(resolved); err != nil || stat.IsDir() {
		return "", fmt.Errorf("%s missing: %s", label, path)
	}
	return resolved, nil
}

// personaDir 校验并解析 persona 目录（必须包相对、存在、含 Markdown、不逃逸）。
// Python: _persona_dir (agent_group.py:330-367)
func personaDir(agentDir string, manifest map[string]any, agentName string) (string, error) {
	persona, ok := manifest["persona"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("AgentTemplate %q must declare persona.dir", agentName)
	}
	rawDir, ok := persona["dir"].(string)
	if !ok {
		return "", fmt.Errorf("AgentTemplate %q must declare persona.dir", agentName)
	}
	if filepath.IsAbs(expandUser(rawDir)) || isWindowsAbs(expandUser(rawDir)) {
		return "", fmt.Errorf("AgentTemplate %q persona.dir must be package-relative", agentName)
	}
	resolved, err := filepath.Abs(filepath.Join(agentDir, expandUser(rawDir)))
	if err != nil {
		return "", fmt.Errorf("AgentTemplate %q persona.dir does not exist: %s", agentName, rawDir)
	}
	if _, statErr := os.Stat(resolved); statErr != nil {
		return "", fmt.Errorf("AgentTemplate %q persona.dir does not exist: %s", agentName, rawDir)
	}
	if !within(resolved, agentDir) {
		return "", fmt.Errorf("AgentTemplate %q persona.dir escapes its package", agentName)
	}
	if stat, err := os.Stat(resolved); err != nil || !stat.IsDir() {
		return "", fmt.Errorf("AgentTemplate %q persona.dir escapes its package", agentName)
	}
	markdownFiles := []string{}
	_ = filepath.WalkDir(resolved, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md") {
			markdownFiles = append(markdownFiles, path)
		}
		return nil
	})
	if len(markdownFiles) == 0 {
		return "", fmt.Errorf("AgentTemplate %q persona.dir has no Markdown files", agentName)
	}
	for _, markdownFile := range markdownFiles {
		if _, err := packageFile(markdownFile, agentDir, fmt.Sprintf("AgentTemplate %q persona Markdown", agentName)); err != nil {
			return "", err
		}
	}
	return resolved, nil
}

// agentNames 解析并校验 manifest agents 花名册。
// Python: _agent_names (agent_group.py:370-390)
func agentNames(payload map[string]any) ([]string, error) {
	rawAgents, ok := payload["agents"].([]any)
	if !ok || len(rawAgents) == 0 {
		return nil, fmt.Errorf("agent_group manifest agents must be a non-empty list")
	}
	names := []string{}
	seen := map[string]bool{}
	for _, rawName := range rawAgents {
		name, err := safeComponent(rawName, "agent name")
		if err != nil {
			return nil, err
		}
		if !rosterNamePattern.MatchString(name) {
			return nil, fmt.Errorf("invalid agent name in agent_group manifest: %q; must start with a lowercase ASCII letter (a-z), followed by lowercase letters, digits (0-9), underscore (_) or hyphen (-)", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate agent name in agent_group manifest: %s", name)
		}
		seen[name] = true
		names = append(names, name)
	}
	if !seen[LeaderMember] {
		return nil, fmt.Errorf("agent_group manifest agents must contain 'leader'")
	}
	return names, nil
}

// sharedSkills 解析组共享 skills：manifest skills 条目 + skills/ 目录扫描。
// Python: _shared_skills (agent_group.py:393-440)
func sharedSkills(packageDir string, payload map[string]any) ([]hschema.SkillSpec, error) {
	rawSkills, _ := payload["skills"].([]any)
	if payload["skills"] != nil && rawSkills == nil {
		return nil, fmt.Errorf("agent_group manifest skills must be a list")
	}
	skillsPath := filepath.Join(packageDir, "skills")
	specs := []hschema.SkillSpec{}
	seenPaths := map[string]bool{}

	appendSkill := func(raw any) error {
		values := map[string]any{}
		if s, ok := raw.(string); ok {
			name, err := safeComponent(s, "skill name")
			if err != nil {
				return err
			}
			values["dir"] = filepath.Join(skillsPath, name)
			values["mode"] = "all"
		} else if m, ok := raw.(map[string]any); ok {
			rawDir, ok := m["dir"].(string)
			if !ok {
				return fmt.Errorf("agent_group manifest skills entries must be names or dir mappings")
			}
			if filepath.IsAbs(expandUser(rawDir)) || isWindowsAbs(expandUser(rawDir)) {
				return fmt.Errorf("agent_group skill dir must be package-relative")
			}
			for k, v := range m {
				values[k] = v
			}
			values["dir"] = filepath.Join(packageDir, rawDir)
		} else {
			return fmt.Errorf("agent_group manifest skills entries must be names or dir mappings")
		}
		skillDir, err := filepath.Abs(values["dir"].(string))
		if err != nil {
			return fmt.Errorf("shared skill not found: %v", raw)
		}
		if _, statErr := os.Stat(skillDir); statErr != nil {
			return fmt.Errorf("shared skill not found: %v", raw)
		}
		if !within(skillDir, packageDir) {
			return fmt.Errorf("shared skill escapes AgentGroup package: %v", raw)
		}
		if stat, err := os.Stat(skillDir); err != nil || !stat.IsDir() {
			return fmt.Errorf("shared skill escapes AgentGroup package: %v", raw)
		}
		if _, err := packageFile(filepath.Join(skillDir, "SKILL.md"), skillDir, fmt.Sprintf("shared skill %q SKILL.md", filepath.Base(skillDir))); err != nil {
			return err
		}
		key := skillDir
		if seenPaths[key] {
			return fmt.Errorf("duplicate shared skill in agent_group manifest: %s", filepath.Base(skillDir))
		}
		seenPaths[key] = true
		values["dir"] = key
		spec, convErr := skillSpecFrom(values)
		if convErr != nil {
			return convErr
		}
		specs = append(specs, spec)
		return nil
	}

	for _, rawSkill := range rawSkills {
		if err := appendSkill(rawSkill); err != nil {
			return nil, err
		}
	}

	if _, err := os.Lstat(skillsPath); err == nil {
		skillsRoot, err := childDir(packageDir, "skills", "skills directory")
		if err != nil {
			return nil, err
		}
		children, _ := os.ReadDir(skillsRoot)
		sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
		for _, child := range children {
			childPath := filepath.Join(skillsRoot, child.Name())
			if stat, err := os.Stat(childPath); err != nil || !stat.IsDir() {
				continue
			}
			if _, err := os.Stat(filepath.Join(childPath, "SKILL.md")); err != nil {
				continue
			}
			if !seenPaths[childPath] {
				rel, _ := filepath.Rel(packageDir, childPath)
				if err := appendSkill(map[string]any{"dir": rel, "mode": "all"}); err != nil {
					return nil, err
				}
			}
		}
	}
	return specs, nil
}

// loadMemberTemplate 加载一个成员的 AgentTemplate spec（身份改写 + leader 规则挂载）。
// Python: _load_member_template (agent_group.py:443-509)
func loadMemberTemplate(packageDir string, agentName string) (hschema.AgentTemplateSpec, error) {
	var template hschema.AgentTemplateSpec

	agentsRoot, err := filepath.Abs(filepath.Join(packageDir, "agents"))
	if err != nil {
		return template, fmt.Errorf("agent directory not found: %s", agentName)
	}
	if _, statErr := os.Stat(agentsRoot); statErr != nil {
		return template, fmt.Errorf("agent directory not found: %s", agentName)
	}
	agentDir, err := childDir(agentsRoot, agentName, "agent directory")
	if err != nil {
		return template, fmt.Errorf("agent directory not found: %s", agentName)
	}
	manifestPath, err := packageFile(filepath.Join(agentDir, "manifest.json"), agentDir, fmt.Sprintf("AgentTemplate manifest for %q", agentName))
	if err != nil {
		return template, err
	}
	manifest, err := readMapping(manifestPath, fmt.Sprintf("AgentTemplate manifest for %q", agentName))
	if err != nil {
		return template, err
	}
	if manifest["package_type"] != "agent_template" {
		return template, fmt.Errorf("AgentTemplate %q must declare package_type='agent_template'", agentName)
	}
	personaPath, err := personaDir(agentDir, manifest, agentName)
	if err != nil {
		return template, err
	}

	agentMD := filepath.Join(agentDir, "AGENT.md")
	agentMDExists := fileOrSymlinkExists(agentMD)
	if agentName == LeaderMember {
		if _, err := packageFile(agentMD, agentDir, "AgentGroup leader AGENT.md"); err != nil {
			return template, err
		}
	} else if agentMDExists {
		return template, fmt.Errorf("AgentGroup member %q must not contain AGENT.md; put its responsibilities in persona instead", agentName)
	}

	template, err = resources.LoadAgentTemplatePackage(manifestPath)
	if err != nil {
		return template, err
	}
	// 团队成员身份由 AgentGroup 花名册/目录定义；模板顶层 name 保持用户可见
	// 展示名。Python: agent_group.py:483-491
	if template.AgentCard.BaseCard.ID != agentName {
		template.AgentCard.BaseCard.ID = agentName
	}

	// leader manifest 通常 persona.dir="."，标准加载器把 AGENT.md 与
	// persona/*.md 一起读。对更窄的 persona 目录保留契约：显式挂一次 AGENT.md。
	// Python: agent_group.py:493-508
	if agentName == LeaderMember && !within(agentMD, personaPath) {
		leaderRulesText, err := os.ReadFile(agentMD)
		if err != nil {
			return template, err
		}
		text := string(leaderRulesText)
		template.PromptSections = append(template.PromptSections, hschema.PromptSectionSpec{
			Name:     "agent_group_leader_rules",
			Content:  map[string]string{"cn": text, "en": text},
			Priority: 15,
		})
	}
	return template, nil
}

// ──────────────────────────── 通用小助手 ────────────────────────────

// expandUser ~ 展开（Python Path.expanduser）。
func expandUser(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), string(os.PathSeparator)))
		}
	}
	return path
}

// isWindowsAbs Windows 盘符绝对路径（Python PureWindowsPath.is_absolute）。
func isWindowsAbs(name string) bool {
	if len(name) >= 3 && name[1] == ':' && (name[2] == '/' || name[2] == '\\') {
		return true
	}
	return strings.HasPrefix(name, "\\\\")
}

// within 判断 path 是否在 root 内（Python Path.is_relative_to）。
func within(path string, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && rel != "")
}

// fileOrSymlinkExists 文件或软链存在（Python exists() or is_symlink()）。
func fileOrSymlinkExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// stringOr 字符串值安全提取（Python isinstance(x, str) 守卫）。
func stringOr(v any) string {
	s, _ := v.(string)
	return s
}

// orAny 首个非 nil（Python a or b）。
func orAny(a, b any) any {
	if a != nil {
		return a
	}
	return b
}

// typeof any 的类型名（instruction 守卫用）。
func typeof(v any) string {
	switch v.(type) {
	case string:
		return "string"
	}
	return ""
}

// skillSpecFrom map → SkillSpec（Python SkillSpec.model_validate）。
func skillSpecFrom(values map[string]any) (hschema.SkillSpec, error) {
	var spec hschema.SkillSpec
	data, err := json.Marshal(values)
	if err != nil {
		return spec, err
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		return spec, err
	}
	return spec, nil
}

// TagsAsText 组卡 tags 的全部语言文本拼接（检索文档用）。
// Python: _localized_values(card.tags)（tool_expert.py:113 经 _search_document）
func (c *AgentGroupCard) TagsAsText() string {
	texts := []string{}
	for _, tag := range c.Tags {
		if tag.Map != nil {
			for _, text := range tag.Map {
				texts = append(texts, text)
			}
		} else if tag.Str != "" {
			texts = append(texts, tag.Str)
		}
	}
	return strings.Join(texts, " ")
}
