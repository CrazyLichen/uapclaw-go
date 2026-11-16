// action_field.go MemberOptimizationAction 的策略校验字段读取适配。
// Python: policy.py:_get 经 getattr(action, field) 直读 dataclass；Go 接口
// 需显式实现（D-c2-1 偏差登记）。

package memberoptimizer

// ActionFieldValue 实现 actiongroups.ActionGetter（Python getattr 语义）。
func (a MemberOptimizationAction) ActionFieldValue(key string) any {
	switch key {
	case "action_id":
		return a.ActionID
	case "role":
		return a.Role
	case "action_group":
		return a.ActionGroup
	case "operation":
		return a.Operation
	case "action_type":
		return a.ActionType
	case "target_path":
		return a.TargetPath
	case "description":
		return a.Description
	case "rationale":
		return a.Rationale
	case "attributed_issue_ids":
		return toAnyList(a.AttributedIssueIDs)
	case "depends_on":
		return toAnyList(a.DependsOn)
	case "run_if":
		return a.RunIf
	case "allowed_skills":
		return toAnyList(a.AllowedSkills)
	case "allowed_tools":
		return toAnyList(a.AllowedTools)
	case "candidate_query":
		return a.CandidateQuery
	case "install_ref":
		return a.InstallRef
	case "expected_effect":
		return a.ExpectedEffect
	case "risk_notes":
		return toAnyList(a.RiskNotes)
	case "declared_write_paths":
		return toAnyList(a.DeclaredWritePaths)
	case "constraints":
		return a.Constraints
	case "member_name":
		return a.MemberName
	default:
		return nil
	}
}

// toAnyList []string → []any。
func toAnyList(list []string) any {
	if list == nil {
		return nil
	}
	out := make([]any, 0, len(list))
	for _, item := range list {
		out = append(out, item)
	}
	return out
}
