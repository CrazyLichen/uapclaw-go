package adapter

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	skillruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleSlashCommand 处理斜杠命令。
// Python: _handle_slash_command() (line 4305-4382)
//
// 按 query 前缀 /evolve* 分发到具体处理器。
// ✅ 已回填：依赖 SkillEvolutionRail
func (d *DeepAdapter) handleSlashCommand(ctx context.Context, query string, sessionID string, mode string) (map[string]any, error) {
	// Python: stripped = query.strip() 后按前缀匹配
	stripped := strings.TrimSpace(query)
	if stripped == "" || !strings.HasPrefix(stripped, "/") {
		return nil, nil // 非 slash 命令
	}

	// Python: 按 stripped 前缀分发（L4319-4348）。
	// /evolve 精确匹配（== "/evolve" 或前缀 "/evolve "），防止 /evolveXXX 误命中
	switch {
	case strings.HasPrefix(stripped, "/evolve_simplify"):
		if errMsg := d.ensureEvolutionRailForSlash(mode); errMsg != "" {
			return map[string]any{"output": errMsg, "result_type": "error"}, nil
		}
		return d.handleEvolveSimplifyCommand(ctx, stripped, sessionID)
	case strings.HasPrefix(stripped, "/evolve_rebuild"):
		if errMsg := d.ensureEvolutionRailForSlash(mode); errMsg != "" {
			return map[string]any{"output": errMsg, "result_type": "error"}, nil
		}
		return d.handleEvolveRebuildCommand(ctx, stripped, sessionID)
	case strings.HasPrefix(stripped, "/evolve_rollback"):
		if errMsg := d.ensureEvolutionRailForSlash(mode); errMsg != "" {
			return map[string]any{"output": errMsg, "result_type": "error"}, nil
		}
		return d.handleEvolveRollbackCommand(ctx, stripped, sessionID)
	case strings.HasPrefix(stripped, "/evolve_list"):
		if errMsg := d.ensureEvolutionRailForSlash(mode); errMsg != "" {
			return map[string]any{"output": errMsg, "result_type": "error"}, nil
		}
		return d.handleEvolveListCommand(ctx, stripped, sessionID)
	case stripped == "/evolve" || strings.HasPrefix(stripped, "/evolve "):
		if errMsg := d.ensureEvolutionRailForSlash(mode); errMsg != "" {
			return map[string]any{"output": errMsg, "result_type": "error"}, nil
		}
		return d.handleEvolveCommand(ctx, stripped, sessionID)
	default:
		return nil, nil
	}
}

// ensureEvolutionRailForSlash 检查演进功能可用性，必要时懒加载 SkillEvolutionRail。
// Python: _ensure_evolution_rail_for_slash() (interface_deep.py L4285-4303)
//
// 返回空字符串表示可用，非空表示错误信息。
func (d *DeepAdapter) ensureEvolutionRailForSlash(mode string) string {
	if mode != "agent.plan" {
		return "当前模式下演进功能不可用，仅 agent.plan 模式支持。"
	}
	evolutionConfig, _ := d.configCache["evolution"].(map[string]any)
	enabled, _ := evolutionConfig["enabled"].(bool)
	if !enabled {
		return "演进功能未启用。"
	}
	// 懒加载：如果 rail 为 nil，尝试构建
	if d.skillEvolutionRail == nil {
		rail := d.buildSkillEvolutionRail()
		if rail != nil {
			if r, ok := rail.(*evolution.SkillEvolutionRail); ok {
				d.skillEvolutionRail = r
			}
		}
	}
	if d.skillEvolutionRail == nil {
		return "演进功能初始化失败。"
	}
	return ""
}

