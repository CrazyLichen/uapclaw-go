// role_attributor.go 第 1 步：角色定责——LLM-as-Judge 把 TeamIssue 归属到角色。
// Python: member_optimizer/role_attributor.py（全量对齐，674 行）。
// 核心智能由 RoleAttributorAgent（DeepAgent）提供；外层组件负责输入装载、
// 证据打包、校验、重试与产物落盘。

package memberoptimizer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
	agentface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/member_optimizer/agentface"
)

// errRoleAttributionFailed 编排层任务失败哨兵（Python RuntimeError 等价）。
var errRoleAttributionFailed = errors.New("Role attribution task failed")

// ──────────────────────────── 结构体 ────────────────────────────

// RoleAttributorAgent LLM-as-Judge 定责 agent。
// Python: role_attributor.py:142-273 RoleAttributorAgent。
type RoleAttributorAgent struct {
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
	// prebuiltAgent 预构建 DeepAgent（Python 无此字段；测试注入点，
	// 等价 Python 测试对 _create_deep_agent 的 monkeypatch）
	prebuiltAgent *agentface.Agent
}

// RoleAttributor 编排全部 TeamIssue 的角色定责。
// Python: role_attributor.py:382-600 RoleAttributor。
type RoleAttributor struct {
	// outputRepair 输出修复配置
	outputRepair harnessrsi.OutputRepairConfig
	// agent 预注入 agent（nil 则按参数构造）
	agent *RoleAttributorAgent
	// semaphore 并发上限（Python asyncio.Semaphore(max_concurrent_issues)）
	semaphore chan struct{}
}

// AttributeParams Attribute 关键字参数收拢（Python attribute(...) 关键字）。
type AttributeParams struct {
	// EvalRef 评估引用
	EvalRef *EvalRef
	// TeamIssues 待定责问题清单
	TeamIssues []harnessrsi.TeamIssue
	// CandidateRoles 候选角色清单
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

// ValidateRoleAttributionOutput 校验定责输出，返回错误清单。
// Python: role_attributor.py:43-67 _validate_role_attribution_output。
func ValidateRoleAttributionOutput(raw map[string]any, candidateRoles []string) []string {
	// Python: errors: list[str] = []
	var errs []string
	// Python: if "issue_id" not in raw: errors.append("missing field: issue_id")
	if _, ok := raw["issue_id"]; !ok {
		errs = append(errs, "missing field: issue_id")
	}
	// Python: if "decision" not in raw: errors.append("missing field: decision")
	if _, ok := raw["decision"]; !ok {
		errs = append(errs, "missing field: decision")
	}
	// Python: if raw.get("decision") == "assigned": ...
	if raw["decision"] == "assigned" {
		if _, ok := raw["role"]; !ok {
			errs = append(errs, "missing field: role for assigned decision")
		} else {
			role := raw["role"]
			found := false
			for _, candidate := range candidateRoles {
				if candidate == role {
					found = true
					break
				}
			}
			if !found {
				errs = append(errs, fmt.Sprintf("role '%v' not in candidate_roles", role))
			}
		}
	}
	// Python: if "confidence" not in raw: ... else: try float(...)
	if _, ok := raw["confidence"]; !ok {
		errs = append(errs, "missing field: confidence")
	} else {
		conf, ok := raw["confidence"].(float64)
		if !ok {
			errs = append(errs, fmt.Sprintf("confidence is not a number: %v", raw["confidence"]))
		} else if conf < 0.0 || conf > 1.0 {
			errs = append(errs, fmt.Sprintf("confidence %v out of range [0, 1]", conf))
		}
	}
	return errs
}

// NormalizeMemberKey 归一化成员键（小写、连字符转下划线）。
// Python: role_attributor.py:70-71 _normalize_member_key。
func NormalizeMemberKey(value any) string {
	// Python: return str(value or "").strip().lower().replace("-", "_")
	text := ""
	if value != nil {
		text = fmt.Sprintf("%v", value)
	}
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(text)), "-", "_")
}

// IsSingleRoleAliasKey 是否单角色别名键。
// Python: role_attributor.py:74-84 _is_single_role_alias_key。
func IsSingleRoleAliasKey(value string) bool {
	return map[string]bool{
		"solver": true, "agent": true, "assistant": true, "default": true,
		"single_agent": true, "single_harness": true, "worker": true,
	}[value]
}

// TargetMembersForIssue 提取问题的目标成员（空则回落 affected_component）。
// Python: role_attributor.py:86-96 _target_members_for_issue。
func TargetMembersForIssue(issue harnessrsi.TeamIssue) []string {
	// Python: targets: list[str] = []
	var targets []string
	// Python: for item in issue.target_members: if isinstance(str) and item.strip()
	for _, item := range issue.TargetMembers {
		if strings.TrimSpace(item) != "" {
			targets = append(targets, strings.TrimSpace(item))
		}
	}
	// Python: if not targets: affected_component = issue.metadata.get("affected_component")
	if len(targets) == 0 {
		if component, ok := issue.Metadata["affected_component"]; ok {
			if text, isStr := component.(string); isStr && strings.TrimSpace(text) != "" {
				targets = append(targets, strings.TrimSpace(text))
			}
		}
	}
	// Python: return list(dict.fromkeys(targets))（保序去重）
	seen := map[string]bool{}
	deduped := targets[:0]
	for _, item := range targets {
		if !seen[item] {
			seen[item] = true
			deduped = append(deduped, item)
		}
	}
	return deduped
}

