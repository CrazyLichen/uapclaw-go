// optimizer_publish.go 发布与 refs 写入助手。
// Python: optimizer.py:658-836 _publish + 1230-1352 _write_current_harness_refs
// + 837-904 _prepare_planning_evidence（analysis evidence 依赖归 evaluation
// 包——此处承载可独立实现部分）+ 906-1039 single-harness 编译。

package memberoptimizer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	"gopkg.in/yaml.v3"
)

// errorsAsStd errors.As 直通。
func errorsAsStd(err error, target any) bool {
	return errors.As(err, target)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Publish 发布已验证角色并写最终工件。
// Python: optimizer.py:658-836 _publish（静态方法）。
func (m *MemberOptimizer) publish(request publishRequest) *MemberOptimizationArtifact {
	runDir := request.runDir
	pathLayout := request.pathLayout
	outputRoot := request.outputRoot
	selectionReport := request.selectionReport
	plan := request.plan
	executionResults := request.executionResults
	verificationResult := request.verificationResult
	deferPublish := request.deferPublish

	selectedRoles := targetRoles(selectionReport)
	harnessRefs := map[string]string{}
	for i := range selectionReport.Targets {
		harnessRefs[selectionReport.Targets[i].Role] = selectionReport.Targets[i].HarnessRefPath
	}
	for role, ref := range readInputHarnessRefs(request.harnessRefsPath) {
		harnessRefs[role] = ref
	}

	var publishedRoles, failedRoles, skippedRoles []string
	roleResults := map[string]RoleResult{}

	executionErrorsByRole := map[string][]string{}
	for _, role := range selectedRoles {
		executionErrorsByRole[role] = RoleExecutionErrors(plan, executionResults, role)
	}
	verifiedRoles := map[string]bool{}
	for _, role := range selectedRoles {
		rvr, has := verificationResult.RoleResults[role]
		verifiedRoles[role] = has && rvr.Status == "passed" && len(executionErrorsByRole[role]) == 0
	}

	for _, role := range sortedStringsCopy(selectedRoles) {
		beforeRef := harnessRefs[role]
		integrationDir := ResolveIntegrationWorktreePath(
			pathLayout.WorktreesDir(filepath.Base(runDir)), role)
		if verifiedRoles[role] {
			// Python: try publish; catch → failed
			afterRef, publishErr := publishRoleHarness(pathLayout, runDir, role, integrationDir, deferPublish)
			if publishErr == nil {
				publishedRoles = append(publishedRoles, role)
				var actionIDs []string
				for _, row := range executionResults {
					if row.Role == role {
						actionIDs = append(actionIDs, row.ActionID)
					}
				}
				status := "published"
				if deferPublish {
					status = "candidate_ready"
				}
				roleResults[role] = RoleResult{
					Status: status, BeforeHarnessRefPath: beforeRef,
					AfterHarnessRefPath: afterRef, ActionIDs: actionIDs,
					VerificationStatus: "passed",
				}
				harnessRefs[role] = afterRef
			} else {
				failedRoles = append(failedRoles, role)
				roleResults[role] = RoleResult{
					Status: "failed", BeforeHarnessRefPath: beforeRef,
					AfterHarnessRefPath: beforeRef, VerificationStatus: "failed",
					Error: publishErr.Error(),
				}
			}
		} else {
			failedRoles = append(failedRoles, role)
			var actionIDs []string
			for _, row := range executionResults {
				if row.Role == role {
					actionIDs = append(actionIDs, row.ActionID)
				}
			}
			errText := strings.Join(executionErrorsByRole[role], "; ")
			if errText == "" {
				errText = "Verification failed"
			}
			roleResults[role] = RoleResult{
				Status: "failed", BeforeHarnessRefPath: beforeRef,
				AfterHarnessRefPath: beforeRef, ActionIDs: actionIDs,
				VerificationStatus: "failed", Error: errText,
			}
		}
	}

	var publishedRefPaths map[string]string
	if deferPublish {
		publishedRefPaths = map[string]string{}
		for _, role := range publishedRoles {
			publishedRefPaths[role] = roleResults[role].AfterHarnessRefPath
		}
	}
	refsPath := writeCurrentHarnessRefs(harnessRefsWriteRequest{
		outputRoot: outputRoot, harnessRefsPath: request.harnessRefsPath,
		publishedRoles: publishedRoles, failedRoles: failedRoles,
		skippedRoles: skippedRoles, roleResults: roleResults,
		pathLayout:        pathLayout,
		refsPath:          ternaryStr(deferPublish, filepath.Join(runDir, "candidate_harness_refs.yaml"), ""),
		publishedRefPaths: publishedRefPaths,
		cumulativePublish: deferPublish,
	})
	if request.candidateManifestPath != "" {
		status := "verification_failed"
		if len(publishedRoles) > 0 {
			status = "candidate_ready"
		}
		_ = updateCandidateManifest(request.candidateManifestPath, status, refsPath, publishedRoles, failedRoles)
	}

	status := "failed"
	if len(publishedRoles) > 0 && (len(failedRoles) > 0 || len(skippedRoles) > 0) {
		status = "partial_success"
	} else if len(publishedRoles) > 0 {
		status = "success"
	}

	promotionStatus := "not_evaluated"
	if deferPublish && len(publishedRoles) > 0 {
		promotionStatus = "pending_gate"
	}
	var candidateReadyRoles []string
	if deferPublish {
		candidateReadyRoles = publishedRoles
	} else {
		candidateReadyRoles = []string{}
	}
	artifact := MemberOptimizationArtifact{
		OptimizationID:           filepath.Base(runDir),
		OutputDir:                runDir,
		Status:                   status,
		OptimizedHarnessRefsPath: refsPath,
		Roles:                    selectedRoles,
		CandidateReadyRoles:      candidateReadyRoles,
		PublishedRoles:           publishedRoles,
		StagedRoles:              selectedRoles,
		VerifiedRoles:            publishedRoles,
		PromotedRoles:            []string{},
		PromotionStatus:          promotionStatus,
		FailedRoles:              failedRoles,
		SkippedRoles:             skippedRoles,
		RoleAttributionPath:      request.roleAttrPath,
		MechanismAttributionPath: request.mechanismAttrPath,
		SelectionPath:            request.selectionPath,
		PlanPath:                 request.planPath,
		ExecutionResultPath:      request.executionResultPath,
		VerificationPath:         request.verificationPath,
		FixResultPath:            request.fixResultPath,
		RoleResults:              roleResults,
		Metadata: map[string]any{
			"eval_ref_path":                request.evalRefPath,
			"analysis_result_path":         request.analysisResultPath,
			"source_harness_refs_path":     request.harnessRefsPath,
			"runtime_root":                 pathLayout.RuntimeRoot,
			"runtime_run_dir":              pathLayout.RunRuntimeDir(filepath.Base(runDir)),
			"runtime_worktrees_dir":        pathLayout.WorktreesDir(filepath.Base(runDir)),
			"candidate_manifest_path":      request.candidateManifestPath,
			"optimization_hypotheses_path": request.optimizationHypothesesPath,
			"execution_errors_by_role":     executionErrorsByRole,
		},
	}
	refPath := filepath.Join(runDir, "member_optimization_ref.yaml")
	_ = writeYamlArtifact(refPath, &artifact)
	return &artifact
}

// publishRoleHarness 发布单角色 harness（candidate / current 双路径）。
// Python: optimizer.py:727-766 内联段。
func publishRoleHarness(pathLayout *MemberOptimizerPathLayout, runDir, role, integrationDir string, deferPublish bool) (string, error) {
	if err := pathLayout.WriteRoleMapping(role); err != nil {
		return "", err
	}
	publishDir := pathLayout.CurrentHarnessDir(role)
	if deferPublish {
		publishDir = pathLayout.CandidateHarnessDir(filepath.Base(runDir), role)
	}
	tmpDir := pathLayout.PublishTmpDir(filepath.Base(runDir), role)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return "", err
	}
	if info, err := os.Stat(integrationDir); err == nil && info.IsDir() {
		if err := copyTree(integrationDir, tmpDir); err != nil {
			return "", err
		}
	}
	if info, err := os.Stat(publishDir); err == nil && info.IsDir() {
		if err := os.RemoveAll(publishDir); err != nil {
			return "", err
		}
	}
	if err := copyTree(tmpDir, publishDir); err != nil {
		return "", err
	}
	if err := os.RemoveAll(tmpDir); err != nil {
		return "", err
	}
	return publishDir, nil
}