// handleEvolveCommand 处理 /evolve 命令。
// 对齐 Python: _handle_evolve_command() (L3773-3920)
//
// 格式：/evolve [list | <skill_name> [<user_query>...]]
//   - 无参数或 "list" → 待审批经验摘要（list_pending_summary）
//   - skill_name 不存在 → 错误提示（列出可用 Skill）
//   - 其余 → 信号检测链：收集会话消息 → SignalDetector 检测 →
//     processed_signal_keys 去重 → evolution_intent 推导 → RequestUserEvolution
//
// S-11: 补齐信号检测链（原实现直接调 RequestUserEvolution，无消息收集/检测/去重/意图推导）。
// M-14: 返回值对齐 Python 格式（output + result_type + approval_chunks），
// approval_chunks 装展平后的 map（原装 *stream.OutputSchema 指针会被流式
// 消费方的双重类型断言静默丢弃）。
func (d *DeepAdapter) handleEvolveCommand(ctx context.Context, query string, sessionID string) (map[string]any, error) {
	if d.skillEvolutionRail == nil {
		logger.Warn(logComponent).Str("session_id", sessionID).Msg("handleEvolveCommand: SkillEvolutionRail not initialized")
		return nil, nil
	}
	rail := d.skillEvolutionRail
	store := rail.EvolutionStore()

	// 解析命令：/evolve [skill_name] [user_query]
	// M-37: 对齐 Python L3794+L3812 两级 split(maxsplit=1)——
	// query.split(1) 取 skill_arg，skill_arg.split(1) 取 skill_name/user_query，
	// user_query 保留内部连续空白且 strip
	parts := splitMaxN(query, 1)
	skillArg := ""
	if len(parts) > 1 {
		skillArg = strings.TrimSpace(parts[1])
	}

	// --- /evolve list (or bare /evolve)：待审批经验摘要 ---
	// M-10 链 A: 对齐 Python L3797-3808——原先错误 fallback 到 handleEvolveListCommand
	if skillArg == "" || skillArg == "list" {
		skillNames := skillruntime.FilterVisibleSkillNames(store.ListSkillNames(ctx))
		if len(skillNames) == 0 {
			return map[string]any{
				"output":      "当前 skills_base_dir 下未找到任何 Skill 目录。",
				"result_type": "answer",
			}, nil
		}
		summary := store.ListPendingSummary(ctx, skillNames)
		return map[string]any{
			"output":      fmt.Sprintf("**Skills 演进记录：**\n\n%s", summary),
			"result_type": "answer",
		}, nil
	}

	// --- /evolve <skill_name> [<user_query>...] ---
	skillParts := splitMaxN(skillArg, 1)
	skillName := ""
	userIntent := ""
	if len(skillParts) >= 1 {
		skillName = strings.TrimSpace(skillParts[0])
	}
	if len(skillParts) >= 2 {
		userIntent = strings.TrimSpace(skillParts[1])
	}

	// Skill 存在性校验（对齐 Python L3816-3825）
	visibleNames := skillruntime.FilterVisibleSkillNames(store.ListSkillNames(ctx))
	skillFound := false
	for _, n := range visibleNames {
		if n == skillName {
			skillFound = true
			break
		}
	}
	if !skillFound {
		available := strings.Join(visibleNames, "、")
		if available == "" {
			available = "（无可用 Skill）"
		}
		return map[string]any{
			"output": fmt.Sprintf(
				"在 skills_base_dir 下未找到 Skill '%s'。\n当前可用 Skill：%s\n可使用 /evolve list 查看所有记录。",
				skillName, available,
			),
			"result_type": "error",
		}, nil
	}

	// 1) 收集会话消息（对齐 Python L3827-3828: _collect_messages_for_evolve）
	parsedMessages := d.collectMessagesForEvolve(sessionID)

	// 2) 信号检测（对齐 Python L3830-3833: SignalDetector(existing_skills).detect）
	//    纯规则检测，不绑 LLM（Python slash 路径同样不调 bind_llm）
	existingSkills := make(map[string]bool, len(visibleNames))
	for _, n := range visibleNames {
		if store.SkillExists(ctx, n) {
			existingSkills[n] = true
		}
	}
	detector := signal.NewConversationSignalDetector(signal.WithExistingSkills(existingSkills))
	var detected []*signal.EvolutionSignal
	if len(parsedMessages) > 0 {
		detected = detector.Detect(parsedMessages)
	}

	// 3) 去重 + 回写 processed_signal_keys（对齐 Python L3835-3841）
	//    S-11 决策点 A：统一使用 rail 内部同款 4 元组指纹 MakeSignalFingerprint——
	//    slash 处理过的信号 rail 后台演进自动跳过（比 Python 的 2 元组键更收敛，
	//    Python 下 slash 与后台各记各账、同一信号可被两边重复演进）
	keys := rail.ProcessedSignalKeys()
	var newSignals []*signal.EvolutionSignal
	for _, sig := range detected {
		fp := signal.MakeSignalFingerprint(sig)
		if !keys[fp] {
			keys[fp] = true
			newSignals = append(newSignals, sig)
		}
	}

	// 4) 归属过滤：只统计 skill_name == 目标 skill 的信号（对齐 Python L3843）
	var attributed []*signal.EvolutionSignal
	for _, sig := range newSignals {
		if sig.SkillName != nil && *sig.SkillName == skillName {
			attributed = append(attributed, sig)
		}
	}

	// 5) 无归属信号且无 user_query → 提示后结束（对齐 Python L3846-3850）
	if len(attributed) == 0 && userIntent == "" {
		return map[string]any{
			"output":      "当前对话未发现明确的演进信号（无工具执行失败、无用户纠正）。\n",
			"result_type": "answer",
		}, nil
	}

	// 6) evolution_intent 三级推导（对齐 Python L3852-3863）：
	//    a. user_query 非空直接用；b. 归属信号摘录；c. 倒序最后一条非空 content
	evolutionIntent := userIntent
	if evolutionIntent == "" {
		for _, sig := range attributed {
			if sig.Excerpt != "" {
				evolutionIntent = sig.Excerpt
				break
			}
		}
	}
	if evolutionIntent == "" {
		for i := len(parsedMessages) - 1; i >= 0; i-- {
			if content, ok := parsedMessages[i]["content"].(string); ok && content != "" {
				evolutionIntent = content
				break
			}
		}
	}

	// 7) 发起演进请求（对齐 Python L3865-3876）
	evolveResult, err := rail.RequestUserEvolution(ctx, skillName, evolutionIntent, false)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("skill", skillName).
			Msg("handleEvolveCommand: evolve generate failed")
		return map[string]any{
			"output":      fmt.Sprintf("演进经验生成失败：%s", err),
			"result_type": "error",
		}, nil
	}

	// 8) 无变更 → 提示（对齐 Python L3878-3882: has_changes 为假）
	if len(evolveResult.Records) == 0 && evolveResult.ApprovalEvent == nil {
		return map[string]any{
			"output":      "当前对话未发现明确的演进信号（无工具执行失败、无用户纠正）。\n",
			"result_type": "answer",
		}, nil
	}

	// 9) 审批事件校验（对齐 Python L3884-3906: _parse_stream_chunk + ask_user_question 校验）
	//    复用 S-21 为 team 路径打通的展平链：OutputSchema → chunkToMap 展平 →
	//    utilsParseStreamChunk 二次展平 → 顶层 event_type/request_id/questions
	approvalChunk := approvalChunkFromEvent(teamHelperEventAsMap(evolveResult.ApprovalEvent))
	if approvalChunk == nil {
		if len(evolveResult.Records) == 0 {
			return map[string]any{
				"output":      "当前对话未发现明确的演进信号（无工具执行失败、无用户纠正）。\n",
				"result_type": "answer",
			}, nil
		}
		return map[string]any{
			"output": fmt.Sprintf(
				"已为 Skill '%s' 生成 %d 条演进经验，但审批事件为空或格式无效。",
				skillName, len(evolveResult.Records),
			),
			"result_type": "error",
		}, nil
	}

	// 10) 成功：从 questions 构建摘要文案（对齐 Python L3908-3920）
	//    兼容 []any（JSON 往返后）与 []map[string]any（原生 BuildSkillApprovalEvent）双形状
	questions := teamHelperQuestionsItems(approvalChunk["questions"])
	summaries := make([]string, 0, len(questions))
	for i, q := range questions {
		qm, _ := q.(map[string]any)
		text, _ := qm["question"].(string)
		if len(text) > 200 {
			text = text[:200]
		}
		summaries = append(summaries, fmt.Sprintf("  %d. %s", i+1, text))
	}

	return map[string]any{
		"output": fmt.Sprintf(
			"已为 Skill '%s' 生成 %d 条演进经验，请审批：\n%s",
			skillName, len(questions), strings.Join(summaries, "\n"),
		),
		"result_type":     "answer",
		"approval_chunks": []map[string]any{approvalChunk},
		"request_id":      ptrToStr(evolveResult.RequestID),
		"skill_name":      evolveResult.SkillName,
	}, nil
}

