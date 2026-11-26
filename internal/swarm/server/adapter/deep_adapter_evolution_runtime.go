// 单 agent 演进 rail 运行时装配面（ensure/unconfigure/refresh/sync 四函数）。
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/interface_deep.py
// 的 _ensure_active_evolution_rails_registered / _unconfigure_active_evolution_rails /
// _refresh_active_evolution_rail_refs / _sync_active_evolution_review_agent_after_reload。
// 上游 `_build_skill_evolution_rail`（:10236）为零调用死代码，本批按规则 8 删除
// Go 侧对它的移植（buildSkillEvolutionRail），统一走 configure_skill_evolution_runtime 路径。
package adapter

import (
	"context"
	"reflect"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	coreevolution "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/subagent"
)

// ──────────────────────────── 结构体 ────────────────────────────

// deepAgentEvolutionConfigurable 将 *harness.DeepAgent 适配为
// evolution.EvolutionConfigurableAgent（DeepAgent.RegisterRail 带 variadic
// CallbackOption，接口形参无 variadic——Go 方法集差异，委托时丢弃可选项）。
type deepAgentEvolutionConfigurable struct {
	agent *harness.DeepAgent
}

func (w *deepAgentEvolutionConfigurable) FindRailsByType(railTypes ...reflect.Type) []agentinterfaces.AgentRail {
	return w.agent.FindRailsByType(railTypes...)
}
func (w *deepAgentEvolutionConfigurable) AddRail(rail agentinterfaces.AgentRail) {
	w.agent.AddRail(rail)
}
func (w *deepAgentEvolutionConfigurable) StripRailsByType(railTypes ...reflect.Type) int {
	return w.agent.StripRailsByType(railTypes...)
}
func (w *deepAgentEvolutionConfigurable) PendingRails() []agentinterfaces.AgentRail {
	return w.agent.PendingRails()
}
func (w *deepAgentEvolutionConfigurable) RemovePendingRail(rail agentinterfaces.AgentRail) {
	w.agent.RemovePendingRail(rail)
}
func (w *deepAgentEvolutionConfigurable) RegisterRail(ctx context.Context, rail agentinterfaces.AgentRail) error {
	return w.agent.RegisterRail(ctx, rail)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ensureActiveEvolutionRailsRegistered 配置、注册并缓存单 agent 演进 rail。
// Python: _ensure_active_evolution_rails_registered() (interface_deep.py:10595-10624)
//
// 步骤：
//
//	Python: 1. if self._instance is None: return
//	Python: 2. resolved_language = self._resolve_runtime_language()
//	Python: 3. evolution_auto_save = get_evolution_auto_save_enabled(config_base or config_cache)
//	Python: 4. if rail._language != resolved_language: await self._unconfigure_active_evolution_rails()
//	Python: 5. disabled_skills = skill_manager.list_execution_disabled_skills()
//	Python: 6. await configure_skill_evolution_runtime(instance, skills_dir=..., llm=..., model=...,
//	           auto_save=evolution_auto_save, signal_trigger=False, review_trigger=True,
//	           disabled_skills=..., language=..., trajectory_span_processor=...)
//	         （configuration.py:106 —— rails 构建参数 signal_trigger=False 恒、
//	           review_trigger=True 恒、review_runtime 必传 EvolutionReviewRuntime(60s)）
//	Python: 7. self._refresh_active_evolution_rail_refs()
//	Python: 8. if rail is not None: rail.auto_save = evolution_auto_save
func (d *DeepAdapter) ensureActiveEvolutionRailsRegistered(ctx context.Context) {
	// Python: 1. if self._instance is None: return
	if d.instance == nil {
		return
	}

	// Python: 2. resolved_language
	resolvedLanguage := d.resolveRuntimeLanguage()

	// Python: 3. evolution_auto_save = get_evolution_auto_save_enabled(config_base or config_cache)
	evolutionAutoSave := evolutionAutoSaveFrom(d.configBaseSnapshot(), d.configCacheSnapshot())

	// Python: 4. 语言变更 → 先卸载旧 rail 栈
	if d.skillEvolutionRail != nil && d.skillEvolutionRail.ReviewLanguage() != resolvedLanguage {
		d.unconfigureActiveEvolutionRails(ctx)
	}

	// Python: 5. disabled_skills
	var disabledSkills []string
	if d.skillManager != nil {
		disabledSkills = d.skillManager.ListExecutionDisabledSkills()
	}

	// skills_dir + model（Python: get_agent_skills_dir() / self._model / model 兜底链）
	skillsDir := workspace.AgentSkillsDir()
	modelName := d.defaultModelNameSnapshot()
	if modelName == "" {
		if v, ok := d.configCacheSnapshot()["model_name"].(string); ok && v != "" {
			modelName = v
		}
	}
	if modelName == "" {
		modelName = "gpt-4"
	}

	// Python: 6. configure_skill_evolution_runtime(...)（configuration.py:106）
	err := coreevolution.ConfigureSkillEvolutionRuntime(
		&deepAgentEvolutionConfigurable{agent: d.instance},
		[]string{skillsDir},
		d.modelSnapshot(),
		modelName,
		demandSpanProcessorOf(),
		false, // team=false：单 agent 形态
		review.NewEvolutionReviewRuntime(60.0),
		evolutionAutoSave,
		resolvedLanguage,
		false, // signal_trigger=False 恒（Python :10615；信号触发收编团队语义）
		true,  // review_trigger=True 恒（Python :10616；审查自检 follow-up）
		disabledSkills, // Python :10612-10614 disabled_skills 经 rail_kwargs 转发
	)
	if err != nil {
		logger.Error(logComponent).Err(err).
			Msg("ensureActiveEvolutionRailsRegistered: configure failed")
		d.refreshActiveEvolutionRailRefs()
		return
	}

	// Python: 7. self._refresh_active_evolution_rail_refs()
	d.refreshActiveEvolutionRailRefs()

	// Python: 8. rail.auto_save = evolution_auto_save
	if d.skillEvolutionRail != nil {
		d.skillEvolutionRail.SetAutoSave(evolutionAutoSave)
	}
	logger.Info(logComponent).
		Str("model", modelName).
		Bool("auto_save", evolutionAutoSave).
		Msg("ensureActiveEvolutionRailsRegistered: evolution rails configured")
}

// unconfigureActiveEvolutionRails 在重建前移除缓存的单 agent 演进 rail。
// Python: _unconfigure_active_evolution_rails() (interface_deep.py:10631-10655)
//
// 步骤：
//
//	Python: 1. rails = [skill_evolution_rail, evolution_interrupt_rail if not None]
//	Python: 2. unconfigure_skill_evolution(self._instance, team=False)（AttributeError 时退化为逐个 unregister）
//	Python: 3. stale_rails 清理（Go 无 _stale_rails 集合，StripRailsByType 已内含）
//	Python: 4. self._skill_evolution_rail = None; self._evolution_interrupt_rail = None
func (d *DeepAdapter) unconfigureActiveEvolutionRails(ctx context.Context) {
	if d.instance == nil {
		return
	}
	// Python: 2. unconfigure_skill_evolution(self._instance, team=False)
	teamFalse := false
	_ = coreevolution.UnconfigureSkillEvolution(&deepAgentEvolutionConfigurable{agent: d.instance}, &teamFalse)
	// Python: 4. 清空缓存引用
	d.skillEvolutionRail = nil
	d.evolutionInterruptRail = nil
}

// refreshActiveEvolutionRailRefs 在 agent-core runtime configure 后刷新缓存 rail 引用。
// Python: _refresh_active_evolution_rail_refs() (interface_deep.py:10657-10677)
//
// 步骤：
//
//	Python: 1. if self._instance is None: return
//	Python: 2. regular_rails = find_rails((SkillEvolutionRail,))
//	   self._skill_evolution_rail = 第一个「SkillEvolutionRail 且非 EvolutionInterruptRail」
//	   —— 注意：Go 的 EvolutionInterruptRail 不是 SkillEvolutionRail 的子类型，
//	   类型排除判别天然成立（Python 需 isinstance 双判，Go 直接按具体类型匹配）。
//	Python: 3. interrupt_rails = find_rails((EvolutionInterruptRail,)); 取第一个
//	Python: 4. subagent_rails = find_rails((SubagentRail,)); 取第一个
func (d *DeepAdapter) refreshActiveEvolutionRailRefs() {
	// Python: 1. if self._instance is None: return
	if d.instance == nil {
		return
	}

	// Python: 2. SkillEvolutionRail（Go 类型精确匹配，等价于 Python 的双 isinstance 判别）
	skillRails := d.instance.FindRailsByType(reflect.TypeOf(&coreevolution.SkillEvolutionRail{}))
	d.skillEvolutionRail = nil
	if len(skillRails) > 0 {
		if rail, ok := skillRails[0].(*coreevolution.SkillEvolutionRail); ok {
			d.skillEvolutionRail = rail
		}
	}

	// Python: 3. EvolutionInterruptRail
	interruptRails := d.instance.FindRailsByType(reflect.TypeOf(&coreevolution.EvolutionInterruptRail{}))
	d.evolutionInterruptRail = nil
	if len(interruptRails) > 0 {
		if rail, ok := interruptRails[0].(*coreevolution.EvolutionInterruptRail); ok {
			d.evolutionInterruptRail = rail
		}
	}

	// Python: 4. SubagentRail
	subagentRails := d.instance.FindRailsByType(reflect.TypeOf(&subagent.SubagentRail{}))
	d.subagentRail = nil
	if len(subagentRails) > 0 {
		if rail, ok := subagentRails[0].(*subagent.SubagentRail); ok {
			d.subagentRail = rail
		}
	}
}

// syncActiveEvolutionReviewAgentAfterReload 在 DeepAgent 热重载后恢复 SkillEvolutionRail 持有的审查子 agent。
// Python: _sync_active_evolution_review_agent_after_reload() (interface_deep.py:10678-10694)
//
// 步骤：
//
//	Python: 1. if instance is None or skill_evolution_rail is None: return
//	Python: 2. if not get_skill_evolution_enabled(config_base or config_cache): return
//	Python: 3. rail._register_evolution_review_agent(self._instance)
//	Python: 4. self._refresh_active_evolution_rail_refs()
func (d *DeepAdapter) syncActiveEvolutionReviewAgentAfterReload(ctx context.Context) {
	// Python: 1.
	if d.instance == nil || d.skillEvolutionRail == nil {
		return
	}
	// Python: 2.
	if !skillEvolutionEnabledFrom(d.configBaseSnapshot(), d.configCacheSnapshot()) {
		return
	}
	// Python: 3. rail._register_evolution_review_agent(instance)
	d.skillEvolutionRail.EnsureEvolutionReviewAgent(d.instance)
	// Python: 4.
	d.refreshActiveEvolutionRailRefs()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// evolutionSectionFrom 归一读取 evolution 配置段：完整配置（react.evolution）
// 与 react 段缓存（顶层 evolution）双形态兼容。
// Python: _get_evolution_config (config.py:317-328)——上游入参恒为完整配置；
// Go adapter 的 configCache 历史存 react 子段，故双形态兼容（Go 侧读取面收敛助手）。
func evolutionSectionFrom(base, cache map[string]any) map[string]any {
	for _, cfg := range []map[string]any{base, cache} {
		if len(cfg) == 0 {
			continue // Python `a or b`：空 dict 为 falsy
		}
		if react, ok := cfg["react"].(map[string]any); ok {
			if evo, ok := react["evolution"].(map[string]any); ok && len(evo) > 0 {
				return evo
			}
			continue
		}
		if evo, ok := cfg["evolution"].(map[string]any); ok && len(evo) > 0 {
			return evo
		}
	}
	return map[string]any{}
}

// skillEvolutionEnabledFrom 读 react.evolution.skill_evolution。
// Python: get_skill_evolution_enabled (config.py:335-337)。
func skillEvolutionEnabledFrom(base, cache map[string]any) bool {
	return evolutionSectionFrom(base, cache)["skill_evolution"] == true
}

// evolutionAutoSaveFrom 读 react.evolution.auto_save。
// Python: get_evolution_auto_save_enabled (config.py:524-526)。
func evolutionAutoSaveFrom(base, cache map[string]any) bool {
	return evolutionSectionFrom(base, cache)["auto_save"] == true
}
