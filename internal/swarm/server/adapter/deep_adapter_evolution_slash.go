// 演进 slash 命令的 rail-independent 处理器（新版）。
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/evolution_slash.py（455 行 1:1）
// + evolution_helpers.py 验证半区（:141-271）。
// 上游 interface_deep.py:17530 直接分派本形态（旧内联 handler 已删，Go 旧体同批删除，
// 规则 8）。依赖：EvolutionStore 直建 + ExperienceQueryService/RebuildService +
// EvolutionArchiveService + evolution commands.go prompt builders。
package adapter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	evolutionlogic "github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter/evolution/logic"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/experience"
	coreevolution "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
	skillruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)

// ──────────────────────────── 常量 ────────────────────────────

// evolutionSlashCommands 命令名表（不含 /evolve 主命令）。
// Python: _COMMANDS（evolution_slash.py:30-35）。
var evolutionSlashCommands = []string{"evolve_simplify", "evolve_rebuild", "evolve_rollback", "evolve_list"}

// defaultReviewAgentName 默认审查子 agent 名。
// Python: _DEFAULT_REVIEW_AGENT_NAME（evolution_slash.py:36）。
const defaultReviewAgentName = "evolution_reviewer"

// evolutionSlashModeWhitelist 合并后演进能力放行模式集。
// Python: handle_evolution_slash_command 内白名单（evolution_slash.py:78-84）。
var evolutionSlashModeWhitelist = map[string]bool{
	"agent": true, "agent.plan": true, "team": true,
	"agent.work.normal": true, "agent.work.plan": true,
	"team.work.normal": true, "team.work.plan": true,
}

// skillPackKind SkillPack root kind 记号。
// Python: SKILLPACK_KIND（skill/skillpack.py）。
const skillPackKind = "skillpack"

