// case_backend_helpers.go 单 harness 后端的组装与工作区机械。
// Python: openjiuwen/rsi/harness_rsi/evaluator/case_backend.py L332-836
// （模块级函数全量对齐）。

package evaluator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
	dataloader "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/data_loader"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator/evalerrors"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/observability/demand"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness"
	railspkg "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
	evolutionpkg "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
	skillsrail "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/skills"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/workspace"
)

// ──────────────────────────── 轨道组装 ────────────────────────────

// singleHarnessRailsParams _single_harness_rails 入参包。
type singleHarnessRailsParams struct {
	teamSkillRefPath    string
	harnessPath         string
	shellOnly           bool
	controlledSkillName string
	workspace           string
}

// singleHarnessRails 组装单 harness 轨道清单。
// Python: case_backend.py:435-474 _single_harness_rails——装配顺序逐条对齐。
func singleHarnessRails(params singleHarnessRailsParams) ([]agentinterfaces.AgentRail, error) {
	// Python: [RSISysOperationRail(shell_only, bash_pipefail=shell_only),
	//          build_single_agent_reliability_rail()]
	sysOpRail := NewRSISysOperationRail(params.shellOnly, params.shellOnly, true)
	reliabilityRail, err := harnessrsi.BuildSingleAgentReliabilityRail()
	if err != nil {
		return nil, evalerrors.NewEvaluationInfrastructureError(err.Error())
	}
	railsList := []agentinterfaces.AgentRail{sysOpRail, reliabilityRail}
	// Python: if controlled_skill_name: rails.append(ControlledSkillTreatmentRail(...))
	if params.controlledSkillName != "" {
		controlled, err := NewControlledSkillTreatmentRail(params.controlledSkillName)
		if err != nil {
			return nil, evalerrors.NewEvaluationInfrastructureError(err.Error())
		}
		railsList = append(railsList, controlled)
	}
	// Python: if workspace is not None: rails.append(HarnessInputRail(harness_path, workspace))
	if params.workspace != "" {
		railsList = append(railsList, NewHarnessInputRail(params.harnessPath, params.workspace))
	}
	// Python: skill_dir = _resolve_skill_dir(...)；RSISkillUseRail(
	//   skills_dir=skill_dir.parent or harness_path/"skills", skill_mode=ALL,
	//   enabled_skills=[skill_dir.name] or None, include_tools=not shell_only,
	//   trigger_at_task_start=True)
	skillDir := ""
	if params.teamSkillRefPath != "" {
		skillDir = resolveSkillDir(params.teamSkillRefPath)
	}
	opts := []skillsrail.SkillUseRailOption{
		skillsrail.WithSkillMode(skillsrail.SkillModeAll),
		skillsrail.WithIncludeTools(!params.shellOnly),
	}
	if skillDir != "" {
		opts = append(opts, skillsrail.WithEnabledSkills([]string{filepath.Base(skillDir)}))
	}
	// Python: skills_dir=skill_dir.parent or harness_path/"skills"
	skillsRoot := filepath.Join(params.harnessPath, "skills")
	if skillDir != "" {
		skillsRoot = filepath.Dir(skillDir)
	}
	rsiSkillRail := NewRSISkillUseRail(true, opts...)
	rsiSkillRail.SetSkillsDir([]string{skillsRoot})
	railsList = append(railsList, rsiSkillRail)
	return railsList, nil
}

// resolveSkillDir 技能目录解析（Python: :476-481 _resolve_skill_dir——
// SKILL.md 文件取父目录；目录原样；其余取父目录）。
func resolveSkillDir(skillRefPath string) string {
	refPath := absPath(skillRefPath)
	if strings.EqualFold(filepath.Base(refPath), "skill.md") {
		return filepath.Dir(refPath)
	}
	if info, err := os.Stat(refPath); err == nil && info.IsDir() {
		return refPath
	}
	return filepath.Dir(refPath)
}