// collectMessagesForEvolve 收集当前会话的缓存消息用于演进信号检测。
// 对齐 Python: _collect_messages_for_evolve(session_id) (L4003-4019)
//
// 从 ReActAgent 的 ContextEngine 取 default_context 会话上下文，
// 读取消息（含历史）并归一化为 map 列表（含 tool_calls）。
// 任一环节缺失（实例/agent/context）或读取失败均返回 nil（Python 同样降级为空列表）。
func (d *DeepAdapter) collectMessagesForEvolve(sessionID string) []map[string]any {
	if d.instance == nil {
		return nil
	}
	reactAgent := d.instance.ReactAgent()
	if reactAgent == nil {
		return nil
	}
	contextEngine := reactAgent.ContextEngine()
	if contextEngine == nil {
		return nil
	}
	// Go 创建侧 context ID 为 "default_context"（react_helpers.go initContext），
	// 非 Python 默认值 "default_context_id"——跟随 Go 创建侧
	mc := contextEngine.GetContext("default_context", sessionID)
	if mc == nil {
		return nil
	}
	// size=0 不限制 + with_history=true，对齐 Python context.get_messages() 默认参数
	rawMessages, err := mc.GetMessages(0, true)
	if err != nil {
		logger.Debug(logComponent).Err(err).Str("session_id", sessionID).
			Msg("collectMessagesForEvolve failed")
		return nil
	}
	return signal.ParseMessages(rawMessages)
}