// frontmatterPattern SKILL.md frontmatter 切分（降级逐行解析用）。
// Python: read_skill_kind 的 re.match(r"^---\s*\n(.*?)\n---", DOTALL)（evolution_helpers.py:173）。
var frontmatterPattern = regexp.MustCompile(`(?s)^---\s*\n(.*?)\n---`)

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionSlashContext 无 rail 依赖的演进 slash 命令上下文。
// Python: EvolutionSlashContext dataclass（frozen）（evolution_slash.py:39-48）。
type EvolutionSlashContext struct {
	// Mode 模式
	Mode string
	// SessionID 会话标识
	SessionID string
	// SkillsDir skills 根目录
	SkillsDir string
	// EvolutionEnabled 演进开关
	EvolutionEnabled bool
	// Language 语言
	Language string
	// ReviewAgentName 审查子 agent 名
	ReviewAgentName string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// HandleEvolutionSlashCommand 处理主动演进 slash 命令（无需挂载 rail）。
// Python: handle_evolution_slash_command()（evolution_slash.py:51-102）。
//
// 步骤：
//
//	Python: 1. 非 str → None（Go 调用方已保证 string）
//	Python: 2. command = _command_name(stripped)；None → nil
//	Python: 3. 模式白名单（7 值）→ error
//	Python: 4. evolution_enabled → error
//	Python: 5. store = EvolutionStore(skills_dir)；初始化失败 → error
//	Python: 6. 按命令分派（list/simplify/rebuild/rollback/evolve）
func HandleEvolutionSlashCommand(ctx context.Context, query string, slashCtx EvolutionSlashContext) map[string]any {
	stripped := strings.TrimSpace(query)
	command := evolutionSlashCommandName(stripped)
	if command == "" {
		return nil
	}
	// Python: 3. 合并后演进能力在 agent/team 模式可用（:78-84）
	if !evolutionSlashModeWhitelist[slashCtx.Mode] {
		displayMode := strings.TrimSpace(slashCtx.Mode)
		if displayMode == "" {
			displayMode = "当前"
		}
		return evolutionSlashError(fmt.Sprintf("%s 模式下演进功能不可用。", displayMode))
	}
	// Python: 4.
	if !slashCtx.EvolutionEnabled {
		return evolutionSlashError("演进功能未启用。")
	}
	// Python: 5. store 初始化
	var store *checkpointing.EvolutionStore
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Warn(logComponent).
					Msg(fmt.Sprintf("[EvolutionSlash] failed to initialize store: %v", r))
				store = nil
			}
		}()
		store = checkpointing.NewEvolutionStore([]string{slashCtx.SkillsDir})
	}()
	if store == nil {
		return evolutionSlashError("演进经验库初始化失败。")
	}

	switch command {
	case "evolve_list":
		return handleEvolveListSlash(ctx, stripped, store, slashCtx)
	case "evolve_simplify":
		return handleEvolveSimplifySlash(ctx, stripped, store, slashCtx)
	case "evolve_rebuild":
		return handleEvolveRebuildSlash(ctx, stripped, store, slashCtx)
	case "evolve_rollback":
		return handleEvolveRollbackSlash(ctx, stripped, store, slashCtx)
	}
	return handleEvolveSlash(ctx, stripped, store, slashCtx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// evolutionSlashCommandName 命令名解析（无匹配返回空）。
// Python: _command_name()（evolution_slash.py:105-112）。
func evolutionSlashCommandName(stripped string) string {
	if stripped == "/evolve" || strings.HasPrefix(stripped, "/evolve ") {
		return "evolve"
	}
	for _, command := range evolutionSlashCommands {
		prefix := "/" + command
		if stripped == prefix || strings.HasPrefix(stripped, prefix+" ") {
			return command
		}
	}
	return ""
}

// evolutionSlashSubject 解析 subject（store.resolve_subject_payload 回退 skill）。
// Python: _subject()（evolution_slash.py:115-128）。
func evolutionSlashSubject(ctx context.Context, store *checkpointing.EvolutionStore, skillName string) map[string]string {
	if payload := store.ResolveSubjectPayload(ctx, skillName); payload != nil {
		kind := strings.TrimSpace(payload["kind"])
		name := strings.TrimSpace(payload["name"])
		if name == "" {
			name = skillName
		}
		if kind != "" {
			return map[string]string{"kind": kind, "name": name}
		}
	}
	return map[string]string{"kind": "skill", "name": skillName}
}

// evolutionSlashFollowup followup 响应。
// Python: _followup_response()（evolution_slash.py:131-137）。
func evolutionSlashFollowup(action, followupPrompt, skillName string) map[string]any {
	return map[string]any{
		"action":          action,
		"followup_prompt": followupPrompt,
		"skill_name":      skillName,
		"result_type":     "followup",
	}
}

// evolutionSlashError error 响应。
// Python: _error()（evolution_slash.py:140-141）。
func evolutionSlashError(output string) map[string]any {
	return map[string]any{"output": output, "result_type": "error"}
}

// evolutionSlashAnswer answer 响应。
// Python: _answer()（evolution_slash.py:144-145）。
func evolutionSlashAnswer(output string) map[string]any {
	return map[string]any{"output": output, "result_type": "answer"}
}

// validateEvolutionSkillBase 校验演进命令目标 skill。
// Python: validate_evolution_skill()（evolution_helpers.py:217-239）。
//
// 步骤：
//
//	Python: 1. _skill_exists（store.skill_exists 回退目录判定）
//	Python: 2. is_skillpack → SKILL_OPERATION_UNSUPPORTED
//	Python: 3. require_skill_md：SKILL.md 存在 + 可写
func validateEvolutionSkillBase(ctx context.Context, store *checkpointing.EvolutionStore, skillName string, requireSkillMD bool) string {
	if !store.SkillExistsForKind(ctx, skillName, "") && !store.SkillExists(ctx, skillName) {
		names := skillruntime.FilterVisibleSkillNames(store.ListSkillNames(ctx))
		available := strings.Join(names, "、")
		if available == "" {
			available = "（无可用 Skill）"
		}
		return fmt.Sprintf("未找到 Skill '%s'。当前可用：%s", skillName, available)
	}
	skillDir := store.ResolveSkillDirForKind(ctx, skillName, false, "")
	if readSkillKind(skillDir) == skillPackKind {
		return "SKILL_OPERATION_UNSUPPORTED: SkillPack 不支持演进操作。"
	}
	if requireSkillMD {
		skillMD := store.FindSkillMD(ctx, skillDir)
		if skillMD == "" {
			skillMD = filepath.Join(skillDir, "SKILL.md")
		}
		if !adapterIsFile(skillMD) {
			return fmt.Sprintf("Skill '%s' 缺少 SKILL.md，无法执行演进生成。", skillName)
		}
		if info, err := os.Stat(skillMD); err == nil && info.Mode().Perm()&0o200 == 0 {
			return fmt.Sprintf("Skill '%s' 的 SKILL.md 不可写，无法执行演进。", skillName)
		}
	}
	return ""
}

// validateTeamEvolutionSkill team 模式校验（仅 Swarm Skill）。
// Python: validate_team_evolution_skill()（evolution_helpers.py:242-256）。
func validateTeamEvolutionSkill(ctx context.Context, store *checkpointing.EvolutionStore, skillName string, requireSkillMD bool) string {
	if baseErr := validateEvolutionSkillBase(ctx, store, skillName, requireSkillMD); baseErr != "" {
		return baseErr
	}
	kind := readSkillKind(store.ResolveSkillDirForKind(ctx, skillName, false, ""))
	if kind != "" && kind != "swarm-skill" && kind != "team-skill" {
		return fmt.Sprintf("集群模式下仅支持演进 Swarm Skill，指定 Skill '%s' 不是 Swarm Skill。", skillName)
	}
	return ""
}

// validateEvolutionLogWritable 校验演进日志目标可写。
// Python: validate_evolution_log_writable()（evolution_helpers.py:259-271）。
func validateEvolutionLogWritable(ctx context.Context, store *checkpointing.EvolutionStore, skillName string) string {
	skillDir := store.ResolveSkillDirForKind(ctx, skillName, false, "")
	if skillDir == "" {
		return ""
	}
	logPath := filepath.Join(skillDir, "evolutions.json")
	target := logPath
	if !adapterExists(logPath) {
		target = skillDir
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm()&0o200 == 0 {
		if adapterExists(logPath) {
			return fmt.Sprintf("Skill '%s' 的 evolutions.json 不可写，无法保存演进经验。", skillName)
		}
		return fmt.Sprintf("Skill '%s' 目录不可写，无法保存演进经验。", skillName)
	}
	return ""
}

// readSkillKind 读 SKILL.md frontmatter 的 kind 字段。
// Python: read_skill_kind()（evolution_helpers.py:163-182）。
func readSkillKind(skillDir string) string {
	if skillDir == "" {
		return ""
	}
	content, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		return ""
	}
	m := frontmatterPattern.FindStringSubmatch(string(content))
	if m == nil {
		return ""
	}
	// Python: yaml.safe_load(frontmatter)["kind"] —— 降级逐行键值解析（仓库先例）
	for _, line := range strings.Split(m[1], "\n") {
		if kind, ok := strings.CutPrefix(strings.TrimSpace(line), "kind:"); ok {
			return strings.TrimSpace(strings.Trim(kind, `"'`))
		}
	}
	return ""
}

// validateEvolutionSkillForContext 按模式选择个人/团队校验。
// Python: _validate_skill()（evolution_slash.py:148-159）。
func validateEvolutionSkillForContext(ctx context.Context, store *checkpointing.EvolutionStore, skillName string, requireSkillMD bool, slashCtx EvolutionSlashContext, subject map[string]string) string {
	subjectKind := subject["kind"]
	if subjectKind == "" {
		subjectKind = "skill"
	}
	if slashCtx.Mode == "team" && subjectKind == "swarm-skill" {
		return validateTeamEvolutionSkill(ctx, store, skillName, requireSkillMD)
	}
	return validateEvolutionSkillBase(ctx, store, skillName, requireSkillMD)
}

// handleEvolveSlash 处理 /evolve。
// Python: _handle_evolve()（evolution_slash.py:200-236）。
//
// 步骤：
//
//	Python: 1. 无参 → list_pending_summary answer
//	Python: 2. subject 解析 + require_md 校验 + 日志可写校验
//	Python: 3. build_evolve_review_command_prompt → followup
func handleEvolveSlash(ctx context.Context, query string, store *checkpointing.EvolutionStore, slashCtx EvolutionSlashContext) map[string]any {
	parts := splitMaxN(query, 2)
	if len(parts) < 2 {
		skillNames := skillruntime.FilterVisibleSkillNames(store.ListSkillNames(ctx))
		if len(skillNames) == 0 {
			return evolutionSlashAnswer("当前 skills_base_dir 下未找到任何 Skill 目录。")
		}
		summary := store.ListPendingSummary(ctx, skillNames)
		return evolutionSlashAnswer(fmt.Sprintf("**Skills 演进记录：**\n\n%s", summary))
	}
	skillName := strings.TrimSpace(parts[1])
	var userIntent *string
	if len(parts) > 2 {
		intent := strings.TrimSpace(parts[2])
		userIntent = &intent
	}

	subject := evolutionSlashSubject(ctx, store, skillName)
	if validationErr := validateEvolutionSkillForContext(ctx, store, skillName, true, slashCtx, subject); validationErr != "" {
		return evolutionSlashError(validationErr)
	}
	if writableErr := validateEvolutionLogWritable(ctx, store, skillName); writableErr != "" {
		return evolutionSlashError(writableErr)
	}

	prompt, err := coreevolution.BuildEvolveReviewCommandPrompt(subjectStringMap(subject), userIntent)
	if err != nil {
		return evolutionSlashError(fmt.Sprintf("演进指令生成失败：%v", err))
	}
	return evolutionSlashFollowup("run_evolve_followup", prompt, skillName)
}

// handleEvolveListSlash 处理 /evolve_list。
// Python: _handle_evolve_list()（evolution_slash.py:239-290）。
func handleEvolveListSlash(ctx context.Context, query string, store *checkpointing.EvolutionStore, slashCtx EvolutionSlashContext) map[string]any {
	parts := strings.Fields(query)
	skillName := ""
	if len(parts) > 1 {
		skillName = parts[1]
	}
	if skillName == "" || strings.HasPrefix(skillName, "--") {
		return evolutionSlashError("请指定 Skill 名称：`/evolve_list <skill_name>`")
	}

	subject := evolutionSlashSubject(ctx, store, skillName)
	if validationErr := validateEvolutionSkillForContext(ctx, store, skillName, false, slashCtx, subject); validationErr != "" {
		return evolutionSlashError(validationErr)
	}

	records := store.GetRecordsByScoreForKind(ctx, skillName, nil, subject["kind"])
	if len(records) == 0 {
		return evolutionSlashAnswer(fmt.Sprintf("Skill '%s' 暂无演进经验。", skillName))
	}

	totalScore := 0.0
	lines := []string{
		fmt.Sprintf("📊 Skill \"%s\" — 经验库摘要\n", skillName),
		fmt.Sprintf("共 %d 条经验 | 平均分：%.2f\n", len(records), func() float64 {
			for _, r := range records {
				totalScore += r.Score
			}
			return totalScore / float64(len(records))
		}()),
		"| # | Score | Used | Effect | Section | Content (preview) |",
		"|---|---:|---|---|---|---|",
	}
	for index, record := range records {
		usedStr, effectStr := "0/0", "+0/-0"
		if record.UsageStats != nil {
			if record.UsageStats.TimesPresented > 0 {
				usedStr = fmt.Sprintf("%d/%d", record.UsageStats.TimesUsed, record.UsageStats.TimesPresented)
			}
			effectStr = fmt.Sprintf("+%d/-%d", record.UsageStats.TimesPositive, record.UsageStats.TimesNegative)
		}
		section := strings.ReplaceAll(record.Change.Section, "|", "\\|")
		preview := record.Change.Content
		if i := strings.IndexByte(preview, '\n'); i >= 0 {
			preview = preview[:i]
		}
		if len(preview) > 40 {
			preview = preview[:40]
		}
		preview = strings.ReplaceAll(preview, "|", "\\|")
		lines = append(lines, fmt.Sprintf("| %d | %.2f | %s | %s | %s | %s |",
			index+1, record.Score, usedStr, effectStr, section, preview))
	}
	lines = append(lines, "", fmt.Sprintf("提示：使用 /evolve_simplify %s 执行智能整理", skillName))
	return evolutionSlashAnswer(strings.Join(lines, "\n"))
}

// handleEvolveSimplifySlash 处理 /evolve_simplify。
// Python: _handle_evolve_simplify()（evolution_slash.py:293-340）。
func handleEvolveSimplifySlash(ctx context.Context, query string, store *checkpointing.EvolutionStore, slashCtx EvolutionSlashContext) map[string]any {
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
	if skillName == "" {
		return evolutionSlashError("请指定 Skill 名称：`/evolve_simplify <skill_name> [user_intent]`")
	}

	subject := evolutionSlashSubject(ctx, store, skillName)
	if validationErr := validateEvolutionSkillForContext(ctx, store, skillName, true, slashCtx, subject); validationErr != "" {
		return evolutionSlashError(validationErr)
	}
	if writableErr := validateEvolutionLogWritable(ctx, store, skillName); writableErr != "" {
		return evolutionSlashError(writableErr)
	}

	// Python: query_service.list_experiences(subject, min_score=None, limit=100, cursor=None,
	//   target=None, section=None, query=None, sort="score_desc")
	queryService := experience.NewExperienceQueryService(store)
	index, err := queryService.ListExperiences(ctx, subjectStringMap(subject),
		nil, 100, "", "", "", "", "score_desc")
	if err != nil {
		return evolutionSlashError(fmt.Sprintf("智能整理分析失败：%v", err))
	}
	items, _ := index["items"].([]map[string]any)
	if len(items) == 0 {
		return evolutionSlashAnswer(fmt.Sprintf("Skill '%s' 暂无演进经验，无需整理。", skillName))
	}

	prompt, err := coreevolution.BuildSimplifyCommandPrompt(subjectStringMap(subject), userIntent, index,
		!boolOf(index["has_more"]))
	if err != nil {
		return evolutionSlashError(fmt.Sprintf("整理指令生成失败：%v", err))
	}
	return evolutionSlashFollowup("run_simplify_followup", prompt, skillName)
}

// handleEvolveRebuildSlash 处理 /evolve_rebuild。
// Python: _handle_evolve_rebuild()（evolution_slash.py:343-391）。
func handleEvolveRebuildSlash(ctx context.Context, query string, store *checkpointing.EvolutionStore, slashCtx EvolutionSlashContext) map[string]any {
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
	if skillName == "" {
		return evolutionSlashError("请指定 Skill 名称：`/evolve_rebuild <skill_name> [user_intent]`")
	}

	subject := evolutionSlashSubject(ctx, store, skillName)
	if validationErr := validateEvolutionSkillForContext(ctx, store, skillName, false, slashCtx, subject); validationErr != "" {
		return evolutionSlashError(validationErr)
	}

	// Python: rebuild_service.prepare_rebuild_context(subject, user_intent=...)（:366-374）
	rebuildService := experience.NewExperienceRebuildService(store, nil)
	rebuildContext, err := rebuildService.PrepareRebuildContext(ctx,
		subjectStringMap(subject), userIntent, 0.5, 40, 20000)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[EvolutionSlash] evolve_rebuild failed")
		return evolutionSlashError(fmt.Sprintf("重建失败：%v", err))
	}
	if rebuildContext == nil {
		return evolutionSlashError(fmt.Sprintf("Skill '%s' 未生成可执行的重建指令。", skillName))
	}
	// Python: archive_error 归档失败分支（:379-383）
	if archiveErr, ok := rebuildContext["archive_error"].(string); ok && archiveErr != "" {
		return evolutionSlashError(fmt.Sprintf("重建失败：无法归档 Skill '%s' 的旧版本：%s", skillName, archiveErr))
	}
	if _, ok := rebuildContext["archive_pair"].(map[string]string); !ok {
		return evolutionSlashError(fmt.Sprintf("重建失败：无法归档 Skill '%s' 的旧版本。", skillName))
	}

	prompt, err := coreevolution.BuildRebuildCommandPrompt(subjectStringMap(subject), userIntent, rebuildContext)
	if err != nil {
		return evolutionSlashError(fmt.Sprintf("重建指令生成失败：%v", err))
	}
	return evolutionSlashFollowup("run_rebuild_followup", prompt, skillName)
}

