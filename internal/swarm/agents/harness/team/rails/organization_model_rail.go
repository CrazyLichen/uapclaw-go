package rails

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	llm "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamModelSetting 持久团队模型设置（后端注入缝的数据载体）。
//
// Python: load_team_model 返回的 setting（organization_team_models.py），
// 数据本体属 25.9 组织运行时，此处只承载 rail 所需的两个字段。
type TeamModelSetting struct {
	// ModelName 持久团队模型名
	ModelName string
	// Selection 执行模型选择（execution_model_selection，dict 形态）
	Selection map[string]any
}

// OrganizationModelBackend 组织模型后端注入缝。
//
// Python 侧 rail 直接调用 jiuwenswarm/server/runtime/organization_team_models
// 的 load_team_model / resolve_team_model；这两个函数的本体（663 行
// organization_* 运行时）属 25.9 专家与组织 ☐，本批不实现。
// Go 侧以本窄接口注入；后端缺省为 nil，rail 按「无持久设置」语义降级运行。
//
// 回收条件：25.9 实施 organization_team_models 时由工厂装配真实后端，
// 删除本缝登记（IMPLEMENTATION_PLAN.md 15.20/25.9 双向标注）。
type OrganizationModelBackend interface {
	// LoadTeamModel 读取持久团队模型设置；无设置返回 (zero, false)。
	// Python: load_team_model(team_id, session_id)
	LoadTeamModel(teamID, sessionID string) (TeamModelSetting, bool)
	// ResolveTeamModel 解析 selection 并构造可用模型，返回模型、
	// 签名输入（Python: compiled.model_dump_json(exclude client_id) 的 payload）。
	// Python: resolve_team_model(setting.selection) + Model(...) 构造。
	// 25.9 装配真实 backend；返回 *llm.Model 具体类型（2026-12-22 收紧，
	// 消除 any 回避——team/rails → core/foundation/llm 无环）。
	ResolveTeamModel(setting TeamModelSetting) (model *llm.Model, signaturePayload string, ok bool)
}