// MatchTargetMember 把 analyzer 的 target_members 解析到唯一候选角色。
// Python: role_attributor.py:98-133 _match_target_member。
func MatchTargetMember(issue harnessrsi.TeamIssue, candidateRoles []MemberRoleCandidate) (*MemberRoleCandidate, string) {
	// Python: if _normalize_member_key(issue.optimization_target) != "member_harness"
	if NormalizeMemberKey(issue.OptimizationTarget) != "member_harness" {
		return nil, "not_member_harness"
	}
	// Python: target_members = _target_members_for_issue(issue); if not target_members
	targetMembers := TargetMembersForIssue(issue)
	if len(targetMembers) == 0 {
		return nil, "missing_target_members"
	}
	// Python: target_keys = {normalized(item) for item in target_members if item}
	targetKeys := map[string]bool{}
	for _, item := range targetMembers {
		if item != "" {
			targetKeys[NormalizeMemberKey(item)] = true
		}
	}
	// Python: for candidate: keys = {role, member_name} ∪ aliases ∪ member_id
	var matches []MemberRoleCandidate
	for _, candidate := range candidateRoles {
		candidateKeys := map[string]bool{
			NormalizeMemberKey(candidate.Role):       true,
			NormalizeMemberKey(candidate.MemberName): true,
		}
		if aliases, ok := candidate.Metadata["aliases"].([]any); ok {
			for _, alias := range aliases {
				if aliasText, isStr := alias.(string); isStr && aliasText != "" {
					candidateKeys[NormalizeMemberKey(aliasText)] = true
				}
			}
		}
		if stableID, ok := candidate.Metadata["member_id"]; ok && stableID != nil {
			candidateKeys[NormalizeMemberKey(stableID)] = true
		}
		for key := range targetKeys {
			if candidateKeys[key] {
				matches = append(matches, candidate)
				break
			}
		}
	}
	// Python: unique = {(role, member_name, harness_ref_path): candidate ...}
	unique := map[string]MemberRoleCandidate{}
	for _, candidate := range matches {
		key := candidate.Role + "\x00" + candidate.MemberName + "\x00" + candidate.HarnessRefPath
		unique[key] = candidate
	}
	// Python: if len(unique) == 1: return next(iter(...)), "target_members_exact_match"
	if len(unique) == 1 {
		for _, candidate := range unique {
			copied := candidate
			return &copied, "target_members_exact_match"
		}
	}
	// Python: if unique: return None, "target_members_ambiguous"
	if len(unique) > 0 {
		return nil, "target_members_ambiguous"
	}
	// Python: if len(candidate_roles) == 1 and target_keys and all(single_role_alias)
	if len(candidateRoles) == 1 && len(targetKeys) > 0 {
		allAlias := true
		for key := range targetKeys {
			if !IsSingleRoleAliasKey(key) {
				allAlias = false
				break
			}
		}
		if allAlias {
			copied := candidateRoles[0]
			return &copied, "target_members_single_role_alias"
		}
	}
	return nil, "target_members_mismatch"
}