// handleEvolveRollbackSlash 处理 /evolve_rollback。
// Python: _handle_evolve_rollback()（evolution_slash.py:394-452）。
//
// 五段校验顺序：①空名（usage 列表）②skill 校验 ③无归档对 ④无 version（只列清单）
// ⑤latest 展开 + 版本合法性。
func handleEvolveRollbackSlash(ctx context.Context, query string, store *checkpointing.EvolutionStore, slashCtx EvolutionSlashContext) map[string]any {
	parts := splitMaxN(query, 2)
	skillName := ""
	if len(parts) > 1 {
		skillName = strings.TrimSpace(parts[1])
	}
	versionRaw := ""
	if len(parts) > 2 {
		versionRaw = strings.TrimSpace(parts[2])
	}
	archiveService := experience.NewEvolutionArchiveService(store)

	// Python: ①空名 → usage 列表（:404-405 + _format_rollback_usage :162-186）
	if skillName == "" {
		return evolutionSlashError(formatRollbackUsage(ctx, store, archiveService))
	}

	subject := evolutionSlashSubject(ctx, store, skillName)
	if validationErr := validateEvolutionSkillForContext(ctx, store, skillName, false, slashCtx, subject); validationErr != "" {
		return evolutionSlashError(validationErr)
	}

	subjectKind := subject["kind"]
	pairs := archiveService.ListPairs(ctx, skillName, subjectKind)
	if len(pairs) == 0 {
		return evolutionSlashError(fmt.Sprintf("Skill '%s' 没有成对归档版本可回滚。", skillName))
	}

	// Python: ④无 version → 版本清单（不执行回滚）（:423-424 + _format_rollback_versions :189-196）
	if versionRaw == "" {
		return evolutionSlashAnswer(formatRollbackVersions(skillName, pairs))
	}

	requestedVersion := experience.NormalizeArchiveVersion(versionRaw)
	if requestedVersion == "" {
		return evolutionSlashError(fmt.Sprintf("版本 `%s` 格式无效，请使用短版本号，例如 `%s`。", versionRaw, pairs[0].Version))
	}

	var pair *experience.EvolutionArchivePair
	if requestedVersion == "latest" {
		pair = &pairs[0]
	} else {
		for i := range pairs {
			if pairs[i].Version == requestedVersion {
				pair = &pairs[i]
				break
			}
		}
	}
	if pair == nil {
		return evolutionSlashError(fmt.Sprintf("版本 `%s` 不存在或归档不成对。", requestedVersion))
	}

	// Python: archive_service.rollback_to_pair(...)（:437-447）
	if !archiveService.RollbackToPair(ctx, skillName, pair, subjectKind) {
		return evolutionSlashError(fmt.Sprintf("回滚失败：无法将 Skill '%s' 回滚到 `%s`。", skillName, pair.Version))
	}
	return evolutionSlashAnswer(fmt.Sprintf(
		"Skill '%s' 已成功回滚到 `%s`。\n\n当前状态已自动归档，可再次回滚恢复。",
		skillName, pair.Version))
}