// controlledSkillName 受控技能名提取（Python: :483-491
// _controlled_skill_name——dict 取 skill_name / str 原样）。
func controlledSkillName(caseMapping map[string]any) string {
	treatment, exists := caseMapping[ControlledSkillTreatmentCaseKey]
	if !exists {
		return ""
	}
	switch v := treatment.(type) {
	case map[string]any:
		return strings.TrimSpace(stringOrTP(v["skill_name"], ""))
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
}

// stageCaseAssets 舞台考题资产进工作区（容器名非空时 docker cp 进容器）。
// Python: case_backend.py:493-506 _stage_case_assets。
func stageCaseAssets(caseMapping map[string]any, workspacePath, containerName string) error {
	// Python: copy_public_assets(case, workspace) → [(relative, path)]
	copied, err := dataloader.CopyPublicAssets(caseMapping, workspacePath)
	if err != nil {
		// Python: except (OSError, ValueError, ...) → 基建错误。
		return evalerrors.NewEvaluationInfrastructureError(fmt.Sprintf("dataset assets could not be prepared: %v", err))
	}
	if containerName == "" {
		return nil
	}
	// Python L478-482：逐资产 docker exec mkdir -p + docker cp 进容器 /testbed。
	// CopyPublicAssets 返回 (relative, 宿主路径) 二元组。
	for _, pair := range copied {
		target := "/testbed/" + filepath.ToSlash(pair[0])
		if _, err := runDocker([]string{"docker", "exec", containerName, "mkdir", "-p", "--", filepath.ToSlash(filepath.Dir(target))}, true, 120); err != nil {
			return evalerrors.NewEvaluationInfrastructureError(fmt.Sprintf("dataset assets could not be prepared: %v", err))
		}
		if _, err := runDocker([]string{"docker", "cp", pair[1], containerName + ":" + target}, true, 120); err != nil {
			return evalerrors.NewEvaluationInfrastructureError(fmt.Sprintf("dataset assets could not be prepared: %v", err))
		}
	}
	return nil
}

// swebenchTestbedEnvironment 直接使用镜像内 testbed 解释器而不跑 conda 激活
// （Python case_backend.py:821 _swebench_testbed_environment：SWE-bench 镜像经
// login-shell profile 激活环境；逐次重放 profile 昂贵且 docker exec 超时会残留
// conda 激活进程）。
func swebenchTestbedEnvironment() map[string]string {
	return map[string]string{
		"CONDA_DEFAULT_ENV": "testbed",
		"CONDA_PREFIX":      "/opt/miniconda3/envs/testbed",
		"PATH": "/opt/miniconda3/envs/testbed/bin:/opt/miniconda3/bin:" +
			"/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"PYTHONNOUSERSITE": "1",
	}
}

// commandRecorderToList 录音机 → metadata command_log（nil → nil）。
func commandRecorderToList(recorder *TerminalBenchCommandRecorder) []map[string]any {
	if recorder == nil {
		return nil
	}
	return recorder.ToList()
}

// taskInputOf 任务输入（Python: :508-510 _case_inputs = task_input(case)）。
func taskInputOf(caseMapping map[string]any) any {
	task, err := dataloader.TaskInput(caseMapping)
	if err != nil {
		return nil
	}
	return task
}

// artifactFilesFromCase 考题引用的产物文件清单（Python: :512-531
// _artifact_files_from_case）。
func artifactFilesFromCase(caseMapping map[string]any, taskText string) []string {
	var candidates []any
	if taskText != "" {
		candidates = append(candidates, taskText)
	}
	for _, key := range []string{"reference", "expected_output", "metadata"} {
		value, exists := caseMapping[key]
		if !exists {
			continue
		}
		if m, isMap := value.(map[string]any); isMap {
			candidates = append(candidates, m)
		} else if text, isStr := value.(string); isStr {
			candidates = append(candidates, text)
		}
	}
	files := []string{}
	seen := map[string]struct{}{}
	for _, value := range candidates {
		for _, file := range artifactFilesFromValue(value) {
			if _, dup := seen[file]; dup {
				continue
			}
			seen[file] = struct{}{}
			files = append(files, file)
		}
	}
	return files
}

// artifactFilesFromValue 值内产物文件名提取（Python: :533-553
// _artifact_files_from_value——正则 + 七个 mapping 键递归）。
func artifactFilesFromValue(value any) []string {
	switch v := value.(type) {
	case string:
		return artifactFilesFromText(v)
	case map[string]any:
		files := []string{}
		for _, key := range []string{
			"expected_artifacts", "required_files", "output_files", "final_files",
			"expected_files", "deliverables", "gold_answer_or_expected_artifact",
		} {
			if inner, exists := v[key]; exists {
				files = append(files, artifactFilesFromValue(inner)...)
			}
		}
		return files
	case []any:
		files := []string{}
		for _, item := range v {
			files = append(files, artifactFilesFromValue(item)...)
		}
		return files
	default:
		return []string{}
	}
}

// artifactFileExtensions 产物扩展名集合（Python 正则里的 (?:html|css|...)）。
var artifactFileExtensions = map[string]struct{}{
	".html": {}, ".css": {}, ".js": {}, ".md": {}, ".json": {},
	".yaml": {}, ".yml": {}, ".py": {}, ".txt": {},
}

// isArtifactNameChar 名称字符判定（Python [\w.-]）。
func isArtifactNameChar(c byte) bool {
	return c == '_' || c == '.' || c == '-' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// artifactFilesFromText 文本内产物文件名提取。
// Python: _artifact_files_from_value 字符串分支正则
// (?<![\w/.-])[\w.-]+\.(?:html|...)(?![\w/.-])——Go regexp 不支持 lookaround，
// 手工扫描等价实现（边界：前邻/后邻字符不得为字母数字下划线点连字符）。
func artifactFilesFromText(text string) []string {
	files := []string{}
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		if !isArtifactNameChar(byte(runes[i])) {
			continue
		}
		start := i
		for i < len(runes) && isArtifactNameChar(byte(runes[i])) {
			i++
		}
		token := string(runes[start:i])
		ext := strings.ToLower(filepath.Ext(token))
		if _, ok := artifactFileExtensions[ext]; !ok {
			i = start
			continue
		}
		// 前邻边界：start-1 不得为名称字符（Python lookbehind 含排除 . - /）。
		if start > 0 && isArtifactNameChar(byte(runes[start-1])) {
			i = start
			continue
		}
		// 后邻边界：i 不得为名称字符。
		if i < len(runes) && isArtifactNameChar(byte(runes[i])) {
			i = start
			continue
		}
		files = append(files, token)
	}
	return files
}

// ──────────────────────────── 输入归一 ────────────────────────────

// normalizeCaseInputForBackend 暴露容器工作区而不改存储的任务输入。
// Python: case_backend.py:555-566 _normalize_case_input_for_backend——文案逐字。
func normalizeCaseInputForBackend(caseMapping map[string]any, value any) any {
	if _, isSwebench := caseMapping["swebench"].(map[string]any); isSwebench {
		if text, isStr := value.(string); isStr {
			return "Execution environment: the repository is mounted at " +
				"`/testbed` inside the task container. Shell commands already run there. " +
				"Use `bash` for repository file inspection and edits; do not guess another path or " +
				"use host filesystem tools.\n\n" + text
		}
	}
	return value
}

// resolveSingleHarnessRef harness 引用解析（Python: :568-574
// _resolve_single_harness_ref——恰好一个非空引用）。
func resolveSingleHarnessRef(harnessRefs map[string]string) (string, string, error) {
	refs := map[string]string{}
	for role, path := range harnessRefs {
		if strings.TrimSpace(path) != "" {
			refs[role] = path
		}
	}
	if len(refs) != 1 {
		return "", "", evalerrors.NewEvaluationInfrastructureError(fmt.Sprintf(
			"single_harness backend requires exactly one harness ref; got %d", len(refs)))
	}
	for role, path := range refs {
		return role, path, nil
	}
	return "", "", nil
}

// ──────────────────────────── 工作区机械 ────────────────────────────

// prepareSingleHarnessWorkspace 准备独立 harness 工作区。
// Python: case_backend.py:576-592 _prepare_single_harness_workspace。
func prepareSingleHarnessWorkspace(caseMapping map[string]any, workspaceDir string) error {
	sourceValue := strings.TrimSpace(stringOrTP(caseMapping["workspace_source_dir"], ""))
	if sourceValue == "" {
		if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
			return err
		}
		return nil
	}
	sourceDir := absPath(sourceValue)
	if info, err := os.Stat(sourceDir); err != nil || !info.IsDir() {
		// Python: FileNotFoundError。
		return fmt.Errorf("workspace_source_dir not found: %s", sourceDir)
	}
	if _, err := os.Stat(workspaceDir); err == nil {
		if rmErr := os.RemoveAll(workspaceDir); rmErr != nil {
			return rmErr
		}
	}
	if err := copyWorkspaceTree(sourceDir, workspaceDir); err != nil {
		return err
	}
	configureGitLFLineEndings(workspaceDir)
	return nil
}