// OrganizationModelRail 在下一次 native 成员模型调用时应用持久专家 Team 模型。
//
// Python: OrganizationModelRail (organization_model_rail.py:22-175)
type OrganizationModelRail struct {
	rails.DeepAgentRail
	// TeamID 团队 ID
	TeamID string
	// SessionID 会话 ID
	SessionID string
	// Role 成员角色（"leader" 才注入策略节与 selection 节）
	Role string
	// backend 组织模型后端（25.9 注入缝；nil = 无持久设置语义）
	backend OrganizationModelBackend

	// signature 上次应用的 payload 签名（Python: self._signature）
	signature string
	// model 上次应用的模型实例（Python: self._model）
	model *llm.Model
	// builder init 时捕获的提示词构建器（Python: self._builder）
	builder saprompt.SystemPromptBuilderInterface
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// organizationModelRailPriority rail 挂载优先级（模型规划 90 之后、运行时模型身份 5 之前）
	// Python: OrganizationModelRail.priority = 6
	organizationModelRailPriority = 6

	// organizationModelPolicySection 策略节名
	// Python: "organization_team_model_policy"
	organizationModelPolicySection = "organization_team_model_policy"

	// organizationModelSelectionSection selection 节名
	// Python: "organization_team_model_selection"
	organizationModelSelectionSection = "organization_team_model_selection"

	// organizationModelSectionPriority 注入节优先级
	// Python: priority=5
	organizationModelSectionPriority = 5

	// organizationModelLogComponent 日志组件
	organizationModelLogComponent = logger.ComponentAgentServer
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOrganizationModelRail 创建组织模型 rail。
//
// Python: OrganizationModelRail.__init__ (organization_model_rail.py:26-33)
func NewOrganizationModelRail(teamID, sessionID, role string, backend OrganizationModelBackend) *OrganizationModelRail {
	r := &OrganizationModelRail{
		DeepAgentRail: *rails.NewDeepAgentRail(),
		TeamID:        teamID,
		SessionID:     sessionID,
		Role:          role,
		backend:       backend,
	}
	r.WithPriority(organizationModelRailPriority)
	return r
}

// Init 捕获提示词构建器，leader 时注入团队模型策略节。
//
// Python: OrganizationModelRail.init (organization_model_rail.py:35-69)
func (r *OrganizationModelRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	r.builder = agent.SystemPromptBuilder()
	if r.Role != "leader" || r.builder == nil {
		return nil
	}
	r.builder.AddSection(saprompt.PromptSection{
		Name:     organizationModelPolicySection,
		Priority: organizationModelSectionPriority,
		Content: map[string]string{
			"cn": "设置团队模型前，先根据创建/邀请结果或 org_view_organization 核对目标身份与已有设置。" +
				"Owner 主团不是子团，禁止对 owner_team_id 调用 org_set_team_model；" +
				"主团当前模型已符合要求时直接执行，不要为了确认而调用切换工具。" +
				"若主团模型不符合要求，应明确说明此工具不能修改主团，不要尝试对子团工具传入主团 ID。" +
				"仅当用户为真实子团指定了模型，且该子团尚无对应的整团持续设置或要求变更时，" +
				"才调用 org_set_team_model，使用真实 team_id 和已配置的模型名。" +
				"子团已有相同 execution_model_selection/持续模型时复用，不要由 Owner 和子团 Leader 重复设置。" +
				"仅当前模型相同但尚无整团持续设置，不等于完成了子团持久化切换。" +
				"该设置覆盖子团 Leader、现有及新建执行成员，持续到下一次明确变更。" +
				"不要只把模型要求写进任务描述，也不要仅创建指定模型的成员来代替整团切换。" +
				"切换失败时明确报告，不能声称已经使用指定模型。",
			"en": "Before changing a Team model, check its identity and saved selection from creation/invitation " +
				"results or org_view_organization. Never pass owner_team_id to org_set_team_model: " +
				"the Owner is not a child Team. If the Owner already uses the requested model, proceed without " +
				"a switch. Otherwise explain that this tool cannot change the Owner model. " +
				"For a real child Team, call org_set_team_model only when the user requests a model and " +
				"the matching persistent Team selection is missing or must change. Reuse a matching " +
				"execution_model_selection; the Owner and child Leader must not set it twice. " +
				"A matching current member model alone does not establish a persistent Team selection. " +
				"This persists for the Leader, existing and new native members until explicitly changed. " +
				"Writing a task constraint or spawning one member is not a Team-wide model switch. " +
				"Report a failed switch instead of claiming the requested model was applied.",
		},
	})
	return nil
}

// Uninit 移除注入的两个节并清空状态。
//
// Python: OrganizationModelRail.uninit (organization_model_rail.py:71-77)
func (r *OrganizationModelRail) Uninit(_ agentinterfaces.BaseAgent) error {
	if r.builder != nil {
		r.builder.RemoveSection(organizationModelPolicySection)
		r.builder.RemoveSection(organizationModelSelectionSection)
	}
	r.builder = nil
	r.model = nil
	r.signature = ""
	return nil
}

// BeforeModelCall 每次模型调用前应用持久团队模型。
//
// Python: OrganizationModelRail.before_model_call (organization_model_rail.py:79-118)
func (r *OrganizationModelRail) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// Python: setting = load_team_model(self.team_id, self.session_id)
	setting, hasSetting := r.loadTeamModel()
	if r.builder != nil {
		r.builder.RemoveSection(organizationModelSelectionSection)
		if hasSetting && r.Role == "leader" {
			// Python: content = json.dumps({team_id, persistent_team_model, execution_model_selection, guidance}, ensure_ascii=False)
			content := jsonMarshalCompact(map[string]any{
				"team_id":                   r.TeamID,
				"persistent_team_model":     setting.ModelName,
				"execution_model_selection": setting.Selection,
				"guidance":                  "Already set for the entire Team. Reuse it unless a different model is requested.",
			})
			r.builder.AddSection(saprompt.PromptSection{
				Name:     organizationModelSelectionSection,
				Content:  map[string]string{"cn": content, "en": content},
				Priority: organizationModelSectionPriority,
			})
		}
	}
	if !hasSetting {
		return nil
	}
	if r.backend == nil {
		return nil
	}
	// 每次调用都重新解析，被移除/禁用的模型绝不在旧默认上静默运行。
	// 凭据只保存在活模型中。
	// Python: compiled = resolve_team_model(setting.selection)
	model, payload, ok := r.backend.ResolveTeamModel(setting)
	if !ok {
		return nil
	}
	signature := sha256Hex(payload)
	agent := cbc.Agent()
	setter, canSet := agent.(interface {
		SetLLM(model *llm.Model)
	})
	if !canSet {
		return fmt.Errorf("Persistent Team model requires an agent with a callable set_llm")
	}
	liveModel := r.liveModelOf(agent)
	if r.signature == signature && liveModel != nil && liveModel == r.model {
		return nil
	}
	if err := r.applyModel(setter, model); err != nil {
		return err
	}
	r.model = model
	r.signature = signature
	return nil
}

