// loader.go 提供 member 优化的上游工件载入（17.4b1 切片）。
// Python: openjiuwen/rsi/harness_rsi/member_optimizer/loader.py。
// 本节仅落地 evaluation_result_analyzer 实际使用的符号闭包：
// EvalRef / load_eval_ref / resolve_candidate_roles 及其私有依赖。
// load_analysis_ref / resolve_team_issues / AnalysisUnavailableError /
// BoundedEvidenceBundle / load_case_evidence 及其依赖 ⤴️ 17.4c 补齐。

package memberoptimizer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvalRef eval_ref.yaml 的解析结果。
// Python: loader.py:23-68 EvalRef（dataclass → Go struct，偏差 D-5）。
type EvalRef struct {
	// EvalID 评估 ID
	EvalID string
	// TeamName 团队名
	TeamName string
	// TeamSkillRefPath team skill 引用路径
	TeamSkillRefPath string
	// HarnessRefsPath harness refs 引用路径
	HarnessRefsPath string
	// EvalDir 评估目录
	EvalDir string
	// CaseResultsDir 案例结果目录
	CaseResultsDir string
	// CaseTracesDir 案例轨迹目录
	CaseTracesDir string
	// SummaryPath 汇总文件路径（可缺失）
	SummaryPath *string
	// Cases 案例原始条目
	Cases []map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// EvalRefFromDict 从原始 mapping 构建 EvalRef。
// Python: loader.py:37-68 EvalRef.from_dict
func EvalRefFromDict(data map[string]any, basePath *string) EvalRef {
	// Python: eval_dir = Path(data.get("eval_dir", ".")); 相对则拼 base_path
	evalDir := stringField(data, "eval_dir", ".")
	evalDir = joinWithBase(evalDir, basePath)

	// Python: cases = data.get("cases", [])
	var cases []map[string]any
	if rawCases, ok := data["cases"].([]any); ok {
		for _, item := range rawCases {
			if caseMap, isMap := item.(map[string]any); isMap {
				cases = append(cases, caseMap)
			}
		}
	}

	// Python: results_dir / traces_dir 默认 results / traces，相对则拼 base_path
	resultsDir := joinWithBase(stringField(data, "case_results_dir", "results"), basePath)
	tracesDir := joinWithBase(stringField(data, "case_traces_dir", "traces"), basePath)

	// Python: summary_path 存在且非空才解析，相对则拼 base_path
	var summaryPath *string
	if raw, ok := data["summary_path"]; ok {
		if str, isStr := raw.(string); isStr && str != "" {
			resolved := joinWithBase(str, basePath)
			summaryPath = &resolved
		}
	}

	return EvalRef{
		EvalID:           stringField(data, "eval_id", ""),
		TeamName:         stringField(data, "team_name", ""),
		TeamSkillRefPath: stringField(data, "team_skill_ref_path", ""),
		HarnessRefsPath:  stringField(data, "harness_refs_path", ""),
		EvalDir:          evalDir,
		CaseResultsDir:   resultsDir,
		CaseTracesDir:    tracesDir,
		SummaryPath:      summaryPath,
		Cases:            cases,
	}
}

// LoadEvalRef 载入并解析 eval_ref.yaml。
// 相对 eval_dir 等字段基于 yaml 所在目录解析。
// Python: loader.py:147-165 load_eval_ref
func LoadEvalRef(evalRefPath string) (EvalRef, error) {
	// Python: path = Path(eval_ref_path).expanduser().resolve()
	path, err := expandUser(evalRefPath)
	if err != nil {
		return EvalRef{}, err
	}
	if path, err = filepath.Abs(path); err != nil {
		return EvalRef{}, err
	}
	// Python: if not path.is_file(): raise FileNotFoundError(...)
	if info, statErr := os.Stat(path); statErr != nil || info.IsDir() {
		return EvalRef{}, fmt.Errorf("eval_ref not found: %s", path)
	}

	// Python: data = yaml.safe_load(f) or {}
	data, err := loadYAMLMapping(path)
	if err != nil {
		return EvalRef{}, err
	}

	// Python: eval_dir 相对则基于 path.parent；from_dict 内再次解析（已绝对则保持）
	parent := dirOf(path)

	// Python: return EvalRef.from_dict(data, base_path=path.parent)
	return EvalRefFromDict(data, &parent), nil
}

// ResolveCandidateRoles 从 harness refs 与可选 Team Skill 绑定解析候选角色。
// 解析顺序（spec Section 2.6.3）：
//  1. 直接传入的 harnessRefsPath
//  2. 回落 evalRef.HarnessRefsPath
//  3. 空列表（由调用方决定 no-op 或失败）
//
// Python: loader.py:254-283 resolve_candidate_roles
func ResolveCandidateRoles(harnessRefsPath string, evalRef *EvalRef, teamSkillRefPath string) []MemberRoleCandidate {
	var candidates []MemberRoleCandidate

	// Python: refs_path = _normalize_harness_refs_path(harness_refs_path)
	refsPath := normalizeHarnessRefsPath(harnessRefsPath)
	if refsPath == "" {
		refsPath = normalizeHarnessRefsPath(evalRef.HarnessRefsPath)
	}

	// Python: if refs_path is None: return candidates
	if refsPath == "" {
		return candidates
	}

	// Python: is_file → _parse_harness_refs_file；is_dir → _parse_harness_refs_dir
	if info, err := os.Stat(refsPath); err == nil {
		if !info.IsDir() {
			candidates = parseHarnessRefsFile(refsPath)
		} else {
			candidates = parseHarnessRefsDir(refsPath)
		}
	}

	// Python: if not candidates and team_skill_ref_path: 兜底
	if len(candidates) == 0 && teamSkillRefPath != "" {
		candidates = supplementFromTeamSkill(teamSkillRefPath, evalRef.EvalDir)
	}

	return candidates
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeHarnessRefsPath 归一化 harness refs 路径：空/不存在返回空串。
// Python: loader.py:247-251 _normalize_harness_refs_path
func normalizeHarnessRefsPath(harnessRefsPath string) string {
	if harnessRefsPath == "" {
		return ""
	}
	p, err := expandUser(harnessRefsPath)
	if err != nil {
		return ""
	}
	if p, err = filepath.Abs(p); err != nil {
		return ""
	}
	// Python: return p if p.exists() else None
	if _, statErr := os.Stat(p); statErr != nil {
		return ""
	}
	return p
}

// resolveHarnessRefPath 相对 harness ref 基于 refs 文件位置解析。
// Python: loader.py:286-293 _resolve_harness_ref_path
func resolveHarnessRefPath(ref any, refsPath string) string {
	str, isStr := ref.(string)
	if !isStr || strings.TrimSpace(str) == "" {
		return ""
	}
	path, err := expandUser(str)
	if err != nil {
		return ""
	}
	if filepath.IsAbs(path) {
		return path
	}
	// Python: str((refs_path.parent / path).resolve())
	resolved, err := filepath.Abs(filepath.Join(filepath.Dir(refsPath), path))
	if err != nil {
		return ""
	}
	return resolved
}

// parseHarnessRefsFile 解析 harness_refs.yaml / .json 为候选角色列表。
// Python: loader.py:296-351 _parse_harness_refs_file
func parseHarnessRefsFile(refsPath string) []MemberRoleCandidate {
	data, err := loadMappingByExt(refsPath)
	if err != nil {
		data = map[string]any{}
	}

	var candidates []MemberRoleCandidate

	// Python: if "roles" in data and isinstance(list): ...
	if roles, ok := data["roles"].([]any); ok {
		for _, entry := range roles {
			roleEntry, isMap := entry.(map[string]any)
			if !isMap {
				continue
			}
			candidates = append(candidates, MemberRoleCandidate{
				Role:           stringField(roleEntry, "role", ""),
				MemberName:     stringField(roleEntry, "member_name", ""),
				HarnessRefPath: resolveHarnessRefPath(roleEntry["harness_ref_path"], refsPath),
				Description:    stringField(roleEntry, "description", ""),
				Metadata:       mapField(roleEntry, "metadata"),
			})
		}
		return candidates
	}

	// Python: harness_refs = data.get("harness_refs", {}); isinstance(dict)
	if harnessRefs, ok := data["harness_refs"].(map[string]any); ok {
		for role, ref := range harnessRefs {
			if _, isStr := ref.(string); !isStr {
				continue
			}
			candidates = append(candidates, MemberRoleCandidate{
				Role:           role,
				MemberName:     role,
				HarnessRefPath: resolveHarnessRefPath(ref, refsPath),
				Description:    "",
				Metadata:       map[string]any{"source": "harness_refs"},
			})
		}
		return candidates
	}

	// Python: 裸 mapping：跳过 version / source 键
	for role, ref := range data {
		if _, isStr := ref.(string); isStr && role != "version" && role != "source" {
			candidates = append(candidates, MemberRoleCandidate{
				Role:           role,
				MemberName:     role,
				HarnessRefPath: resolveHarnessRefPath(ref, refsPath),
				Description:    "",
				Metadata:       map[string]any{"source": "harness_refs"},
			})
		}
	}
	return candidates
}

// parseHarnessRefsDir 解析 harness refs 目录：子目录名成为角色（按目录名排序）。
// Python: loader.py:354-368 _parse_harness_refs_dir
func parseHarnessRefsDir(refsPath string) []MemberRoleCandidate {
	var candidates []MemberRoleCandidate
	entries, err := os.ReadDir(refsPath)
	if err != nil {
		return candidates
	}
	// Python: sorted(refs_path.iterdir())（按名字排序）
	names := make([]string, 0, len(entries))
	byName := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
			byName[entry.Name()] = filepath.Join(refsPath, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		candidates = append(candidates, MemberRoleCandidate{
			Role:           name,
			MemberName:     name,
			HarnessRefPath: byName[name],
			Description:    "",
			Metadata:       map[string]any{"source": "directory"},
		})
	}
	return candidates
}

// supplementFromTeamSkill 从 TeamAgentSpec 绑定补充候选角色。
// Python: loader.py:371-414 _supplement_from_team_skill
func supplementFromTeamSkill(teamSkillRefPath string, basePath string) []MemberRoleCandidate {
	var candidates []MemberRoleCandidate
	// Python: path = base_path / team_skill_ref_path if not absolute
	path := teamSkillRefPath
	if !filepath.IsAbs(path) && basePath != "" {
		path = filepath.Join(basePath, path)
	}

	if info, statErr := os.Stat(path); statErr != nil || info.IsDir() {
		return candidates
	}

	data, err := loadMappingByExt(path)
	if err != nil {
		return candidates
	}

	// Python: leader = data.get("leader", {}); member_name
	if leader, ok := data["leader"].(map[string]any); ok {
		if memberName := stringField(leader, "member_name", ""); memberName != "" {
			candidates = append(candidates, MemberRoleCandidate{
				Role:           memberName,
				MemberName:     memberName,
				HarnessRefPath: "",
				Description:    "Team leader",
				Metadata:       map[string]any{"source": "team_skill_binding"},
			})
		}
	}

	// Python: for member in data.get("predefined_members", [])
	if members, ok := data["predefined_members"].([]any); ok {
		for _, entry := range members {
			member, isMap := entry.(map[string]any)
			if !isMap {
				continue
			}
			if mname := stringField(member, "member_name", ""); mname != "" {
				candidates = append(candidates, MemberRoleCandidate{
					Role:           mname,
					MemberName:     mname,
					HarnessRefPath: stringField(member, "harness_ref_path", ""),
					Description:    stringField(member, "description", ""),
					Metadata:       map[string]any{"source": "team_skill_binding"},
				})
			}
		}
	}

	return candidates
}

// loadYAMLMapping 读取 YAML 文件为 mapping（空文件得空 mapping）。
// Python: loader.py:158-159 / 183-184 / 199-200 的 yaml.safe_load(f) or {}
func loadYAMLMapping(path string) (map[string]any, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := yaml.Unmarshal(content, &data); err != nil {
		return nil, err
	}
	if data == nil {
		data = map[string]any{}
	}
	return data, nil
}

// loadMappingByExt 按后缀分派 YAML/JSON 读取 mapping。
// Python: loader.py:298-302 / 382-386
func loadMappingByExt(path string) (map[string]any, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	var data map[string]any
	if ext == ".yaml" || ext == ".yml" {
		if err := yaml.Unmarshal(content, &data); err != nil {
			return nil, err
		}
	} else {
		if err := json.Unmarshal(content, &data); err != nil {
			return nil, err
		}
	}
	if data == nil {
		data = map[string]any{}
	}
	return data, nil
}

// stringField 读取 mapping 的字符串字段（缺失/类型不符取默认值）。
func stringField(data map[string]any, key, fallback string) string {
	if raw, ok := data[key]; ok {
		if str, isStr := raw.(string); isStr {
			return str
		}
	}
	return fallback
}

// mapField 读取 mapping 字段（缺失/类型不符得空 map）。
func mapField(data map[string]any, key string) map[string]any {
	if raw, ok := data[key]; ok {
		if m, isMap := raw.(map[string]any); isMap {
			return m
		}
	}
	return map[string]any{}
}

// joinWithBase 相对路径拼 basePath（绝对路径或 nil basePath 保持原样）。
// Python: if base_path is not None and not p.is_absolute(): p = base_path / p
func joinWithBase(path string, basePath *string) string {
	if basePath == nil || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(*basePath, path)
}

// dirOf 返回路径的父目录。
func dirOf(path string) string {
	return filepath.Dir(path)
}