// singleHarnessWorkspaceDir 工作区目录（Python: :594-606
// _single_harness_workspace_dir——Windows + 外部工作区走短路径哈希）。
func singleHarnessWorkspaceDir(caseMapping map[string]any, outputDir, sessionID string) string {
	hasExternalWorkspace := strings.TrimSpace(stringOrTP(caseMapping["workspace_source_dir"], "")) != "" ||
		func() bool { _, ok := caseMapping["swebench"].(map[string]any); return ok }()
	if !runningOnWindows() || !hasExternalWorkspace {
		return filepath.Join(outputDir, "workspace")
	}
	// Python: runtime_root = Path(".local/w"); safe = sha256(f"{dir}\0{session}")[:16]
	runtimeRoot := ".local/w"
	workspaceKey := absPath(outputDir) + "\x00" + sessionID
	sum := sha256.Sum256([]byte(workspaceKey))
	return filepath.Join(runtimeRoot, fmt.Sprintf("%x", sum[:])[:16])
}

// copyWorkspaceTree 拷贝工作区树（Python: :608-637 _copy_workspace_tree——
// Windows 走 robocopy /E；其余 copytree）。
func copyWorkspaceTree(sourceDir, workspaceDir string) error {
	if runningOnWindows() {
		// Python: robocopy ... /E /R:1 /W:1 ...（returncode > 7 报错）。
		return fmt.Errorf("robocopy is required to prepare a Windows evaluation workspace")
	}
	return copyTreeAll(sourceDir, workspaceDir)
}

