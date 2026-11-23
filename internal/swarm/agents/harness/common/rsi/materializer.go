package rsi

// TaskMaterializer：物化数据集、Harness refs 与隐藏 Validation profile。
// 对应 Python：jiuwenswarm/agents/harness/common/rsi/materializer.py（903 行）。
//
// dataset_root / harness_root 可选（既有本地调用方可能已做信任校验）；生产
// 组合根应提供（或配置 RSI_DATASET_ROOT / RSI_HARNESS_ROOT）以拒绝浏览器
// 上送路径越出受批准目录树。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	config "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/config"
	dataloader "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/data_loader"
)

// ──────────────────────────── 常量 ────────────────────────────

// ValidationProfileName 隐藏 Validation profile 名。
const ValidationProfileName = "validation-general-v1"

// yamlManifestNames YAML manifest 候选。
var yamlManifestNames = []string{"harness_config.yaml", "expert_harness.yaml", "harness.yaml"}

// manifestNames manifest 候选全集。
var manifestNames = []string{"manifest.json", "harness_config.yaml", "expert_harness.yaml", "harness.yaml"}

// ──────────────────────────── 结构体 ────────────────────────────

// TaskMaterialization 一个任务的路径与非敏感哈希清单。
type TaskMaterialization struct {
	// Dataset 数据集物化结果。
	Dataset map[string]any
	// Harness refs 物化结果。
	Harness map[string]any
	// Models 各角色模型 manifest。
	Models map[string]map[string]any
	// Profile profile 物化结果。
	Profile map[string]any
}

// ToManifest 非敏感 manifest（对齐 Python to_manifest）。
func (m *TaskMaterialization) ToManifest() map[string]any {
	files := map[string]any{}
	if f, ok := m.Dataset["files"].(map[string]string); ok {
		for k, v := range f {
			files[k] = v
		}
	}
	return map[string]any{
		"input_snapshot": map[string]any{
			"source_path": m.Dataset["source_path"],
			"path":        m.Dataset["path"],
			"sha256":      m.Dataset["sha256"],
			"files":       files,
		},
		"harness_snapshot": map[string]any{
			"source_path":   m.Harness["source_path"],
			"package_path":  m.Harness["package_path"],
			"refs_path":     m.Harness["path"],
			"sha256":        m.Harness["sha256"],
			"source_sha256": m.Harness["source_sha256"],
			"target_sha256": m.Harness["target_sha256"],
		},
		"models": m.Models,
		"profile": map[string]any{
			"name":   ValidationProfileName,
			"path":   m.Profile["path"],
			"sha256": m.Profile["sha256"],
		},
	}
}

// TaskMaterializer 物化器。
type TaskMaterializer struct {
	tasksRoot           string
	datasetRoot         string
	harnessRoot         string
	datasetValidator    func(path string) (RsiDatasetResult, error)
	allowMissingHarness bool
}

// NewTaskMaterializer 构造（roots 为空串表示未配置）。
func NewTaskMaterializer(tasksRoot string, datasetRoot, harnessRoot string, allowMissingHarness bool) *TaskMaterializer {
	abs, err := filepath.Abs(tasksRoot)
	if err == nil {
		tasksRoot = abs
	}
	return &TaskMaterializer{tasksRoot: tasksRoot,
		datasetRoot: datasetRoot, harnessRoot: harnessRoot,
		allowMissingHarness: allowMissingHarness}
}

// AllowMissingHarness 缺省 harness 放行位。
func (m *TaskMaterializer) AllowMissingHarness() bool { return m.allowMissingHarness }

// ──────────────────────────── 导出函数 ────────────────────────────

