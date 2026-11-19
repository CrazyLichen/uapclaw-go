// 匹配持久化评估并构建批次本地证据，不发起模型调用（source_evidence.py 一比一对齐）。
package singleharness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/evaluator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/evaluator/judger"
	memberoptimizer "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer"
)

// ignoredEvidenceDirs 材料身份忽略目录（Python _IGNORED）。
var ignoredEvidenceDirs = map[string]bool{".git": true, "__pycache__": true, ".pytest_cache": true}

// envRefPattern 环境变量引用（Python ${(\w+)}）。
var envRefPattern = regexp.MustCompile(`\$\{(\w+)\}`)

// ──────────────────────────── 导出函数 ────────────────────────────

// ReadEvidenceMapping 读取 YAML 映射，失败返回空映射（Python read_mapping）。
func ReadEvidenceMapping(path string) map[string]any {
	return harnessrsi.ReadYAMLMapping(path)
}

// EvaluationContext 识别被执行的 Harness 与评估输入，排除晋级记账（Python evaluation_context）。
// 偏差 D-d3：async → 同步（本函数无 IO 等待）。
func EvaluationContext(harnessRefsPath string, evaluatorConfig any, cases []map[string]any) map[string]any {
	emptyEvalRef := memberoptimizer.EvalRefFromDict(map[string]any{}, nil)
	roles := memberoptimizer.ResolveCandidateRoles(harnessRefsPath, &emptyEvalRef, "")
	harnesses := map[string]string{}
	for _, role := range roles {
		harnesses[role.Role] = role.HarnessRefPath
	}
	complete := len(harnesses) > 0
	for _, path := range harnesses {
		if _, err := os.Stat(path); err != nil {
			complete = false
			break
		}
	}
	runtime := map[string]any{}
	for _, entry := range os.Environ() {
		pair := strings.SplitN(entry, "=", 2)
		if len(pair) != 2 {
			continue
		}
		key := pair[0]
		if strings.HasPrefix(key, "EVOBENCH_") || strings.HasPrefix(key, "SWEBENCH_") || strings.HasPrefix(key, "RSI_EVOBENCH_") {
			runtime[key] = pair[1]
		}
	}
	signature := ""
	if complete {
		harnessIdentities := map[string]any{}
		for role, path := range harnesses {
			harnessIdentities[role] = HarnessIdentity(path)
		}
		signature = EvidenceDigest(map[string]any{
			"harnesses": harnessIdentities,
			"evaluator": MaterialIdentity(evaluatorConfig, cwdOrDot(), nil),
			"runtime":   runtime,
		})
	}
	caseDigests := map[string]any{}
	for _, item := range cases {
		caseID := toString(item["case_id"])
		base := cwdOrDot()
		if casePath, present := item["case_path"]; present && toString(casePath) != "" {
			if abs, err := filepath.Abs(toString(casePath)); err == nil {
				base = filepath.Dir(abs)
			}
		}
		caseDigests[caseID] = EvidenceDigest(MaterialIdentity(item, base, nil))
	}
	return map[string]any{
		"version":   1,
		"signature": signature,
		"cases":     caseDigests,
	}
}

// StampEvaluation 把评估上下文写入评估 YAML（Python stamp_evaluation；偏差 D-d3：async 无关）。
func StampEvaluation(path string, context map[string]any) error {
	payload := ReadEvidenceMapping(path)
	payload["evaluation_context"] = context
	target := filepath.Clean(path)
	temporary := strings.TrimSuffix(target, filepath.Ext(target)) + ".tmp"
	data, err := marshalYAMLDocument(payload)
	if err != nil {
		return err
	}
	if err := os.WriteFile(temporary, data, 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, target)
}

