// Python 源：openjiuwen/agent_teams/experts/bound_group.py（95 行 1:1）
//
// 团队自身的 spec 里记录的「装配来源 AgentGroup」。host 用一个 AgentGroup
// 装配团队时，会给每个非 leader 成员把能力快照写进 agents[<member>]（或把
// 启动配置写进 ExternalCliMemberSpec）、把 persona 写进预定义成员的 prompt。
// spawn_expert 从这份 exactly-that 数据（而非磁盘上的包）再孵化同款成员——
// 即使团队建好之后包升级或被移除，每个实例仍与预定义一致。

package experts

import (
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BoundGroupMember 绑定 AgentGroup 的一个非 leader 成员（按团队装配时的形态）。
// Python: BoundGroupMember (bound_group.py:27-40)
type BoundGroupMember struct {
	// Name 成员名
	Name string
	// DisplayName 显示名
	DisplayName string
	// Desc 描述
	Desc string
	// Persona 预定义成员的 prompt（渲染后的 persona）
	Persona string
	// Snapshot native 成员的 agents[<member>].agent_template_spec
	Snapshot *hschema.AgentTemplateSpec
	// ExternalCLI 外部运行时成员的启动配置
	ExternalCLI *atschema.ExternalCliAgentSpec
}

// BoundAgentGroup 团队绑定的 AgentGroup：组名与可 spawn 成员模板。
// Python: BoundAgentGroup (bound_group.py:43-56)
type BoundAgentGroup struct {
	// Name 组名
	Name string
	// Members 可 spawn 成员模板（花名册序）
	Members []BoundGroupMember
}

// Member 按名取模板成员；缺失得 nil。Python: member (:57-59)
func (b *BoundAgentGroup) Member(name string) *BoundGroupMember {
	for i := range b.Members {
		if b.Members[i].Name == name {
			return &b.Members[i]
		}
	}
	return nil
}

// MemberNames 花名册序的成员名。Python: member_names (:61-63)
func (b *BoundAgentGroup) MemberNames() []string {
	names := make([]string, 0, len(b.Members))
	for _, member := range b.Members {
		names = append(names, member.Name)
	}
	return names
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BoundAgentGroupOf 从团队 spec 读绑定的 AgentGroup；未绑定得 nil。
// 预定义成员计为模板成员的条件：是外部 CLI 成员，或普通 teammate 且其
// agents[<member>] spec 携带能力快照。
// Python: bound_agent_group (bound_group.py:66-91)
func BoundAgentGroupOf(spec *atschema.TeamAgentSpec) *BoundAgentGroup {
	if spec.BoundGroupName == "" && spec.AgentGroupName == "" {
		// Python: spec.agent_group_name——Go TeamAgentSpec.AgentGroupName
		// （T3 随 blueprint 补字段）；BoundGroupName 是 expert_binding 的
		// group（F_125，另一语义），不参与本判定。
		return nil
	}
	groupName := spec.AgentGroupName
	members := []BoundGroupMember{}
	for i := range spec.PredefinedMembers {
		predefined := &spec.PredefinedMembers[i]
		if predefined.ExternalCLI != nil {
			// Python: isinstance(predefined, ExternalCliMemberSpec) 分支
			members = append(members, BoundGroupMember{
				Name:        predefined.MemberName,
				DisplayName: predefined.DisplayName,
				Desc:        predefined.Desc,
				Persona:     predefined.Prompt,
				ExternalCLI: predefined.ExternalCLI,
			})
			continue
		}
		agentSpec, hasAgent := spec.Agents[predefined.MemberName]
		if predefined.RoleType != atschema.TeamRoleTeammate || !hasAgent || agentSpec.AgentTemplateSpec == nil {
			continue
		}
		members = append(members, BoundGroupMember{
			Name:        predefined.MemberName,
			DisplayName: predefined.DisplayName,
			Desc:        predefined.Desc,
			Persona:     predefined.Prompt,
			Snapshot:    agentSpec.AgentTemplateSpec,
		})
	}
	return &BoundAgentGroup{Name: groupName, Members: members}
}