// handleEvolveListCommand 处理 /evolve_list 命令。
// 对齐 Python: _handle_evolve_list_command() (L4021-4080)
//
// 格式：/evolve_list <skill_name> [--sort score]
// 展示指定 Skill 的经验库分数表格（与 team 版 HandleTeamEvolveListCommand 共用
// buildEvolveListSummary——M-10 链 B：原先返回 {"action": "evolve_list", "skills": [...]}
// 结构化 map，与 Python 的文本表格格式完全不同）。
func (d *DeepAdapter) handleEvolveListCommand(ctx context.Context, query string, sessionID string) (map[string]any, error) {
	if d.skillEvolutionRail == nil {
		logger.Warn(logComponent).Str("session_id", sessionID).Msg("handleEvolveListCommand: SkillEvolutionRail not initialized")
		return nil, nil
	}
	store := d.skillEvolutionRail.EvolutionStore()

	// Python: parts = query.split(); skill_name = parts[1] if len(parts) > 1 else ""
	parts := strings.Fields(query)
	skillName := ""
	if len(parts) > 1 {
		skillName = parts[1]
	}
	// Python: if not skill_name or skill_name.startswith("--")
	if skillName == "" || strings.HasPrefix(skillName, "--") {
		return map[string]any{
			"output":      "请指定 Skill 名称：`/evolve_list <skill_name>`",
			"result_type": "error",
		}, nil
	}

	if !store.SkillExists(ctx, skillName) {
		available := strings.Join(skillruntime.FilterVisibleSkillNames(store.ListSkillNames(ctx)), "、")
		if available == "" {
			available = "（无可用 Skill）"
		}
		return map[string]any{
			"output":      fmt.Sprintf("未找到 Skill '%s'。当前可用：%s", skillName, available),
			"result_type": "error",
		}, nil
	}

	records := store.GetRecordsByScore(ctx, skillName, nil)
	if len(records) == 0 {
		return map[string]any{
			"output":      fmt.Sprintf("Skill '%s' 暂无演进经验。", skillName),
			"result_type": "answer",
		}, nil
	}

	return map[string]any{
		"output":      buildEvolveListSummary(skillName, records),
		"result_type": "answer",
	}, nil
}