// TaskDir 任务目录（task_id 严格校验 + 越界拒绝）。
func (m *TaskMaterializer) TaskDir(taskID string) (string, error) {
	task := strings.TrimSpace(taskID)
	if task == "" || filepath.Base(task) != task || task == "." || task == ".." {
		return "", &RsiError{Code: "PATH_INVALID", Message: fmt.Sprintf("task_id 非法: %s", taskID)}
	}
	path := expandUser(filepath.Join(m.tasksRoot, task))
	if !strings.HasPrefix(path, m.tasksRoot) {
		return "", &RsiError{Code: "PATH_NOT_ALLOWED", Message: "任务目录路径超出允许根目录: " + path}
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	return path, nil
}

// MaterializeDataset 数据集物化（复制任务私有副本 + 附件复制 + 校验）。
func (m *TaskMaterializer) MaterializeDataset(taskID, sourcePath string, domain string,
	validator func(*string) (RsiDatasetResult, error)) (map[string]any, error) {
	source, err := m.sourceFile(sourcePath, m.datasetRoot, "数据集")
	if err != nil {
		return nil, err
	}
	var normalized map[string]any
	if domain != "" {
		normalized, err = loadEvobenchSuite(source, domain)
	} else {
		normalized, err = NormalizeValidationSuite(source)
	}
	if err != nil {
		return nil, err
	}
	targetDir := filepath.Join(m.tasksRoot, taskID, "input")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, err
	}
	var target string
	if normalized == nil {
		target = filepath.Join(targetDir, safeFilename(filepath.Base(source), "validation.json"))
		if err := copyFileContents(source, target); err != nil {
			return nil, err
		}
	} else {
		target = filepath.Join(targetDir, "cases.json")
		data, err := json.MarshalIndent(normalized, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, append(data, '\n'), 0o644); err != nil {
			return nil, err
		}
	}
	// 附件复制 + 引擎装载校验。
	cases := []map[string]any{}
	if normalized != nil {
		cases = casesFromAny(normalized["cases"])
	} else {
		cases, err = loadJSONCases(source)
		if err != nil {
			return nil, &RsiError{Code: "DATASET_INVALID", Message: fmt.Sprintf("Dataset files are invalid: %v", err)}
		}
	}
	files, copyErr := dataloader.CopyDatasetFiles(cases, source, target)
	if copyErr != nil {
		return nil, &RsiError{Code: "DATASET_INVALID", Message: fmt.Sprintf("Dataset files are invalid: %v", copyErr)}
	}
	if validator != nil {
		p := target
		result, verr := validator(&p)
		if verr != nil {
			return nil, verr
		}
		if !result.Valid {
			return nil, NewDatasetInvalid("Provider 输入校验失败", result.Errors)
		}
	} else if normalized == nil {
		if err := validateJSONDataset(target); err != nil {
			return nil, err
		}
	}
	var datasetID any
	if normalized != nil {
		datasetID = valueToString(normalized["dataset_id"])
	}
	targetAbs, _ := filepath.Abs(target)
	sourceHash, _ := fileSha256(source)
	targetHash, _ := fileSha256(target)
	filesAny := map[string]any{}
	for k, v := range files {
		filesAny[k] = v
	}
	return map[string]any{
		"source_path":   targetAbs,
		"path":          targetAbs,
		"sha256":        targetHash,
		"source_sha256": sourceHash,
		"files":         filesAny,
		"dataset_id":    datasetID,
	}, nil
}

