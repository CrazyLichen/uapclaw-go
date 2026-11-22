package rails

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/security"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
)

// TeamPermissionPolicyRail 告知 leader 队友所受的基础权限规则。
//
// 仅在 enable_permissions 为真时挂载到 leader。
// 该节描述基础权限配置，使 leader 在调用 spawn_teammate 时
// 能做出知情的收窄决策。
//
// Python: TeamPermissionPolicyRail (team_permission_policy_rail.py:15-73)
type TeamPermissionPolicyRail struct {
	rails.DeepAgentRail
	// systemPromptBuilder 当前成员持有的系统提示词构建器（init 时捕获）
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
	// permissionsConfig 基础权限配置字典（PermissionEngine 消费格式）
	permissionsConfig map[string]any
	// language 输出语言（"cn" 或 "en"）
	language string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// teamPermissionPolicyRailPriority rail 挂载优先级
	// Python: TeamPermissionPolicyRail.priority = 5
	teamPermissionPolicyRailPriority = 5

	// teamPermissionPolicySection 注入节名
	// Python: TeamPermissionPolicyRail.SECTION_NAME = "team_permission_policy"
	teamPermissionPolicySection = "team_permission_policy"

	// teamPermissionPolicySectionPriority 注入节优先级
	// Python: TeamPermissionPolicyRail.SECTION_PRIORITY = 39
	teamPermissionPolicySectionPriority = 39
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamPermissionPolicyRail 创建权限策略 rail。
//
// Python: TeamPermissionPolicyRail.__init__ (team_permission_policy_rail.py:27-36)
func NewTeamPermissionPolicyRail(permissionsConfig map[string]any, language string) *TeamPermissionPolicyRail {
	if language == "" {
		language = "cn"
	}
	r := &TeamPermissionPolicyRail{
		DeepAgentRail:     *rails.NewDeepAgentRail(),
		permissionsConfig: permissionsConfig,
		language:          language,
	}
	r.WithPriority(teamPermissionPolicyRailPriority)
	return r
}

// Init 捕获当前成员持有的系统提示词构建器。
//
// Python: TeamPermissionPolicyRail.init (team_permission_policy_rail.py:38-40)
func (r *TeamPermissionPolicyRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	r.systemPromptBuilder = agent.SystemPromptBuilder()
	return nil
}

// Uninit 移除注入的策略节。
//
// Python: TeamPermissionPolicyRail.uninit (team_permission_policy_rail.py:42-47)
func (r *TeamPermissionPolicyRail) Uninit(_ agentinterfaces.BaseAgent) error {
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection(teamPermissionPolicySection)
	}
	r.systemPromptBuilder = nil
	return nil
}

// BeforeModelCall 每次模型调用前注入权限策略。
//
// Python: TeamPermissionPolicyRail.before_model_call (team_permission_policy_rail.py:49-73)
func (r *TeamPermissionPolicyRail) BeforeModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	if r.systemPromptBuilder == nil {
		return nil
	}

	permissionsText := security.FormatBasePermissionsForDesc(r.permissionsConfig, r.language)
	if permissionsText == "" {
		return nil
	}

	heading := "# Teammate 权限规则\n\n"
	if r.language != "cn" {
		heading = "# Teammate Permission Rules\n\n"
	}

	r.systemPromptBuilder.AddSection(saprompt.PromptSection{
		Name: teamPermissionPolicySection,
		Content: map[string]string{
			r.language: heading + permissionsText + "\n",
		},
		Priority: teamPermissionPolicySectionPriority,
	})
	return nil
}