// MatchingCases 优先取最新匹配的检查点；不完整/基础设施结果不算证据（Python matching_cases）。
func MatchingCases(paths []string, context map[string]any) map[string]matchedCase {
	selected := map[string]matchedCase{}
	seen := map[string]bool{}
	signature, _ := context["signature"].(string)
	if signature == "" {
		return selected
	}
	rawContextCases, _ := context["cases"].(map[string]any)
	for i := len(paths) - 1; i >= 0; i-- {
		path := paths[i]
		payload := ReadEvidenceMapping(path)
		recorded, ok := payload["evaluation_context"].(map[string]any)
		if !ok {
			continue
		}
		recordedSignature, _ := recorded["signature"].(string)
		if recordedSignature != signature {
			continue
		}
		rawCases, _ := payload["cases"].([]any)
		for _, rawCase := range rawCases {
			item, ok := rawCase.(map[string]any)
			if !ok {
				continue
			}
			caseID := toString(item["case_id"])
			if seen[caseID] {
				continue
			}
			if _, expected := rawContextCases[caseID]; !expected {
				continue
			}
			if toString(rawContextCases[caseID]) != toString(recordedCases(recorded)[caseID]) {
				continue
			}
			seen[caseID] = true
			metadata, _ := item["metadata"].(map[string]any)
			if metadata == nil {
				metadata = map[string]any{}
			}
			status, _ := item["status"].(string)
			skip, _ := metadata["infrastructure_skip"].(bool)
			score, isNumber := numericOf(item["score"])
			if (status != "passed" && status != "failed") || skip {
				continue
			}
			if !isNumber || isNonFinite(item["score"]) {
				continue
			}
			pathsOK := true
			for _, key := range []string{"result_path", "trace_path"} {
				value := toString(item[key])
				if value == "" {
					pathsOK = false
					break
				}
				if info, err := os.Stat(value); err != nil || info.IsDir() {
					pathsOK = false
					break
				}
			}
			if !pathsOK {
				continue
			}
			result, resultErr := readJSONMapping(item["result_path"])
			trace, traceErr := readJSONMapping(item["trace_path"])
			if resultErr != nil || traceErr != nil {
				continue
			}
			if len(trace) == 0 {
				continue
			}
			resultStatus, _ := result["status"].(string)
			executionStatus, _ := result["execution_status"].(string)
			if resultStatus == "error" || resultStatus == "skipped" || executionStatus == "error" {
				continue
			}
			if evaluation, ok := result["evaluation"].(map[string]any); ok && judger.IsExecutionOnlyEvaluation(evaluation) {
				continue
			}
			_ = score
			selected[caseID] = matchedCase{Path: path, Case: item}
		}
	}
	return selected
}