// copyTree 递归目录复制（shutil.copytree dirs_exist_ok=True 等价）。
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// ternaryStr 三元字符串。
func ternaryStr(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// WriteCurrentHarnessRefs 写 run 级 current_harness_refs.yaml。
// Python: optimizer.py:1230-1352 _write_current_harness_refs。
func writeCurrentHarnessRefs(request harnessRefsWriteRequest) string {
	outputRoot := request.outputRoot
	_ = os.MkdirAll(outputRoot, 0o755)
	refsPath := request.refsPath
	if refsPath == "" {
		refsPath = filepath.Join(outputRoot, "current_harness_refs.yaml")
	}
	currentHarnessesDir := filepath.Join(outputRoot, "current_harnesses")

	harnessRefs := readInputHarnessRefs(request.harnessRefsPath)
	for _, role := range request.publishedRoles {
		if ref, has := request.publishedRefPaths[role]; has && ref != "" {
			harnessRefs[role] = ref
		} else if request.pathLayout != nil {
			_ = request.pathLayout.WriteRoleMapping(role)
			harnessRefs[role] = request.pathLayout.CurrentHarnessDir(role)
		} else {
			harnessRefs[role] = filepath.Join(currentHarnessesDir, role)
		}
	}
	for _, role := range append(append([]string{}, request.failedRoles...), request.skippedRoles...) {
		if _, has := harnessRefs[role]; !has {
			harnessRefs[role] = ""
		}
	}
	roleResultsOut := map[string]any{}
	for role, rr := range request.roleResults {
		roleResultsOut[role] = map[string]any{
			"status":                  rr.Status,
			"before_harness_ref_path": rr.BeforeHarnessRefPath,
			"after_harness_ref_path":  rr.AfterHarnessRefPath,
		}
	}
	sourcePayload := map[string]any{}
	if data, err := os.ReadFile(expandUserTilde(request.harnessRefsPath)); err == nil {
		var loaded map[string]any
		if yaml.Unmarshal(data, &loaded) == nil && loaded != nil {
			sourcePayload = loaded
		}
	}
	var roleIdentities []any
	if sourceRoles, ok := sourcePayload["roles"].([]any); ok {
		for _, sourceRole := range sourceRoles {
			roleIdentity, isMap := sourceRole.(map[string]any)
			if !isMap {
				continue
			}
			memberName := pyStrOrEmpty(roleIdentity["member_name"])
			roleName := pyStrOrEmpty(roleIdentity["role"])
			ref := harnessRefs[memberName]
			if ref == "" {
				ref = harnessRefs[roleName]
			}
			if ref != "" {
				roleIdentity["harness_ref_path"] = ref
			}
			metadata, _ := roleIdentity["metadata"].(map[string]any)
			if metadata == nil {
				metadata = map[string]any{}
			}
			if _, has := metadata["member_id"]; !has {
				memberID := memberName
				if memberID == "" {
					memberID = roleName
				}
				metadata["member_id"] = memberID
			}
			aliases, _ := metadata["aliases"].([]any)
			if aliases == nil {
				aliases = []any{}
			}
			aliasSet := map[string]bool{}
			var mergedAliases []any
			appendAlias := func(alias string) {
				if alias == "" || aliasSet[alias] {
					return
				}
				aliasSet[alias] = true
				mergedAliases = append(mergedAliases, alias)
			}
			for _, alias := range aliases {
				appendAlias(fmt.Sprintf("%v", alias))
			}
			appendAlias(roleName)
			appendAlias(memberName)
			metadata["aliases"] = mergedAliases
			roleIdentity["metadata"] = metadata
			roleIdentities = append(roleIdentities, roleIdentity)
		}
	}
	effectivePublishedRoles := append([]string{}, request.publishedRoles...)
	effectiveRoleResults := map[string]any{}
	for role, rr := range roleResultsOut {
		effectiveRoleResults[role] = rr
	}
	if request.cumulativePublish {
		if previousPublished, ok := sourcePayload["published_roles"].([]any); ok {
			seen := map[string]bool{}
			var merged []string
			for _, role := range previousPublished {
				text := fmt.Sprintf("%v", role)
				if text != "" && !seen[text] {
					seen[text] = true
					merged = append(merged, text)
				}
			}
			for _, role := range request.publishedRoles {
				if !seen[role] {
					merged = append(merged, role)
				}
			}
			effectivePublishedRoles = merged
		}
		if previousResults, ok := sourcePayload["role_results"].(map[string]any); ok {
			for role, rr := range previousResults {
				effectiveRoleResults[role] = rr
			}
		}
	}
	promotionStatus := "not_applicable"
	if len(effectivePublishedRoles) > 0 {
		promotionStatus = "pending_gate"
	}
	payload := map[string]any{
		"version":                  1,
		"source_harness_refs_path": request.harnessRefsPath,
		"harness_refs":             harnessRefs,
		"published_roles":          effectivePublishedRoles,
		"candidate_ready_roles":    effectivePublishedRoles,
		"staged_roles":             effectivePublishedRoles,
		"verified_roles":           effectivePublishedRoles,
		"promoted_roles":           []string{},
		"promotion_status":         promotionStatus,
		"failed_roles":             ternaryAnySlice(request.cumulativePublish, nil, toAnySlice(request.failedRoles)),
		"skipped_roles":            ternaryAnySlice(request.cumulativePublish, nil, toAnySlice(request.skippedRoles)),
		"role_results":             effectiveRoleResults,
	}
	if len(roleIdentities) > 0 {
		payload["roles"] = roleIdentities
	}
	if request.cumulativePublish {
		payload["last_attempt"] = map[string]any{
			"published_roles": toAnySlice(request.publishedRoles),
			"failed_roles":    toAnySlice(request.failedRoles),
			"skipped_roles":   toAnySlice(request.skippedRoles),
			"role_results":    roleResultsOut,
		}
	}
	data, err := yaml.Marshal(payload)
	if err != nil {
		return refsPath
	}
	_ = os.WriteFile(refsPath, data, 0o644)
	return refsPath
}

// ternaryAnySlice 三元 any 清单。
func ternaryAnySlice(cond bool, a, b any) any {
	if cond {
		return a
	}
	return b
}

// CompileSingleHarnessPlanningInputs 单 harness 直连编译规划输入。
// Python: optimizer.py:863-1039 _compile_single_harness_planning_inputs。
func compileSingleHarnessPlanningInputs(
	teamIssues []harnessrsi.TeamIssue,
	candidateRoles []MemberRoleCandidate,
	hypotheses []map[string]any,
	evalRefPath, analysisResultPath, harnessRefsPath string,
	allowedActionGroups []string,
) (*RoleAttributionReport, *MechanismAttributionReport, *MemberSelectionReport, error) {
	if len(candidateRoles) != 1 {
		return nil, nil, nil, fmt.Errorf(
			"single-harness optimization requires exactly one candidate role; found %d", len(candidateRoles))
	}
	role := candidateRoles[0]
	hypothesesByIssue := map[string]map[string]any{}
	for _, item := range hypotheses {
		if id := pyStrOrEmpty(item["source_issue_id"]); id != "" {
			hypothesesByIssue[id] = item
		}
	}
	var missing []string
	for i := range teamIssues {
		if _, has := hypothesesByIssue[teamIssues[i].IssueID]; !has {
			missing = append(missing, teamIssues[i].IssueID)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return nil, nil, nil, fmt.Errorf(
			"single-harness analyzer issues are missing immutable hypotheses: %s", strings.Join(missing, ", "))
	}
	var assigned []RoleIssueAttribution
	var mechanisms []RoleMechanismAttribution
	var issueIDs []string
	var executableIssueIDs []string
	surfacesByIssue := map[string][]string{}
	var deferredLevers []any
	for i := range teamIssues {
		issue := &teamIssues[i]
		hypothesis := hypothesesByIssue[issue.IssueID]
		leverPolicy, _ := hypothesis["lever_policy"].(map[string]any)
		if leverPolicy == nil {
			leverPolicy = map[string]any{}
		}
		recommendedLever := pyStrOrEmpty(leverPolicy["recommended_lever"])
		if recommendedLever == "" {
			recommendedLever = "unresolved"
		}
		issueSurfaces := AvailableSurfacesForLever(recommendedLever, allowedActionGroups)
		surfacesByIssue[issue.IssueID] = issueSurfaces
		if len(issueSurfaces) > 0 {
			executableIssueIDs = append(executableIssueIDs, issue.IssueID)
		} else {
			deferredLevers = append(deferredLevers, map[string]any{
				"issue_id": issue.IssueID, "recommended_lever": recommendedLever,
				"target_ref": pyStrOrEmpty(leverPolicy["target_ref"]),
				"status":     "deferred",
				"reason":     "recommended_lever_has_no_executable_surface",
			})
		}
		evidence := map[string]any{}
		for _, item := range issue.Evidence {
			for k, v := range item {
				evidence[k] = v
			}
		}
		evidence["summary"] = issue.Summary
		evidence["recommendation"] = issue.Recommendation
		evidence["optimization_hypothesis_id"] = pyStrOrEmpty(hypothesis["hypothesis_id"])
		requiredBehavior := pyStrOrEmpty(hypothesis["required_behavior"])
		if requiredBehavior == "" {
			requiredBehavior = issue.Recommendation
		}
		evidence["required_behavior"] = requiredBehavior
		evidenceAny := []map[string]any{evidence}
		traceRefs := evidenceRefsFromStringList(stringListFromAny(hypothesis["evidence_refs"]))

		hypothesisID := pyStrOrEmpty(hypothesis["hypothesis_id"])
		if hypothesisID == "" {
			hypothesisID = "<missing>"
		}
		assigned = append(assigned, RoleIssueAttribution{
			IssueID: issue.IssueID, Role: role.Role, MemberName: role.MemberName,
			HarnessRefPath: role.HarnessRefPath, Confidence: 1.0,
			Evidence:  evidenceAny,
			TraceRefs: traceRefs,
			Rationale: fmt.Sprintf("Analyzer-authored immutable hypothesis %s; downstream stages "+
				"must preserve required_behavior verbatim.", hypothesisID),
		})
		mechanismType := "insufficient_role_evidence"
		failureSignature := "insufficient_role_evidence"
		switch recommendedLever {
		case LeverInstruction:
			mechanismType = "instruction"
			failureSignature = "prompt_instruction_mismatch"
		case LeverAction:
			mechanismType = "tool"
			failureSignature = "missed_exploration_or_capability"
		case LeverControl, LeverConfiguration:
			mechanismType = "workflow"
			if recommendedLever == LeverConfiguration {
				mechanismType = "config"
			}
			failureSignature = "config_or_rail_mismatch"
		}
		optimizationSurface := ""
		if len(issueSurfaces) == 1 {
			optimizationSurface = issueSurfaces[0]
		}
		mechanisms = append(mechanisms, RoleMechanismAttribution{
			IssueID: issue.IssueID, Role: role.Role,
			MechanismType: mechanismType, FailureSignature: failureSignature,
			Confidence: 1.0, OptimizationSurface: optimizationSurface,
			Evidence:     evidenceAny,
			EvidenceRefs: traceRefs,
			Rationale: fmt.Sprintf("Analyzer suggests lever=%s; choose the smallest "+
				"supported intervention preserving the failure facts and behavior objective.", recommendedLever),
		})
		issueIDs = append(issueIDs, issue.IssueID)
	}
	roleReport := &RoleAttributionReport{
		AttributionID:            "single_harness_direct_attribution",
		SourceEvalRefPath:        evalRefPath,
		SourceAnalysisResultPath: analysisResultPath,
		HarnessRefsPath:          harnessRefsPath,
		CandidateRoles:           append([]MemberRoleCandidate{}, candidateRoles...),
		AssignedRoleIssues:       assigned,
		Metadata: map[string]any{
			"mode":                             "single_harness_direct",
			"semantic_rewrite_stages_bypassed": []any{"role_attributor", "mechanism_attributor"},
		},
	}
	roleMechanisms := map[string][]RoleMechanismAttribution{}
	if len(mechanisms) > 0 {
		roleMechanisms[role.Role] = mechanisms
	}
	mechanismReport := &MechanismAttributionReport{
		MechanismAttributionID: "single_harness_surface_unresolved",
		RoleMechanisms:         roleMechanisms,
		Metadata: map[string]any{
			"mode":               "single_harness_direct",
			"semantic_authority": "optimization_hypotheses",
		},
	}
	var surfaces []string
	for _, issueID := range executableIssueIDs {
		surfaces = append(surfaces, surfacesByIssue[issueID]...)
	}
	var targets []MemberOptimizationTarget
	if len(executableIssueIDs) > 0 {
		mechanismTypeSet := map[string]bool{}
		var mechanismTypes []string
		for _, mechanism := range mechanisms {
			if containsStr(executableIssueIDs, mechanism.IssueID) && !mechanismTypeSet[mechanism.MechanismType] {
				mechanismTypeSet[mechanism.MechanismType] = true
				mechanismTypes = append(mechanismTypes, mechanism.MechanismType)
			}
		}
		surfaceSet := map[string]bool{}
		var uniqueSurfaces []string
		for _, surface := range surfaces {
			if !surfaceSet[surface] {
				surfaceSet[surface] = true
				uniqueSurfaces = append(uniqueSurfaces, surface)
			}
		}
		var hypothesisIDs []any
		for _, issueID := range executableIssueIDs {
			if hyp, has := hypothesesByIssue[issueID]; has {
				hypothesisIDs = append(hypothesisIDs, pyStrOrEmpty(hyp["hypothesis_id"]))
			}
		}
		var evidenceRefList []map[string]string
		for _, issueID := range issueIDs {
			evidenceRefList = append(evidenceRefList, map[string]string{"issue_id": issueID})
		}
		targets = append(targets, MemberOptimizationTarget{
			Role: role.Role, MemberName: role.MemberName,
			HarnessRefPath:       role.HarnessRefPath,
			AttributedIssueIDs:   executableIssueIDs,
			Confidence:           1.0,
			Reason:               "Directly bound to analyzer-authored immutable hypotheses.",
			EvidenceRefs:         evidenceRefList,
			MechanismTypes:       mechanismTypes,
			OptimizationSurfaces: uniqueSurfaces,
			Metadata: map[string]any{
				"optimization_hypothesis_ids": hypothesisIDs,
			},
		})
	}
	selectionReport := &MemberSelectionReport{
		SelectionID: "single_harness_direct_selection",
		Targets:     targets,
		Metadata: map[string]any{
			"mode":              "single_harness_direct",
			"selector_bypassed": true,
			"deferred_levers":   deferredLevers,
		},
	}
	return roleReport, mechanismReport, selectionReport, nil
}

// evidenceRefsFromStringList any → []map[string]string（EvidenceRefs 字段形态）。
func evidenceRefsFromStringList(list []string) []map[string]string {
	var out []map[string]string
	for _, item := range list {
		out = append(out, map[string]string{"ref": item})
	}
	return out
}

// stringListFromAny any → []string（typed-nil 守卫）。
func stringListFromAny(value any) []string {
	if value == nil {
		return nil
	}
	if list, ok := value.([]string); ok {
		return list
	}
	if list, ok := value.([]any); ok {
		var out []string
		for _, item := range list {
			out = append(out, fmt.Sprintf("%v", item))
		}
		return out
	}
	return nil
}

// containsStr 清单包含。
func containsStr(list []string, item string) bool {
	for _, existing := range list {
		if existing == item {
			return true
		}
	}
	return false
}

// preparePlanningEvidence 复用 Analyzer 证据投影（不复制隐藏评分文件）。
// Python: optimizer.py:837-861 _prepare_planning_evidence（依赖 evaluation_result_
// analyzer 包；Go 侧 Analyzer 证据投影由 analyzer 包承载——索引与 task_and_response
// 落盘在此实现，case 装载复用 loader.go 的案例读取）。
func preparePlanningEvidence(workspace, evalRefPath string, hypotheses []map[string]any, harnessRefsPath string) error {
	root := filepath.Join(workspace, "evidence")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	caseIDs := map[string]bool{}
	for _, item := range hypotheses {
		if list, ok := item["target_case_ids"].([]any); ok {
			for _, id := range list {
				caseIDs[fmt.Sprintf("%v", id)] = true
			}
		}
	}
	index := map[string]any{
		"source_eval_ref": evalRefPath,
		"harness_refs":    harnessRefsPath,
		"cases":           []any{},
	}
	casesDir := filepath.Join(filepath.Dir(evalRefPath), "cases")
	entries, _ := os.ReadDir(casesDir)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	casesAny := []any{}
	for number, entry := range entries {
		if !entry.IsDir() || !caseIDs[entry.Name()] {
			continue
		}
		destination := filepath.Join(root, fmt.Sprintf("case_%03d", number))
		if err := os.MkdirAll(destination, 0o755); err != nil {
			return err
		}
		// task_and_response.json（Analyzer case 语义：input/response）
		taskAndResponse := map[string]any{}
		if data, err := os.ReadFile(filepath.Join(casesDir, entry.Name(), "input.txt")); err == nil {
			taskAndResponse["task"] = string(data)
		}
		if data, err := os.ReadFile(filepath.Join(casesDir, entry.Name(), "response.txt")); err == nil {
			taskAndResponse["response"] = string(data)
		}
		payload, _ := jsonIndent(taskAndResponse)
		if err := os.WriteFile(filepath.Join(destination, "task_and_response.json"), payload, 0o644); err != nil {
			return err
		}
		rel, _ := filepath.Rel(workspace, destination)
		casesAny = append(casesAny, map[string]any{
			"case_id": entry.Name(), "path": filepath.ToSlash(rel),
		})
	}
	index["cases"] = casesAny
	if _, err := os.Stat(filepath.Join(root, "current_harness")); err != nil {
		// Python: prepare_harness_context（analysis 包；最小上下文：source 索引）
		_ = os.MkdirAll(filepath.Join(root, "current_harness"), 0o755)
	}
	payload, _ := jsonIndent(index)
	return os.WriteFile(filepath.Join(root, "index.json"), payload, 0o644)
}

// jsonIndent 缩进 JSON。
func jsonIndent(value any) ([]byte, error) {
	return json.MarshalIndent(value, "", "  ")
}