// handleEvolveSimplifyCommand 处理 /evolve_simplify 命令。
// 格式：/evolve_simplify skill_name [user_intent]
func (d *DeepAdapter) handleEvolveSimplifyCommand(ctx context.Context, query string, sessionID string) (map[string]any, error) {
	if d.skillEvolutionRail == nil {
		logger.Warn(logComponent).Str("session_id", sessionID).Msg("handleEvolveSimplifyCommand: SkillEvolutionRail not initialized")
		return nil, nil
	}

	// M-37: 对齐 Python L4088 parts = query.split(maxsplit=2)
	parts := splitMaxN(query, 2)
	skillName := ""
	if len(parts) > 1 {
		skillName = parts[1]
	}
	var userIntent *string
	if len(parts) > 2 {
		intent := parts[2]
		userIntent = &intent
	}

	// Python L4091-4106：空名 + skill_exists 双校验（返回 output/result_type）
	if resp := evolveSkillArgError(ctx, d.skillEvolutionRail, skillName, "/evolve_simplify <skill_name> [user_intent]"); resp != nil {
		return resp, nil
	}

	// Python: result = await rail.request_simplify(skill_name, user_intent=user_intent)
	result, err := d.skillEvolutionRail.RequestSimplify(ctx, skillName, userIntent)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("skill", skillName).Msg("handleEvolveSimplifyCommand: RequestSimplify failed")
		return map[string]any{
			"output":      fmt.Sprintf("智能整理分析失败：%s", err),
			"result_type": "error",
		}, nil
	}

	// Python: return self._approval_response_from_simplify_result(...)
	// 复用 team 侧一比一实现（含 OutputSchema 展平与 question_count）
	return approvalResultFromEventOrItems(
		skillName,
		result.ApprovalEvent,
		result.Actions,
		fmt.Sprintf("Skill '%s' 经验库状态良好，无需整理。", skillName),
		fmt.Sprintf("Skill '%s' 精简方案已生成，但审批事件为空或格式无效。", skillName),
	), nil
}

// handleEvolveRebuildCommand 处理 /evolve_rebuild 命令。
// 格式：/evolve_rebuild skill_name [user_intent]
// 返回 followup_prompt 用于注入 agent loop 执行 skill-creator
func (d *DeepAdapter) handleEvolveRebuildCommand(ctx context.Context, query string, sessionID string) (map[string]any, error) {
	if d.skillEvolutionRail == nil {
		logger.Warn(logComponent).Str("session_id", sessionID).Msg("handleEvolveRebuildCommand: SkillEvolutionRail not initialized")
		return nil, nil
	}

	// M-37: 对齐 Python L4123 parts = query.split(maxsplit=2)
	parts := splitMaxN(query, 2)
	skillName := ""
	if len(parts) > 1 {
		skillName = parts[1]
	}
	var userIntent *string
	if len(parts) > 2 {
		intent := parts[2]
		userIntent = &intent
	}

	// Python L4126-4141：空名 + skill_exists 双校验
	if resp := evolveSkillArgError(ctx, d.skillEvolutionRail, skillName, "/evolve_rebuild <skill_name> [user_intent]"); resp != nil {
		return resp, nil
	}

	// Python: followup_prompt = await rail.request_rebuild(skill_name, user_intent=user_intent, min_score=0.5)
	followupPrompt, err := d.skillEvolutionRail.RequestRebuild(ctx, skillName, userIntent, 0.5)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("skill", skillName).Msg("handleEvolveRebuildCommand: RequestRebuild failed")
		return map[string]any{
			"output":      fmt.Sprintf("重建分析失败：%s", err),
			"result_type": "error",
		}, nil
	}

	if followupPrompt == "" {
		// Python L4148-4152：未生成指令 → error（原 status:no_rebuild 契约不符）
		return map[string]any{
			"output":      fmt.Sprintf("Skill '%s' 未生成可执行的重建指令。", skillName),
			"result_type": "error",
		}, nil
	}

	// Python L4154-4159：action/followup_prompt/skill_name + result_type=followup
	// （消费端 extractRebuildFollowupPrompt 依赖 followup_prompt）
	return map[string]any{
		"action":          "run_rebuild_followup",
		"followup_prompt": followupPrompt,
		"skill_name":      skillName,
		"result_type":     "followup",
	}, nil
}

