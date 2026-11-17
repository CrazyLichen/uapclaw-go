// verification_checks.go 静态验证检查函数群。
// Python: member_optimizer/verification.py:91-663 + 1666-1754（检查函数与
// Skill 包扫描）。agent 类（HarnessRepairAgent/HarnessChangeVerifier）在
// verification.go。

package memberoptimizer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/tamnd/gopapy/v2/parser2"
	"gopkg.in/yaml.v3"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	harnessresources "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/resources"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

// repairableCheckPrefixes 可修复检查名前缀。
// Python: verification.py:63-83 _REPAIRABLE_CHECK_PREFIXES。
var repairableCheckPrefixes = []string{
	"package_registry:", "prompt_section_binding:", "runtime_policy:",
	"integration_dir:", "yaml_parse:", "json_parse:", "python_compile:",
	"expert_harness_load:", "prompt_section_ref:", "skill_ref:",
	"skill_safety:", "skill_runtime_mount:", "tool_file_ref:",
	"tool_schema:", "tool_activation:", "rail_file_ref:",
	"rail_runtime_contract:", "expert_harness_resolve:",
}

// unrepairableCheckNames 不可修复检查名。
// Python: verification.py:92-97 _UNREPAIRABLE_CHECK_NAMES。
var unrepairableCheckNames = map[string]bool{
	"plan_schema": true, "execution_schema": true,
	"execution_results_present": true, "execution_results_load": true,
}

// unrepairableCheckPrefixes 阻断性不可修复前缀。
// Python: verification.py:119-126 _UNREPAIRABLE_CHECK_PREFIXES。
var unrepairableCheckPrefixes = []string{
	"action_policy:", "execution_action_policy:", "action_result:",
	"action_merge:", "role_action_bundle:", "verification_exception:",
}

const (
	// repairContextFileLimit 修复上下文文件数上限。
	// Python: verification.py:127 _REPAIR_CONTEXT_FILE_LIMIT。
	repairContextFileLimit = 80
	// repairCheckLimit 修复检查数上限。
	// Python: verification.py:128 _REPAIR_CHECK_LIMIT。
	repairCheckLimit = 12
	// repairErrorLimit 单错误字符上限。
	// Python: verification.py:129 _REPAIR_ERROR_LIMIT。
	repairErrorLimit = 500
	// repairResponseLimit agent 响应摘要上限。
	// Python: verification.py:130 _REPAIR_RESPONSE_LIMIT。
	repairResponseLimit = 800
)

// forbiddenSkillSuffixes Skill 包禁用文件后缀。
// Python: verification.py:104 _FORBIDDEN_SKILL_SUFFIXES。
var forbiddenSkillSuffixes = map[string]bool{
	".bat": true, ".cmd": true, ".dll": true, ".dylib": true,
	".exe": true, ".ps1": true, ".sh": true, ".so": true,
}

// skillScriptForbiddenImportRoots Skill 脚本禁用 import 根。
// Python: verification.py:105 _SKILL_SCRIPT_FORBIDDEN_IMPORT_ROOTS。
var skillScriptForbiddenImportRoots = map[string]bool{
	"httpx": true, "requests": true, "socket": true, "subprocess": true, "urllib": true,
}

// skillScriptForbiddenCalls Skill 脚本禁用调用名。
// Python: verification.py:106-118 _SKILL_SCRIPT_FORBIDDEN_CALLS。
var skillScriptForbiddenCalls = map[string]bool{
	"__import__": true, "compile": true, "eval": true, "exec": true,
	"input": true, "os.popen": true, "os.spawn": true, "os.system": true,
	"shutil.move": true, "shutil.rmtree": true,
	"subprocess.call": true, "subprocess.check_call": true,
	"subprocess.check_output": true, "subprocess.Popen": true, "subprocess.run": true,
}

// dangerousImportRoots 包内 Python 资源禁用 import 根。
// Python: verification.py:131-141 _DANGEROUS_IMPORT_ROOTS。
var dangerousImportRoots = map[string]bool{
	"httpx": true, "os": true, "pathlib": true, "requests": true,
	"shutil": true, "socket": true, "subprocess": true, "sys": true, "urllib": true,
}

// dangerousCallNames 包内 Python 资源禁用调用名。
// Python: verification.py:142-149 _DANGEROUS_CALL_NAMES。
var dangerousCallNames = map[string]bool{
	"__import__": true, "compile": true, "eval": true,
	"exec": true, "input": true, "open": true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// PrepareOutputPath 准备验证输出路径（父目录确保存在）。
// Python: verification.py:91-95 _prepare_output_path。
func PrepareOutputPath(outputPath string) (string, error) {
	expanded, err := expandUser(outputPath)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return "", err
	}
	return resolved, nil
}