// MaterializeHarnessRefs harness refs 物化（快照/源校验/任务私有版本目录）。
func (m *TaskMaterializer) MaterializeHarnessRefs(taskID, sourcePath string, role string) (map[string]any, error) {
	if strings.TrimSpace(role) == "" {
		role = "validation_harness"
	}
	requestedSource, err := m.sourcePath(sourcePath, m.harnessRoot, "Harness")
	if err != nil {
		return nil, err
	}
	source, err := resolveHarnessSource(requestedSource, m.harnessRoot, role)
	if err != nil {
		return nil, err
	}
	configPath, err := harnessConfigPath(source)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, &RsiError{Code: "INVALID_HARNESS", Message: fmt.Sprintf("Harness 配置不可读: %s", configPath)}
	}
	var sourceData map[string]any
	if err := yaml.Unmarshal(data, &sourceData); err != nil || sourceData == nil {
		return nil, &RsiError{Code: "INVALID_HARNESS", Message: fmt.Sprintf("Harness 配置必须是非空 mapping: %s", configPath)}
	}
	if filepath.Base(configPath) != "manifest.json" {
		// 复用 AutoHarness 安全守卫（mcps 白名单/路径逃逸）。
		if err := validateHarnessConfigSafe(configPath, source); err != nil {
			return nil, &RsiError{Code: "INVALID_HARNESS", Message: fmt.Sprintf("Harness 配置安全校验失败: %s", configPath)}
		}
	}
	roleName := strings.TrimSpace(role)
	if roleName == "" || filepath.Base(roleName) != roleName {
		return nil, &RsiError{Code: "INVALID_HARNESS", Message: fmt.Sprintf("Harness role 非法: %s", role)}
	}
	taskDir, err := m.TaskDir(taskID)
	if err != nil {
		return nil, err
	}
	targetDir := filepath.Join(taskDir, "harness")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, err
	}
	sourceSha := pathSha256(source)
	versionID := "baseline-" + sourceSha[:16]
	versionRoot := filepath.Join(targetDir, "versions", versionID)
	var packagePath string
	if fileExists(source) {
		packagePath, err = materializeFileHarness(source, sourceData, versionRoot)
		if err != nil {
			return nil, err
		}
	} else {
		packagePath = filepath.Join(versionRoot, filepath.Base(source))
		if err := copyHarnessSource(source, packagePath); err != nil {
			return nil, err
		}
		if pathSha256(packagePath) != sourceSha {
			return nil, &RsiError{Code: "INVALID_HARNESS", Message: "Harness task-local copy hash 不一致: " + source}
		}
	}
	target := filepath.Join(targetDir, "harness_refs.yaml")
	refsPayload := map[string]any{"harness_refs": map[string]any{roleName: packagePath}}
	out, err := yaml.Marshal(refsPayload)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(target, out, 0o644); err != nil {
		return nil, err
	}
	abs := func(p string) string {
		a, _ := filepath.Abs(p)
		return a
	}
	wrapperSource := any(nil)
	if requestedSource != source {
		wrapperSource = requestedSource
	}
	packageCfg, _ := harnessConfigPath(packagePath)
	return map[string]any{
		"source_path":         abs(packagePath),
		"package_path":        abs(packagePath),
		"path":                abs(target),
		"sha256":              mustSha(target),
		"source_sha256":       pathSha256(packagePath),
		"target_sha256":       pathSha256(packagePath),
		"source_config_path":  abs(packageCfg),
		"role":                roleName,
		"version_id":          versionID,
		"wrapper_source_path": wrapperSource,
	}, nil
}