// handleEvolveRollbackCommand 处理 /evolve_rollback 命令。
// 格式：/evolve_rollback skill_name [version]
//
// Python: _handle_evolve_rollback_command (L4159-4242) 五段校验顺序：
// ①空名（列可回滚 Skill 及版本数）②skill_exists ③无归档 ④无 version（只列清单，
// **不执行回滚**）⑤latest 展开 + 版本合法性。缺任一段都会导致「想查版本却直接回滚」。
func (d *DeepAdapter) handleEvolveRollbackCommand(ctx context.Context, query string, sessionID string) (map[string]any, error) {
	if d.skillEvolutionRail == nil {
		logger.Warn(logComponent).Str("session_id", sessionID).Msg("handleEvolveRollbackCommand: SkillEvolutionRail not initialized")
		return nil, nil
	}

	// M-37: 对齐 Python L4165 parts = query.split(maxsplit=2)，version strip
	parts := splitMaxN(query, 2)
	skillName := ""
	if len(parts) > 1 {
		skillName = parts[1]
	}
	version := ""
	if len(parts) > 2 {
		version = strings.TrimSpace(parts[2])
	}
	store := d.skillEvolutionRail.EvolutionStore()

	// ① 空名 → 列出可回滚 Skill 及各自版本数（Python L4170-4181）
	if skillName == "" {
		archivesHint := ""
		if store != nil {
			for _, name := range skillruntime.FilterVisibleSkillNames(store.ListSkillNames(ctx)) {
				if count := countSkillBodyVersions(store.ListArchives(ctx, name)); count > 0 {
					archivesHint += fmt.Sprintf("\n  - **%s**: %d 个版本", name, count)
				}
			}
		}
		output := "请指定 Skill 名称：`/evolve_rollback <skill_name> [version]`"
		if archivesHint != "" {
			output += "\n\n可回滚的 Skill：" + archivesHint
		}
		return map[string]any{"output": output, "result_type": "error"}, nil
	}

	// ② skill 存在性（Python L4183-4188）
	if resp := evolveSkillArgError(ctx, d.skillEvolutionRail, skillName, "/evolve_rollback <skill_name> [version]"); resp != nil {
		return resp, nil
	}

	// ③ 归档清单为空（Python L4190-4195）
	var bodyVersions []string
	if store != nil {
		bodyVersions = skillBodyVersions(store.ListArchives(ctx, skillName))
	}
	if len(bodyVersions) == 0 {
		return map[string]any{
			"output":      fmt.Sprintf("Skill '%s' 没有归档版本可回滚。", skillName),
			"result_type": "error",
		}, nil
	}

	// ④ 未指定版本 → 只列清单让用户选（禁止直接回滚最新版）
	if version == "" {
		lines := []string{fmt.Sprintf("**Skill '%s' 可用归档版本（最新在前）：**\n", skillName)}
		for i, v := range bodyVersions {
			marker := ""
			if i == 0 {
				marker = " ← 最近"
			}
			lines = append(lines, fmt.Sprintf("  %d. `%s` (%s)%s", i+1, v, formatArchiveTimestamp(v), marker))
		}
		lines = append(lines, fmt.Sprintf("\n用法：`/evolve_rollback %s SKILL.v<时间戳>.md`", skillName))
		lines = append(lines, fmt.Sprintf("快捷回滚到最近版本：`/evolve_rollback %s latest`", skillName))
		return map[string]any{"output": strings.Join(lines, "\n"), "result_type": "answer"}, nil
	}

	// ⑤ latest 简写展开 + 版本合法性（Python L4211-4222）
	if version == "latest" {
		version = bodyVersions[0]
	}
	if !containsString(bodyVersions, version) {
		hint := make([]string, 0, 5)
		for _, v := range bodyVersions {
			if len(hint) == 5 {
				break
			}
			hint = append(hint, "`"+v+"`")
		}
		return map[string]any{
			"output":      fmt.Sprintf("版本 `%s` 不存在。可用版本：%s", version, strings.Join(hint, "、")),
			"result_type": "error",
		}, nil
	}

	// Python L4224-4236：执行回滚
	success, err := d.skillEvolutionRail.RollbackSkill(ctx, skillName, &version)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("skill", skillName).Msg("handleEvolveRollbackCommand: RollbackSkill failed")
		return map[string]any{
			"output":      fmt.Sprintf("回滚失败：%s", err),
			"result_type": "error",
		}, nil
	}
	if !success {
		return map[string]any{
			"output":      fmt.Sprintf("Skill '%s' 回滚失败，请检查归档版本是否有效。", skillName),
			"result_type": "error",
		}, nil
	}
	return map[string]any{
		"output":      fmt.Sprintf("Skill '%s' 已成功回滚到 `%s`。\n\n（当前状态已自动归档，可再次回滚恢复。）", skillName, version),
		"result_type": "answer",
	}, nil
}