// configureGitLFLineEndings 保持 Terminal-Bench git 工作区字节稳定（Windows）。
// Python: case_backend.py:639-668 _configure_git_lf_line_endings——core.autocrlf=false
// / core.eol=lf / core.safecrlf=false；git 不存在则跳过。
func configureGitLFLineEndings(workspaceDir string) {
	gitExec, err := findGitExecutable()
	if err != nil {
		return
	}
	seen := map[string]struct{}{}
	repoDirs := []string{}
	if info, err := os.Stat(filepath.Join(workspaceDir, ".git")); err == nil && info.IsDir() {
		repoDirs = append(repoDirs, workspaceDir)
		seen[workspaceDir] = struct{}{}
	}
	_ = filepath.Walk(workspaceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() || info.Name() != ".git" {
			return nil
		}
		repoDir := filepath.Dir(path)
		if _, dup := seen[repoDir]; dup {
			return nil
		}
		seen[repoDir] = struct{}{}
		repoDirs = append(repoDirs, repoDir)
		return nil
	})
	for _, repoDir := range repoDirs {
		for _, pair := range [][2]string{
			{"core.autocrlf", "false"},
			{"core.eol", "lf"},
			{"core.safecrlf", "false"},
		} {
			// Python: subprocess.run(check=False)——失败静默。
			runGitConfig(gitExec, repoDir, pair[0], pair[1])
		}
	}
}