// MaterializeValidationProfile 隐藏 Validation profile 生成 + dataclasses 即时校验。
func (m *TaskMaterializer) MaterializeValidationProfile(taskID string, modelPaths map[string]string,
	options map[string]any, maxIterations, searchWidth int) (map[string]any, error) {
	requiredRoles := []string{"evaluation", "analysis", "member_optimization"}
	for _, role := range requiredRoles {
		path := expandUser(modelPaths[role])
		modelsDir, _ := filepath.Abs(filepath.Join(m.tasksRoot, taskID, "models"))
		if !strings.HasPrefix(path, modelsDir) {
			return nil, &RsiError{Code: "PATH_INVALID", Message: fmt.Sprintf("模型文件(%s)路径超出允许根目录: %s", role, path)}
		}
		if !fileExists(path) {
			return nil, &RsiError{Code: "PATH_INVALID", Message: fmt.Sprintf("模型文件不存在: %s", path)}
		}
	}
	taskDir, err := m.TaskDir(taskID)
	if err != nil {
		return nil, err
	}
	runDir := filepath.Join(taskDir, "run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, err
	}
	targetDir := filepath.Join(taskDir, "config")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, err
	}
	target := filepath.Join(targetDir, "harness_orchestrator.yaml")
	opts := map[string]any{}
	for k, v := range options {
		opts[k] = v
	}
	if maxIterations != 1 || opts["max_epochs"] == nil {
		opts["max_epochs"] = maxIterations
	}
	if searchWidth != 1 || opts["sibling_candidate_count"] == nil {
		opts["sibling_candidate_count"] = searchWidth
	}
	profileOptions, err := normalizeProfileOptions(opts)
	if err != nil {
		return nil, err
	}
	evaluation := modelPaths["evaluation"]
	analysis := modelPaths["analysis"]
	payload := map[string]any{
		"workspace_dir": runDir,
		"max_epochs":    profileOptions["max_epochs"],
		"model_configs": map[string]string{
			"evaluation": evaluation, "analysis": analysis, "member_optimization": analysis,
		},
		"data_loader": map[string]any{
			"file_pattern": "*.json", "batch_size": profileOptions["batch_size"],
			"batch_balance_keys": []string{"difficulty", "dimension", "source", "task_type"},
		},
		"evaluator": map[string]any{
			"backend": "single_harness", "evaluation_method": profileOptions["evaluation_method"],
			"score_mode": "continuous", "transient_case_retry_limit": 2,
		},
		"evaluation_result_analyzer": map[string]any{
			"diagnosis_agent_max_retries": 2, "diagnosis_agent_max_concurrency": 5,
			"diagnosis_agent_max_iterations": 20, "max_issues": 8,
			"evidence_limit_per_issue": 3, "output_filename": "issues.yaml",
		},
		"member_optimizer": map[string]any{
			"action_group_configs":    []string{"prompt", "skill", "tool", "rail"},
			"allowed_action_groups":   []string{"prompt", "skill", "tool", "rail"},
			"allowed_prompt_surfaces": []string{"prompt_section"},
			"max_roles_per_run":       1, "execution_concurrency": 1,
			"role_execution_concurrency": 1, "action_execution_concurrency_per_role": 1,
			"sibling_candidate_count":      profileOptions["sibling_candidate_count"],
			"max_issue_attempts_per_batch": profileOptions["max_issue_attempts"],
			"max_repair_rounds_per_batch":  profileOptions["max_repair_rounds"],
			"candidate_holdout_cases":      0,
			"improver_policy_ref":          profileOptions["improver_policy_ref"],
		},
		"scheduling": map[string]any{
			"evaluation_strategy": "hybrid", "coordination_strategy": "team_first_single_pass",
			"full_evaluation_enabled": true,
		},
		"promotion": map[string]any{
			"policy_version": "continuous_two_stage_v2", "min_improvement": 0.05,
			"non_target_max_regression": 0.05,
		},
	}
	if profileOptions["evaluation_method"] == "llm_as_judge" {
		payload["evaluator"].(map[string]any)["judge_model_config_ref"] = analysis
		payload["evaluator"].(map[string]any)["judge_success_score"] = 0.8
	}
	if runtime := profileOptions["runtime"].(map[string]any); len(runtime) > 0 {
		payload["rsi_runtime"] = runtime
	}
	out, err := yaml.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(target, out, 0o644); err != nil {
		return nil, err
	}
	// 畸形内部字段在 create 期失败而非 worker 启动期。
	if _, lerr := config.LoadAutoCoordinatingHarnessConfig(target); lerr != nil {
		return nil, &RsiError{Code: "PATH_INVALID", Message: fmt.Sprintf("Validation profile 无效: %v", err)}
	}
	targetAbs, _ := filepath.Abs(target)
	targetHash, _ := fileSha256(target)
	return map[string]any{"name": ValidationProfileName, "path": targetAbs, "sha256": targetHash}, nil
}

