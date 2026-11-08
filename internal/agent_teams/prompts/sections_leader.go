package prompts

import (
	"strings"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// 本文件承载 leader 的渐进式披露三件套（F_76 / F_122 / F_125）。
//
// leader 的系统提示词不含任何协作政策，只有一段起手 + 调用方自己的提示词；
// 其余全部由 build_team 的工具结果披露，或（framework 建队时）以
// <team-policy> 块随首条输入到达。这三段分别对应三种开局：
//   - team_bootstrap    —— leader 自己调 build_team 建队（默认）
//   - team_prebuilt     —— 框架已建好团队，没有 build_team 这一步
//   - team_bound_group  —— scheduled + 绑定专家团，指示先在团里找
//
// Python: prompts/sections.py 的 build_leader_*_section 三函数

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildLeaderBootstrapSection 构建 leader 的系统提示词团队 section（F_76）。
//
// leader 的系统提示词不含任何协作政策——只有它是谁、该走哪个机制、以及
// 「先建队」这一句。其余全部在那次工具调用的结果里披露，所以 leader 永远
// 不会读到它这个团队并不运行的某个调度模式的约定（见 BuildLeaderPolicyDisclosure）。
//
// {{collaboration_mechanism}} 槽位装的是 build_team 与 swarmflow 的决策指南，
// 它自己一个模板（leader_swarmflow），只在 leader 真的持有 swarmflow 工具时
// 才填——与工具工厂门控那个工具是同一个信号。没有这个工具时槽位塌成空，
// bootstrap 就只剩一句「先建队」，leader 不会为一个它没法运行的机制纠结。
//
// Python: build_leader_bootstrap_section (prompts/sections.py L151-188)
func BuildLeaderBootstrapSection(swarmflowEnabled bool, language string) (*saprompt.PromptSection, error) {
	mechanism := ""
	if swarmflowEnabled {
		tpl, err := LoadTemplate("leader_swarmflow", language)
		if err != nil {
			return nil, err
		}
		mechanism = "\n" + strings.TrimSpace(tpl.Content()) + "\n"
	}
	tpl, err := LoadTemplate("leader_bootstrap", language)
	if err != nil {
		return nil, err
	}
	body := strings.TrimSpace(tpl.Render(map[string]string{"collaboration_mechanism": mechanism}))
	section := saprompt.NewPromptSection(SectionBootstrap, map[string]string{language: body + "\n"}, 11)
	return &section, nil
}

// BuildLeaderBoundGroupSection 构建命名绑定 AgentGroup 的 leader section。
//
// 只在 scheduled 调度 + 绑定了专家团 + 专家工具已接线时加上——与把
// view_experts 限定在该团的信号相同，于是「先在这个团里找」的提示词和
// 做这件事的工具一起出现。它只报团名：成员要靠 view_experts 找出来再按需
// 拉起，所以这里不读任何包，前缀保持字节稳定。
//
// Python: build_leader_bound_group_section (prompts/sections.py L190-220)
func BuildLeaderBoundGroupSection(group, language string) (*saprompt.PromptSection, error) {
	tpl, err := LoadTemplate("leader_bound_group", language)
	if err != nil {
		return nil, err
	}
	body := strings.TrimSpace(tpl.Render(map[string]string{"group": group}))
	section := saprompt.NewPromptSection(SectionBoundGroup, map[string]string{language: body + "\n"}, 12)
	return &section, nil
}

// BuildLeaderPrebuiltSection 构建 framework 建队 leader 的开场 section（F_122）。
//
// bootstrap 在 bootstrap_mode="framework" 下的对应物：leader 启动时团队
// 已经存在，也没有 build_team 可调，所以本节只说它自己是谁、这个团队是个
// 尚无成员的通用团队、以及政策 / 团队信息 / 名册会随第一条输入以
// <team-policy> / <team-context> 块到达。它不带协作机制槽位：那条
// 「build_team 还是 swarmflow」的路由指南在这里指的是一条不存在的路径。
//
// Python: build_leader_prebuilt_section (prompts/sections.py L222-252)
func BuildLeaderPrebuiltSection(language string) (*saprompt.PromptSection, error) {
	tpl, err := LoadTemplate("leader_prebuilt", language)
	if err != nil {
		return nil, err
	}
	body := strings.TrimSpace(tpl.Content())
	section := saprompt.NewPromptSection(SectionPrebuilt, map[string]string{language: body + "\n"}, 11)
	return &section, nil
}