// formatRollbackUsage 空名时的可回滚 Skill 清单。
// Python: _format_rollback_usage()（evolution_slash.py:162-186）。
func formatRollbackUsage(ctx context.Context, store *checkpointing.EvolutionStore, archiveService *experience.EvolutionArchiveService) string {
	lines := []string{"请指定 Skill 名称：`/evolve_rollback <skill_name> [version]`"}
	var rollbackable []string
	skillNames := skillruntime.FilterVisibleSkillNames(store.ListSkillNames(ctx))
	for _, name := range skillNames {
		subject := evolutionSlashSubject(ctx, store, name)
		pairs := archiveService.ListPairs(ctx, name, subject["kind"])
		if len(pairs) > 0 {
			rollbackable = append(rollbackable, fmt.Sprintf("  - **%s**: %d 个版本，最新 `%s`", name, len(pairs), pairs[0].Version))
		}
	}
	if len(rollbackable) > 0 {
		lines = append(lines, "", "可回滚的 Skill：")
		lines = append(lines, rollbackable...)
	}
	return strings.Join(lines, "\n")
}

// formatRollbackVersions 版本清单（最新在前）。
// Python: _format_rollback_versions()（evolution_slash.py:189-196）。
func formatRollbackVersions(skillName string, pairs []experience.EvolutionArchivePair) string {
	lines := []string{fmt.Sprintf("**Skill '%s' 可用回滚版本（最新在前）：**\n", skillName)}
	for index, pair := range pairs {
		marker := ""
		if index == 0 {
			marker = " ← 最近"
		}
		lines = append(lines, fmt.Sprintf("%d. `%s`%s", index+1, pair.Version, marker))
	}
	lines = append(lines,
		fmt.Sprintf("\n用法：`/evolve_rollback %s %s`", skillName, pairs[0].Version),
		fmt.Sprintf("快捷回滚到最近版本：`/evolve_rollback %s latest`", skillName))
	return strings.Join(lines, "\n")
}

// adapterIsFile 路径存在且为常规文件。
func adapterIsFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// adapterExists 路径存在（不区分类型）。
func adapterExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// subjectStringMap map[string]string → map[string]any（evoschema 入参形状）。
func subjectStringMap(subject map[string]string) map[string]any {
	out := make(map[string]any, len(subject))
	for k, v := range subject {
		out[k] = v
	}
	return out
}

// annotateEvolveSlashResult 出口注记（Python evolution_slash_result，:350-368）：
// 命令名/来源/显示级别 setdefault；nil 结果透传。
func annotateEvolveSlashResult(query string, result map[string]any) map[string]any {
	if result == nil {
		return nil
	}
	return evolutionlogic.EvolutionSlashResult(
		evolutionlogic.EvolutionSlashCommandName(query), result, nil)
}