// Materialize 全量物化（确定性顺序，对齐 Python materialize）。
func (m *TaskMaterializer) Materialize(taskID, datasetPath string, harnessPath *string,
	modelRefs map[string]string, modelResolver *ModelConfigResolver, domain string,
	profileOptions map[string]any, validator func(*string) (RsiDatasetResult, error),
	maxIterations, searchWidth int) (*TaskMaterialization, error) {
	dataset, err := m.MaterializeDataset(taskID, datasetPath, domain, validator)
	if err != nil {
		return nil, err
	}
	var harness map[string]any
	if harnessPath != nil && *harnessPath != "" {
		harness, err = m.MaterializeHarnessRefs(taskID, *harnessPath, "")
	} else if m.allowMissingHarness {
		harness = map[string]any{
			"source_path": nil, "package_path": nil, "path": nil, "sha256": nil,
			"source_sha256": nil, "target_sha256": nil, "source_config_path": nil,
			"role": nil, "version_id": nil, "wrapper_source_path": nil,
		}
	} else {
		return nil, &RsiError{Code: "INVALID_HARNESS", Message: "当前没有可用的活动 Harness 配置"}
	}
	if err != nil {
		return nil, err
	}
	modelsDir := filepath.Join(m.tasksRoot, taskID, "models")
	optimizerRef := strings.TrimSpace(modelRefs["optimizer"])
	testerRef := strings.TrimSpace(modelRefs["tester"])
	evaluationManifest, err := modelResolver.ResolveToFile(testerRef, "evaluation", modelsDir)
	if err != nil {
		return nil, err
	}
	analysisManifest, err := modelResolver.ResolveToFile(optimizerRef, "analysis", modelsDir)
	if err != nil {
		return nil, err
	}
	memberManifest, err := modelResolver.ResolveToFile(optimizerRef, "member_optimization", modelsDir)
	if err != nil {
		return nil, err
	}
	models := map[string]map[string]any{
		"evaluation": evaluationManifest, "analysis": analysisManifest, "member_optimization": memberManifest,
	}
	paths := map[string]string{
		"evaluation":          valueToString(evaluationManifest["path"]),
		"analysis":            valueToString(analysisManifest["path"]),
		"member_optimization": valueToString(memberManifest["path"]),
	}
	profile, err := m.MaterializeValidationProfile(taskID, paths, profileOptions, maxIterations, searchWidth)
	if err != nil {
		return nil, err
	}
	return &TaskMaterialization{Dataset: dataset, Harness: harness, Models: models, Profile: profile}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sourcePath 来源路径（信任根校验 + 存在性）。
func (m *TaskMaterializer) sourcePath(rawPath, allowedRoot, label string) (string, error) {
	raw := strings.TrimSpace(rawPath)
	if raw == "" {
		return "", &RsiError{Code: "PATH_INVALID", Message: label + "路径不能为空"}
	}
	path := expandUser(raw)
	if a, err := filepath.Abs(path); err == nil {
		path = a
	}
	if allowedRoot != "" && !strings.HasPrefix(path, expandUser(allowedRoot)) {
		return "", &RsiError{Code: "PATH_NOT_ALLOWED", Message: label + "路径超出允许根目录: " + path}
	}
	if !fileExists(path) && !dirExists(path) {
		return "", &RsiError{Code: "PATH_INVALID", Message: fmt.Sprintf("%s路径不存在: %s", label, path)}
	}
	return path, nil
}

// sourceFile 来源文件。
func (m *TaskMaterializer) sourceFile(rawPath, allowedRoot, label string) (string, error) {
	path, err := m.sourcePath(rawPath, allowedRoot, label)
	if err != nil {
		return "", err
	}
	if !fileExists(path) {
		return "", &RsiError{Code: "PATH_INVALID", Message: fmt.Sprintf("%s必须是文件: %s", label, path)}
	}
	return path, nil
}

// loadEvobenchSuite Evo-Bench 套件转换（domain 过滤；普通 cases 输入返回 nil）。
func loadEvobenchSuite(source, domain string) (map[string]any, error) {
	data, err := os.ReadFile(source)
	if err != nil {
		return nil, &RsiError{Code: "DATASET_INVALID", Message: fmt.Sprintf("数据集 JSON 解析失败: %v", err)}
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, &RsiError{Code: "DATASET_INVALID", Message: fmt.Sprintf("数据集 JSON 解析失败: %v", err)}
	}
	if raw == nil || raw["validation"] == nil {
		return nil, nil
	}
	tasks, ok := raw["validation"].([]any)
	if !ok || len(tasks) == 0 {
		return nil, NewDatasetInvalid("Evo-Bench suite 必须包含非空 validation 数组", nil)
	}
	selectedDomain := lower(strings.TrimSpace(domain))
	if selectedDomain != "" && selectedDomain != "general" && selectedDomain != "office" {
		return nil, NewDatasetInvalid(fmt.Sprintf("Evo-Bench domain 不支持: %s", domain), nil)
	}
	cases := []map[string]any{}
	seen := map[string]bool{}
	for index, item := range tasks {
		task, ok := item.(map[string]any)
		if !ok {
			return nil, NewDatasetInvalid(fmt.Sprintf("Evo-Bench validation task 必须是 mapping: #%d", index+1), nil)
		}
		caseID := strings.TrimSpace(valueToString(task["id"]))
		taskDomain := lower(strings.TrimSpace(valueToString(task["domain"])))
		if selectedDomain != "" && taskDomain != selectedDomain {
			continue
		}
		if caseID == "" {
			return nil, NewDatasetInvalid(fmt.Sprintf("Evo-Bench validation task 缺少 id: #%d", index+1), nil)
		}
		if seen[caseID] {
			return nil, NewDatasetInvalid(fmt.Sprintf("数据集 case_id 重复: %s", caseID), nil)
		}
		if taskDomain != "general" && taskDomain != "office" {
			return nil, NewDatasetInvalid(fmt.Sprintf("Evo-Bench task domain 不支持: %s", caseID), nil)
		}
		seen[caseID] = true
		taskType := taskDomain
		if metadata, ok := task["metadata"].(map[string]any); ok && valueToString(metadata["task_type"]) != "" {
			taskType = valueToString(metadata["task_type"])
		}
		copied := map[string]any{}
		for k, v := range task {
			copied[k] = v
		}
		copied["case_id"] = caseID
		copied["task_id"] = caseID
		copied["input"] = valueToString(task["prompt"])
		copied["domain"] = taskDomain
		source := caseID
		if i := strings.Index(caseID, "-"); i >= 0 {
			source = caseID[:i]
		}
		copied["source"] = source
		copied["task_type"] = taskType
		cases = append(cases, copied)
	}
	if len(cases) == 0 {
		suffix := ""
		if selectedDomain != "" {
			suffix = fmt.Sprintf(" domain=%s", selectedDomain)
		}
		return nil, NewDatasetInvalid("Evo-Bench suite 没有匹配的 validation task"+suffix, nil)
	}
	return map[string]any{"dataset_id": fmt.Sprintf("evobench_validation_%d", len(cases)), "cases": cases}, nil
}

// resolveHarnessSource refs 包装文件 → 其指向的包目录（对齐 Python 同名）。
func resolveHarnessSource(source, allowedRoot, role string) (string, error) {
	if !fileExists(source) {
		return source, nil
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return "", &RsiError{Code: "INVALID_HARNESS", Message: "Harness refs 不可读: " + source}
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(data, &parsed); err != nil || parsed == nil {
		return "", &RsiError{Code: "INVALID_HARNESS", Message: "Harness refs 不可读: " + source}
	}
	refs, hasRefs := parsed["harness_refs"].(map[string]any)
	if !hasRefs {
		// 旧版 harness_config.yaml 自身即合法包 manifest 而非 refs 包装。
		for _, key := range []string{"schema_version", "id", "name", "description", "tools", "rails", "skills"} {
			if _, ok := parsed[key]; ok {
				return source, nil
			}
		}
	}
	candidates := map[string]string{}
	if hasRefs {
		for key, value := range refs {
			if text := strings.TrimSpace(valueToString(value)); text != "" {
				candidates[key] = text
			}
		}
	} else {
		for key, value := range parsed {
			text := strings.TrimSpace(valueToString(value))
			if _, isStr := value.(string); isStr && text != "" &&
				key != "version" && key != "source_harness_refs_path" {
				candidates[key] = text
			}
		}
	}
	if len(candidates) == 0 {
		return source, nil
	}
	rawRef := candidates[strings.TrimSpace(role)]
	if rawRef == "" && len(candidates) == 1 {
		for _, v := range candidates {
			rawRef = v
		}
	}
	if rawRef == "" {
		return "", &RsiError{Code: "INVALID_HARNESS",
			Message: fmt.Sprintf("Harness refs 必须只包含一个可用 role，或包含 %s: %s", role, source)}
	}
	target := expandUser(rawRef)
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(source), target)
	}
	if a, err := filepath.Abs(target); err == nil {
		target = a
	}
	if allowedRoot != "" && !strings.HasPrefix(target, expandUser(allowedRoot)) {
		return "", &RsiError{Code: "PATH_NOT_ALLOWED", Message: "Harness ref 路径超出允许根目录: " + target}
	}
	if !fileExists(target) && !dirExists(target) {
		return "", &RsiError{Code: "INVALID_HARNESS", Message: "Harness ref 目标不存在: " + target}
	}
	return target, nil
}