// ──────────────────────────── 系统提示词 ────────────────────────────

// singleHarnessSystemPrompt 独立评估 agent 系统提示词。
// Python: case_backend.py:670-680 _single_harness_system_prompt（文案逐字）。
func singleHarnessSystemPrompt(roleName, workspacePath string) string {
	base := fmt.Sprintf("You are the standalone evaluation agent for role `%s`. "+
		"Solve the given task with the bound expert harness and available local tools. "+
		"Write any produced files under the current workspace unless the task explicitly "+
		"names another working directory. Preserve existing line endings when editing files; "+
		"Terminal-Bench verifiers may compare exact file hashes.", roleName)
	if workspacePath != "" {
		return base + fmt.Sprintf(" Your task workspace and default output directory is `%s`. "+
			"Loaded plugin and skill directories are read-only capability sources, not task "+
			"workspaces. Do not write deliverables, temporary files or validation reports there.",
			workspacePath)
	}
	return base
}

// ──────────────────────────── metadata ────────────────────────────

// singleHarnessMetadataParams _single_harness_metadata 入参包。
type singleHarnessMetadataParams struct {
	roleName                 string
	workspaceBefore          map[string]map[string]any
	workspaceAfter           map[string]map[string]any
	teamSkillRefPath         string
	controlledSkillTreatment map[string]any
	skillTriggers            []map[string]any
	commandRecorder          []map[string]any
	swebenchModelPatchPath   string
	solverMaxIterations      int
	runtimeConfig            map[string]any
	executionTermination     map[string]any
	runtimeLogs              []string
	executionEvidence        map[string]any
	evidenceError            string
}

// singleHarnessMetadata 组装执行 metadata。
// Python: case_backend.py:682-722 _single_harness_metadata。
func singleHarnessMetadata(params singleHarnessMetadataParams) map[string]any {
	metadata := map[string]any{
		"member_id":             params.roleName,
		"member_role":           params.roleName,
		"solver_max_iterations": params.solverMaxIterations,
		"runtime_config":        params.runtimeConfig,
		"execution_termination": params.executionTermination,
		"runtime_logs":          params.runtimeLogs,
		"execution_evidence":    params.executionEvidence,
		"workspace_changes":     workspaceChanges(params.workspaceBefore, params.workspaceAfter),
	}
	if params.executionEvidence == nil {
		metadata["execution_evidence"] = map[string]any{"status": "incomplete", "reasons": []string{"capture_not_started"}}
	}
	if teamSkill := singleHarnessTeamSkillMetadata(params.teamSkillRefPath); teamSkill != nil {
		metadata["team_skill"] = teamSkill
	}
	if params.controlledSkillTreatment != nil {
		metadata["controlled_skill_treatment"] = params.controlledSkillTreatment
	}
	if len(params.skillTriggers) > 0 {
		metadata["skill_triggers"] = params.skillTriggers
	}
	if params.commandRecorder != nil {
		metadata["command_log"] = params.commandRecorder
	}
	if params.swebenchModelPatchPath != "" {
		metadata["swebench_model_patch_path"] = params.swebenchModelPatchPath
	}
	if params.evidenceError != "" {
		metadata["evidence_error"] = params.evidenceError
	}
	return metadata
}

// singleHarnessTeamSkillMetadata 团队技能元数据。
// Python: case_backend.py:695-722 _single_harness_team_skill_metadata——
// SKILL.md 可读时附 sha256。
func singleHarnessTeamSkillMetadata(teamSkillRefPath string) map[string]any {
	if teamSkillRefPath == "" {
		return nil
	}
	skillDir := resolveSkillDir(teamSkillRefPath)
	metadata := map[string]any{
		"ref_path":      teamSkillRefPath,
		"skills_root":   filepath.Dir(skillDir),
		"enabled_skill": filepath.Base(skillDir),
		"skill_mode":    "all",
	}
	raw, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		return metadata
	}
	sum := sha256.Sum256(raw)
	metadata["skill_md_sha256"] = fmt.Sprintf("%x", sum)
	return metadata
}

