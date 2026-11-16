// mechanism_attributor.go 第 2 步：病根归因——角色内部失败机制诊断。
// Python: member_optimizer/mechanism_attributor.py（全量对齐，523 行）。
// 只处理第 1 步 assigned_role_issues 中的角色；使用封闭的
// failure_signature 与 mechanism_type 分类法。

package memberoptimizer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
)

// errMechanismAttributionFailed 编排层任务失败哨兵（Python RuntimeError 等价）。
var errMechanismAttributionFailed = errors.New("Mechanism attribution task failed")

// mechanismTypeValues 排除 rail 后的机制类型集合（Python module 常量）。
// Python: mechanism_attributor.py:17 MECHANISM_TYPE_VALUES。
// 枚举真值清单见 schema.go MechanismType；此处为过滤视图。
// failure_signature / optimization_surface 同理（Python :18-19）。

// ──────────────────────────── 结构体 ────────────────────────────

// MechanismAttributorAgent LLM-as-Judge 病根归因 agent。
// Python: mechanism_attributor.py:94-222 MechanismAttributorAgent。
type MechanismAttributorAgent struct {
	// modelConfigRef 模型配置文件引用
	modelConfigRef string
	// retryLimit 归因重试上限
	retryLimit int
	// outputRepair 输出修复配置
	outputRepair harnessrsi.OutputRepairConfig
	// workspace 工作空间路径（空串等价 Python None）
	workspace string
	// agentSkillsDirs agent 技能目录
	agentSkillsDirs []string
	// prebuiltAgent 预构建 DeepAgent（测试注入点，等价 Python monkeypatch）
	prebuiltAgent any
}

// MechanismAttributor 编排全部已定责角色的病根归因。
// Python: mechanism_attributor.py:335-523 MechanismAttributor。
type MechanismAttributor struct {
	// outputRepair 输出修复配置
	outputRepair harnessrsi.OutputRepairConfig
	// agent 预注入 agent（nil 则按参数构造）
	agent *MechanismAttributorAgent
	// semaphore 并发上限（Python asyncio.Semaphore(max_concurrent_roles)）
	semaphore chan struct{}
}