// BeforeToolCall 持久团队模型存在时禁止外部 CLI/bridge 成员。
//
// Python: OrganizationModelRail.before_tool_call (organization_model_rail.py:120-131)
func (r *OrganizationModelRail) BeforeToolCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}
	if inputs.ToolName != "spawn_external_cli" && inputs.ToolName != "spawn_bridge_agent" {
		return nil
	}
	if _, hasSetting := r.loadTeamModel(); !hasSetting {
		return nil
	}
	errorMsg := "This Team has a persistent native model selection; external CLI/bridge members are unsupported."
	toolCallID := ""
	if inputs.ToolCall != nil {
		toolCallID = inputs.ToolCall.ID
	}
	cbc.SetExtra("_skip_tool", true)
	inputs.ToolResult = map[string]any{"success": false, "error": errorMsg}
	inputs.ToolMsg = llmschema.NewToolMessage(toolCallID, errorMsg)
	return nil
}

// AfterToolCall 在 Leader 决定切换前暴露所有权与已保存的子团模型。
//
// Python: OrganizationModelRail.after_tool_call (organization_model_rail.py:133-175)
func (r *OrganizationModelRail) AfterToolCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}
	if inputs.ToolName != "org_view_organization" || cbc.Exception() != nil {
		return nil
	}
	if r.backend == nil {
		return nil
	}
	result, isMap := inputs.ToolResult.(map[string]any)
	if !isMap {
		return nil
	}
	success, _ := result["success"].(bool)
	data, isData := result["data"].(map[string]any)
	if !success || !isData {
		return nil
	}
	ownerID, _ := data["owner_team_id"].(string)
	leaders, _ := data["leaders"].([]any)
	for _, item := range leaders {
		leader, isLeader := item.(map[string]any)
		if !isLeader {
			continue
		}
		leaderTeamID, _ := leader["team_id"].(string)
		if leaderTeamID == "" {
			continue
		}
		delete(leader, "execution_model_name")
		delete(leader, "execution_model_selection")
		delete(leader, "execution_model_error")
		leader["is_owner"] = leaderTeamID == ownerID
		if leader["is_owner"].(bool) {
			continue
		}
		setting, hasSetting := r.loadTeamModelFor(leaderTeamID)
		if !hasSetting {
			continue
		}
		leader["execution_model_name"] = setting.ModelName
		leader["execution_model_selection"] = setting.Selection
	}
	data["model_selection_guidance"] = ("Never call org_set_team_model for owner_team_id. Reuse a child's matching persistent " +
		"execution_model_selection; only set a child model when missing or explicitly changed.")
	if inputs.ToolMsg != nil {
		body := map[string]any{"success": success, "data": data}
		inputs.ToolMsg.Content = llmschema.NewTextContent(jsonMarshalCompact(body))
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadTeamModel 经后端读取持久团队模型设置；后端缺省 = 无设置。
func (r *OrganizationModelRail) loadTeamModel() (TeamModelSetting, bool) {
	return r.loadTeamModelFor(r.TeamID)
}

// loadTeamModelFor 经后端读取指定团队的持久模型设置；后端缺省 = 无设置。
func (r *OrganizationModelRail) loadTeamModelFor(teamID string) (TeamModelSetting, bool) {
	if r.backend == nil {
		return TeamModelSetting{}, false
	}
	return r.backend.LoadTeamModel(teamID, r.SessionID)
}

// liveModelOf 读取 agent 的活模型（Python: get_llm()，ValueError → None）。
func (r *OrganizationModelRail) liveModelOf(agent agentinterfaces.BaseAgent) *llm.Model {
	getter, canGet := agent.(interface {
		GetLLM() (*llm.Model, error)
	})
	if !canGet {
		return nil
	}
	m, err := getter.GetLLM()
	if err != nil {
		return nil
	}
	return m
}

// applyModel 应用模型（Python: set_llm(model)）。
func (r *OrganizationModelRail) applyModel(setter interface{ SetLLM(model *llm.Model) }, model *llm.Model) error {
	setter.SetLLM(model)
	return nil
}

// sha256Hex 计算字符串 SHA-256 十六进制摘要。
//
// Python: hashlib.sha256(payload.encode()).hexdigest()
func sha256Hex(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// 编译时验证 rail 满足接口
var _ agentinterfaces.AgentRail = (*OrganizationModelRail)(nil)
