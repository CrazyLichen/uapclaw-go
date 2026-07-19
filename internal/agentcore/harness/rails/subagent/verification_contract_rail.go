package subagent

import (
	"context"

	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// VerificationContractRail 向父 Agent 注入验证门控契约。
// 挂载在父 Agent（实现代理）上，而非验证代理自身。
//
// "非平凡"定义：
//   - 单轮内编辑了 3 个或更多文件
//   - 后端、API 或服务变更
//   - 基础设施或配置变更
//
// 父代理拥有门控——它不能自行指定判决，
// 必须循环（修复→重验）直到验证代理发出 VERDICT: PASS。
//
// 对齐 Python: VerificationContractRail (openjiuwen/harness/rails/subagent/verification_contract_rail.py)
type VerificationContractRail struct {
	rails.DeepAgentRail
	// promptBuilder 系统提示词构建器引用
	promptBuilder saprompt.SystemPromptBuilderInterface
	// section 预构建的契约 section
	section *saprompt.PromptSection
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// verificationContractRailPriority VerificationContractRail 优先级
	// 对齐 Python: VerificationContractRail.priority = 88
	// Priority 88: 在 PlanModeRail(85) 之后、TodoRail(90) 之前，
	// 位于组装提示词末尾附近，作为"最后提醒"
	verificationContractRailPriority = 88

	// contractSectionPriority 契约节优先级
	// 对齐 Python: _CONTRACT_PRIORITY = 88
	contractSectionPriority = 88
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// 提示词一比一复刻 Python 原文，不做自行翻译

	// contractEN 英文验证门控契约
	// 对齐 Python: _CONTRACT_EN
	contractEN = "## Verification Gate\n\n" +
		"After any non-trivial implementation turn, you MUST spawn the verification " +
		"agent before reporting completion to the user.\n\n" +
		"**Non-trivial means any of:**\n" +
		"- 3 or more file edits in a single turn\n" +
		"- Backend, API, or service changes\n" +
		"- Infrastructure or configuration changes\n\n" +
		"**How to spawn:**\n" +
		"Use task_tool with subagent_type=\"verification_agent\". Pass:\n" +
		"1. The original user request (verbatim)\n" +
		"2. All files changed (full paths)\n" +
		"3. The approach you took\n" +
		"4. Plan file path if one was used\n\n" +
		"**On VERDICT: PASS**\n" +
		"Spot-check the report: re-run 2-3 of the commands listed in the verification " +
		"report and confirm the output matches what the verifier observed. If every " +
		"spot-checked command matches, report completion to the user.\n\n" +
		"**On VERDICT: FAIL**\n" +
		"Do not report completion. Fix the issue, then re-invoke task_tool with " +
		"subagent_type=\"verification_agent\". The same verification session will resume " +
		"(deterministic session ID). Pass the previous FAIL output and describe what " +
		"you fixed. Repeat until VERDICT: PASS.\n\n" +
		"**On VERDICT: PARTIAL**\n" +
		"Report what was verified and what could not be verified due to environmental " +
		"limitations (e.g. service could not start, tool unavailable). Be explicit " +
		"about the gap.\n\n" +
		"**You cannot self-assign any verdict.** Only the verification agent issues " +
		"PASS, FAIL, or PARTIAL. Your own checks and caveats do not substitute."

	// contractCN 中文验证门控契约
	// 对齐 Python: _CONTRACT_CN
	contractCN = "## 验证门控\n\n" +
		"在任何非平凡实现轮次之后，你必须在向用户汇报完成之前启动验证代理。\n\n" +
		"**非平凡指以下任意情况：**\n" +
		"- 单轮内编辑了 3 个或更多文件\n" +
		"- 后端、API 或服务变更\n" +
		"- 基础设施或配置变更\n\n" +
		"**如何启动：**\n" +
		"使用 task_tool，subagent_type=\"verification_agent\"。传入：\n" +
		"1. 原始用户请求（原文）\n" +
		"2. 所有已更改的文件（完整路径）\n" +
		"3. 你采用的实现方式\n" +
		"4. 计划文件路径（如有）\n\n" +
		"**收到 VERDICT: PASS 时**\n" +
		"抽查报告：从验证报告中重新运行 2-3 条命令，确认输出与验证代理观察到的一致。" +
		"若每条抽查命令均匹配，则向用户汇报完成。\n\n" +
		"**收到 VERDICT: FAIL 时**\n" +
		"不得汇报完成。修复问题后，再次调用 task_tool，subagent_type=\"verification_agent\"。" +
		"同一验证会话将继续（确定性会话 ID）。传入之前的 FAIL 输出并说明你修复了什么。" +
		"重复此过程直到收到 VERDICT: PASS。\n\n" +
		"**收到 VERDICT: PARTIAL 时**\n" +
		"汇报哪些内容已验证，哪些因环境限制（如服务无法启动、工具不可用）未能验证。" +
		"请明确说明缺口所在。\n\n" +
		"**你不能自行指定任何判决。** 只有验证代理才能发出 PASS、FAIL 或 PARTIAL。" +
		"你自己的检查和注意事项不能替代验证代理的判决。"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewVerificationContractRail 创建 VerificationContractRail 实例。
//
// 对齐 Python: VerificationContractRail()
func NewVerificationContractRail() *VerificationContractRail {
	r := &VerificationContractRail{
		DeepAgentRail: *rails.NewDeepAgentRail(),
	}
	r.WithPriority(verificationContractRailPriority)
	return r
}

// Init 初始化钩子：捕获 system_prompt_builder，预构建契约 section。
//
// 对齐 Python: VerificationContractRail.init(agent)
// Python L140-153
func (r *VerificationContractRail) Init(agent agentinterfaces.BaseAgent) error {
	r.promptBuilder = agent.SystemPromptBuilder()

	// 预构建契约 section
	// 对齐 Python L148-152:
	//   self._section = PromptSection(
	//       name=SectionName.VERIFICATION_CONTRACT,
	//       content={"en": _CONTRACT_EN, "cn": _CONTRACT_CN},
	//       priority=_CONTRACT_PRIORITY,
	//   )
	section := saprompt.PromptSection{
		Name:     hsections.SectionVerificationContract,
		Content:  map[string]string{"en": contractEN, "cn": contractCN},
		Priority: contractSectionPriority,
	}
	r.section = &section

	logger.Info(logger.ComponentAgentCore).Msg("[VerificationContractRail] 已初始化")
	return nil
}

// BeforeModelCall 每轮注入验证门控契约 section。
//
// 先移除再添加，避免跨轮次累积重复。
//
// 对齐 Python: VerificationContractRail.before_model_call(ctx)
// Python L155-169
func (r *VerificationContractRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.promptBuilder == nil || r.section == nil {
		return nil
	}

	// 对齐 Python L167-168:
	//   self.system_prompt_builder.remove_section(SectionName.VERIFICATION_CONTRACT)
	//   self.system_prompt_builder.add_section(self._section)
	r.promptBuilder.RemoveSection(hsections.SectionVerificationContract)
	r.promptBuilder.AddSection(*r.section)

	logger.Debug(logger.ComponentAgentCore).Msg("[VerificationContractRail] 已注入验证契约 section")
	return nil
}

// compile-time check
var _ agentinterfaces.AgentRail = (*VerificationContractRail)(nil)