// MechanismAttributeParams Attribute 关键字参数收拢（Python attribute(...) 关键字）。
type MechanismAttributeParams struct {
	// RoleAttributionReport 第 1 步定责报告
	RoleAttributionReport *RoleAttributionReport
	// EvalRef 评估引用
	EvalRef *EvalRef
	// CandidateRoles 候选角色（Python list[Any]；实际 MemberRoleCandidate）
	CandidateRoles []MemberRoleCandidate
	// ModelConfigRef 模型配置引用
	ModelConfigRef string
	// AttributionRetryLimit 归因重试上限（默认 2）
	AttributionRetryLimit int
	// MaxCasesPerIssue 每问题最大案例数（默认 3）
	MaxCasesPerIssue int
	// MaxTraceChars 轨迹字符上限（默认 4000）
	MaxTraceChars int
	// MaxResultChars 结果字符上限（默认 2000）
	MaxResultChars int
	// AgentWorkspace agent 工作空间
	AgentWorkspace string
	// AgentSkillsDirs agent 技能目录
	AgentSkillsDirs []string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateMechanismAttributionOutput 校验病根归因输出，返回错误清单。
// Python: mechanism_attributor.py:48-91 _validate_mechanism_attribution_output。
func ValidateMechanismAttributionOutput(raw map[string]any, assignedRoles map[string]bool) []string {
	// Python: errors: list[str] = []
	var errs []string
	// Python: if "role" not in raw: ...; elif raw["role"] not in assigned_roles: ...
	if _, ok := raw["role"]; !ok {
		errs = append(errs, "missing field: role")
	} else if !assignedRoles[fmt.Sprintf("%v", raw["role"])] {
		errs = append(errs, fmt.Sprintf("role '%v' not in assigned roles", raw["role"]))
	}
	// Python: attributions = raw.get("attributions"); if not isinstance(list): ...
	attributions, ok := raw["attributions"].([]any)
	if !ok {
		errs = append(errs, "attributions must be a list")
		return errs
	}
	// Python: for i, attr in enumerate(attributions): ...
	for i, attr := range attributions {
		attrMap, isMap := attr.(map[string]any)
		if !isMap {
			errs = append(errs, fmt.Sprintf("attributions[%d] must be an object", i))
			continue
		}
		// Python: if "issue_id" not in attr: errors.append(f"attributions[{i}]: missing issue_id")
		if _, ok := attrMap["issue_id"]; !ok {
			errs = append(errs, fmt.Sprintf("attributions[%d]: missing issue_id", i))
		}
		// Python: mt = attr.get("mechanism_type", ""); if mt not in MECHANISM_TYPE_VALUES: ...
		if mt := pyStr(attrMap["mechanism_type"]); !mechanismTypeEnumValid(mt) {
			errs = append(errs, fmt.Sprintf("attributions[%d]: unknown mechanism_type '%s'", i, mt))
		}
		// Python: fs = attr.get("failure_signature", ""); ...
		if fs := pyStr(attrMap["failure_signature"]); !failureSignatureEnumValid(fs) {
			errs = append(errs, fmt.Sprintf("attributions[%d]: unknown failure_signature '%s'", i, fs))
		}
		// Python: surface = str(attr.get("optimization_surface", "") or ""); ...
		surface := strings.TrimSpace(pyStr(attrMap["optimization_surface"]))
		if surface != "" && !optimizationSurfaceEnumValid(surface) {
			errs = append(errs, fmt.Sprintf("attributions[%d]: unknown optimization_surface '%s'", i, surface))
		}
		// Python: if surface == "none" and (mt != ... or fs != ...): errors.append(...)
		if surface == "none" {
			mt := pyStr(attrMap["mechanism_type"])
			fs := pyStr(attrMap["failure_signature"])
			if mt != "insufficient_role_evidence" || fs != "insufficient_role_evidence" {
				errs = append(errs, fmt.Sprintf(
					"attributions[%d]: optimization_surface 'none' requires insufficient_role_evidence mechanism and signature", i))
			}
		}
		// Python: try: conf = float(attr.get("confidence", -1)); ...
		conf, isNum := attrMap["confidence"].(float64)
		if !isNum {
			errs = append(errs, fmt.Sprintf("attributions[%d]: confidence is not a number", i))
		} else if conf < 0.0 || conf > 1.0 {
			errs = append(errs, fmt.Sprintf("attributions[%d]: confidence %v out of range", i, conf))
		}
	}
	return errs
}

// SummarizeTrainingSignal 摘要训练信号元数据。
// Python: mechanism_attributor.py:300-313 _summarize_training_signal。
func SummarizeTrainingSignal(metadata any) string {
	// Python: if not isinstance(metadata, dict): return "none"
	metaMap, ok := metadata.(map[string]any)
	if !ok {
		return "none"
	}
	// Python: signal = metadata.get("training_signal"); if not isinstance(dict): return "none"
	signal, ok := metaMap["training_signal"].(map[string]any)
	if !ok {
		return "none"
	}
	// Python: compact = {...四字段...}; return str(compact)
	compact := map[string]any{
		"expected_failure_modes": pyOrNone(signal["expected_failure_modes"]),
		"capability_gap":         pyStr(signal["capability_gap"])[:pyMin(len(pyStr(signal["capability_gap"])), 500)],
		"target_surfaces":        pyOrNone(signal["target_surfaces"]),
		"difficulty_rationale":   pyStr(signal["difficulty_rationale"])[:pyMin(len(pyStr(signal["difficulty_rationale"])), 500)],
	}
	return pyDictReprOrdered(compact)
}

// SummarizeRoleIssueAttribution 摘要定责证据的归因字典（前 4 条）。
// Python: mechanism_attributor.py:315-333 _summarize_role_issue_attribution。
func SummarizeRoleIssueAttribution(evidence []map[string]any) string {
	// Python: compact = []; for item in evidence[:4]: ...
	var compact []map[string]any
	for _, item := range evidence[:pyMin(len(evidence), 4)] {
		attribution, ok := item["attribution"].(map[string]any)
		if !ok {
			continue
		}
		compact = append(compact, map[string]any{
			"target_ref":        pyStr(attribution["target_ref"]),
			"root_cause":        pyStr(attribution["root_cause"])[:pyMin(len(pyStr(attribution["root_cause"])), 500)],
			"critical_mistake":  pyStr(attribution["critical_mistake"])[:pyMin(len(pyStr(attribution["critical_mistake"])), 500)],
			"general_mechanism": pyStr(attribution["general_mechanism"])[:pyMin(len(pyStr(attribution["general_mechanism"])), 500)],
			"evidence_refs":     pyOrNone(attribution["evidence_refs"]),
		})
	}
	// Python: return str(compact) if compact else "none"
	if len(compact) == 0 {
		return "none"
	}
	return pyListStr(compact)
}

// NewMechanismAttributorAgent 构造病根归因 agent（Python __init__）。
func NewMechanismAttributorAgent(modelConfigRef string, attributionRetryLimit int, workspace string, agentSkillsDirs []string, outputRepair *harnessrsi.OutputRepairConfig) *MechanismAttributorAgent {
	// Python: self._output_repair = output_repair or OutputRepairConfig()
	repair := harnessrsi.DefaultOutputRepairConfig()
	if outputRepair != nil {
		repair = *outputRepair
	}
	return &MechanismAttributorAgent{
		modelConfigRef:  modelConfigRef,
		retryLimit:      attributionRetryLimit,
		outputRepair:    repair,
		workspace:       workspace,
		agentSkillsDirs: append([]string{}, agentSkillsDirs...),
	}
}

// NewMechanismAttributorAgentWithAgent 以预构建 agent 构造（测试注入点）。
func NewMechanismAttributorAgentWithAgent(modelConfigRef string, attributionRetryLimit int) *MechanismAttributorAgent {
	agent := NewMechanismAttributorAgent(modelConfigRef, attributionRetryLimit, "", nil, nil)
	agent.prebuiltAgent = "fake-deep-agent"
	return agent
}

// AttributeRoleMechanisms 归因单个角色的全部问题。
// Python: mechanism_attributor.py:111-165 attribute_role_mechanisms。
func (a *MechanismAttributorAgent) AttributeRoleMechanisms(ctx context.Context, role string, roleIssues []RoleIssueAttribution, caseEvidence map[string]map[string]map[string]any, harnessSummary string) ([]RoleMechanismAttribution, error) {
	// Python: agent = create_mechanism_attribution_agent(...)
	agent, err := a.createDeepAgent(ctx)
	if err != nil {
		return nil, err
	}
	// Python: user_message = self._build_user_message(...)
	userMessage := BuildMechanismAttributionUserMessage(role, roleIssues, caseEvidence, harnessSummary)
	// Python: candidate_roles = {ri.role for ri in role_issues}
	assignedRoles := map[string]bool{}
	for _, ri := range roleIssues {
		assignedRoles[ri.Role] = true
	}
	// Python: raw = await invoke_member_optimizer_agent_structured(...)
	raw, err := agentsBridge.InvokeStructured(ctx, StructuredInvokeParams{
		Agent:       agent,
		AgentName:   "MechanismAttributorAgent[" + role + "]",
		UserMessage: userMessage,
		SessionID:   "mech_attr_" + role,
		RetryLimit:  a.retryLimit,
		OutputRepair: &a.outputRepair,
		ParseResponse:     agentsBridge.ParseYamlOrJSONObject,
		ValidateResponse:  func(data map[string]any) []string { return ValidateMechanismAttributionOutput(data, assignedRoles) },
		BuildRetryMessage: func(_ map[string]any, errText string) string {
			// Python: lambda _previous, error: self._build_retry_message(user_message, str(error))
			return MechanismAttributorAgentBuildRetryMessage(userMessage, errText)
		},
	})
	if err != nil {
		return nil, err
	}
	// Python: attributions = []; for attr_data in raw.get("attributions", []): ...
	var attributions []RoleMechanismAttribution
	attrList, _ := raw["attributions"].([]any)
	for _, attrData := range attrList {
		attrMap, ok := attrData.(map[string]any)
		if !ok {
			continue
		}
		// Python: role=raw.get("role", role)（raw 顶层 role 优先）
		attributionRole := role
		if rawRole, ok := raw["role"].(string); ok && rawRole != "" {
			attributionRole = rawRole
		}
		evidenceRefs := pyOrNoneTraceRefsFromAny(attrMap["evidence_refs"])
		attributions = append(attributions, RoleMechanismAttribution{
			IssueID:            pyStr(attrMap["issue_id"]),
			Role:               attributionRole,
			MechanismType:      pyStr(attrMap["mechanism_type"]),
			FailureSignature:   pyStr(attrMap["failure_signature"]),
			Confidence:         pyFloatOrZero(attrMap["confidence"]),
			OptimizationSurface: pyStr(attrMap["optimization_surface"]),
			Evidence:           pyEvidenceAnyToMaps(attrMap["evidence"]),
			EvidenceRefs:       evidenceRefs,
			Rationale:          pyStr(attrMap["rationale"]),
		})
	}
	return attributions, nil
}

// MechanismAttributorAgentBuildRetryMessage 构造重试消息。
// Python: mechanism_attributor.py:216-221 _build_retry_message。
func MechanismAttributorAgentBuildRetryMessage(original string, errText string) string {
	// Python: f"{original}\n\n## Validation Error\n\n{error}\n\nPlease correct ..."
	return original + "\n\n## Validation Error\n\n" + errText + "\n\nPlease correct the JSON output and return a valid response."
}

// BuildMechanismAttributionUserMessage 构造病根归因用户消息。
// Python: mechanism_attributor.py:168-214 _build_user_message。
func BuildMechanismAttributionUserMessage(role string, roleIssues []RoleIssueAttribution, caseEvidence map[string]map[string]map[string]any, harnessSummary string) string {
	// Python: issues_text（逐条 issue_id/confidence/rationale/evidence_summary/analyzer_attribution）
	var issueLines []string
	for _, ri := range roleIssues {
		summary := ""
		if len(ri.Evidence) > 0 {
			summary = pyStr(ri.Evidence[0]["summary"])
		}
		issueLines = append(issueLines,
			"  - issue_id: "+ri.IssueID+
				"\n    confidence: "+pyStr(ri.Confidence)+
				"\n    rationale: "+ri.Rationale+
				"\n    evidence_summary: "+summary+
				"\n    analyzer_attribution: "+SummarizeRoleIssueAttribution(ri.Evidence))
	}
	issuesText := strings.Join(issueLines, "\n")
	// Python: evidence_text（逐案 status/response[:300]/evaluation/training_signal/behavior_trace）
	var caseLines []string
	for cid, ev := range caseEvidence {
		result := ev["result"]
		if result == nil {
			result = map[string]any{}
		}
		trace := ev["trace"]
		if trace == nil {
			trace = map[string]any{}
		}
		metadata := result["metadata"]
		if metadata == nil {
			metadata = map[string]any{}
		}
		behaviorTrace := trace["behavior_trace"]
		if behaviorTrace == nil {
			behaviorTrace = map[string]any{}
		}
		responseText := pyStr(result["response"])
		caseLines = append(caseLines,
			"  Case "+cid+":"+
				"\n    status: "+pyStr(result["status"])+
				"\n    response: "+responseText[:pyMin(len(responseText), 300)]+
				"\n    evaluation: "+pyStr(trace["evaluation"])+
				"\n    training_signal: "+SummarizeTrainingSignal(metadata)+
				"\n    behavior_trace: "+SummarizeBehaviorTrace(behaviorTrace))
	}
	// Python: dict 迭代序为插入序；Go map 无序——按 case_id 排序保证确定性（键序偏差登记 spec §4）
	keys := make([]string, 0, len(caseEvidence))
	for cid := range caseEvidence {
		keys = append(keys, cid)
	}
	sort.Strings(keys)
	evidenceText := strings.Join(caseLines, "\n")
	// Python: f-string 模板
	return "## Role to Diagnose\n\nrole: " + role +
		"\nNumber of attributed issues: " + fmt.Sprintf("%d", len(roleIssues)) +
		"\n\n## Attributed Issues\n\n" + issuesText +
		"\n\n## Case Evidence (Bounded Excerpts)\n\n" + evidenceText +
		"\n\n## Role Harness Summary\n\n" + harnessSummary +
		"\n\n## Output\n\nReturn ONLY a JSON object as specified in the system prompt.\n"
}

// NewMechanismAttributor 构造编排器（Python __init__）。
func NewMechanismAttributor(maxConcurrentRoles int, outputRepair *harnessrsi.OutputRepairConfig) *MechanismAttributor {
	repair := harnessrsi.DefaultOutputRepairConfig()
	if outputRepair != nil {
		repair = *outputRepair
	}
	// Python: self._semaphore = asyncio.Semaphore(max_concurrent_roles)
	if maxConcurrentRoles <= 0 {
		maxConcurrentRoles = 1
	}
	return &MechanismAttributor{
		outputRepair: repair,
		semaphore:    make(chan struct{}, maxConcurrentRoles),
	}
}

// NewMechanismAttributorWithAgent 以预注入 agent 构造（Python mechanism_attributor_agent 参数）。
func NewMechanismAttributorWithAgent(agent *MechanismAttributorAgent, maxConcurrentRoles int) *MechanismAttributor {
	attr := NewMechanismAttributor(maxConcurrentRoles, nil)
	attr.agent = agent
	return attr
}

// Attribute 对全部已定责角色执行病根归因。
// Python: mechanism_attributor.py:365-466 attribute。
func (o *MechanismAttributor) Attribute(ctx context.Context, params MechanismAttributeParams) (*MechanismAttributionReport, error) {
	// Python 默认值对齐（def 签名默认）
	if params.AttributionRetryLimit == 0 {
		params.AttributionRetryLimit = 2
	}
	if params.MaxCasesPerIssue == 0 {
		params.MaxCasesPerIssue = 3
	}
	if params.MaxTraceChars == 0 {
		params.MaxTraceChars = 4000
	}
	if params.MaxResultChars == 0 {
		params.MaxResultChars = 2000
	}
	// Python: agent = self._get_agent(...)
	agent := o.getAgent(params.ModelConfigRef, params.AttributionRetryLimit, params.AgentWorkspace, params.AgentSkillsDirs)

	// Python: assigned_roles = {ri.role for ri in ...}; if not assigned_roles: 空报告
	assignedRoles := map[string]bool{}
	for _, ri := range params.RoleAttributionReport.AssignedRoleIssues {
		assignedRoles[ri.Role] = true
	}
	if len(assignedRoles) == 0 {
		return &MechanismAttributionReport{
			MechanismAttributionID: "mechanism_attribution_" + shortHex8(),
			SourceRoleAttributionPath: "",
			RoleMechanisms:         map[string][]RoleMechanismAttribution{},
			SkippedRoles:           []string{},
			Warnings:               []string{"No assigned roles to process"},
			Metadata:               map[string]any{"model_config_ref": params.ModelConfigRef},
		}, nil
	}
	// Python: grouped = {}; for ri: grouped.setdefault(role, []).append(ri)
	grouped := map[string][]RoleIssueAttribution{}
	for _, ri := range params.RoleAttributionReport.AssignedRoleIssues {
		grouped[ri.Role] = append(grouped[ri.Role], ri)
	}
	// Python: harness_refs = {r.role: r for r in candidate_roles}
	harnessRefs := map[string]MemberRoleCandidate{}
	for _, r := range params.CandidateRoles {
		harnessRefs[r.Role] = r
	}

	type mechResult struct {
		role         string
		attributions []RoleMechanismAttribution
		err          error
	}
	// Python: tasks = [...]; asyncio.gather(..., return_exceptions=True)
	roles := make([]string, 0, len(grouped))
	for role := range grouped {
		roles = append(roles, role)
	}
	sort.Strings(roles) // Go map 无序——排序保证结果处理序确定性（Python gather 保序按 tasks 列表序；等价键序偏差）
	results := make([]mechResult, len(roles))
	var wg sync.WaitGroup
	for i, role := range roles {
		wg.Add(1)
		go func(idx int, role string) {
			defer wg.Done()
			// Python: async with self._semaphore:
			o.semaphore <- struct{}{}
			defer func() { <-o.semaphore }()
			// Python: all_case_ids = {ref["case_id"] for ri in role_issues for ref in ri.trace_refs if "case_id" in ref}
			allCaseIDs := map[string]bool{}
			for _, ri := range grouped[role] {
				for _, ref := range ri.TraceRefs {
					if cid, ok := ref["case_id"]; ok {
						allCaseIDs[cid] = true
					}
				}
			}
			caseIDList := make([]string, 0, len(allCaseIDs))
			for cid := range allCaseIDs {
				caseIDList = append(caseIDList, cid)
			}
			// Python: case_evidence = load_case_evidence(...)
			caseEvidence := LoadCaseEvidence(params.EvalRef, caseIDList, params.MaxCasesPerIssue, params.MaxTraceChars, params.MaxResultChars)
			// Python: harness_summary = self._build_harness_summary(...)
			harnessSummary := BuildHarnessSummary(role, grouped[role], harnessRefs)
			// Python: attributions = await agent.attribute_role_mechanisms(...)
			attributions, err := agent.AttributeRoleMechanisms(ctx, role, grouped[role], caseEvidence, harnessSummary)
			if err != nil {
				results[idx] = mechResult{role: role, err: err}
				return
			}
			results[idx] = mechResult{role: role, attributions: attributions}
		}(i, role)
	}
	wg.Wait()

	// Python: for result in results: if Exception: raise RuntimeError(f"Mechanism attribution task failed: ...")
	roleMechanisms := map[string][]RoleMechanismAttribution{}
	var skippedRoles []string
	var warnings []string
	for _, result := range results {
		if result.err != nil {
			return nil, fmt.Errorf("%w: %v", errMechanismAttributionFailed, result.err)
		}
		if result.attributions != nil {
			roleMechanisms[result.role] = result.attributions
		} else {
			skippedRoles = append(skippedRoles, result.role)
			warnings = append(warnings, "role "+result.role+": agent returned no attributions")
		}
	}
	// Python: return MechanismAttributionReport(...)
	return &MechanismAttributionReport{
		MechanismAttributionID: "mechanism_attribution_" + shortHex8(),
		SourceRoleAttributionPath: "",
		RoleMechanisms:         roleMechanisms,
		SkippedRoles:           skippedRoles,
		Warnings:               warnings,
		Metadata: map[string]any{
			"model_config_ref": params.ModelConfigRef,
			"roles_processed":  pyStrListSorted(roleMechanisms),
			"roles_skipped":    pyStrList(skippedRoles),
		},
	}, nil
}

// BuildHarnessSummary 构造角色 harness 摘要。
// Python: mechanism_attributor.py:468-477 _build_harness_summary。
func BuildHarnessSummary(role string, _ []RoleIssueAttribution, harnessRefs map[string]MemberRoleCandidate) string {
	// Python: harness_ref = harness_refs.get(role); if harness_ref: ...
	if harnessRef, ok := harnessRefs[role]; ok {
		return "Role: " + role + ", Harness: " + harnessRef.HarnessRefPath + ", Description: " + harnessRef.Description
	}
	// Python: return f"Role: {role} (no harness ref available)"
	return "Role: " + role + " (no harness ref available)"
}

// WriteMechanismAttributionReport 落盘病根归因报告（mechanism_attribution.yaml）。
// Python: mechanism_attributor.py:479-521 write_report。
func WriteMechanismAttributionReport(report *MechanismAttributionReport, outputDir string) (string, error) {
	// Python: path = output_dir / "mechanism_attribution.yaml"; path.parent.mkdir(...)
	path := filepath.Join(outputDir, "mechanism_attribution.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	// Python: mechanisms_by_role = {role: [{...九字段...} for a in attributions]}
	mechanismsByRole := map[string]any{}
	for role, attributions := range report.RoleMechanisms {
		list := make([]any, 0, len(attributions))
		for _, a := range attributions {
			list = append(list, map[string]any{
				"issue_id":             a.IssueID,
				"role":                 a.Role,
				"mechanism_type":       a.MechanismType,
				"failure_signature":    a.FailureSignature,
				"confidence":           a.Confidence,
				"optimization_surface": a.OptimizationSurface,
				"evidence":             pyOrNoneList(a.Evidence),
				"evidence_refs":        pyOrNoneTraceRefs(a.EvidenceRefs),
				"rationale":            a.Rationale,
			})
		}
		mechanismsByRole[role] = list
	}
	// Python: payload = {...}; yaml.safe_dump(payload, allow_unicode=True, sort_keys=False)
	payload := map[string]any{
		"mechanism_attribution_id":   report.MechanismAttributionID,
		"source_role_attribution_path": report.SourceRoleAttributionPath,
		"role_mechanisms":            mechanismsByRole,
		"skipped_roles":              pyStrList(report.SkippedRoles),
		"warnings":                   pyStrList(report.Warnings),
		"metadata":                   pyOrNone(report.Metadata),
	}
	data, err := yaml.Marshal(payload)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// createDeepAgent 创建病根归因 DeepAgent。
// Python: mechanism_attributor.py 无独立方法（agent 内联 create_mechanism_attribution_agent）。
func (a *MechanismAttributorAgent) createDeepAgent(ctx context.Context) (any, error) {
	// 测试注入点：等价 Python monkeypatch
	if a.prebuiltAgent != nil {
		return a.prebuiltAgent, nil
	}
	// Python: create_mechanism_attribution_agent(model_config_ref=..., workspace=self._workspace or ".", agent_skills_dirs=...)
	workspace := a.workspace
	if workspace == "" {
		workspace = "."
	}
	if agentsBridge.CreateMechanismAttributionAgent == nil {
		return nil, errors.New("agents bridge not registered: import member_optimizer/agents (blank) to wire implementation")
	}
	return agentsBridge.CreateMechanismAttributionAgent(ctx, AttributionAgentParams{
		ModelConfigRef:  a.modelConfigRef,
		Workspace:       workspace,
		AgentSkillsDirs: a.agentSkillsDirs,
	})
}

// getAgent 取预注入 agent 或按参数构造。
// Python: mechanism_attributor.py:348-363 _get_agent。
func (o *MechanismAttributor) getAgent(modelConfigRef string, attributionRetryLimit int, agentWorkspace string, agentSkillsDirs []string) *MechanismAttributorAgent {
	// Python: if self._agent is not None: return self._agent
	if o.agent != nil {
		return o.agent
	}
	// Python: return MechanismAttributorAgent(...)
	return NewMechanismAttributorAgent(modelConfigRef, attributionRetryLimit, agentWorkspace, agentSkillsDirs, &o.outputRepair)
}

// mechanismTypeEnumValid mechanism_type 是否在封闭分类内（排除 rail）。
// Python: mechanism_attributor.py:17 MECHANISM_TYPE_VALUES。
func mechanismTypeEnumValid(value string) bool {
	switch value {
	case "prompt", "instruction", "workflow", "skill", "tool", "mcp",
		"memory", "context", "config", "knowledge", "insufficient_role_evidence":
		return true
	}
	return false
}

// failureSignatureEnumValid failure_signature 是否在封闭分类内（排除 config_or_rail_mismatch）。
// Python: mechanism_attributor.py:18-19 FAILURE_SIGNATURE_VALUES。
func failureSignatureEnumValid(value string) bool {
	switch value {
	case "repeated_failure_loop", "tool_call_failure", "stalled_objective",
		"missed_exploration_or_capability", "stale_memory_or_context",
		"skill_code_failure", "subagent_or_workflow_handoff_failure",
		"prompt_instruction_mismatch", "insufficient_role_evidence":
		return true
	}
	return false
}

// optimizationSurfaceEnumValid optimization_surface 是否在封闭分类内（排除 rail）。
// Python: mechanism_attributor.py:19 OPTIMIZATION_SURFACE_VALUES。
func optimizationSurfaceEnumValid(value string) bool {
	switch value {
	case "none", "config", "prompt_section", "identity", "soul", "skill", "tool":
		return true
	}
	return false
}

// pyEvidenceAnyToMaps any → []map[string]any（nil 安全）。
func pyEvidenceAnyToMaps(value any) []map[string]any {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, isMap := item.(map[string]any); isMap {
			out = append(out, m)
		}
	}
	return out
}

// pyOrNoneTraceRefsFromAny any → []map[string]string（nil 安全，空返回 []）。
func pyOrNoneTraceRefsFromAny(value any) []map[string]string {
	list, ok := value.([]any)
	if !ok || len(list) == 0 {
		return []map[string]string{}
	}
	out := make([]map[string]string, 0, len(list))
	for _, item := range list {
		if m, isMap := item.(map[string]any); isMap {
			converted := map[string]string{}
			for key, v := range m {
				converted[key] = pyStr(v)
			}
			out = append(out, converted)
		}
	}
	return out
}

// pyDictReprOrdered 等价 Python str(dict)（Python 3.7+ 保插入序；Go 按固定序输出保证确定性）。
func pyDictReprOrdered(m map[string]any) string {
	return pyRepr(m)
}

// pyStrListSorted 角色键排序转 any 列表。
func pyStrListSorted(roleMechanisms map[string][]RoleMechanismAttribution) any {
	keys := make([]string, 0, len(roleMechanisms))
	for role := range roleMechanisms {
		keys = append(keys, role)
	}
	sort.Strings(keys)
	return pyStrList(keys)
}