// controlledEvidenceOf 受控治疗证据提取。
func controlledEvidenceOf(rail *ControlledSkillTreatmentRail) map[string]any {
	if rail == nil {
		return nil
	}
	return rail.Evidence()
}

// collectSkillTriggers 汇总各技能轨道的任务触发记录。
// Python: [record for rail in skill_use_rails for record in rail.task_trigger_records()]
func collectSkillTriggers(rails []*RSISkillUseRail) []map[string]any {
	records := []map[string]any{}
	for _, rail := range rails {
		records = append(records, rail.TaskTriggerRecords()...)
	}
	return records
}

// ──────────────────────────── 快照与变更 ────────────────────────────

// prunedWalkDirs 快照剪除目录（Python: _snapshot_workspace pruned_dirs）。
var prunedWalkDirs = map[string]struct{}{
	".git": {}, ".mypy_cache": {}, ".pytest_cache": {}, ".ruff_cache": {},
	".venv": {}, "__pycache__": {}, "node_modules": {}, "venv": {},
}

// snapshotWorkspace 有界工作区文件快照。
// Python: case_backend.py:724-756 _snapshot_workspace——size + mtime_ns。
func snapshotWorkspace(workspaceDir string) map[string]map[string]any {
	if info, err := os.Stat(workspaceDir); err != nil || !info.IsDir() {
		return map[string]map[string]any{}
	}
	snapshot := map[string]map[string]any{}
	var filePaths []string
	_ = filepath.Walk(workspaceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if err == nil && info.IsDir() {
				// Python: dirs[:] = [name for name in dirs if name not in pruned_dirs]
				if _, pruned := prunedWalkDirs[info.Name()]; pruned && path != workspaceDir {
					return filepath.SkipDir
				}
			}
			return nil
		}
		filePaths = append(filePaths, path)
		return nil
	})
	sort.Strings(filePaths)
	for _, path := range filePaths {
		rel, err := filepath.Rel(workspaceDir, path)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if skipTraceSnapshotPath(rel) {
			continue
		}
		stat, err := os.Stat(path)
		if err != nil {
			continue
		}
		snapshot[rel] = map[string]any{"size": stat.Size(), "mtime_ns": stat.ModTime().UnixNano()}
	}
	return snapshot
}

// workspaceChanges 前后快照对比。
// Python: case_backend.py:758-772 _workspace_changes——added/modified/removed
// 各截前 200。
func workspaceChanges(before, after map[string]map[string]any) map[string][]string {
	added := []string{}
	modified := []string{}
	removed := []string{}
	for path := range after {
		if _, existed := before[path]; !existed {
			added = append(added, path)
		}
	}
	for path, beforeEntry := range before {
		afterEntry, existed := after[path]
		if !existed {
			removed = append(removed, path)
			continue
		}
		if fmt.Sprint(beforeEntry["size"]) != fmt.Sprint(afterEntry["size"]) ||
			beforeEntry["mtime_ns"] != afterEntry["mtime_ns"] {
			modified = append(modified, path)
		}
	}
	sort.Strings(added)
	sort.Strings(modified)
	sort.Strings(removed)
	const limit = 200
	if len(added) > limit {
		added = added[:limit]
	}
	if len(modified) > limit {
		modified = modified[:limit]
	}
	if len(removed) > limit {
		removed = removed[:limit]
	}
	return map[string][]string{"added": added, "modified": modified, "removed": removed}
}

