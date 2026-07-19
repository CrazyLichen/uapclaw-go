package llm_call

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InstructionOptimizer 通过 LLM 文本梯度优化改写 system_prompt 和 user_prompt。
//
// Backward 阶段：从失败信号提取 bad cases → 调用 LLM 生成文本梯度
// （分析为什么 prompt 失败）→ 预计算优化后的 system_prompt 和 user_prompt。
// Step 阶段：返回预计算的优化后 prompt，由 Trainer 统一 apply 到 LLMCallOperator。
//
// 对应 Python: openjiuwen/agent_evolving/optimizer/llm_call/instruction_optimizer.py InstructionOptimizer
type InstructionOptimizer struct {
	LLMCallOptimizerBase
	// model LLM 调用实例
	model *llm.Model
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent llm_call 包日志组件常量
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInstructionOptimizer 创建 InstructionOptimizer 实例。
//
// 对应 Python: InstructionOptimizer(model_config, model_client_config)
func NewInstructionOptimizer(model *llm.Model) *InstructionOptimizer {
	return &InstructionOptimizer{
		model: model,
	}
}

// Bind 过滤并绑定可优化的 Operator，返回匹配数量。
//
// 对应 Python: BaseOptimizer.bind(operators, targets, **config)
func (o *InstructionOptimizer) Bind(operators map[string]operator.Operator, targets []string, config map[string]any) int {
	return o.BaseOptimizerMixin.Bind(operators, targets, config)
}

// AddTrajectory 缓存 Trajectory 供 backward 阶段查询。
func (o *InstructionOptimizer) AddTrajectory(traj *trajectory.Trajectory) {
	o.BaseOptimizerMixin.AddTrajectory(traj)
}

// GetTrajectories 返回当前缓存的轨迹列表（副本）。
func (o *InstructionOptimizer) GetTrajectories() []*trajectory.Trajectory {
	return o.BaseOptimizerMixin.GetTrajectories()
}

// ClearTrajectories 清空轨迹缓存。
func (o *InstructionOptimizer) ClearTrajectories() {
	o.BaseOptimizerMixin.ClearTrajectories()
}

// Parameters 返回梯度容器的副本。
func (o *InstructionOptimizer) Parameters() map[string]*optimizer.TextualParameter {
	return o.BaseOptimizerMixin.Parameters()
}

// SelectSignals 仅保留失败驱动信号用于 prompt 优化。
//
// 对齐 Python: InstructionOptimizer._select_signals(signals)
//
// 过滤规则：
//   - 信号类型为 execution_failure / low_score / user_correction / collaboration_failure
//   - 或者 context.score == 0
func (o *InstructionOptimizer) SelectSignals(signals []*signal.EvolutionSignal) []*signal.EvolutionSignal {
	failureTypes := map[string]bool{
		"execution_failure":    true,
		"low_score":           true,
		"user_correction":     true,
		"collaboration_failure": true,
	}

	var selected []*signal.EvolutionSignal
	for _, sig := range signals {
		ctx := sig.Context
		if ctx == nil {
			ctx = map[string]any{}
		}
		// score == 0 的信号也保留
		if score, ok := ctx["score"]; ok && score == 0 {
			selected = append(selected, sig)
			continue
		}
		if failureTypes[sig.SignalType] {
			selected = append(selected, sig)
		}
	}
	return selected
}

// Backward 反向传播：从信号计算梯度并预计算优化后 prompt。
//
// 对齐 Python: BaseOptimizer.backward(signals)
//   self._validate_parameters()
//   self._selected_signals = self._select_signals(signals)
//   try: await self._backward(signals)
func (o *InstructionOptimizer) Backward(ctx context.Context, signals []*signal.EvolutionSignal) error {
	o.ValidateParameters()

	// 对齐 Python: self._selected_signals = self._select_signals(signals)
	selected := o.SelectSignals(signals)
	o.BaseOptimizerMixin.SetSelectedSignals(selected)

	if err := o.backward(ctx, selected); err != nil {
		return exception.NewBaseError(
			exception.NewStatusCode("TOOLCHAIN_OPTIMIZER_BACKWARD_EXECUTION_ERROR", 174040, ""),
			exception.WithMsg(err.Error()),
			exception.WithCause(err),
		)
	}
	return nil
}

// Step 生成更新映射，由 Trainer.apply_updates 统一应用。
//
// 对齐 Python: BaseOptimizer.step()
//   self._validate_parameters()
//   try: updates = self._step()
//   self.clear_trajectories()
//   return updates or {}
func (o *InstructionOptimizer) Step() map[schema.UpdateKey]any {
	o.ValidateParameters()
	updates := o.step()
	o.ClearTrajectories()
	return updates
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// backward 反向传播主逻辑。
//
// 对齐 Python: InstructionOptimizer._backward(signals)
//
// 逻辑：
//  1. 遍历每个 parameter，清空上一轮优化缓存
//  2. 没有选中信号则跳过
//  3. 生成文本梯度
//  4. 设置 system_prompt / user_prompt 梯度
//  5. 根据 targets 决定优化方式：optimizeBoth / optimizeSingle
//  6. 结果写入 param.SetGradient("xxx_optimized", val)
func (o *InstructionOptimizer) backward(ctx context.Context, selectedSignals []*signal.EvolutionSignal) error {
	params := o.BaseOptimizerMixin.Parameters()
	ops := o.BaseOptimizerMixin.Operators()
	targets := o.BaseOptimizerMixin.Targets()

	for opID, param := range params {
		op, ok := ops[opID]
		if !ok {
			continue
		}

		// 对齐 Python: param.set_gradient("system_prompt_optimized", None)
		//             param.set_gradient("user_prompt_optimized", None)
		param.SetGradient("system_prompt_optimized", nil)
		param.SetGradient("user_prompt_optimized", nil)

		// 对齐 Python: if not self._selected_signals: continue
		if len(selectedSignals) == 0 {
			continue
		}

		// 对齐 Python: textual_gradient = await self._generate_textual_gradient(op)
		gradient, err := o.generateTextualGradient(ctx, op)
		if err != nil {
			logger.Error(logComponent).
				Str("method", "backward").
				Str("operator_id", opID).
				Err(err).
				Msg("[optimizer] 生成文本梯度失败")
			continue
		}

		// 对齐 Python:
		//   if not self._is_target_frozen(op, "system_prompt"):
		//       param.set_gradient("system_prompt", textual_gradient)
		if !o.isTargetFrozen(op, "system_prompt") {
			param.SetGradient("system_prompt", gradient)
		}
		// 对齐 Python:
		//   if not self._is_target_frozen(op, "user_prompt"):
		//       param.set_gradient("user_prompt", textual_gradient)
		if !o.isTargetFrozen(op, "user_prompt") {
			param.SetGradient("user_prompt", gradient)
		}

		// 对齐 Python: 预计算优化后 prompt
		//   has_sys = "system_prompt" in self._targets and not self._is_target_frozen(op, "system_prompt")
		//   has_usr = "user_prompt" in self._targets and not self._is_target_frozen(op, "user_prompt")
		hasSys := containsTarget(targets, "system_prompt") && !o.isTargetFrozen(op, "system_prompt")
		hasUsr := containsTarget(targets, "user_prompt") && !o.isTargetFrozen(op, "user_prompt")

		if hasSys && hasUsr {
			// 对齐 Python: sys_val, usr_val = await self._optimize_both(op, param)
			sysVal, usrVal, err := o.optimizeBoth(ctx, op, param)
			if err != nil {
				logger.Error(logComponent).
					Str("method", "backward").
					Str("operator_id", opID).
					Err(err).
					Msg("[optimizer] 联合优化失败")
				continue
			}
			if sysVal != "" {
				param.SetGradient("system_prompt_optimized", sysVal)
			}
			if usrVal != "" {
				param.SetGradient("user_prompt_optimized", usrVal)
			}
		} else if hasSys {
			// 对齐 Python: val = await self._optimize_single(op, param, "system_prompt")
			val, err := o.optimizeSingle(ctx, op, param, "system_prompt")
			if err != nil {
				logger.Error(logComponent).
					Str("method", "backward").
					Str("operator_id", opID).
					Err(err).
					Msg("[optimizer] 单独优化 system_prompt 失败")
				continue
			}
			if val != "" {
				param.SetGradient("system_prompt_optimized", val)
			}
		} else if hasUsr {
			// 对齐 Python: val = await self._optimize_single(op, param, "user_prompt")
			val, err := o.optimizeSingle(ctx, op, param, "user_prompt")
			if err != nil {
				logger.Error(logComponent).
					Str("method", "backward").
					Str("operator_id", opID).
					Err(err).
					Msg("[optimizer] 单独优化 user_prompt 失败")
				continue
			}
			if val != "" {
				param.SetGradient("user_prompt_optimized", val)
			}
		}
	}
	return nil
}

// step 返回预计算的优化后 prompt 映射。
//
// 对齐 Python: InstructionOptimizer._step()
//   updates = {}
//   for op_id, param in self._parameters.items():
//       sys_val = param.get_gradient("system_prompt_optimized")
//       usr_val = param.get_gradient("user_prompt_optimized")
//       if sys_val: updates[(op_id, "system_prompt")] = sys_val
//       if usr_val: updates[(op_id, "user_prompt")] = usr_val
//   return updates if updates else None
func (o *InstructionOptimizer) step() map[schema.UpdateKey]any {
	updates := make(map[schema.UpdateKey]any)
	params := o.BaseOptimizerMixin.Parameters()

	for opID, param := range params {
		if sysVal := param.GetGradient("system_prompt_optimized"); sysVal != nil {
			if s, ok := sysVal.(string); ok && s != "" {
				updates[schema.UpdateKey{opID, "system_prompt"}] = s
			}
		}
		if usrVal := param.GetGradient("user_prompt_optimized"); usrVal != nil {
			if s, ok := usrVal.(string); ok && s != "" {
				updates[schema.UpdateKey{opID, "user_prompt"}] = s
			}
		}
	}

	return updates
}

// generateTextualGradient 使用 LLM 分析为什么当前 prompt 失败。
//
// 对齐 Python: InstructionOptimizer._generate_textual_gradient(op)
//   system_tpl = self._get_prompt_template(op, "system_prompt")
//   user_tpl = self._get_prompt_template(op, "user_prompt")
//   messages = CREATE_PROMPT_TEXTUAL_GRADIENT_TEMPLATE.format({...}).to_messages()
//   raw_response = (await self._model.invoke(messages)).content
//   return raw_response if isinstance(raw_response, str) else str(raw_response)
func (o *InstructionOptimizer) generateTextualGradient(ctx context.Context, op operator.Operator) (string, error) {
	sysTpl := o.getPromptTemplate(op, "system_prompt")
	usrTpl := o.getPromptTemplate(op, "user_prompt")

	keywords := map[string]any{
		"system_prompt":     evolving.GetContentStringFromTemplate(sysTpl),
		"user_prompt":       evolving.GetContentStringFromTemplate(usrTpl),
		"bad_cases":         o.formatBadCases(),
		"tools_description": "None",
	}

	formatted, err := CreatePromptTextualGradientTemplate.Format(keywords)
	if err != nil {
		return "", err
	}
	messages, err := formatted.ToMessages()
	if err != nil {
		return "", err
	}

	return o.invokeLLM(ctx, messages)
}

// invokeLLM 调用 LLM 并返回字符串内容。
//
// 对齐 Python: InstructionOptimizer._invoke_llm(messages)
//   raw = (await self._model.invoke(messages)).content
//   return raw if isinstance(raw, str) else str(raw)
func (o *InstructionOptimizer) invokeLLM(ctx context.Context, messages []llmschema.BaseMessage) (string, error) {
	msgsParam := model_clients.NewMessagesParam(messages...)
	response, err := o.model.Invoke(ctx, msgsParam)
	if err != nil {
		return "", err
	}
	return response.GetContent().Text(), nil
}

// optimizeBoth 联合优化 system 和 user prompt。
//
// 对齐 Python: InstructionOptimizer._optimize_both(op, param)
//   system_tpl = self._get_prompt_template(op, "system_prompt")
//   user_tpl = self._get_prompt_template(op, "user_prompt")
//   gradient = param.get_gradient("system_prompt") or ""
//   messages = PROMPT_INSTRUCTION_OPTIMIZE_BOTH_TEMPLATE.format({...}).to_messages()
//   raw_response = await self._invoke_llm(messages)
//   sys_prompt = self._extract_tag(raw_response, "SYSTEM_PROMPT_OPTIMIZED")
//   usr_prompt = self._extract_tag(raw_response, "USER_PROMPT_OPTIMIZED")
//   sys_prompt = await self._restore_placeholders(...) if sys_prompt else None
//   usr_prompt = await self._restore_placeholders(...) if usr_prompt else None
//   return sys_prompt, usr_prompt
func (o *InstructionOptimizer) optimizeBoth(ctx context.Context, op operator.Operator, param *optimizer.TextualParameter) (string, string, error) {
	sysTpl := o.getPromptTemplate(op, "system_prompt")
	usrTpl := o.getPromptTemplate(op, "user_prompt")

	// 对齐 Python: gradient = param.get_gradient("system_prompt") or ""
	gradient, _ := param.GetGradient("system_prompt").(string)

	keywords := map[string]any{
		"system_prompt":            evolving.GetContentStringFromTemplate(sysTpl),
		"user_prompt":              evolving.GetContentStringFromTemplate(usrTpl),
		"bad_cases":                o.formatBadCases(),
		"reflections_on_bad_cases": gradient,
		"tools_description":        "None",
	}

	formatted, err := PromptInstructionOptimizeBothTemplate.Format(keywords)
	if err != nil {
		return "", "", err
	}
	messages, err := formatted.ToMessages()
	if err != nil {
		return "", "", err
	}

	rawResponse, err := o.invokeLLM(ctx, messages)
	if err != nil {
		return "", "", err
	}

	// 对齐 Python:
	//   sys_prompt = self._extract_tag(raw_response, "SYSTEM_PROMPT_OPTIMIZED")
	//   usr_prompt = self._extract_tag(raw_response, "USER_PROMPT_OPTIMIZED")
	sysPrompt := extractTag(rawResponse, "SYSTEM_PROMPT_OPTIMIZED")
	usrPrompt := extractTag(rawResponse, "USER_PROMPT_OPTIMIZED")

	// 对齐 Python:
	//   sys_prompt = await self._restore_placeholders(
	//       TuneUtils.get_content_string_from_template(system_tpl),
	//       sys_prompt or "",
	//   ) if sys_prompt else None
	if sysPrompt != "" {
		sysPrompt, err = o.restorePlaceholders(ctx, evolving.GetContentStringFromTemplate(sysTpl), sysPrompt)
		if err != nil {
			logger.Warn(logComponent).
				Str("method", "optimizeBoth").
				Err(err).
				Msg("[optimizer] 恢复 system_prompt 占位符失败")
		}
	}

	if usrPrompt != "" {
		usrPrompt, err = o.restorePlaceholders(ctx, evolving.GetContentStringFromTemplate(usrTpl), usrPrompt)
		if err != nil {
			logger.Warn(logComponent).
				Str("method", "optimizeBoth").
				Err(err).
				Msg("[optimizer] 恢复 user_prompt 占位符失败")
		}
	}

	return sysPrompt, usrPrompt, nil
}

// optimizeSingle 单独优化一个 prompt。
//
// 对齐 Python: InstructionOptimizer._optimize_single(op, param, prompt_type)
//   target_tpl = self._get_prompt_template(op, prompt_type)
//   gradient = param.get_gradient(prompt_type) or ""
//   messages = PROMPT_INSTRUCTION_OPTIMIZE_TEMPLATE.format({...}).to_messages()
//   raw_response = await self._invoke_llm(messages)
//   optimized = self._extract_tag(raw_response, "PROMPT_OPTIMIZED")
//   if optimized:
//       optimized = await self._restore_placeholders(...)
//   return optimized
func (o *InstructionOptimizer) optimizeSingle(ctx context.Context, op operator.Operator, param *optimizer.TextualParameter, promptType string) (string, error) {
	targetTpl := o.getPromptTemplate(op, promptType)

	// 对齐 Python: gradient = param.get_gradient(prompt_type) or ""
	gradient, _ := param.GetGradient(promptType).(string)

	keywords := map[string]any{
		"prompt_instruction":       evolving.GetContentStringFromTemplate(targetTpl),
		"bad_cases":                o.formatBadCases(),
		"reflections_on_bad_cases": gradient,
		"tools_description":        "None",
	}

	formatted, err := PromptInstructionOptimizeTemplate.Format(keywords)
	if err != nil {
		return "", err
	}
	messages, err := formatted.ToMessages()
	if err != nil {
		return "", err
	}

	rawResponse, err := o.invokeLLM(ctx, messages)
	if err != nil {
		return "", err
	}

	// 对齐 Python: optimized = self._extract_tag(raw_response, "PROMPT_OPTIMIZED")
	optimized := extractTag(rawResponse, "PROMPT_OPTIMIZED")
	if optimized == "" {
		return "", nil
	}

	// 对齐 Python:
	//   if optimized:
	//       optimized = await self._restore_placeholders(
	//           TuneUtils.get_content_string_from_template(target_tpl),
	//           optimized,
	//       )
	optimized, err = o.restorePlaceholders(ctx, evolving.GetContentStringFromTemplate(targetTpl), optimized)
	if err != nil {
		logger.Warn(logComponent).
			Str("method", "optimizeSingle").
			Str("prompt_type", promptType).
			Err(err).
			Msg("[optimizer] 恢复占位符失败")
	}
	return optimized, nil
}

// formatBadCases 格式化选中的失败信号为 LLM 提示词文本。
//
// 对齐 Python: InstructionOptimizer._format_bad_cases()
//   parts = []
//   for signal in self._selected_signals:
//       ctx = signal.context or {}
//       formatted = CREATE_BAD_CASE_TEMPLATE.format({
//           "question": ctx.get("question", ""),
//           "label": ctx.get("label", ""),
//           "answer": ctx.get("answer", ""),
//           "reason": ctx.get("reason", ""),
//       })
//       content = formatted.content
//       if isinstance(content, str): parts.append(content)
//       elif content: parts.append(str(content))
//   return "".join(parts)
func (o *InstructionOptimizer) formatBadCases() string {
	selectedSignals := o.BaseOptimizerMixin.SelectedSignals()
	var parts []string
	for _, sig := range selectedSignals {
		ctx := sig.Context
		if ctx == nil {
			ctx = map[string]any{}
		}

		question, _ := ctx["question"].(string)
		label, _ := ctx["label"].(string)
		answer, _ := ctx["answer"].(string)
		reason, _ := ctx["reason"].(string)

		keywords := map[string]any{
			"question": question,
			"label":    label,
			"answer":   answer,
			"reason":   reason,
		}
		formatted, err := CreateBadCaseTemplate.Format(keywords)
		if err != nil {
			continue
		}
		if s, ok := formatted.Content.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "")
}

// restorePlaceholders 确保优化后 prompt 保留与原始 prompt 相同的占位符。
//
// 对齐 Python: InstructionOptimizer._restore_placeholders(original_prompt, optimized_prompt)
//
// 逻辑：
//  1. 提取原始和优化后 prompt 的 input_keys
//  2. 计算缺失的占位符
//  3. 如果没有缺失 → 直接返回
//  4. 如果有缺失 → 用 LLM 恢复
//  5. 如果 LLM 恢复后仍有缺失 → 手动追加
func (o *InstructionOptimizer) restorePlaceholders(ctx context.Context, originalPrompt, optimizedPrompt string) (string, error) {
	// 对齐 Python:
	//   original_keys = PromptAssembler(original_prompt).input_keys
	//   optimized_keys = PromptAssembler(optimized_prompt).input_keys
	originalAssembler, err := prompt.NewPromptAssembler(originalPrompt)
	if err != nil {
		return optimizedPrompt, nil
	}
	optimizedAssembler, err := prompt.NewPromptAssembler(optimizedPrompt)
	if err != nil {
		return optimizedPrompt, nil
	}

	originalKeys := originalAssembler.InputKeys()
	optimizedKeys := optimizedAssembler.InputKeys()

	// 对齐 Python: missing = set(original_keys) - set(optimized_keys)
	optimizedKeySet := make(map[string]bool, len(optimizedKeys))
	for _, k := range optimizedKeys {
		optimizedKeySet[k] = true
	}

	var missing []string
	for _, k := range originalKeys {
		if !optimizedKeySet[k] {
			missing = append(missing, k)
		}
	}

	// 对齐 Python: if not missing: return optimized_prompt
	if len(missing) == 0 {
		return optimizedPrompt, nil
	}

	// 对齐 Python:
	//   messages = PLACEHOLDER_RESTORE_TEMPLATE.format({
	//       "original_prompt": original_prompt,
	//       "revised_prompt": optimized_prompt,
	//       "all_placeholders": str(list(original_keys)),
	//       "missing_placeholders": str(list(missing)),
	//   }).to_messages()
	//   raw = await self._invoke_llm(messages)
	keywords := map[string]any{
		"original_prompt":      originalPrompt,
		"revised_prompt":       optimizedPrompt,
		"all_placeholders":     fmt.Sprintf("%v", originalKeys),
		"missing_placeholders": fmt.Sprintf("%v", missing),
	}

	formatted, err := PlaceholderRestoreTemplate.Format(keywords)
	if err != nil {
		return appendMissingPlaceholders(optimizedPrompt, missing), nil
	}

	messages, err := formatted.ToMessages()
	if err != nil {
		return appendMissingPlaceholders(optimizedPrompt, missing), nil
	}

	raw, err := o.invokeLLM(ctx, messages)
	if err != nil {
		return appendMissingPlaceholders(optimizedPrompt, missing), nil
	}

	// 对齐 Python:
	//   restored_keys = PromptAssembler(raw).input_keys
	//   still_missing = set(original_keys) - set(restored_keys)
	//   if still_missing:
	//       placeholder_text = "\n".join(f"{{{{{ph}}}}}" for ph in still_missing)
	//       raw = str(raw) + "\n" + placeholder_text
	//   return raw if isinstance(raw, str) else optimized_prompt
	restoredAssembler, err := prompt.NewPromptAssembler(raw)
	if err != nil {
		return raw, nil
	}
	restoredKeys := restoredAssembler.InputKeys()
	restoredKeySet := make(map[string]bool, len(restoredKeys))
	for _, k := range restoredKeys {
		restoredKeySet[k] = true
	}

	var stillMissing []string
	for _, k := range originalKeys {
		if !restoredKeySet[k] {
			stillMissing = append(stillMissing, k)
		}
	}

	if len(stillMissing) > 0 {
		raw = appendMissingPlaceholders(raw, stillMissing)
	}

	return raw, nil
}

// extractTag 提取 XML 标签内容。
//
// 对齐 Python: InstructionOptimizer._extract_tag(response, tag)
//   pattern = rf"<{tag}>(.*?)</{tag}>"
//   match = re.search(pattern, response, re.DOTALL)
//   if not match: return None
//   content = match.group(1)
//   return content.replace("<prompt_base>", "").replace("</prompt_base>", "")
func extractTag(response, tag string) string {
	pattern := regexp.MustCompile(fmt.Sprintf(`(?s)<%s>(.*?)</%s>`, regexp.QuoteMeta(tag), regexp.QuoteMeta(tag)))
	match := pattern.FindStringSubmatch(response)
	if len(match) < 2 {
		return ""
	}
	content := match[1]
	content = strings.ReplaceAll(content, "<prompt_base>", "")
	content = strings.ReplaceAll(content, "</prompt_base>", "")
	return content
}

// appendMissingPlaceholders 手动追加缺失的占位符。
//
// 对齐 Python:
//   placeholder_text = "\n".join(f"{{{{{ph}}}}}" for ph in still_missing)
//   raw = str(raw) + "\n" + placeholder_text
func appendMissingPlaceholders(prompt string, missing []string) string {
	var lines []string
	for _, ph := range missing {
		lines = append(lines, "{{"+ph+"}}")
	}
	return prompt + "\n" + strings.Join(lines, "\n")
}

// containsTarget 检查 target 是否在列表中。
func containsTarget(targets []string, target string) bool {
	for _, t := range targets {
		if t == target {
			return true
		}
	}
	return false
}