// MaterializeSource 只复制本批证据；绝不把相邻用例暴露给 Analyzer（Python materialize_source）。
// 偏差 D-d3：async → 同步。
func MaterializeSource(cases []map[string]any, selected map[string]matchedCase, outputDir, harnessRefsPath string, context map[string]any, reusedCaseIDs map[string]bool) (string, error) {
	caseRefs := []map[string]any{}
	provenance := map[string][]string{}
	var provenanceOrder []string
	for index, item := range cases {
		caseID := toString(item["case_id"])
		match, ok := selected[caseID]
		if !ok {
			return "", fmt.Errorf("case %s not selected for materialization", caseID)
		}
		origin, ref := match.Path, match.Case
		source := filepath.Dir(toString(ref["result_path"]))
		target := filepath.Join(outputDir, "cases", fmt.Sprintf("c%03d", index+1))
		if err := copyTree(source, target); err != nil {
			return "", err
		}
		copied := map[string]any{}
		for key, value := range ref {
			copied[key] = value
		}
		for _, key := range []string{"result_path", "trace_path", "case_path"} {
			rawPath := toString(ref[key])
			if rawPath == "" {
				continue
			}
			if withinDir(rawPath, source) {
				rel, err := filepath.Rel(source, rawPath)
				if err != nil {
					return "", err
				}
				copied[key] = filepath.Join(target, rel)
			}
		}
		caseRefs = append(caseRefs, copied)
		if _, present := provenance[origin]; !present {
			provenanceOrder = append(provenanceOrder, origin)
		}
		provenance[origin] = append(provenance[origin], caseID)
	}
	summaryPath, err := evaluator.NewMetricsCollector().Collect(filepath.Join(outputDir, "cases"), filepath.Join(outputDir, "summary.json"))
	if err != nil {
		return "", err
	}
	first := ReadEvidenceMapping(provenanceOrder[0])
	dataset, _ := first["dataset"].(map[string]any)
	if dataset == nil {
		dataset = map[string]any{}
	}
	reusedList := make([]string, 0, len(reusedCaseIDs))
	for caseID := range reusedCaseIDs {
		reusedList = append(reusedList, caseID)
	}
	sort.Strings(reusedList)
	evaluatedList := []string{}
	for _, item := range cases {
		caseID := toString(item["case_id"])
		if !reusedCaseIDs[caseID] {
			evaluatedList = append(evaluatedList, caseID)
		}
	}
	evaluations := []map[string]any{}
	for _, path := range provenanceOrder {
		evaluations = append(evaluations, map[string]any{"eval_ref_path": path, "case_ids": provenance[path]})
	}
	outputDigest := EvidenceDigest(outputDir)[:16]
	payload := map[string]any{
		"eval_id":             fmt.Sprintf("source-%s", outputDigest),
		"harness_refs_path":   harnessRefsPath,
		"team_skill_ref_path": "",
		"dataset":             dataset,
		"eval_dir":            outputDir,
		"case_results_dir":    filepath.Join(outputDir, "cases"),
		"case_traces_dir":     filepath.Join(outputDir, "cases"),
		"summary_path":        summaryPath,
		"cases":               caseRefs,
		"evaluation_context":  context,
		"source_evidence": map[string]any{
			"reused_case_ids":    reusedList,
			"evaluated_case_ids": evaluatedList,
			"evaluations":        evaluations,
		},
	}
	output := filepath.Join(outputDir, "eval_ref.yaml")
	temporary := strings.TrimSuffix(output, filepath.Ext(output)) + ".tmp"
	data, err := marshalYAMLDocument(payload)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(temporary, data, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(temporary, output); err != nil {
		return "", err
	}
	return output, nil
}

// ──────────────────────────── 结构体 ────────────────────────────

// matchedCase 匹配到的证据用例及其来源评估文件。
type matchedCase struct {
	// Path 来源评估文件路径
	Path string
	// Case 用例行
	Case map[string]any
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// EvidenceDigest JSON 兼容值的 SHA-256 摘要（Python _digest）。
func EvidenceDigest(value any) string {
	encoded, err := json.Marshal(canonicalizeValue(value))
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// MaterialIdentity 递归材料身份：引用的配置/资产一并哈希（Python _material_identity）。
// 展开的凭据只进入摘要，绝不写入持久化 manifest；引用配置的环在文件哈希处终止。
func MaterialIdentity(value any, base string, seen map[string]bool) any {
	if seen == nil {
		seen = map[string]bool{}
	}
	switch v := value.(type) {
	case map[string]any:
		identity := map[string]any{}
		for key, item := range v {
			identity[key] = MaterialIdentity(item, base, seen)
		}
		return identity
	case []any:
		identity := make([]any, 0, len(v))
		for _, item := range v {
			identity = append(identity, MaterialIdentity(item, base, seen))
		}
		return identity
	}
	text, ok := value.(string)
	if !ok || text == "" {
		return value
	}
	expanded := envRefPattern.ReplaceAllStringFunc(text, func(match string) string {
		name := envRefPattern.FindStringSubmatch(match)[1]
		if env, present := os.LookupEnv(name); present {
			return env
		}
		return match
	})
	path := expandUser(expanded)
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	info, statErr := os.Stat(path)
	if statErr != nil || !info.Mode().IsRegular() {
		// 文本提示词与远程/容器路径不是本地文件引用
		return expanded
	}
	resolved, resErr := filepath.EvalSymlinks(path)
	if resErr != nil {
		return expanded
	}
	content, readErr := os.ReadFile(resolved)
	if readErr != nil {
		return expanded
	}
	sum := sha256.Sum256(content)
	identity := map[string]any{
		"path":   resolved,
		"sha256": hex.EncodeToString(sum[:]),
	}
	ext := strings.ToLower(filepath.Ext(resolved))
	normalized := filepath.Clean(resolved)
	if (ext == ".yaml" || ext == ".yml" || ext == ".json") && !seen[normalized] {
		marked := map[string]bool{}
		for key := range seen {
			marked[key] = true
		}
		marked[normalized] = true
		identity["references"] = MaterialIdentity(ReadEvidenceMapping(resolved), filepath.Dir(resolved), marked)
	}
	return identity
}

// HarnessIdentity harness 路径身份：目录则递归清单，否则按材料身份（Python _harness_identity）。
func HarnessIdentity(rawPath string) any {
	info, err := os.Stat(rawPath)
	if err != nil || !info.IsDir() {
		cwd, _ := os.Getwd()
		return MaterialIdentity(rawPath, cwd, nil)
	}
	// 只有不可变包递归，模型工作区/输出目录不递归
	files := map[string]any{}
	var paths []string
	walkErr := filepath.WalkDir(rawPath, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, path)
		return nil
	})
	if walkErr != nil {
		return map[string]any{"path": rawPath, "files": files}
	}
	sort.Strings(paths)
	for _, path := range paths {
		entryInfo, statErr := os.Stat(path)
		if statErr != nil || !entryInfo.Mode().IsRegular() {
			continue
		}
		relative, relErr := filepath.Rel(rawPath, path)
		if relErr != nil || relative == "." {
			continue
		}
		posix := filepath.ToSlash(relative)
		skip := false
		for _, part := range strings.Split(posix, "/") {
			if ignoredEvidenceDirs[part] {
				skip = true
				break
			}
		}
		ext := filepath.Ext(posix)
		if skip || ext == ".pyc" || ext == ".pyo" {
			continue
		}
		files[posix] = MaterialIdentity(path, rawPath, nil)
	}
	resolved, _ := filepath.EvalSymlinks(rawPath)
	if resolved == "" {
		resolved = rawPath
	}
	return map[string]any{"path": resolved, "files": files}
}

// recordedCases 取 recorded.cases 映射（缺失为空映射）。
func recordedCases(recorded map[string]any) map[string]any {
	cases, _ := recorded["cases"].(map[string]any)
	if cases == nil {
		return map[string]any{}
	}
	return cases
}

// cwdOrDot 当前目录（失败回退 "."）。
func cwdOrDot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

// readJSONMapping 读 JSON 文件为映射。
func readJSONMapping(rawPath any) (map[string]any, error) {
	path := toString(rawPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// numericOf 数值提取。
func numericOf(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case float64:
		return v, true
	}
	return 0, false
}

// isNonFinite 非有限浮点判定。
func isNonFinite(value any) bool {
	f, ok := value.(float64)
	return ok && (f != f || f > 1.7976931348623157e308 || f < -1.7976931348623157e308)
}

// canonicalizeValue 递归把 map 键排序序列化所需的规范化（json.Marshal 对 map 自动按键排序）。
func canonicalizeValue(value any) any {
	return value
}

// copyTree 递归复制目录（Python shutil.copytree dirs_exist_ok=True）。
func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(source, path)
		if relErr != nil {
			return relErr
		}
		destination := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if mkdirErr := os.MkdirAll(filepath.Dir(destination), 0o755); mkdirErr != nil {
			return mkdirErr
		}
		return os.WriteFile(destination, data, 0o644)
	})
}

// marshalYAMLDocument YAML 序列化（Python yaml.safe_dump allow_unicode=True sort_keys=False；
// 偏差 D-d4：yaml.v3 对 map 按键排序输出，键序非插入序，内容等价）。
func marshalYAMLDocument(payload map[string]any) ([]byte, error) {
	return yaml.Marshal(payload)
}