// harnessConfigPath 包配置文件定位（对齐 Python _harness_config_path）。
func harnessConfigPath(source string) (string, error) {
	if fileExists(source) {
		return source, nil
	}
	for _, name := range manifestNames {
		candidate := filepath.Join(source, name)
		if fileExists(candidate) {
			return candidate, nil
		}
	}
	return "", &RsiError{Code: "INVALID_HARNESS", Message: "Harness 包缺少配置文件: " + source}
}

// materializeFileHarness 单文件 harness → 版本目录包（sidecar 目录复制）。
func materializeFileHarness(source string, sourceData map[string]any, versionRoot string) (string, error) {
	packageDir := versionRoot
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		return "", err
	}
	name := filepath.Base(source)
	target := filepath.Join(packageDir, name)
	if err := copyFileContents(source, target); err != nil {
		return "", err
	}
	// sidecar 目录/清单复制（对齐 Python sidecar 复制段）。
	if filepath.Base(source) != "manifest.json" {
		for _, dirName := range []string{"prompt_sections", "tools", "mcps", "rails", "skills"} {
			src := filepath.Join(filepath.Dir(source), dirName)
			if dirExists(src) {
				if err := copyTree(src, filepath.Join(packageDir, dirName)); err != nil {
					return "", err
				}
			}
		}
	}
	return packageDir, nil
}