// skipTraceSnapshotPath 快照路径跳过判定。
// Python: case_backend.py:774-788 _skip_trace_snapshot_path。
func skipTraceSnapshotPath(path string) bool {
	normalized := strings.TrimPrefix(filepath.ToSlash(path), "./")
	parts := strings.Split(normalized, "/")
	if len(parts) == 0 || (len(parts) == 1 && parts[0] == "") {
		return true
	}
	if isRuntimeWorkspaceMetadata(normalized) {
		return true
	}
	switch parts[0] {
	case ".git", "__pycache__", ".pytest_cache", ".mypy_cache", ".ruff_cache":
		return true
	}
	for _, part := range parts {
		if part == "node_modules" || part == ".venv" || part == "venv" {
			return true
		}
	}
	switch strings.ToLower(filepath.Ext(normalized)) {
	case ".pyc", ".pyo", ".log":
		return true
	}
	return false
}

// isRuntimeWorkspaceMetadata 运行时自有文件判定。
// Python: case_backend.py:790-804 _is_runtime_workspace_metadata。
func isRuntimeWorkspaceMetadata(path string) bool {
	normalized := strings.TrimPrefix(filepath.ToSlash(path), "./")
	switch normalized {
	case "AGENT.md", "SOUL.md", "IDENTITY.md", "USER.md", "HEARTBEAT.md":
		return true
	}
	parts := strings.Split(normalized, "/")
	if len(parts) == 0 || (len(parts) == 1 && parts[0] == "") {
		return true
	}
	switch parts[0] {
	case "context", "memory", "messages", "todo":
		return true
	}
	if (parts[0] == "agents" || parts[0] == "skills") && parts[len(parts)-1] == ".workspace" {
		return true
	}
	return false
}

// ──────────────────────────── 轨道挂载与装配 ────────────────────────────

// attachTrajectoryRailParams _attach_single_harness_trajectory_rail 入参包。
type attachTrajectoryRailParams struct {
	outputDir string
	roleName  string
}

// attachSingleHarnessTrajectoryRail 持久化原生工具调用供候选能力门读取。
// Python: case_backend.py:395-419 _attach_single_harness_trajectory_rail——
// 偏差登记：Go 观测为 demand 单例（无 additional_span_processors 注入、无
// acquire/release 生命周期），初始化段省略；RoleFileTrajectoryStore 一致。
func attachSingleHarnessTrajectoryRail(_ context.Context, agent *harness.DeepAgent,
	params attachTrajectoryRailParams) *RSITrajectoryRail {
	traceRoot := filepath.Join(params.outputDir, RoleTrajectoryDirName)
	role := params.roleName
	if role == "" {
		role = "solver"
	}
	store := NewRoleFileTrajectoryStore(traceRoot, role, true)
	rail := NewRSITrajectoryRail(evolutionpkg.WithTrajectoryStore(store))
	agent.AddRail(rail)
	return rail
}

// enforceContainerSysOperationRail 容器模式收口 workspace 编辑。
// Python: case_backend.py:331-341 _enforce_container_sys_operation_rail——
// SkillUseRail.include_tools=false + 摘除 SysOperationRail + 重挂 shell_only 版。
func enforceContainerSysOperationRail(ctx context.Context, agent *harness.DeepAgent) error {
	skillType := reflect.TypeOf(&skillsrail.SkillUseRail{})
	for _, rail := range agent.FindRailsByType(skillType) {
		if sr, ok := rail.(*skillsrail.SkillUseRail); ok {
			sr.SetIncludeTools(false)
		}
	}
	sysType := reflect.TypeOf(&railspkg.SysOperationRail{})
	agent.StripRailsByType(sysType)
	rsiSysOp := NewRSISysOperationRail(true, true, true)
	return agent.RegisterRail(ctx, rsiSysOp)
}

// runAgentForCase 经空响应恢复跑 agent。
// Python: run_agent_with_empty_response_recovery(agent, {"query": ...}, session=...)
// ——偏差：Go Runner.RunAgent 签名差异，经闭包对齐（见 AgentInvokeFunc 登记）。
func runAgentForCase(ctx context.Context, agent *harness.DeepAgent, query any, sessionID string) (any, error) {
	invoke := func(_ context.Context, inputs map[string]any) (map[string]any, error) {
		return runDeepAgent(ctx, agent, inputs, sessionID)
	}
	result, err := RunAgentWithEmptyResponseRecovery(ctx, invoke, map[string]any{"query": query})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return result, nil
}