// evolveSkillArgError 校验 Skill 名称参数（空名 / 不存在），返回 Python 同款错误响应。
//
// Python: simplify/rebuild/rollback 三处共同的「空名 + store.skill_exists」双校验，
// 文案一致，仅使用示例串不同。
func evolveSkillArgError(ctx context.Context, rail *evolution.SkillEvolutionRail, skillName, usage string) map[string]any {
	if strings.TrimSpace(skillName) == "" {
		return map[string]any{
			"output":      "请指定 Skill 名称：`" + usage + "`",
			"result_type": "error",
		}
	}
	store := rail.EvolutionStore()
	if store == nil {
		return nil
	}
	if store.SkillExists(ctx, skillName) {
		return nil
	}
	available := "（无可用 Skill）"
	if joined := strings.Join(skillruntime.FilterVisibleSkillNames(store.ListSkillNames(ctx)), "、"); joined != "" {
		available = joined
	}
	return map[string]any{
		"output":      fmt.Sprintf("未找到 Skill '%s'。当前可用：%s", skillName, available),
		"result_type": "error",
	}
}

// skillBodyVersions 过滤出技能主体归档版本（Python: [a for a in archives if a.startswith("SKILL.v")]）。
func skillBodyVersions(archives []string) []string {
	out := make([]string, 0, len(archives))
	for _, a := range archives {
		if strings.HasPrefix(a, "SKILL.v") {
			out = append(out, a)
		}
	}
	return out
}

// countSkillBodyVersions 统计主体归档版本数（空名清单的「N 个版本」）。
func countSkillBodyVersions(archives []string) int {
	return len(skillBodyVersions(archives))
}

// formatArchiveTimestamp 归档文件名转可读时间（Python: SKILL.v20261030_120000.md → 2026-10-30 12:00:00 UTC）。
func formatArchiveTimestamp(archive string) string {
	ts := strings.ReplaceAll(strings.ReplaceAll(archive, "SKILL.v", ""), ".md", "")
	if len(ts) < 15 {
		return ts
	}
	return fmt.Sprintf("%s-%s-%s %s:%s:%s UTC", ts[0:4], ts[4:6], ts[6:8], ts[9:11], ts[11:13], ts[13:15])
}

// containsString 切片包含判定。
func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

// handleGovernanceApproval 处理治理审批。
// ✅ 已回填（对齐 Python: _handle_governance_approval()）
//
// 根据 request_id 前缀路由到 OnApproveSimplify / OnRejectSimplify
func (d *DeepAdapter) handleGovernanceApproval(ctx context.Context, requestID string, answers []ApprovalAnswer, approvalType string) bool {
	if d.skillEvolutionRail == nil {
		logger.Warn(logComponent).Str("request_id", requestID).Msg("handleGovernanceApproval: SkillEvolutionRail not initialized")
		return false
	}

	switch approvalType {
	case "approve":
		result, err := d.skillEvolutionRail.OnApproveSimplify(ctx, requestID)
		if err != nil {
			logger.Error(logComponent).Err(err).Str("request_id", requestID).Msg("handleGovernanceApproval: OnApproveSimplify failed")
			return false
		}
		if result != nil {
			logger.Info(logComponent).Str("request_id", requestID).Any("result", result).Msg("handleGovernanceApproval: simplify executed")
		}
		return true
	case "reject":
		d.skillEvolutionRail.OnRejectSimplify(requestID)
		logger.Info(logComponent).Str("request_id", requestID).Msg("handleGovernanceApproval: simplify rejected")
		return true
	default:
		logger.Warn(logComponent).Str("approval_type", approvalType).Msg("handleGovernanceApproval: unknown approval type")
		return false
	}
}

// ptrToStr 将字符串指针转为字符串，nil 返回空字符串。
func ptrToStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