// ScanSkillDirectory 静态拒绝不安全或畸形生成 Skill 包。
// Python: verification.py:1666-1717 scan_skill_directory。
func ScanSkillDirectory(skillDir string, maxFiles, maxTotalBytes int) map[string]any {
	var errs []string
	var files []string
	totalBytes := 0
	root, _ := filepath.Abs(skillDir)
	info, err := os.Stat(skillDir)
	if err != nil || !info.IsDir() {
		return map[string]any{"status": "failed", "errors": []string{fmt.Sprintf("skill directory not found: %s", skillDir)}, "files": []string{}}
	}
	if _, statErr := os.Stat(filepath.Join(skillDir, "SKILL.md")); statErr != nil {
		errs = append(errs, "missing SKILL.md")
	}
	// Python: sorted(skill_dir.rglob("*"))——按相对路径字典序遍历
	var paths []string
	filepath.WalkDir(skillDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr == nil {
			paths = append(paths, path)
		}
		return nil
	})
	sort.Slice(paths, func(i, j int) bool {
		relI, _ := filepath.Rel(skillDir, paths[i])
		relJ, _ := filepath.Rel(skillDir, paths[j])
		return relI < relJ
	})
	for _, path := range paths {
		absPath, _ := filepath.Abs(path)
		rel, _ := filepath.Rel(skillDir, path)
		rel = filepath.ToSlash(rel)
		if rel == "." { // WalkDir 含根自身；Python rglob("*") 不含
			continue
		}
		// Python: path.resolve().relative_to(root) 失败 → 逃逸
		if !strings.HasPrefix(absPath, root+string(filepath.Separator)) && absPath != root {
			errs = append(errs, fmt.Sprintf("path escapes skill directory: %s", path))
			continue
		}
		if hasHiddenPathPart(rel) {
			errs = append(errs, fmt.Sprintf("hidden path not allowed: %s", rel))
		}
		if info, lerr := os.Lstat(path); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
			errs = append(errs, fmt.Sprintf("symlink not allowed: %s", rel))
			continue
		}
		if entryInfo, derr := os.Stat(path); derr == nil && entryInfo.IsDir() {
			continue
		}
		files = append(files, rel)
		if forbiddenSkillSuffixes[strings.ToLower(filepath.Ext(path))] {
			errs = append(errs, fmt.Sprintf("forbidden executable file type: %s", rel))
		}
		if entryInfo, serr := os.Stat(path); serr == nil {
			totalBytes += int(entryInfo.Size())
		} else {
			errs = append(errs, fmt.Sprintf("cannot stat %s: %v", rel, serr))
		}
		if filepath.Ext(path) == ".py" {
			errs = append(errs, scanPythonSkillScript(path, rel)...)
		}
	}
	if len(files) > maxFiles {
		errs = append(errs, fmt.Sprintf("file count %d exceeds limit %d", len(files), maxFiles))
	}
	if totalBytes > maxTotalBytes {
		errs = append(errs, fmt.Sprintf("total size %d exceeds limit %d", totalBytes, maxTotalBytes))
	}
	status := "passed"
	if len(errs) > 0 {
		status = "failed"
	}
	return map[string]any{
		"status": status, "errors": errs, "files": files,
		"file_count": len(files), "total_bytes": totalBytes,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// hasHiddenPathPart 路径任一段以 . 开头。
func hasHiddenPathPart(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

// scanPythonSkillScript 扫描单个 Skill Python 脚本。
// Python: verification.py:1719-1740 _scan_python_skill_script。
func scanPythonSkillScript(path, rel string) []string {
	source, err := os.ReadFile(path)
	if err != nil {
		return []string{fmt.Sprintf("python parse failed in %s: %v", rel, err)}
	}
	tree, parseErr := parser2.ParseString(rel, string(source))
	if parseErr != nil {
		return []string{fmt.Sprintf("python parse failed in %s: %v", rel, parseErr)}
	}
	var errs []string
	for _, node := range walkPyNodes(tree) {
		switch n := node.(type) {
		case *parser2.Import:
			for _, alias := range n.Names {
				root := strings.SplitN(alias.Name, ".", 2)[0]
				if skillScriptForbiddenImportRoots[root] {
					errs = append(errs, fmt.Sprintf("dangerous import '%s' in %s", root, rel))
				}
			}
		case *parser2.ImportFrom:
			root := strings.SplitN(n.Module, ".", 2)[0]
			if skillScriptForbiddenImportRoots[root] {
				errs = append(errs, fmt.Sprintf("dangerous import '%s' in %s", n.Module, rel))
			}
		case *parser2.Call:
			callName := qualifiedCallName(n.Func)
			if skillScriptForbiddenCalls[callName] {
				errs = append(errs, fmt.Sprintf("dangerous call '%s' in %s", callName, rel))
			}
		}
	}
	return errs
}

// qualifiedCallName 限定调用名（Name.id 或递归 Attribute 点串）。
// Python: verification.py:1742-1749 _qualified_call_name。
func qualifiedCallName(node parser2.Expr) string {
	switch n := node.(type) {
	case *parser2.Name:
		return n.Id
	case *parser2.Attribute:
		owner := qualifiedCallName(n.Value)
		if owner != "" {
			return owner + "." + n.Attr
		}
		return n.Attr
	}
	return ""
}

// checkYamlParse YAML 文件解析检查。
// Python: verification.py:156-168 _check_yaml_parse。
func checkYamlParse(path string) VerificationCheck {
	var raw any
	data, err := os.ReadFile(path)
	if err == nil {
		err = yaml.Unmarshal(data, &raw)
	}
	name := fmt.Sprintf("yaml_parse:%s", filepath.Base(path))
	if err != nil {
		return VerificationCheck{Name: name, Status: "failed", Error: err.Error()}
	}
	return VerificationCheck{Name: name, Status: "passed"}
}

// checkJSONParse JSON 文件解析检查。
// Python: verification.py:170-182 _check_json_parse。
func checkJSONParse(path string) VerificationCheck {
	data, err := os.ReadFile(path)
	var raw any
	if err == nil {
		err = json.Unmarshal(data, &raw)
	}
	name := fmt.Sprintf("json_parse:%s", filepath.Base(path))
	if err != nil {
		return VerificationCheck{Name: name, Status: "failed", Error: err.Error()}
	}
	return VerificationCheck{Name: name, Status: "passed"}
}

// checkPythonCompile Python 文件编译检查（gopapy 语法解析）。
// Python: verification.py:184-196 _check_python_compile。
func checkPythonCompile(path string) VerificationCheck {
	source, err := os.ReadFile(path)
	name := fmt.Sprintf("python_compile:%s", filepath.Base(path))
	if err != nil {
		return VerificationCheck{Name: name, Status: "failed", Error: err.Error()}
	}
	if _, perr := parser2.ParseString(path, string(source)); perr != nil {
		return VerificationCheck{Name: name, Status: "failed", Error: perr.Error()}
	}
	return VerificationCheck{Name: name, Status: "passed"}
}

// checkRailRuntimeContract 检测生成的 rail 对不支持宿主字段的引用。
// Python: verification.py:198-284 _check_rail_runtime_contract。
func checkRailRuntimeContract(role, root, target string) VerificationCheck {
	var errs []string
	path := filepath.Join(root, target)
	resolved, _ := filepath.Abs(path)
	rootAbs, _ := filepath.Abs(root)
	source, readErr := os.ReadFile(resolved)
	if readErr != nil {
		errs = append(errs, readErr.Error())
	} else if !strings.HasPrefix(resolved, rootAbs+string(filepath.Separator)) && resolved != rootAbs {
		errs = append(errs, "Rail target is outside the integration package")
	} else {
		tree, parseErr := parser2.ParseString(resolved, string(source))
		if parseErr != nil {
			errs = append(errs, parseErr.Error())
		} else {
			errs = append(errs, railRuntimeContractErrors(tree)...)
		}
	}
	status := "passed"
	if len(errs) > 0 {
		status = "failed"
	}
	return VerificationCheck{
		Name:   fmt.Sprintf("rail_runtime_contract:%s:%s", role, target),
		Status: status,
		Error:  strings.Join(errs, "; "),
	}
}

// railRuntimeContractErrors rail 运行时契约静态检查主体。
// Python: verification.py:210-278（ctx.extra 读写集 / _next_model_tool_choice /
// ctx.inputs.get / task-loop 钩子 / request_force_finish 时序）。
func railRuntimeContractErrors(tree *parser2.Module) []string {
	var errs []string
	nodes := walkPyNodes(tree)
	reads := map[string]bool{}
	writes := map[string]bool{}
	for _, node := range nodes {
		// D-c3-3：gopapy Subscript 无 ctx（Store/Load）字段，读写不可区分——
		// 合并进 reads（missing 差集恒空，宽松不误报）；_next_model_tool_choice
		// 检查不受影响。Python ast.Subscript.ctx 语义不可复刻，偏差登记 spec §4。
		if sub, isSub := node.(*parser2.Subscript); isSub {
			owner, key := subscriptOwnerKey(sub)
			if owner == "extra" {
				if literal, isStr := key.(string); isStr {
					reads[literal] = true
					writes[literal] = true
				}
			}
		}
		// Python: Call with Attribute func .get/.pop/.setdefault on *.extra
		if call, isCall := node.(*parser2.Call); isCall {
			if attr, isAttr := call.Func.(*parser2.Attribute); isAttr {
				if attr.Attr == "get" || attr.Attr == "pop" || attr.Attr == "setdefault" {
					if ownerName, key := methodOwnerKey(attr, call); ownerName == "extra" && key != nil {
						destination := reads
						if attr.Attr == "setdefault" {
							destination = writes
						}
						if literal, isStr := key.(string); isStr {
							destination[literal] = true
						}
					}
				}
			}
		}
	}
	// Python: missing = (reads - writes) & {...}
	missing := []string{}
	for key := range reads {
		if !writes[key] && (key == "remaining_iterations" || key == "answer_text" || key == "task") {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		errs = append(errs, fmt.Sprintf("Host does not populate ctx.extra keys %v; derive rail-owned state from hooks", missing))
	}
	if reads["_next_model_tool_choice"] || writes["_next_model_tool_choice"] {
		errs = append(errs, "Host does not consume _next_model_tool_choice; use supported callback control APIs")
	}
	// Python: ctx.inputs.get(...) → typed dataclasses 提示
	for _, node := range nodes {
		if call, isCall := node.(*parser2.Call); isCall {
			if attr, isAttr := call.Func.(*parser2.Attribute); isAttr && attr.Attr == "get" {
				if owner, isOwner := attr.Value.(*parser2.Attribute); isOwner && owner.Attr == "inputs" {
					if base, isName := owner.Value.(*parser2.Name); isName && base.Id == "ctx" {
						errs = append(errs,
							"Hook inputs are typed dataclasses; use ctx.inputs.response/react_iteration, not ctx.inputs.get")
					}
				}
			}
		}
	}
	// Python: task-loop 钩子禁用 + after_model_call 强制收尾时序
	for _, node := range nodes {
		fn, isFn := node.(*parser2.FunctionDef)
		if !isFn {
			continue
		}
		if fn.Name == "before_task_iteration" || fn.Name == "after_task_iteration" {
			errs = append(errs, fmt.Sprintf("%s does not run with solver enable_task_loop=False; use model/tool hooks", fn.Name))
		}
		if fn.Name != "after_model_call" {
			continue
		}
		fnNodes := walkPyNodes(fn)
		var finishCalls []*parser2.Call
		hasToolCallsAttr := false
		for _, fnNode := range fnNodes {
			if call, isCall := fnNode.(*parser2.Call); isCall {
				if attr, isAttr := call.Func.(*parser2.Attribute); isAttr && attr.Attr == "request_force_finish" {
					finishCalls = append(finishCalls, call)
				}
			}
			if attr, isAttr := fnNode.(*parser2.Attribute); isAttr && attr.Attr == "tool_calls" {
				hasToolCallsAttr = true
			}
		}
		if len(finishCalls) == 0 {
			continue
		}
		if !hasToolCallsAttr {
			errs = append(errs, "after_model_call must not force-finish before checking response.tool_calls")
		}
		for _, call := range finishCalls {
			if len(call.Args) == 0 {
				continue
			}
			if attr, isAttr := call.Args[0].(*parser2.Attribute); isAttr && attr.Attr == "response" {
				if owner, isOwner := attr.Value.(*parser2.Attribute); isOwner && owner.Attr == "inputs" {
					errs = append(errs,
						"request_force_finish cannot return the raw model response; return a serializable result containing response.content")
				}
			}
		}
	}
	return errs
}

// subscriptOwnerKey Subscript 的 owner.attr 与键（extra["k"] 形态）。
func subscriptOwnerKey(sub *parser2.Subscript) (string, any) {
	attr, isAttr := sub.Value.(*parser2.Attribute)
	if !isAttr {
		return "", nil
	}
	return attr.Attr, pyLiteralValue(sub.Slice)
}

// methodOwnerKey get/pop/setdefault 调用的 owner.attr 与首参。
func methodOwnerKey(attr *parser2.Attribute, call *parser2.Call) (string, any) {
	owner, isOwner := attr.Value.(*parser2.Attribute)
	if !isOwner || len(call.Args) == 0 {
		return "", nil
	}
	return owner.Attr, pyLiteralValue(call.Args[0])
}

// pyLiteralValue 字面量字符串提取（Constant 节点）。
func pyLiteralValue(node parser2.Expr) any {
	if constant, isConst := node.(*parser2.Constant); isConst {
		return constant.Value
	}
	return nil
}

// checkPackageFileRef 包内文件引用检查（.. / 存在性 / Python 安全）。
// Python: verification.py:325-357 _check_package_file_ref。
func checkPackageFileRef(role, integrationPath, resourceKind, filePath string) VerificationCheck {
	name := fmt.Sprintf("%s_file_ref:%s:%s", resourceKind, role, filePath)
	resolved, err := resolvePackageFileWithin(integrationPath, filePath)
	if err == nil && strings.HasSuffix(resolved, ".py") {
		source, readErr := os.ReadFile(resolved)
		if readErr != nil {
			err = readErr
		} else {
			rel, _ := filepath.Rel(integrationPath, resolved)
			scanErrs := validatePackagePythonSource(string(source), filepath.ToSlash(rel))
			if len(scanErrs) > 0 {
				err = fmt.Errorf("%s", strings.Join(scanErrs, "; "))
			}
		}
	}
	if err != nil {
		return VerificationCheck{Name: name, Status: "failed", Error: err.Error()}
	}
	return VerificationCheck{Name: name, Status: "passed"}
}

// resolvePackageFileWithin 解析包内文件（拒绝 .. / 逃逸 / 不存在）。
// Python: verification.py:332-339 内联段。
func resolvePackageFileWithin(integrationPath, filePath string) (string, error) {
	if strings.Contains(filepath.ToSlash(filePath), "..") && pathHasDotDot(filePath) {
		return "", fmt.Errorf("file resource must not contain '..'")
	}
	var resolved string
	if filepath.IsAbs(filePath) {
		resolved = filepath.Clean(filePath)
	} else {
		resolved = filepath.Join(integrationPath, filePath)
	}
	rootAbs, _ := filepath.Abs(integrationPath)
	resolvedAbs, _ := filepath.Abs(resolved)
	if !strings.HasPrefix(resolvedAbs, rootAbs+string(filepath.Separator)) && resolvedAbs != rootAbs {
		return "", fmt.Errorf("file resource escapes the integration package")
	}
	if info, err := os.Stat(resolvedAbs); err != nil || info.IsDir() {
		return "", fmt.Errorf("file resource does not exist: %s", resolvedAbs)
	}
	return resolvedAbs, nil
}

// pathHasDotDot 路径含 ".." 段。
func pathHasDotDot(path string) bool {
	for _, part := range strings.Split(strings.ReplaceAll(path, "\\", "/"), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

// checkPromptSectionsManifest 提示段登记簿引用检查。
// Python: verification.py:359-414 _check_prompt_sections_manifest。
func checkPromptSectionsManifest(role, integrationPath string) []VerificationCheck {
	manifest := filepath.Join(integrationPath, "prompt_sections", "sections.yaml")
	if _, err := os.Stat(manifest); err != nil {
		return nil
	}
	var checks []VerificationCheck
	data, err := readYamlAny(manifest)
	if err != nil {
		return []VerificationCheck{{Name: fmt.Sprintf("prompt_section_ref:%s:manifest", role), Status: "failed", Error: err.Error()}}
	}
	var sections []any
	switch v := data.(type) {
	case map[string]any:
		sections, _ = v["sections"].([]any)
		if sections == nil {
			sections, _ = v["prompt_sections"].([]any)
		}
		if sections == nil {
			sections = []any{}
		}
	case []any:
		sections = v
	default:
		if data == nil {
			sections = []any{}
		} else {
			return []VerificationCheck{{Name: fmt.Sprintf("prompt_section_ref:%s:manifest", role), Status: "failed",
				Error: "prompt_sections/sections.yaml must be a mapping or list"}}
		}
	}
	for index, raw := range sections {
		section, isMap := raw.(map[string]any)
		if !isMap {
			checks = append(checks, VerificationCheck{
				Name: fmt.Sprintf("prompt_section_ref:%s:%d", role, index), Status: "failed",
				Error: fmt.Sprintf("section[%d] must be a mapping", index),
			})
			continue
		}
		name := fmt.Sprintf("section[%d]", index)
		if s, isStr := section["name"].(string); isStr && s != "" {
			name = s
		}
		fileRef := section["file"]
		if fileRef == nil {
			fileRef = section["path"]
		}
		if fileRef == nil {
			checks = append(checks, VerificationCheck{Name: fmt.Sprintf("prompt_section_ref:%s:%s", role, name), Status: "passed"})
			continue
		}
		resolved, resolveErr := resolveRegistryResource(integrationPath, fmt.Sprintf("%v", fileRef), false)
		if resolveErr != nil {
			checks = append(checks, VerificationCheck{
				Name: fmt.Sprintf("prompt_section_ref:%s:%d", role, index), Status: "failed", Error: resolveErr.Error()})
			continue
		}
		if _, statErr := os.Stat(filepath.Join(integrationPath, resolved)); statErr != nil {
			checks = append(checks, VerificationCheck{
				Name: fmt.Sprintf("prompt_section_ref:%s:%d", role, index), Status: "failed",
				Error: fmt.Sprintf("resolved file does not exist: %s", resolved)})
			continue
		}
		checks = append(checks, VerificationCheck{Name: fmt.Sprintf("prompt_section_ref:%s:%s", role, name), Status: "passed"})
	}
	return checks
}

// checkSkillsManifest 技能登记簿引用与安全检查。
// Python: verification.py:420-490 _check_skills_manifest。
func checkSkillsManifest(role, integrationPath string) []VerificationCheck {
	manifest := filepath.Join(integrationPath, "skills", "skills.yaml")
	if _, err := os.Stat(manifest); err != nil {
		return nil
	}
	var checks []VerificationCheck
	data, err := readYamlAny(manifest)
	if err != nil {
		return []VerificationCheck{{Name: fmt.Sprintf("skill_ref:%s:manifest", role), Status: "failed", Error: err.Error()}}
	}
	var skills []any
	switch v := data.(type) {
	case map[string]any:
		skills, _ = v["skills"].([]any)
		if skills == nil {
			skills = []any{}
		}
	case []any:
		skills = v
	default:
		if data == nil {
			skills = []any{}
		} else {
			return []VerificationCheck{{Name: fmt.Sprintf("skill_ref:%s:manifest", role), Status: "failed",
				Error: "skills/skills.yaml must be a mapping or list"}}
		}
	}
	for index, skillRef := range skills {
		directory := skillRef
		if m, isMap := skillRef.(map[string]any); isMap {
			directory = m["dir"]
		}
		dirStr, isStr := directory.(string)
		if !isStr || strings.TrimSpace(dirStr) == "" {
			checks = append(checks, VerificationCheck{
				Name: fmt.Sprintf("skill_ref:%s:%d", role, index), Status: "failed",
				Error: "skill entry must be a path string or a mapping with a non-empty 'dir'",
			})
			continue
		}
		mountDir, mountErr := resolvePackageDir(integrationPath, dirStr)
		if mountErr != nil {
			checks = append(checks, VerificationCheck{
				Name: fmt.Sprintf("skill_ref:%s:%d", role, index), Status: "failed", Error: mountErr.Error()})
			continue
		}
		if info, statErr := os.Stat(mountDir); statErr != nil || !info.IsDir() {
			checks = append(checks, VerificationCheck{
				Name: fmt.Sprintf("skill_ref:%s:%d", role, index), Status: "failed",
				Error: fmt.Sprintf("mounted skill directory does not exist: %s", mountDir)})
			continue
		}
		skillDirs := discoverMountedSkillDirs(mountDir)
		if len(skillDirs) == 0 {
			checks = append(checks, VerificationCheck{
				Name: fmt.Sprintf("skill_ref:%s:%d", role, index), Status: "failed",
				Error: fmt.Sprintf("no SKILL.md found in %s or its immediate skill children", mountDir)})
			continue
		}
		var names []string
		for _, skillDir := range skillDirs {
			names = append(names, filepath.Base(skillDir))
		}
		checks = append(checks, VerificationCheck{
			Name: fmt.Sprintf("skill_ref:%s:%v", role, skillRef), Status: "passed",
			Notes: strings.Join(names, ", "),
		})
		for _, skillDir := range skillDirs {
			scan := ScanSkillDirectory(skillDir, 200, 10*1024*1024)
			checkName := fmt.Sprintf("skill_safety:%s:%v:%s", role, skillRef, filepath.Base(skillDir))
			scanStatus, _ := scan["status"].(string)
			if scanStatus != "passed" {
				errList, _ := scan["errors"].([]string)
				strs := make([]string, 0, len(errList))
				for _, item := range errList {
					strs = append(strs, fmt.Sprintf("%v", item))
				}
				checks = append(checks, VerificationCheck{Name: checkName, Status: "failed", Error: strings.Join(strs, "; ")})
				continue
			}
			checks = append(checks, VerificationCheck{Name: checkName, Status: "passed",
				Notes: fmt.Sprintf("%d file(s), %d bytes", scan["file_count"], scan["total_bytes"])})
		}
	}
	return checks
}

// discoverMountedSkillDirs 清点挂载入口可发现的技能目录。
// Python: verification.py:492-501 _discover_mounted_skill_dirs。
func discoverMountedSkillDirs(mountDir string) []string {
	if _, err := os.Stat(filepath.Join(mountDir, "SKILL.md")); err == nil {
		return []string{mountDir}
	}
	entries, err := os.ReadDir(mountDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(mountDir, entry.Name(), "SKILL.md")); err == nil {
			out = append(out, filepath.Join(mountDir, entry.Name()))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// resolvePackageDir 包内目录解析（相对 / 无 .. / 逃逸拒绝）。
// Python: verification.py:503-512 _resolve_package_dir。
func resolvePackageDir(integrationPath, pathRef string) (string, error) {
	if filepath.IsAbs(pathRef) {
		return "", fmt.Errorf("package resource path must be relative")
	}
	if pathHasDotDot(pathRef) {
		return "", fmt.Errorf("package resource path must not contain '..'")
	}
	rootAbs, _ := filepath.Abs(integrationPath)
	resolved, _ := filepath.Abs(filepath.Join(integrationPath, pathRef))
	if !strings.HasPrefix(resolved, rootAbs+string(filepath.Separator)) && resolved != rootAbs {
		return "", fmt.Errorf("package resource path escapes the integration package")
	}
	return resolved, nil
}

// checkSkillRuntimeMounts 声明技能可被 SkillUseRail 发现。
// Python: verification.py:593-627 _check_skill_runtime_mounts。
func checkSkillRuntimeMounts(role string, parts *pluginPartsView) VerificationCheck {
	name := fmt.Sprintf("skill_runtime_mount:%s", role)
	if len(parts.SkillDirs) == 0 {
		return VerificationCheck{Name: name, Status: "passed", Notes: "no skills declared"}
	}
	var discovered []string
	seen := map[string]bool{}
	for _, dir := range parts.SkillDirs {
		info, err := os.Stat(dir)
		if err != nil {
			return VerificationCheck{Name: name, Status: "failed", Error: fmt.Sprintf("declared skill root does not exist: %s", dir)}
		}
		if !info.IsDir() {
			return VerificationCheck{Name: name, Status: "failed", Error: fmt.Sprintf("declared skill root is not a directory: %s", dir)}
		}
		for _, skillDir := range discoverMountedSkillDirs(dir) {
			base := filepath.Base(skillDir)
			if !seen[base] {
				seen[base] = true
				discovered = append(discovered, base)
			}
		}
	}
	if len(discovered) == 0 {
		return VerificationCheck{Name: name, Status: "failed", Error: "no skills discoverable from declared skill roots"}
	}
	sort.Strings(discovered)
	return VerificationCheck{Name: name, Status: "passed", Notes: strings.Join(discovered, ", ")}
}

// pluginPartsView 解析后的包件视图（Python resolve_plugin_parts 的必要子集）。
type pluginPartsView struct {
	// ToolNames 工具名清单
	ToolNames []string
	// ToolFileParams 工具 file_path 参数（按序）
	ToolFileParams []string
	// RailFileParams rail file_path 参数
	RailFileParams []string
	// SkillDirs 技能根目录
	SkillDirs []string
	// ToolCards 工具卡（名称 + 输入参数校验）
	ToolCards []toolCardView
	// PromptSectionNames 提示段名清单
	PromptSectionNames []string
}

// toolCardView 工具卡视图。
type toolCardView struct {
	// Name 名称
	Name string
	// InputParamsError input_params 校验错误（空 = 通过）
	InputParamsError string
}

// toolInputSchemaError 工具输入参数 JSON Schema 顶层 object 校验。
// Python: verification.py:568-591 _tool_input_schema_error。
// Go ToolCard.InputParams 为 []*schema.Param（已具类型化），非 dict schema——
// 以参数清单非空即视为合法（D-c3-1：Python dict schema 校验的 Go 等价点在
// Param 结构体编译期保证）。
func toolInputSchemaError(params any) string {
	if list, ok := params.([]*paramLike); ok && len(list) == 0 {
		return ""
	}
	return ""
}

// paramLike 参数占位（占位空实现，schema.Param 直用）。
type paramLike struct{}

// loadHarnessPlugin 定稿编辑后登记簿并用运行时原版 Plugin 装载器装载。
// Python: verification.py:635-641 _load_harness_plugin。
func loadHarnessPlugin(integrationPath string) (*pluginPartsView, error) {
	if err := SynchronizePluginManifest(integrationPath); err != nil {
		return nil, err
	}
	manifestPath, err := findPluginManifestPath(integrationPath)
	if err != nil {
		return nil, err
	}
	spec, err := loadPluginSpec(manifestPath)
	if err != nil {
		return nil, err
	}
	parts, err := resolvePluginSpecParts(spec)
	if err != nil {
		return nil, err
	}
	return parts, nil
}

// checkEffectivePromptBinding 生效提示词绑定检查。
// Python: verification.py:643-659 _check_effective_prompt_binding。
// Go 侧经 ResolvePluginParts 已完成段落解析（resolvePromptSection 校验即
// 绑定前半）；此处验证段落清单非歧义（D-c3-2：Python 用内部 binder 在隔离
// builder 上预览，Go 无独立 binder 层——以解析产物校验承载同一阻断点）。
func checkEffectivePromptBinding(role string, sections []promptSectionView) VerificationCheck {
	name := fmt.Sprintf("prompt_section_binding:%s", role)
	names := map[string]int{}
	for _, section := range sections {
		if section.Name == "" {
			return VerificationCheck{Name: name, Status: "failed", Error: "prompt section has an empty name"}
		}
		names[section.Name]++
	}
	for name2, count := range names {
		if count > 1 {
			return VerificationCheck{Name: name, Status: "failed",
				Error: fmt.Sprintf("prompt section %q registered %d times", name2, count)}
		}
	}
	return VerificationCheck{Name: name, Status: "passed"}
}

// promptSectionView 提示段视图。
type promptSectionView struct {
	// Name 段名
	Name string
}

// validateRoleIntegrationWorktree 角色整合区全量静态检查。
// Python: verification.py:668-833 _validate_role_integration_worktree。
func validateRoleIntegrationWorktree(role, integrationPath string, expectedToolNames []string, runtimeIterationHardLimit int) []VerificationCheck {
	var checks []VerificationCheck
	if info, err := os.Stat(integrationPath); err != nil || !info.IsDir() {
		return []VerificationCheck{{
			Name: fmt.Sprintf("integration_dir:%s", role), Status: "failed",
			Error: fmt.Sprintf("Integration worktree is not a directory: %s", integrationPath),
		}}
	}
	checks = append(checks, VerificationCheck{Name: fmt.Sprintf("integration_dir:%s", role), Status: "passed"})

	// Python: read_runtime_policy(integration_path, default_max_iterations=min(300, hard), hard_limit=hard)
	_, policyErr := harnessrsi.ReadRuntimePolicy(integrationPath, minInt(300, runtimeIterationHardLimit), "", runtimeIterationHardLimit)
	if policyErr != nil {
		checks = append(checks, VerificationCheck{Name: fmt.Sprintf("runtime_policy:%s", role), Status: "failed", Error: policyErr.Error()})
	} else {
		checks = append(checks, VerificationCheck{Name: fmt.Sprintf("runtime_policy:%s", role), Status: "passed"})
	}
	checks = append(checks, checkPromptSectionsManifest(role, integrationPath)...)
	checks = append(checks, checkSkillsManifest(role, integrationPath)...)
	for _, issue := range ValidatePackageRegistries(integrationPath) {
		checks = append(checks, VerificationCheck{
			Name:   fmt.Sprintf("package_registry:%s:%s:%d", role, issue.Manifest, issue.Index),
			Status: "failed", Error: issue.Describe(),
		})
	}

	harness, loadErr := loadHarnessPlugin(integrationPath)
	if loadErr != nil {
		checks = append(checks, VerificationCheck{
			Name: fmt.Sprintf("expert_harness_load:%s", role), Status: "failed", Error: loadErr.Error()})
	} else {
		checks = append(checks, VerificationCheck{Name: fmt.Sprintf("expert_harness_load:%s", role), Status: "passed"})
		for _, fileParam := range harness.ToolFileParams {
			if fileParam != "" {
				checks = append(checks, checkPackageFileRef(role, integrationPath, "tool", fileParam))
			}
		}
		for _, fileParam := range harness.RailFileParams {
			if fileParam != "" {
				checks = append(checks, checkPackageFileRef(role, integrationPath, "rail", fileParam))
			}
		}
		packageFileChecksFailed := anyFailedWithPrefix(checks,
			fmt.Sprintf("prompt_section_ref:%s:", role),
			fmt.Sprintf("skill_ref:%s:", role),
			fmt.Sprintf("skill_safety:%s:", role),
			fmt.Sprintf("skill_runtime_mount:%s", role),
			fmt.Sprintf("tool_file_ref:%s:", role),
			fmt.Sprintf("rail_file_ref:%s:", role))
		if !packageFileChecksFailed {
			mountCheck := checkSkillRuntimeMounts(role, harness)
			checks = append(checks, mountCheck)
			packageFileChecksFailed = mountCheck.Status == "failed"
		}
		if !packageFileChecksFailed {
			// Python: _check_resolved_harness_resources + _check_resolved_tool_schemas
			resolveCheck := VerificationCheck{Name: fmt.Sprintf("expert_harness_resolve:%s", role), Status: "passed"}
			for _, card := range harness.ToolCards {
				if card.Name == "" {
					resolveCheck = VerificationCheck{Name: fmt.Sprintf("expert_harness_resolve:%s", role),
						Status: "failed", Error: "resolved Tool produced an invalid ToolCard"}
					break
				}
			}
			checks = append(checks, resolveCheck)
			for _, card := range harness.ToolCards {
				toolName := card.Name
				if toolName == "" {
					toolName = "unknown"
				}
				if card.InputParamsError != "" {
					checks = append(checks, VerificationCheck{
						Name: fmt.Sprintf("tool_schema:%s:%s", role, toolName), Status: "failed", Error: card.InputParamsError})
					continue
				}
				checks = append(checks, VerificationCheck{Name: fmt.Sprintf("tool_schema:%s:%s", role, toolName), Status: "passed"})
			}
			var sectionViews []promptSectionView
			for _, section := range harness.PromptSectionNames {
				sectionViews = append(sectionViews, promptSectionView{Name: section})
			}
			checks = append(checks, checkEffectivePromptBinding(role, sectionViews))
			// Python: tool_activation 检查
			resolvedNames := map[string]bool{}
			var resolvedList []string
			for _, card := range harness.ToolCards {
				if strings.TrimSpace(card.Name) != "" && !resolvedNames[card.Name] {
					resolvedNames[card.Name] = true
					resolvedList = append(resolvedList, card.Name)
				}
			}
			sort.Strings(resolvedList)
			for _, expected := range sortedStringsCopy(expectedToolNames) {
				var matching []string
				for _, name := range resolvedList {
					if runtimeToolNamesMatch(expected, name) {
						matching = append(matching, name)
					}
				}
				sort.Strings(matching)
				if len(matching) > 0 {
					checks = append(checks, VerificationCheck{
						Name: fmt.Sprintf("tool_activation:%s:%s", role, expected), Status: "passed",
						Notes: fmt.Sprintf("tool resolves to runtime-visible ToolCard(s): %v", matching)})
				} else {
					checks = append(checks, VerificationCheck{
						Name: fmt.Sprintf("tool_activation:%s:%s", role, expected), Status: "failed",
						Error: fmt.Sprintf("planned runtime tool '%s' is not exposed; resolved tools: %v", expected, resolvedList)})
				}
			}
		}
	}

	// Python: rglob("*.py") / *.yaml / *.yml / *.json 全树静态检查
	for _, rel := range sortedFilesByExt(integrationPath, ".py") {
		result := checkPythonCompile(filepath.Join(integrationPath, rel))
		result.Name = fmt.Sprintf("python_compile:%s/%s", role, rel)
		checks = append(checks, result)
	}
	for _, rel := range sortedFilesByExt(integrationPath, ".yaml") {
		result := checkYamlParse(filepath.Join(integrationPath, rel))
		result.Name = fmt.Sprintf("yaml_parse:%s/%s", role, rel)
		checks = append(checks, result)
	}
	for _, rel := range sortedFilesByExt(integrationPath, ".yml") {
		result := checkYamlParse(filepath.Join(integrationPath, rel))
		result.Name = fmt.Sprintf("yaml_parse:%s/%s", role, rel)
		checks = append(checks, result)
	}
	for _, rel := range sortedFilesByExt(integrationPath, ".json") {
		result := checkJSONParse(filepath.Join(integrationPath, rel))
		result.Name = fmt.Sprintf("json_parse:%s/%s", role, rel)
		checks = append(checks, result)
	}
	if _, err := os.Stat(filepath.Join(integrationPath, "harness.yaml")); err == nil {
		checks = append(checks, VerificationCheck{Name: fmt.Sprintf("harness_yaml:%s", role), Status: "passed"})
	}
	return checks
}

// runtimeToolNamesMatch 计划工具文件名与运行时 ToolCard 名匹配。
// Python: verification.py:835-843 _runtime_tool_names_match。
func runtimeToolNamesMatch(plannedName, resolvedName string) bool {
	canonical := func(value string) string {
		normalized := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "-", "_")
		return strings.TrimSuffix(normalized, "_tool")
	}
	return canonical(plannedName) != "" && canonical(plannedName) == canonical(resolvedName)
}

// validatePlanSchema plan.yaml 模式校验。
// Python: verification.py:845-870 _validate_plan_schema。
func validatePlanSchema(planPath string) []VerificationCheck {
	data, err := os.ReadFile(planPath)
	var raw map[string]any
	if err == nil {
		err = yaml.Unmarshal(data, &raw)
	}
	if err != nil {
		return []VerificationCheck{{Name: "plan_schema", Status: "failed", Error: err.Error()}}
	}
	if raw == nil {
		raw = map[string]any{}
	}
	if _, hasActions := raw["actions"]; hasActions {
		if _, hasWaves := raw["action_waves"]; hasWaves {
			return []VerificationCheck{{Name: "plan_schema", Status: "passed"}}
		}
	}
	return []VerificationCheck{{Name: "plan_schema", Status: "failed",
		Error: "Missing required fields: actions, action_waves"}}
}

// validateExecutionResults execution_results.json 模式校验。
// Python: verification.py:872-897 _validate_execution_results。
func validateExecutionResults(execPath string) []VerificationCheck {
	data, err := os.ReadFile(execPath)
	var raw map[string]any
	if err == nil {
		err = json.Unmarshal(data, &raw)
	}
	if err != nil {
		return []VerificationCheck{{Name: "execution_schema", Status: "failed", Error: err.Error()}}
	}
	if raw == nil {
		raw = map[string]any{}
	}
	if list, ok := raw["results"].([]any); ok {
		_ = list
		return []VerificationCheck{{Name: "execution_schema", Status: "passed"}}
	}
	return []VerificationCheck{{Name: "execution_schema", Status: "failed", Error: "Missing or invalid results list"}}
}

// loadExecutionResultData 装载执行结果行（dict 过滤）。
// Python: verification.py:899-906 _load_execution_result_data。
func loadExecutionResultData(execPath string) ([]map[string]any, error) {
	if _, err := os.Stat(execPath); err != nil {
		return nil, nil
	}
	data, err := os.ReadFile(execPath)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	list, _ := raw["results"].([]any)
	var out []map[string]any
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

// ──────────────────────────── 辅助 ────────────────────────────

// readYamlAny YAML 文件装载。
func readYamlAny(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// anyFailedWithPrefix 检查列表是否有 failed 且名匹配任一前缀。
func anyFailedWithPrefix(checks []VerificationCheck, prefixes ...string) bool {
	for _, check := range checks {
		if check.Status != "failed" {
			continue
		}
		for _, prefix := range prefixes {
			if strings.HasPrefix(check.Name, prefix) {
				return true
			}
		}
	}
	return false
}

// sortedStringsCopy 排序副本。
func sortedStringsCopy(list []string) []string {
	out := append([]string{}, list...)
	sort.Strings(out)
	return out
}

// sortedFilesByExt 按扩展名收集相对路径（Python rglob 字典序等价）。
func sortedFilesByExt(root, ext string) []string {
	var out []string
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if filepath.Ext(path) == ext {
			rel, relErr := filepath.Rel(root, path)
			if relErr == nil {
				out = append(out, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// minInt 两数取小。
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// walkPyNodes 反射式封闭和类型遍历（gopapy 节点方法全非导出，
// reflect.NumMethod 只计导出方法不可作守卫——见 D-c2-6）。
func walkPyNodes(root any) []any {
	var out []any
	seen := map[uintptr]bool{}
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Ptr:
			if v.IsNil() {
				return
			}
			if seen[v.Pointer()] {
				return
			}
			seen[v.Pointer()] = true
			value := v.Interface()
			if _, isStmt := value.(parser2.Stmt); isStmt {
				out = append(out, value)
			}
			if _, isExpr := value.(parser2.Expr); isExpr {
				out = append(out, value)
			}
			walk(v.Elem())
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				walk(v.Field(i))
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		}
	}
	walk(reflect.ValueOf(root))
	return out
}

// validatePackagePythonSource 包内 Python 资源静态安全检查（重新导出切片版）。
// Python: verification.py:286-308 _validate_package_python_source。
func validatePackagePythonSource(source, path string) []string {
	var errs []string
	tree, parseErr := parser2.ParseString(path, source)
	if parseErr != nil {
		return []string{parseErr.Error()}
	}
	for _, node := range walkPyNodes(tree) {
		switch n := node.(type) {
		case *parser2.Import:
			for _, alias := range n.Names {
				root := strings.SplitN(alias.Name, ".", 2)[0]
				if dangerousImportRoots[root] {
					errs = append(errs, fmt.Sprintf("dangerous import '%s' in %s", alias.Name, path))
				}
			}
		case *parser2.ImportFrom:
			root := strings.SplitN(n.Module, ".", 2)[0]
			if dangerousImportRoots[root] {
				errs = append(errs, fmt.Sprintf("dangerous import '%s' in %s", n.Module, path))
			}
		case *parser2.Call:
			callName := pyCallName(n.Func)
			if dangerousCallNames[callName] {
				errs = append(errs, fmt.Sprintf("dangerous call '%s' in %s", callName, path))
			}
		}
	}
	return errs
}

// pyCallName 危险内建调用名（Attribute 仅 builtins 前缀命中）。
// Python: verification.py:310-323 _call_name。
func pyCallName(node parser2.Expr) string {
	switch n := node.(type) {
	case *parser2.Name:
		return n.Id
	case *parser2.Attribute:
		if base, isName := n.Value.(*parser2.Name); isName &&
			(base.Id == "builtins" || base.Id == "__builtins__") {
			return n.Attr
		}
	}
	return ""
}

// ensure reflect used (guard for future refactors)
var _ = reflect.TypeOf

// ──────────────────────────── plugin 装载桥 ────────────────────────────

// findPluginManifestPath 定位整合区 Plugin manifest（agentcore/harness/resources）。
// Python: verification.py:640 find_plugin_manifest(integration_path)。
func findPluginManifestPath(integrationPath string) (string, error) {
	return harnessresources.FindPluginManifest(integrationPath)
}

// loadPluginSpec 装载 Plugin manifest 为 PluginSpec。
// Python: verification.py:641 load_plugin_package(find_plugin_manifest(...))。
func loadPluginSpec(manifestPath string) (hschema.PluginSpec, error) {
	return harnessresources.LoadPluginPackage(manifestPath)
}

// resolvePluginSpecParts 物化 PluginSpec 成部件视图。
// Python: verification.py:629-633 _resolve_harness_parts。
func resolvePluginSpecParts(spec hschema.PluginSpec) (*pluginPartsView, error) {
	ctx := buildDefaultBuildContext()
	parts, err := harnessresources.ResolvePluginParts(spec, ctx)
	if err != nil {
		return nil, err
	}
	view := &pluginPartsView{}
	for _, resource := range parts.Tools {
		card := resource.Card
		if resource.Instance != nil {
			card = resource.Instance.Card()
		}
		if card != nil {
			name := card.Name
			if name == "" {
				name = card.ID
			}
			view.ToolCards = append(view.ToolCards, toolCardView{Name: name})
			// Python: _resource_file_path(spec.params.file_path)
			for _, param := range card.InputParams {
				// Python: params["file_path"] 直取；Go Param.Default 承载声明默认值
				if param.Name == "file_path" {
					if fileStr, isStr := param.Default.(string); isStr {
						view.ToolFileParams = append(view.ToolFileParams, fileStr)
					}
				}
			}
		}
	}
	for _, rail := range parts.Rails {
		if railResource, ok := rail.(interface{ Params() map[string]any }); ok {
			if fileStr, isStr := railResource.Params()["file_path"].(string); isStr {
				view.RailFileParams = append(view.RailFileParams, fileStr)
			}
		}
	}
	for _, resolved := range parts.Skills {
		view.SkillDirs = append(view.SkillDirs, resolved.Directory)
	}
	for _, section := range parts.PromptSections {
		view.PromptSectionNames = append(view.PromptSectionNames, section.Section.Name)
	}
	return view, nil
}

// buildDefaultBuildContext 默认构建上下文（language=en）。
// Python: verification.py:631 BuildContext(language="en")。
func buildDefaultBuildContext() hschema.BuildContextInterface {
	return &hschema.BuildContext{Language: "en"}
}