// ──────────────────────────── 杂项助手 ────────────────────────────

// absPath 展开并取绝对路径（Python Path(...).expanduser().resolve()）。
func absPath(path string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// workspaceOf 构造工作区对象（Python create_deep_agent(workspace=str(dir))）。
func workspaceOf(workspaceDir string) *workspace.Workspace {
	return workspace.NewWorkspace(workspaceDir, "en")
}

// demandProcessor 进程级轨迹 processor 单例。
func demandProcessor() *trajectory.TrajectorySpanProcessor {
	if p := demand.GetTrajectorySpanProcessor(); p != nil {
		if tp, ok := p.(*trajectory.TrajectorySpanProcessor); ok {
			return tp
		}
	}
	return nil
}

// collectJSONLLogs 收集 logs 目录下相对路径 JSONL 清单（排序）。
// Python: sorted((case_root / "logs").rglob("*.jsonl"))。
func collectJSONLLogs(logRoot string) ([]string, error) {
	var paths []string
	err := filepath.Walk(logRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".jsonl") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	rel := make([]string, 0, len(paths))
	caseRoot := filepath.Dir(logRoot)
	for _, path := range paths {
		r, err := filepath.Rel(caseRoot, path)
		if err != nil {
			continue
		}
		rel = append(rel, filepath.ToSlash(r))
	}
	return rel, nil
}

// marshalIndentSafe 缩进 JSON（Python json.dumps(indent=2)）。
func marshalIndentSafe(value map[string]any) ([]byte, error) {
	return json.MarshalIndent(value, "", "  ")
}

// toFloatAny 数值转换（timeout_sec 缺省 1800 用）。
func toFloatAny(raw any) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// ifThen 条件取值（Python 三元表达式等价）。
func ifThen(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// runningOnWindows 平台判定（Python os.name == "nt"）。
func runningOnWindows() bool { return os.PathSeparator == '\\' }

// stringOrTPAny 任意值字符串化兜底。
func stringOrTPAny(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

// compileArtifactPattern 编译产物文件名模式（Go regexp 不支持 lookbehind——
// 用捕获组边界等价实现，Python 原式 (?<![\w/.-])...(?!...)
// 语义：前后邻字符不得为字母数字下划线点连字符）。
func compileArtifactPattern() *regexp.Regexp {
	return regexp.MustCompile(`(?:^|[\w/.-])?([\w.-]+\.(?:html|css|js|md|json|ya?ml|py|txt))(?=$|[\w/.-])`)
}

// findGitExecutable 定位 git（Python shutil.which("git")）。
func findGitExecutable() (string, error) { return exec.LookPath("git") }

// runGitConfig 写一条 git config（Python subprocess.run(check=False)）。
func runGitConfig(gitExec, repoDir, key, value string) {
	_ = exec.Command(gitExec, "-C", repoDir, "config", key, value).Run()
}

// copyTreeAll 全树拷贝（Python shutil.copytree）。
func copyTreeAll(sourceDir, workspaceDir string) error {
	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(sourceDir, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(workspaceDir, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(target, raw, info.Mode().Perm())
	})
}

// runDeepAgent 经 Runner 跑一次 agent invoke。
// Python: Runner.run_agent(agent, inputs, session=session_id)——Go Runner.RunAgent
// 以 AgentRef/SessionRef 寻址运行时注册对象；DeepAgent 实例直调内部执行链
// （偏差登记：实例化运行 vs 注册寻址，回收条件=Runner 实例化 API 落地）。
func runDeepAgent(ctx context.Context, agent *harness.DeepAgent, inputs map[string]any, sessionID string) (map[string]any, error) {
	// 偏差：sessionID 以 Runner 会话注册寻址（Go Invoke 无 session 形参），
	// 会话上下文由考场装配段绑定；此处直调实例。
	return agent.Invoke(ctx, inputs)
}
