// harness_context.go 把被评估 Harness 声明投映为只读证据工作区。
// Python: openjiuwen/rsi/harness_rsi/evaluation_result_analyzer/harness_context.py（全量对齐）。
// 只暴露完整声明指令，绝不暴露模型设置或环境数据。

package evaluationresultanalyzer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	hres "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/resources"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// PrepareHarnessContext 暴露完整声明指令，绝不暴露模型设置或环境数据。
// 评估引用优先于调用方当前 Harness：归因必须描述产出记录评估的对象。
// 无包引用的 legacy 团队运行保持既有 evidence-only 行为。
// Python: harness_context.py:112-217 prepare_harness_context
func PrepareHarnessContext(evalRefPath, harnessRefsPath, runtimeDir string) (map[string]any, error) {
	// Python: evaluation = load_eval_ref(eval_ref_path)
	evaluation, err := memberoptimizer.LoadEvalRef(evalRefPath)
	if err != nil {
		return nil, err
	}
	// Python: refs = evaluation.harness_refs_path or harness_refs_path
	refs := stringOr(evaluation.HarnessRefsPath, harnessRefsPath)
	if refs == "" {
		return map[string]any{"status": "not_provided", "roles": []any{}}, nil
	}
	// Python: refs_path 相对则基于 eval_ref 父目录
	refsPath, _ := expandUserPath(refs)
	if !filepath.IsAbs(refsPath) {
		abs, _ := filepath.Abs(evalRefPath)
		refsPath = filepath.Join(filepath.Dir(abs), refsPath)
	}
	if _, statErr := os.Stat(refsPath); statErr != nil {
		return nil, fmt.Errorf("Evaluated Harness references not found: %s", refsPath)
	}
	// Python: roles = resolve_candidate_roles(str(refs_path), evaluation)
	roles := memberoptimizer.ResolveCandidateRoles(refsPath, &evaluation, "")
	if len(roles) == 0 {
		return nil, fmt.Errorf("Evaluated Harness references contain no roles")
	}
	// Python: destination = runtime_dir / "current_harness"; mkdir(exist_ok=False)
	destination := filepath.Join(runtimeDir, "current_harness")
	if err := os.Mkdir(destination, 0o755); err != nil {
		return nil, fmt.Errorf("current_harness dir create failed (exist_ok=False 语义): %w", err)
	}
	result := map[string]any{
		"status": "available",
		"scope":  "package declarations, not activation evidence; runtime_config.json holds effective runtime values",
		"roles":  []any{},
	}
	resultRoles := result["roles"].([]any)

	for index, role := range roles {
		// Python: for index, role in enumerate(roles, 1)
		roleNumber := index + 1
		root, raw, declErr := declarations(role.HarnessRefPath)
		if declErr != nil {
			return nil, declErr
		}
		// Python: out = destination / f"role_{index:03d}"; out.mkdir()
		out := filepath.Join(destination, fmt.Sprintf("role_%03d", roleNumber))
		if err := os.Mkdir(out, 0o755); err != nil {
			return nil, err
		}
		// Python: aliases = role.metadata.get("aliases", [])
		var aliasesList any
		aliasesList = valueOr(role.Metadata, "aliases", []any{})
		row := map[string]any{
			"role":            role.Role,
			"member_name":     role.MemberName,
			"aliases":         aliasesList,
			"prompt_sections": []any{},
			"skills":          []any{},
			"tools":           resourceNames(raw["tools"]),
			"rails":           resourceNames(raw["rails"]),
		}

		// Python: runtime = _local_path(root, "runtime.yaml")
		runtimePath, runtimeErr := localPath(root, "runtime.yaml")
		if runtimeErr != nil {
			return nil, runtimeErr
		}
		runtimeDeclaration := map[string]any{}
		if isFile(runtimePath) {
			rd, rdErr := readMappingLocal(root, "runtime.yaml")
			if rdErr != nil {
				return nil, rdErr
			}
			runtimeDeclaration = rd
		}
		// Python: restricted_runtime 仅 max_iterations/on_max_iterations
		restrictedRuntime := map[string]any{}
		for _, key := range []string{"max_iterations", "on_max_iterations"} {
			if value, ok := runtimeDeclaration[key]; ok {
				restrictedRuntime[key] = value
			}
		}
		// Python: runtime_target = out / "runtime.yaml"; write_text(yaml.safe_dump(..., sort_keys=False))
		runtimeTarget := filepath.Join(out, "runtime.yaml")
		yamlContent, yamlErr := yaml.Marshal(restrictedRuntime)
		if yamlErr != nil {
			return nil, yamlErr
		}
		if err := os.WriteFile(runtimeTarget, yamlContent, 0o644); err != nil {
			return nil, err
		}
		relRuntime, _ := filepath.Rel(runtimeDir, runtimeTarget)
		runtimeStatus := "inherited_defaults"
		if isFile(runtimePath) {
			runtimeStatus = "declared"
		}
		row["runtime_config"] = map[string]any{
			"status":                    runtimeStatus,
			"path":                      filepath.ToSlash(relRuntime),
			"declaration":               restrictedRuntime,
			"editable_fields":           []string{"max_iterations", "on_max_iterations"},
			"allowed_on_max_iterations": []string{"error", "return_observations"},
			"max_iterations_range":      "positive integer <= effective runtime_config.json max_iterations_hard_limit",
			"effective_values":          "runtime_evidence/runtime_config.json; missing historical values are unknown",
			"policy":                    "Only these two fields are editable. Host limits, models, authentication and scoring are read-only.",
		}

		// Python: for name in ("identity.md", "soul.md"): _local_path(root, name)（路径逃逸校验）
		for _, name := range []string{"identity.md", "soul.md"} {
			if _, escErr := localPath(root, name); escErr != nil {
				return nil, escErr
			}
		}

		// Python: sections = raw.get("prompt_sections") or raw.get("prompts") or []
		sections := raw["prompt_sections"]
		if sections == nil {
			sections = raw["prompts"]
		}
		if sections == nil {
			sections = []any{}
		}
		// Python: if isinstance(sections, dict): sections = sections.get("sections", [])
		if secMap, isMap := sections.(map[string]any); isMap {
			sections = valueOr(secMap, "sections", []any{})
		}
		// Python: file 条目路径校验
		for _, section := range asList(sections) {
			if secMap, isMap := section.(map[string]any); isMap && secMap["file"] != nil {
				if _, escErr := localPath(root, hres.ResolveSectionFile(root, toDisplayString(secMap["file"]))); escErr != nil {
					return nil, escErr
				}
			}
		}
		// Python: manifest.json 存在走 spec 装配，否则 normalize
		var normalized []map[string]any
		if isFile(filepath.Join(root, "manifest.json")) {
			specs, specErr := hres.BuildPromptSectionSpecs(sections, root, root)
			if specErr != nil {
				return nil, specErr
			}
			for _, spec := range specs {
				normalized = append(normalized, specToMap(spec))
			}
		} else {
			for _, section := range asList(sections) {
				if secMap, isMap := section.(map[string]any); isMap {
					normalized = append(normalized, hres.NormalizePromptSection(secMap, root))
				}
			}
		}
		// Python: identity/soul 无 manifest 时追加全文
		for _, name := range []string{"identity", "soul"} {
			source, srcErr := localPath(root, name+".md")
			if srcErr != nil {
				return nil, srcErr
			}
			if isFile(source) && !isFile(filepath.Join(root, "manifest.json")) {
				content, readErr := os.ReadFile(source)
				if readErr != nil {
					return nil, readErr
				}
				normalized = append(normalized, map[string]any{
					"name": name, "content": string(content),
				})
			}
		}
		// Python: prompt_{number:03d}.json 落盘（content JSON）
		for number, section := range normalized {
			target := filepath.Join(out, fmt.Sprintf("prompt_%03d.json", number+1))
			contentJSON, marshalErr := json.Marshal(valueOr(section, "content", map[string]any{}))
			if marshalErr != nil {
				return nil, marshalErr
			}
			if err := os.WriteFile(target, contentJSON, 0o644); err != nil {
				return nil, err
			}
			rel, _ := filepath.Rel(runtimeDir, target)
			row["prompt_sections"] = append(row["prompt_sections"].([]any), map[string]any{
				"name": stringField(section, "name", ""),
				"path": filepath.ToSlash(rel),
			})
		}

		// Python: skill 文件拷贝（SkillManager 三态发现）
		skillPaths, skillErr := skillFiles(root, raw["skills"])
		if skillErr != nil {
			return nil, skillErr
		}
		for number, source := range skillPaths {
			target := filepath.Join(out, fmt.Sprintf("skill_%03d.md", number+1))
			content, readErr := os.ReadFile(source)
			if readErr != nil {
				return nil, readErr
			}
			if err := os.WriteFile(target, content, 0o644); err != nil {
				return nil, err
			}
			rel, _ := filepath.Rel(runtimeDir, target)
			row["skills"] = append(row["skills"].([]any), map[string]any{
				"name": filepath.Base(filepath.Dir(source)),
				"path": filepath.ToSlash(rel),
			})
		}

		// Python: tools/rails file 项拷贝为 {group}_{number:03d}.py
		for _, group := range []string{"tools", "rails"} {
			items, _ := row[group].([]map[string]any)
			for number, item := range items {
				filePath, _ := item["file"].(string)
				if filePath == "" {
					continue
				}
				source, escErr := localPath(root, filePath)
				if escErr != nil {
					return nil, escErr
				}
				target := filepath.Join(out, fmt.Sprintf("%s_%03d.py", group, number+1))
				content, readErr := os.ReadFile(source)
				if readErr != nil {
					return nil, readErr
				}
				if err := os.WriteFile(target, content, 0o644); err != nil {
					return nil, err
				}
				rel, _ := filepath.Rel(runtimeDir, target)
				item["path"] = filepath.ToSlash(rel)
			}
		}

		resultRoles = append(resultRoles, row)
	}
	result["roles"] = resultRoles

	// Python: (destination / "index.json").write_text(json.dumps(..., indent=2))
	indexJSON, marshalErr := json.MarshalIndent(result, "", "  ")
	if marshalErr != nil {
		return nil, marshalErr
	}
	if err := os.WriteFile(filepath.Join(destination, "index.json"), indexJSON, 0o644); err != nil {
		return nil, err
	}
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// localPath 解析包内路径并严防逃逸。
// Python: harness_context.py:22-26 _local_path —— Python (root / path) 对绝对路径
// 是覆盖语义（Path("/r") / "/abs/x" == "/abs/x"），Go filepath.Join 是拼接语义，
// 须显式分支（对齐审查要点：platform/path 语义逐字段核对）。
func localPath(root, path string) (string, error) {
	joined := path
	if !filepath.IsAbs(joined) {
		joined = filepath.Join(root, joined)
	}
	resolved := resolvePath(joined)
	// Python: if not resolved.is_relative_to(root): raise ValueError(...)
	rootResolved := resolvePath(root)
	if !withinBase(resolved, rootResolved) {
		return "", fmt.Errorf("Harness evidence reference leaves its package: %s", path)
	}
	return resolved, nil
}

// readMappingLocal 读取包内 mapping（JSON/YAML 按后缀）。
// Python: harness_context.py:29-34 _read_mapping
func readMappingLocal(root, path string) (map[string]any, error) {
	resolved, err := localPath(root, path)
	if err != nil {
		return nil, err
	}
	content, readErr := os.ReadFile(resolved)
	if readErr != nil {
		return nil, readErr
	}
	var data any
	if strings.HasSuffix(path, ".json") {
		if jsonErr := json.Unmarshal(content, &data); jsonErr != nil {
			return nil, jsonErr
		}
	} else {
		if yamlErr := yaml.Unmarshal(content, &data); yamlErr != nil {
			return nil, yamlErr
		}
	}
	m, isMap := data.(map[string]any)
	if !isMap {
		return nil, fmt.Errorf("Harness evidence must contain a mapping: %s", path)
	}
	return m, nil
}

// asList 等价 Python _as_list（None→[]，标量→单元素）。
// Python: harness_context.py:37-38
func asList(value any) []any {
	if list, isList := value.([]any); isList {
		return list
	}
	if value == nil {
		return []any{}
	}
	return []any{value}
}

// declarations 读取 Harness 包声明（仅五键，杜绝子代理配置与凭据读取）。
// Python: harness_context.py:41-66 _declarations
func declarations(path string) (string, map[string]any, error) {
	// Python: root = path.resolve() if path.is_dir() else path.resolve().parent
	abs := resolvePath(path)
	root := abs
	if !isDir(abs) {
		root = filepath.Dir(abs)
	}
	// Python: main = find_plugin_manifest(path); raw = _read_mapping(root, main)
	main, mainErr := hres.FindPluginManifest(path)
	if mainErr != nil {
		return "", nil, mainErr
	}
	raw, rawErr := readMappingLocal(root, main)
	if rawErr != nil {
		return "", nil, rawErr
	}
	// Python: if main.name != "manifest.json": config 合并 + 四个 yaml 列表合并
	if filepath.Base(main) != "manifest.json" {
		configPath := filepath.Join(root, "config.json")
		if isFile(configPath) {
			configData, configErr := readMappingLocal(root, configPath)
			if configErr != nil {
				return "", nil, configErr
			}
			for key, value := range configData {
				if _, exists := raw[key]; !exists {
					raw[key] = value
				}
			}
		}
		// Python: 四个 (key, relative, nested) 组合
		for _, spec := range []struct{ key, relative, nested string }{
			{"prompt_sections", "prompt_sections/sections.yaml", "sections"},
			{"skills", "skills/skills.yaml", "skills"},
			{"tools", "tools/tools.yaml", "tools"},
			{"rails", "rails/rails.yaml", "rails"},
		} {
			source, srcErr := localPath(root, spec.relative)
			if srcErr != nil {
				return "", nil, srcErr
			}
			if !isFile(source) {
				continue
			}
			content, readErr := os.ReadFile(source)
			if readErr != nil {
				return "", nil, readErr
			}
			var items any
			if yamlErr := yaml.Unmarshal(content, &items); yamlErr != nil {
				return "", nil, yamlErr
			}
			if items == nil {
				items = []any{}
			}
			if m, isMap := items.(map[string]any); isMap {
				items = valueOr(m, spec.nested, []any{})
			}
			itemsList, isList := items.([]any)
			if !isList {
				return "", nil, fmt.Errorf("Harness evidence %s must contain a list", spec.relative)
			}
			if _, isList := raw[spec.key].([]any); !isList {
				if _, exists := raw[spec.key]; exists {
					return "", nil, fmt.Errorf("Harness evidence %s must contain a list", spec.relative)
				}
				raw[spec.key] = []any{}
			}
			// Python: raw.setdefault(key, []).extend(items)
			raw[spec.key] = append(raw[spec.key].([]any), itemsList...)
		}
	}
	// Python: 仅保留五键（不使用完整 runtime loader：其可读子代理配置与凭据）
	out := map[string]any{}
	for _, key := range []string{"prompt_sections", "prompts", "skills", "tools", "rails"} {
		if value, ok := raw[key]; ok {
			out[key] = value
		}
	}
	return root, out, nil
}

// resourceNames 资源声明投影（builtin/name/file/class 等键白名单）。
// Python: harness_context.py:69-82 _resource_names
func resourceNames(items any) []map[string]any {
	var declarations []map[string]any
	for _, item := range asList(items) {
		switch v := item.(type) {
		case string:
			declarations = append(declarations, map[string]any{"builtin": v})
		case map[string]any:
			entry := map[string]any{}
			for _, key := range []string{"builtin", "builtins", "name", "names", "file", "class", "import_path", "class_name"} {
				if value, ok := v[key]; ok {
					entry[key] = value
				}
			}
			declarations = append(declarations, entry)
		}
	}
	return declarations
}

// skillFiles 技能文件三态发现（文件 / 技能目录 / 直接父目录）。
// Python: harness_context.py:85-109 _skill_files
func skillFiles(root string, declarations any) ([]string, error) {
	var found []string
	seen := map[string]bool{}
	for _, declaration := range asList(declarations) {
		// Python: value = declaration.get("dir") if isinstance(dict) else declaration
		var value any = declaration
		if m, isMap := declaration.(map[string]any); isMap {
			value = m["dir"]
		}
		str, isStr := value.(string)
		if !isStr || str == "" {
			return nil, fmt.Errorf("Declared skill must identify a local directory or SKILL.md")
		}
		source, srcErr := localPath(root, str)
		if srcErr != nil {
			return nil, srcErr
		}
		if isFile(source) {
			if !seen[source] {
				seen[source] = true
				found = append(found, source)
			}
			continue
		}
		if !isDir(source) {
			return nil, fmt.Errorf("Declared skill path not found: %s", source)
		}
		// Python: children = sorted(source.iterdir())
		entries, readErr := os.ReadDir(source)
		if readErr != nil {
			return nil, readErr
		}
		var children []string
		for _, entry := range entries {
			children = append(children, entry.Name())
		}
		sort.Strings(children)
		// Python: 直接子项含 SKILL.md（大小写不敏感）
		direct := ""
		for _, child := range children {
			if strings.EqualFold(child, "skill.md") {
				direct = child
				break
			}
		}
		if direct != "" {
			resolved, resErr := localPath(root, filepath.Join(source, direct))
			if resErr != nil {
				return nil, resErr
			}
			if !seen[resolved] {
				seen[resolved] = true
				found = append(found, resolved)
			}
			continue
		}
		// Python: 子目录内找 SKILL.md
		for _, child := range children {
			directory, dirErr := localPath(root, filepath.Join(source, child))
			if dirErr != nil {
				return nil, dirErr
			}
			if !isDir(directory) {
				continue
			}
			subEntries, subErr := os.ReadDir(directory)
			if subErr != nil {
				return nil, subErr
			}
			var subChildren []string
			for _, entry := range subEntries {
				subChildren = append(subChildren, entry.Name())
			}
			sort.Strings(subChildren)
			for _, file := range subChildren {
				if strings.EqualFold(file, "skill.md") {
					resolved, resErr := localPath(root, filepath.Join(directory, file))
					if resErr != nil {
						return nil, resErr
					}
					if !seen[resolved] {
						seen[resolved] = true
						found = append(found, resolved)
					}
					break
				}
			}
		}
	}
	return found, nil
}

// specToMap PromptSectionSpec → map（等价 Python spec.model_dump()）。
func specToMap(spec hschema.PromptSectionSpec) map[string]any {
	out := map[string]any{"name": spec.Name}
	content := map[string]any{}
	for lang, text := range spec.Content {
		content[lang] = text
	}
	out["content"] = content
	if spec.Priority != 0 {
		out["priority"] = spec.Priority
	}
	if spec.RenderParams != nil {
		params := map[string]any{}
		for key, value := range spec.RenderParams {
			params[key] = value
		}
		out["render_params"] = params
	}
	return out
}

// mapValue any → map[string]any（nil 安全）。
func mapValue(value any) map[string]any {
	if m, isMap := value.(map[string]any); isMap {
		return m
	}
	return map[string]any{}
}