// TraceRefsForIssue 提取轨迹引用（raw evidence_refs 优先，回落 affected_cases）。
// Python: role_attributor.py:135-140 _trace_refs_for_issue。
func TraceRefsForIssue(issue harnessrsi.TeamIssue, raw map[string]any) []map[string]string {
	// Python: refs = raw.get("evidence_refs", []); if isinstance(list) and refs
	if refs, ok := raw["evidence_refs"].([]any); ok && len(refs) > 0 {
		var out []map[string]string
		for _, ref := range refs {
			if refMap, isMap := ref.(map[string]any); isMap {
				converted := map[string]string{}
				for key, value := range refMap {
					converted[key] = fmt.Sprintf("%v", value)
				}
				out = append(out, converted)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	// Python: return [{"case_id": str(case_id)} for case_id in issue.affected_cases if case_id]
	var out []map[string]string
	for _, caseID := range issue.AffectedCases {
		if caseID != "" {
			out = append(out, map[string]string{"case_id": caseID})
		}
	}
	return out
}

// NewRoleAttributorAgent 构造定责 agent（Python __init__）。
func NewRoleAttributorAgent(modelConfigRef string, attributionRetryLimit int, workspace string, agentSkillsDirs []string, outputRepair *harnessrsi.OutputRepairConfig) *RoleAttributorAgent {
	// Python: self._output_repair = output_repair or OutputRepairConfig()
	repair := harnessrsi.DefaultOutputRepairConfig()
	if outputRepair != nil {
		repair = *outputRepair
	}
	return &RoleAttributorAgent{
		modelConfigRef:  modelConfigRef,
		retryLimit:      attributionRetryLimit,
		outputRepair:    repair,
		workspace:       workspace,
		agentSkillsDirs: append([]string{}, agentSkillsDirs...),
	}
}

// NewRoleAttributorAgentWithAgent 以预构建 agent 构造（测试注入点，
// 等价 Python 测试对 _create_deep_agent 的 monkeypatch）。
func NewRoleAttributorAgentWithAgent(modelConfigRef string, attributionRetryLimit int, agent *agentface.Agent) *RoleAttributorAgent {
	agent2 := NewRoleAttributorAgent(modelConfigRef, attributionRetryLimit, "", nil, nil)
	agent2.prebuiltAgent = agent
	return agent2
}

// AttributeIssue 归因单个问题（重试循环由结构化调用承担）。
// Python: role_attributor.py:167-195 attribute_issue。
func (a *RoleAttributorAgent) AttributeIssue(ctx context.Context, evidenceBundle *BoundedEvidenceBundle) (map[string]any, error) {
	// Python: agent = self._create_deep_agent()
	agent, err := a.createDeepAgent(ctx)
	if err != nil {
		return nil, err
	}
	// Python: candidate_roles = [r.role for r in evidence_bundle.candidate_roles]
	candidateRoles := make([]string, 0, len(evidenceBundle.CandidateRoles))
	for _, r := range evidenceBundle.CandidateRoles {
		candidateRoles = append(candidateRoles, r.Role)
	}
	// Python: issue_data = evidence_bundle.issue; session_id = f"role_attr_{issue_data.get('issue_id', 'unknown')}"
	issueID := "unknown"
	if id, ok := evidenceBundle.Issue["issue_id"]; ok && id != nil {
		issueID = fmt.Sprintf("%v", id)
	}
	// Python: return await invoke_member_optimizer_agent_structured(...)
	return agentsBridge.InvokeStructured(ctx, StructuredInvokeParams{
		Agent:             agent,
		AgentName:         "RoleAttributorAgent",
		UserMessage:       BuildRoleAttributionUserMessage(evidenceBundle),
		SessionID:         "role_attr_" + issueID,
		RetryLimit:        a.retryLimit,
		OutputRepair:      &a.outputRepair,
		ParseResponse:     agentsBridge.ParseJSONObject,
		ValidateResponse:  func(raw map[string]any) []string { return ValidateRoleAttributionOutput(raw, candidateRoles) },
		BuildRetryMessage: RoleAttributorAgentBuildRetryMessage,
	})
}

// RoleAttributorAgentBuildRetryMessage 构造重试消息。
// Python: role_attributor.py:197-215 _build_retry_message。
func RoleAttributorAgentBuildRetryMessage(previous map[string]any, errText string) string {
	// Python: error_text = "; ".join(...) if isinstance(error, list) else str(error)
	errorText := errText
	// Python: previous_text = json.dumps(previous, ensure_ascii=False, indent=2) if previous else "{}"
	previousText := "{}"
	if len(previous) > 0 {
		if data, err := json.MarshalIndent(previous, "", "  "); err == nil {
			previousText = string(data)
		}
	}
	return "## Previous Invalid Output\n\n```json\n" + previousText + "\n```\n\n## Validation Error\n\n" +
		errorText + "\n\nReturn ONLY a corrected JSON object matching the role attribution schema.\n"
}

// BuildRoleAttributionUserMessage 构造定责用户消息。
// Python: role_attributor.py:217-273 _build_user_message。
func BuildRoleAttributionUserMessage(evidenceBundle *BoundedEvidenceBundle) string {
	// Python: issue = evidence_bundle.issue; ...
	issue := evidenceBundle.Issue
	// Python: roles_text = "\n".join(f"  - role: ...\n    member_name: ...\n    harness_ref: ...\n    description: ...")
	var roleLines []string
	for _, r := range evidenceBundle.CandidateRoles {
		roleLines = append(roleLines,
			"  - role: "+r.Role+"\n    member_name: "+r.MemberName+"\n    harness_ref: "+r.HarnessRefPath+"\n    description: "+r.Description)
	}
	rolesText := strings.Join(roleLines, "\n")
	// Python: cases_text 组装（case_traces 按 case_id 对齐）
	var casesBuilder strings.Builder
	for _, caseItem := range evidenceBundle.CaseResults {
		caseID, _ := caseItem["case_id"].(string)
		var traceCase map[string]any
		for _, t := range evidenceBundle.CaseTraces {
			if tid, _ := t["case_id"].(string); tid == caseID {
				traceCase = t
				break
			}
		}
		if traceCase == nil {
			traceCase = map[string]any{}
		}
		casesBuilder.WriteString("\n  Case " + caseID + ":")
		casesBuilder.WriteString("\n    status: " + pyStr(caseItem["status"]))
		casesBuilder.WriteString("\n    score: " + pyStr(caseItem["score"]))
		casesBuilder.WriteString("\n    response_summary: " + pyStr(caseItem["response"])[:pyMin(len(pyStr(caseItem["response"])), 200)])
		casesBuilder.WriteString("\n    trace_evaluation: " + pyStr(traceCase["evaluation"]))
		casesBuilder.WriteString("\n    behavior_trace: " + SummarizeBehaviorTrace(traceCase["behavior_trace"]))
	}
	casesText := casesBuilder.String()
	// Python: f-string 模板
	return "## Issue to Attribute\n\n" +
		"- issue_id: " + pyStr(issue["issue_id"]) + "\n" +
		"- category: " + pyStr(issue["category"]) + "\n" +
		"- severity: " + pyStr(issue["severity"]) + "\n" +
		"- summary: " + pyStr(issue["summary"]) + "\n" +
		"- optimization_target: " + pyStr(issue["optimization_target"]) + "\n" +
		"- target_members: " + pyListStr(issue["target_members"]) + "\n" +
		"- affected_component: " + pyStr(issue["affected_component"]) + "\n" +
		"- suspected_team_scope: " + pyStr(issue["suspected_team_scope"]) + "\n" +
		"- recommendation: " + pyStr(issue["recommendation"]) + "\n" +
		"- analyzer_attribution: " + SummarizeIssueAttribution(issue) + "\n" +
		"- issue_evidence: " + SummarizeIssueEvidence(issue["evidence"]) + "\n\n" +
		"## Affected Cases\n\n" + casesText + "\n\n" +
		"## Candidate Roles\n\n" + rolesText + "\n\n" +
		"## Output Instructions\n\n" +
		"Return ONLY a JSON object as specified in the system prompt.\n" +
		"Do not include any explanation outside the JSON.\n"
}

// SummarizeBehaviorTrace 摘要行为轨迹（四段拼接）。
// Python: role_attributor.py:274-345 _summarize_behavior_trace。
func SummarizeBehaviorTrace(behaviorTrace any) string {
	// Python: if not isinstance(behavior_trace, dict): return "none"
	traceMap, isMap := behaviorTrace.(map[string]any)
	if !isMap {
		return "none"
	}
	// Python: workspace_changes = behavior_trace.get("workspace_changes") or {}
	var parts []string
	// Python: workspace_changes 段
	if changes, ok := traceMap["workspace_changes"].(map[string]any); ok {
		added := pyAnyList(changes["added"])
		modified := pyAnyList(changes["modified"])
		removed := pyAnyList(changes["removed"])
		if len(added) > 0 || len(modified) > 0 || len(removed) > 0 {
			parts = append(parts, "workspace_changes=added:"+pyListStr(added[:pyMin(len(added), 12)])+
				", modified:"+pyListStr(modified[:pyMin(len(modified), 12)])+
				", removed:"+pyListStr(removed[:pyMin(len(removed), 12)]))
		}
	}
	// Python: command_log 段
	if commandLog, ok := traceMap["command_log"].([]any); ok && len(commandLog) > 0 {
		var commands []map[string]any
		for _, item := range commandLog[:pyMin(len(commandLog), 12)] {
			itemMap, isMap := item.(map[string]any)
			if !isMap {
				continue
			}
			commands = append(commands, map[string]any{
				"command":   pyStr(itemMap["command"])[:pyMin(len(pyStr(itemMap["command"])), 240)],
				"exit_code": itemMap["exit_code"],
				"stderr":    pyStr(itemMap["stderr_excerpt"])[:pyMin(len(pyStr(itemMap["stderr_excerpt"])), 240)],
			})
		}
		if len(commands) > 0 {
			parts = append(parts, "commands="+pyListStr(commands))
		}
	}
	// Python: trajectory_window_summary 段
	if window, ok := traceMap["trajectory_window_summary"].(map[string]any); ok {
		signatures := pyAnyList(window["failure_signatures"])
		var recentEvents []map[string]any
		if events, ok := window["recent_events"].([]any); ok {
			for _, item := range events[:pyMin(len(events), 12)] {
				itemMap, isMap := item.(map[string]any)
				if !isMap {
					continue
				}
				recentEvents = append(recentEvents, map[string]any{
					"event_type": pyStr(itemMap["event_type"])[:pyMin(len(pyStr(itemMap["event_type"])), 80)],
					"summary":    pyStr(itemMap["summary"])[:pyMin(len(pyStr(itemMap["summary"])), 300)],
				})
			}
		}
		if len(signatures) > 0 || len(recentEvents) > 0 {
			parts = append(parts, "trajectory_window=failure_signatures:"+pyListStr(signatures[:pyMin(len(signatures), 12)])+
				", recent_events:"+pyListStr(recentEvents))
		}
	}
	// Python: normalized_trace_summary 段
	if normalized, ok := traceMap["normalized_trace_summary"].(map[string]any); ok {
		var traceRefs []map[string]any
		if traces, ok := normalized["traces"].([]any); ok {
			for _, trace := range traces[:pyMin(len(traces), 4)] {
				traceMap2, isMap := trace.(map[string]any)
				if !isMap {
					continue
				}
				messages, _ := traceMap2["messages"].([]any)
				for _, message := range messages[:pyMin(len(messages), 12)] {
					messageMap, isMap := message.(map[string]any)
					if !isMap {
						continue
					}
					toolCalls, _ := messageMap["tool_calls"].([]any)
					for _, call := range toolCalls[:pyMin(len(toolCalls), 4)] {
						callMap, isMap := call.(map[string]any)
						if !isMap || callMap["error"] == nil {
							continue
						}
						traceRefs = append(traceRefs, map[string]any{
							"trace_id":      traceMap2["trace_id"],
							"role":          traceMap2["member_role"],
							"message_index": messageMap["message_index"],
							"step_pointer":  callMap["step_pointer"],
							"tool":          callMap["name"],
							"error":         pyStr(callMap["error"])[:pyMin(len(pyStr(callMap["error"])), 240)],
						})
					}
				}
			}
		}
		if len(traceRefs) > 0 {
			parts = append(parts, "normalized_trace_failures="+pyListStr(traceRefs))
		}
	}
	// Python: return " | ".join(parts) if parts else "none"
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, " | ")
}

// SummarizeIssueAttribution 摘要问题归因元数据。
// Python: role_attributor.py:347-363 _summarize_issue_attribution。
func SummarizeIssueAttribution(issue map[string]any) string {
	// Python: metadata = issue.get("metadata") if isinstance(dict) else {}
	metadata, ok := issue["metadata"].(map[string]any)
	if !ok {
		metadata = map[string]any{}
	}
	// Python: attribution = metadata.get("attribution") if isinstance(metadata, dict) else None
	attribution, ok := metadata["attribution"].(map[string]any)
	if !ok {
		return "none"
	}
	// Python: json.dumps({...五字段...}, ensure_ascii=False)
	// Go 侧复用 pyJSONDump（键序 sort_keys 风格，内容与分隔符对齐；键序偏差登记 spec §4）
	payload := map[string]any{
		"root_cause":        pyStr(attribution["root_cause"])[:pyMin(len(pyStr(attribution["root_cause"])), 500)],
		"critical_mistake":  pyStr(attribution["critical_mistake"])[:pyMin(len(pyStr(attribution["critical_mistake"])), 500)],
		"general_mechanism": pyStr(attribution["general_mechanism"])[:pyMin(len(pyStr(attribution["general_mechanism"])), 500)],
		"target_ref":        pyStr(attribution["target_ref"]),
		"evidence_refs":     pyOrNone(attribution["evidence_refs"]),
	}
	return pyJSONDump(payload)
}

// SummarizeIssueEvidence 压缩问题证据（前 4 条四字段）。
// Python: role_attributor.py:364-380 _summarize_issue_evidence。
func SummarizeIssueEvidence(evidence any) string {
	// Python: if not isinstance(evidence, list): return "none"
	items, ok := evidence.([]any)
	if !ok {
		return "none"
	}
	// Python: for item in evidence[:4]: compact.append({...四字段...})
	var compact []map[string]any
	for _, item := range items[:pyMin(len(items), 4)] {
		itemMap, isMap := item.(map[string]any)
		if !isMap {
			continue
		}
		compact = append(compact, map[string]any{
			"case_id":                       itemMap["case_id"],
			"signals":                       pyOrNone(itemMap["signals"]),
			"attribution":                   pyOrNone(itemMap["attribution"]),
			"trajectory_failure_signatures": pyOrNone(itemMap["trajectory_failure_signatures"]),
		})
	}
	return pyJSONDump(compact)
}

// NewRoleAttributor 构造编排器（Python __init__）。
func NewRoleAttributor(maxConcurrentIssues int, outputRepair *harnessrsi.OutputRepairConfig) *RoleAttributor {
	repair := harnessrsi.DefaultOutputRepairConfig()
	if outputRepair != nil {
		repair = *outputRepair
	}
	// Python: self._semaphore = asyncio.Semaphore(max_concurrent_issues)
	if maxConcurrentIssues <= 0 {
		maxConcurrentIssues = 1
	}
	return &RoleAttributor{
		outputRepair: repair,
		semaphore:    make(chan struct{}, maxConcurrentIssues),
	}
}

// NewRoleAttributorWithAgent 以预注入 agent 构造（Python role_attributor_agent 参数）。
func NewRoleAttributorWithAgent(agent *RoleAttributorAgent, maxConcurrentIssues int) *RoleAttributor {
	attr := NewRoleAttributor(maxConcurrentIssues, nil)
	attr.agent = agent
	return attr
}

// Attribute 定责全部问题（有界并发），产出定责报告。
// Python: role_attributor.py:412-600 attribute。
func (o *RoleAttributor) Attribute(ctx context.Context, params AttributeParams) (*RoleAttributionReport, error) {
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

	type attrResult struct {
		roleAttr *RoleIssueAttribution
		unass    *UnassignedMemberIssue
		warning  string
		err      error
	}
	// Python: tasks = [process_one(issue) ...]; asyncio.gather(..., return_exceptions=True)
	results := make([]attrResult, len(params.TeamIssues))
	var wg sync.WaitGroup
	for i := range params.TeamIssues {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Python: async with self._semaphore:
			o.semaphore <- struct{}{}
			defer func() { <-o.semaphore }()
			roleAttr, unass, warning, err := o.processOne(ctx, agent, params, params.TeamIssues[idx])
			results[idx] = attrResult{roleAttr: roleAttr, unass: unass, warning: warning, err: err}
		}(i)
	}
	wg.Wait()
	// Python: for result in results: if isinstance(result, Exception): raise RuntimeError(...)
	var assigned []RoleIssueAttribution
	var unassigned []UnassignedMemberIssue
	var warnings []string
	for _, result := range results {
		if result.err != nil {
			// Python: raise RuntimeError(f"Role attribution task failed: {result}") from result
			return nil, fmt.Errorf("%w: %v", errRoleAttributionFailed, result.err)
		}
		if result.roleAttr != nil {
			assigned = append(assigned, *result.roleAttr)
		} else if result.unass != nil {
			unassigned = append(unassigned, *result.unass)
		}
		if result.warning != "" {
			warnings = append(warnings, result.warning)
		}
	}
	// Python: assigned.sort(key=lambda a: (a.issue_id, a.role)); unassigned.sort(key=issue_id)
	sort.SliceStable(assigned, func(i, j int) bool {
		if assigned[i].IssueID != assigned[j].IssueID {
			return assigned[i].IssueID < assigned[j].IssueID
		}
		return assigned[i].Role < assigned[j].Role
	})
	sort.SliceStable(unassigned, func(i, j int) bool {
		return unassigned[i].IssueID < unassigned[j].IssueID
	})
	// Python: return RoleAttributionReport(...)
	return &RoleAttributionReport{
		AttributionID:            "role_attribution_" + shortHex8(),
		SourceEvalRefPath:        fmt.Sprintf("%v", *params.EvalRef),
		SourceAnalysisResultPath: "",
		HarnessRefsPath:          "",
		CandidateRoles:           params.CandidateRoles,
		AssignedRoleIssues:       assigned,
		UnassignedIssues:         unassigned,
		Warnings:                 warnings,
		Metadata: map[string]any{
			"model_config_ref":      params.ModelConfigRef,
			"issue_count":           len(params.TeamIssues),
			"assigned_count":        len(assigned),
			"unassigned_count":      len(unassigned),
			"target_member_routing": "exact_match_or_single_role_alias",
		},
	}, nil
}

// WriteRoleAttributionReport 落盘定责报告（role_attribution.yaml）。
// Python: role_attributor.py:602-660 write_report。
func WriteRoleAttributionReport(report *RoleAttributionReport, outputDir string) (string, error) {
	// Python: path = output_dir / "role_attribution.yaml"; path.parent.mkdir(...)
	path := filepath.Join(outputDir, "role_attribution.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	// Python: role_issues 列表组装（含 assignment_source / evidence_quality 提取）
	var roleIssues []map[string]any
	for _, ri := range report.AssignedRoleIssues {
		assignmentSource := ""
		evidenceQuality := ""
		if len(ri.Evidence) > 0 {
			if v, ok := ri.Evidence[0]["assignment_source"].(string); ok {
				assignmentSource = v
			}
			if v, ok := ri.Evidence[0]["evidence_quality"].(string); ok {
				evidenceQuality = v
			}
		}
		roleIssues = append(roleIssues, map[string]any{
			"issue_id":          ri.IssueID,
			"role":              ri.Role,
			"member_name":       ri.MemberName,
			"harness_ref_path":  ri.HarnessRefPath,
			"confidence":        ri.Confidence,
			"assignment_source": assignmentSource,
			"evidence_quality":  evidenceQuality,
			"evidence":          pyOrNoneList(ri.Evidence),
			"trace_refs":        pyOrNoneTraceRefs(ri.TraceRefs),
			"rationale":         ri.Rationale,
			"role_output_refs":  pyOrNoneTraceRefs(ri.RoleOutputRefs),
		})
	}
	// Python: unass_issues 列表组装
	var unassIssues []map[string]any
	for _, ui := range report.UnassignedIssues {
		unassIssues = append(unassIssues, map[string]any{
			"issue_id":   ui.IssueID,
			"reason":     ui.Reason,
			"evidence":   pyOrNoneList(ui.Evidence),
			"trace_refs": pyOrNoneTraceRefs(ui.TraceRefs),
			"rationale":  ui.Rationale,
		})
	}
	// Python: candidate_list 列表组装
	var candidateList []map[string]any
	for _, cr := range report.CandidateRoles {
		candidateList = append(candidateList, map[string]any{
			"role":             cr.Role,
			"member_name":      cr.MemberName,
			"harness_ref_path": cr.HarnessRefPath,
			"description":      cr.Description,
			"metadata":         pyOrNone(cr.Metadata),
		})
	}
	// Python: payload = {...}; yaml.safe_dump(payload, allow_unicode=True, sort_keys=False)
	payload := map[string]any{
		"attribution_id":              report.AttributionID,
		"source_eval_ref_path":        report.SourceEvalRefPath,
		"source_analysis_result_path": report.SourceAnalysisResultPath,
		"harness_refs_path":           report.HarnessRefsPath,
		"candidate_roles":             pyOrNoneList(candidateList),
		"assigned_role_issues":        pyOrNoneList(roleIssues),
		"unassigned_issues":           pyOrNoneList(unassIssues),
		"warnings":                    pyStrList(report.Warnings),
		"metadata":                    pyOrNone(report.Metadata),
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

// createDeepAgent 创建定责 DeepAgent。
// Python: role_attributor.py:159-165 _create_deep_agent。
func (a *RoleAttributorAgent) createDeepAgent(ctx context.Context) (*agentface.Agent, error) {
	// 测试注入点：等价 Python 测试 monkeypatch _create_deep_agent
	if a.prebuiltAgent != nil {
		return a.prebuiltAgent, nil
	}
	// Python: return create_role_attribution_agent(model_config_ref=..., workspace=self._workspace or ".", agent_skills_dirs=...)
	workspace := a.workspace
	if workspace == "" {
		workspace = "."
	}
	if agentsBridge.CreateRoleAttributionAgent == nil {
		return nil, errors.New("agents bridge not registered: import member_optimizer/agents (blank) to wire implementation")
	}
	return agentsBridge.CreateRoleAttributionAgent(ctx, AttributionAgentParams{
		ModelConfigRef:  a.modelConfigRef,
		Workspace:       workspace,
		AgentSkillsDirs: a.agentSkillsDirs,
	})
}

// getAgent 取预注入 agent 或按参数构造。
// Python: role_attributor.py:395-410 _get_agent。
func (o *RoleAttributor) getAgent(modelConfigRef string, attributionRetryLimit int, agentWorkspace string, agentSkillsDirs []string) *RoleAttributorAgent {
	// Python: if self._agent is not None: return self._agent
	if o.agent != nil {
		return o.agent
	}
	// Python: return RoleAttributorAgent(...)
	return NewRoleAttributorAgent(modelConfigRef, attributionRetryLimit, agentWorkspace, agentSkillsDirs, &o.outputRepair)
}

// processOne 单问题定责（信号量内执行）。
// Python: role_attributor.py:442-596 process_one。
func (o *RoleAttributor) processOne(ctx context.Context, agent *RoleAttributorAgent, params AttributeParams, issue harnessrsi.TeamIssue) (*RoleIssueAttribution, *UnassignedMemberIssue, string, error) {
	// Python: case_ids = issue.affected_cases if issue.affected_cases else []
	caseIDs := issue.AffectedCases
	// Python: case_evidence = load_case_evidence(eval_ref=..., max_cases=..., max_trace_chars=..., max_result_chars=...)
	caseEvidence := LoadCaseEvidence(params.EvalRef, caseIDs, params.MaxCasesPerIssue, params.MaxTraceChars, params.MaxResultChars)
	// Python: bundle = BoundedEvidenceBundle(issue={...}, case_results=[...], case_traces=[...], candidate_roles=...)
	var caseResults []map[string]any
	var caseTraces []map[string]any
	for _, cid := range caseIDs {
		ev, ok := caseEvidence[cid]
		if !ok {
			continue
		}
		result := map[string]any{"case_id": cid}
		for key, value := range ev["result"] {
			result[key] = value
		}
		caseResults = append(caseResults, result)
		trace := map[string]any{"case_id": cid}
		for key, value := range ev["trace"] {
			trace[key] = value
		}
		caseTraces = append(caseTraces, trace)
	}
	affectedComponent, _ := issue.Metadata["affected_component"].(string)
	improvementBrief := pyOrNone(issue.Metadata["improvement_brief"])
	candidateQuery, _ := issue.Metadata["candidate_query"].(string)
	bundle := &BoundedEvidenceBundle{
		Issue: map[string]any{
			"issue_id":             issue.IssueID,
			"category":             issue.Category,
			"severity":             issue.Severity,
			"summary":              issue.Summary,
			"affected_cases":       pyListStr(issue.AffectedCases),
			"evidence":             pyEvidenceFirst5(issue.Evidence),
			"suspected_team_scope": issue.SuspectedTeamScope,
			"optimization_target":  issue.OptimizationTarget,
			"target_members":       pyListStr(issue.TargetMembers),
			"affected_component":   affectedComponent,
			"recommendation":       issue.Recommendation,
		},
		CaseResults:    caseResults,
		CaseTraces:     caseTraces,
		CandidateRoles: params.CandidateRoles,
	}
	_ = improvementBrief
	_ = candidateQuery
	// Python: raw = await agent.attribute_issue(bundle)
	raw, err := agent.AttributeIssue(ctx, bundle)
	if err != nil {
		return nil, nil, "", err
	}
	// Python: target_candidate, target_match_status = _match_target_member(issue, candidate_roles)
	targetCandidate, targetMatchStatus := MatchTargetMember(issue, params.CandidateRoles)
	// Python: decision = raw.get("decision", "unassigned")
	decision := "unassigned"
	if d, ok := raw["decision"].(string); ok && d != "" {
		decision = d
	}
	// Python: if decision == "assigned" or target_candidate is not None: ...（assigned 分支）
	if decision == "assigned" || targetCandidate != nil {
		rawRole, _ := raw["role"].(string)
		role := rawRole
		if decision != "assigned" && targetCandidate != nil {
			role = targetCandidate.Role
		}
		if targetCandidate != nil && decision != "assigned" {
			role = targetCandidate.Role
		} else if targetCandidate != nil && rawRole != targetCandidate.Role {
			role = targetCandidate.Role
			targetMatchStatus = "target_members_overrode_unassigned_or_mismatch"
		}
		harnessRef := ""
		memberName := ""
		for _, r := range params.CandidateRoles {
			if r.Role == role {
				harnessRef = r.HarnessRefPath
				memberName = r.MemberName
				break
			}
		}
		// Python: assignment_source / evidence_quality
		assignmentSource := targetMatchStatus
		if decision == "assigned" && targetCandidate == nil {
			assignmentSource = "llm_assigned"
		}
		evidenceQuality := "case_evidence"
		if assignmentSource != "llm_assigned" && decision != "assigned" {
			evidenceQuality = "thin"
		}
		// Python: raw_confidence = float(raw.get("confidence", 0.0)); confidence = ...
		rawConfidence := pyFloatOrZero(raw["confidence"])
		confidence := rawConfidence
		if decision != "assigned" {
			confidence = pyMinFloat(0.5, pyMaxFloat(rawConfidence, 0.5))
		}
		// Python: role_attr = RoleIssueAttribution(...)
		// Python: issue_id=raw.get("issue_id", issue.issue_id)（键存在用 raw 值，否则回落）
		attrIssueID := issue.IssueID
		if _, exists := raw["issue_id"]; exists {
			attrIssueID = pyStr(raw["issue_id"])
		}
		roleAttr := &RoleIssueAttribution{
			IssueID:        attrIssueID,
			Role:           role,
			HarnessRefPath: harnessRef,
			Confidence:     confidence,
			Evidence: []map[string]any{{
				"summary":             issue.Summary,
				"severity":            issue.Severity,
				"category":            issue.Category,
				"recommendation":      issue.Recommendation,
				"metadata":            pyOrNone(issue.Metadata),
				"improvement_brief":   pyOrNone(issue.Metadata["improvement_brief"]),
				"candidate_query":     candidateQuery,
				"optimization_target": issue.OptimizationTarget,
				"target_members":      pyListStr(issue.TargetMembers),
				"affected_component":  affectedComponent,
				"target_match_status": targetMatchStatus,
				"assignment_source":   assignmentSource,
				"evidence_quality":    evidenceQuality,
			}},
			TraceRefs:  TraceRefsForIssue(issue, raw),
			Rationale:  pyStrOrEmpty(raw["rationale"]),
			MemberName: memberName,
		}
		return roleAttr, nil, "", nil
	}
	// Python: else 分支（unassigned）
	targetMatchStatusOut := targetMatchStatus
	if targetMatchStatusOut == "" {
		targetMatchStatusOut = "not_matched"
	}
	reason := ""
	if targetMatchStatusOut == "target_members_ambiguous" || targetMatchStatusOut == "target_members_mismatch" {
		reason = targetMatchStatusOut
	} else {
		if r, ok := raw["reason"].(string); ok && r != "" {
			reason = r
		} else if targetMatchStatusOut != "" {
			reason = targetMatchStatusOut
		} else {
			reason = "insufficient_evidence"
		}
	}
	unass := &UnassignedMemberIssue{
		IssueID: issue.IssueID,
		Reason:  reason,
		Evidence: []map[string]any{{
			"summary":             issue.Summary,
			"severity":            issue.Severity,
			"category":            issue.Category,
			"recommendation":      issue.Recommendation,
			"metadata":            pyOrNone(issue.Metadata),
			"improvement_brief":   pyOrNone(issue.Metadata["improvement_brief"]),
			"candidate_query":     candidateQuery,
			"optimization_target": issue.OptimizationTarget,
			"target_members":      pyListStr(issue.TargetMembers),
			"affected_component":  affectedComponent,
			"target_match_status": targetMatchStatusOut,
		}},
		TraceRefs: TraceRefsForIssue(issue, raw),
		Rationale: pyStrOrEmpty(raw["rationale"]),
	}
	if rationale, ok := raw["rationale"].(string); !ok || rationale == "" {
		unass.Rationale = "Insufficient evidence to attribute."
	}
	// Python: warning = None; if target_match_status in {ambiguous, mismatch}: warning = f"{issue_id}: {status}"
	warning := ""
	if targetMatchStatusOut == "target_members_ambiguous" || targetMatchStatusOut == "target_members_mismatch" {
		warning = issue.IssueID + ": " + targetMatchStatusOut
	}
	return nil, unass, warning, nil
}

// shortHex8 生成 8 位十六进制短 ID（Python uuid.uuid4().hex[:8]）。
func shortHex8() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
}