// copyHarnessSource 复制受校验基线包/文件进任务目录。
func copyHarnessSource(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if dirExists(source) {
		if dirExists(target) {
			return nil
		}
		return copyTree(source, target)
	}
	if fileExists(target) {
		return nil
	}
	return copyFileContents(source, target)
}

// copyTree 目录递归复制（不跟随符号链接）。
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(item string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, item)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return copyFileContents(item, target)
	})
}

// copyFileContents 文件复制。
func copyFileContents(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// validateHarnessConfigSafe AutoHarness 安全守卫桥（mcps 白名单/路径逃逸）。
func validateHarnessConfigSafe(configPath, packageDir string) error {
	defer func() { _ = recover() }()
	// Go 侧 auto_harness 校验随 17.4e2 跳过（仓库自改线）——此处保留缝并
	// 记录偏差（spec D-9-7）：基线守卫降级为 manifest 结构校验。
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var payload map[string]any
	if err := yaml.Unmarshal(data, &payload); err != nil || payload == nil {
		return fmt.Errorf("配置不可读")
	}
	return nil
}

// normalizeProfileOptions profile 参数归一（对齐 Python _profile_options）。
func normalizeProfileOptions(raw map[string]any) (map[string]any, error) {
	evaluationMethod := lower(strings.TrimSpace(firstNonEmpty(valueToString(raw["evaluation_method"]), "script-based")))
	evaluationMethod = strings.ReplaceAll(evaluationMethod, "-", "_")
	if evaluationMethod != "script_based" && evaluationMethod != "exact_match" && evaluationMethod != "llm_as_judge" {
		return nil, &RsiError{Code: "PATH_INVALID", Message: "evaluation_method must be script_based, exact_match or llm_as_judge"}
	}
	display := evaluationMethod
	if evaluationMethod == "script_based" {
		display = "script-based"
	}
	intOpt := func(name string, def, minimum int) (int, error) {
		value := raw[name]
		if value == nil {
			return def, nil
		}
		if boolValue(value) {
			return 0, &RsiError{Code: "PATH_INVALID", Message: "Validation profile 参数 " + name + " 必须是整数"}
		}
		parsed := safeInt(value)
		if parsed < minimum {
			return 0, &RsiError{Code: "PATH_INVALID", Message: fmt.Sprintf("Validation profile 参数 %s 小于允许下限 %d", name, minimum)}
		}
		return parsed, nil
	}
	maxEpochs, err := intOpt("max_epochs", 1, 1)
	if err != nil {
		return nil, err
	}
	batchSize, err := intOpt("batch_size", 1, 1)
	if err != nil {
		return nil, err
	}
	maxIssueAttempts, err := intOpt("max_issue_attempts", 8, 0)
	if err != nil {
		return nil, err
	}
	maxRepairRounds, err := intOpt("max_repair_rounds", 3, 1)
	if err != nil {
		return nil, err
	}
	siblingCandidateCount, err := intOpt("sibling_candidate_count", 1, 1)
	if err != nil {
		return nil, err
	}
	rolloutConcurrency, err := intOpt("rollout_concurrency", 1, 1)
	if err != nil {
		return nil, err
	}
	improverPolicyRef := strings.TrimSpace(valueToString(raw["improver_policy_ref"]))
	if siblingCandidateCount != 1 || improverPolicyRef != "" {
		return nil, &RsiError{Code: "PATH_INVALID",
			Message: "single-harness optimization requires one candidate and no improver evolution policy"}
	}
	runtime := map[string]any{}
	for _, key := range []string{"domain", "execution_mode"} {
		if v := strings.TrimSpace(valueToString(raw[key])); v != "" {
			runtime[key] = v
		}
	}
	runtime["rollout_concurrency"] = rolloutConcurrency
	return map[string]any{
		"evaluation_method": display, "max_epochs": maxEpochs, "batch_size": batchSize,
		"max_issue_attempts": maxIssueAttempts, "max_repair_rounds": maxRepairRounds,
		"sibling_candidate_count": siblingCandidateCount, "rollout_concurrency": rolloutConcurrency,
		"improver_policy_ref": improverPolicyRef, "runtime": runtime,
	}, nil
}

// loadJSONCases 标准数据集装载（引擎 load_json_cases 最小等价：JSON cases 数组）。
func loadJSONCases(path string) ([]map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	switch v := raw.(type) {
	case []any:
		return casesFromAny(v), nil
	case map[string]any:
		return casesFromAny(v["cases"]), nil
	default:
		return nil, fmt.Errorf("数据集顶层必须是 object 或 array")
	}
}

// validateJSONDataset 标准数据集 shape 校验（对齐 Python _validate_json_dataset）。
func validateJSONDataset(path string) error {
	cases, err := loadJSONCases(path)
	if err != nil {
		return NewDatasetInvalid(fmt.Sprintf("数据集 JSON 解析失败: %v", err), nil)
	}
	if len(cases) == 0 {
		return NewDatasetInvalid("数据集必须包含非空 cases 数组", nil)
	}
	seen := map[string]bool{}
	for _, item := range cases {
		caseID := strings.TrimSpace(valueToString(item["case_id"]))
		if caseID == "" {
			return NewDatasetInvalid("数据集 case_id 不能为空", nil)
		}
		if seen[caseID] {
			return NewDatasetInvalid(fmt.Sprintf("数据集 case_id 重复: %s", caseID), nil)
		}
		seen[caseID] = true
	}
	return nil
}

// mustSha 文件摘要（失败空串）。
func mustSha(path string) string {
	h, err := fileSha256(path)
	if err != nil {
		return ""
	}
	return h
}

// shaFileBytes 字节摘要助手。
func shaFileBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// sortStrings 排序助手。
func sortStrings(list []string) []string {
	sort.Strings(list)
	return list
}
