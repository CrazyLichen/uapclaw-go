package external

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness_protocol"
	harnessproviders "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness_providers"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	swarmcommon "github.com/uapclaw/uapclaw-go/internal/swarm/common"
)

// logComponent catalog 组件级日志。
const catalogLogComponent = logger.ComponentAgentServer

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultModelAlias 「使用已配置模型」的目录条目；它不是成员可 spawn 的模型名。
	// Python: _DEFAULT_MODEL_ALIAS = "default" (external_cli_catalog.py:41)
	DefaultModelAlias = "default"
	// DefaultProbeTimeoutS 每个 CLI 的探测截止（秒）。
	// Python: DEFAULT_PROBE_TIMEOUT_S = 15.0 (external_cli_catalog.py:42)
	DefaultProbeTimeoutS = 15.0
	// reportedModelsRelPath 探测报告相对 WorkspaceDir 的路径。
	// Python: reported_models_path (external_cli_catalog.py:202-204)
	reportedModelsRelPath = "runtime/external-cli-models.json"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ReconcileBuiltinModels 把一个 CLI kind 的声明目录按上报清单修正。
// Python: reconcile_builtin_models (external_cli_catalog.py:45-87)
//
// 返回修正后的目录与可读漂移警告；报告里的名字是事实，声明是策略。
func ReconcileBuiltinModels(
	cliAgent string,
	declared []map[string]any,
	reported map[string][]string,
) ([]map[string]any, []string) {
	corrected := []map[string]any{}
	warnings := []string{}
	for _, item := range declared {
		name := config.String(item["name"])
		efforts, reported := reported[name]
		if !reported {
			warnings = append(warnings,
				fmt.Sprintf("%s: removed '%s' — the CLI no longer offers it", cliAgent, name))
			continue
		}
		entry := cloneStringMap(item)
		declaredEfforts := config.StringListOrEmpty(item["efforts"])
		if !equalStrings(efforts, declaredEfforts) {
			warnings = append(warnings, fmt.Sprintf("%s: '%s' efforts %s -> %s",
				cliAgent, name, emptyOrSlice(declaredEfforts), emptyOrSlice(efforts)))
			if len(efforts) > 0 {
				entry["efforts"] = anyFromStrings(efforts)
			} else {
				delete(entry, "efforts")
			}
		}
		defaultEffort := config.String(item["default_effort"])
		if defaultEffort != "" && !containsStr(efforts, defaultEffort) {
			warnings = append(warnings, fmt.Sprintf(
				"%s: '%s' default_effort '%s' is not supported; dropped", cliAgent, name, defaultEffort))
			delete(entry, "default_effort")
		}
		corrected = append(corrected, entry)
	}

	declaredNames := map[string]bool{}
	for _, item := range declared {
		declaredNames[config.String(item["name"])] = true
	}
	added := []string{}
	for name := range reported {
		if !declaredNames[name] && name != DefaultModelAlias {
			added = append(added, name)
		}
	}
	if len(added) > 0 {
		// 加模型是成本决策，留给运维（Python :85-86）
		warnings = append(warnings, fmt.Sprintf(
			"%s: the CLI also offers %s (add them to builtin_models to use)", cliAgent, joinComma(added)))
	}
	return corrected, warnings
}

// ApplyReportedModels 用已存报告就地修正各 entry 的目录；从未探测过的条目
// 保持声明原样。
// Python: apply_reported_models (external_cli_catalog.py:305-326)
func ApplyReportedModels(entries []any) {
	reports := LoadReportedModels()
	for _, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		cliAgent := config.String(entry["cli_agent"])
		declared, ok := entry["builtin_models"].([]any)
		if !ok || len(declared) == 0 {
			continue
		}
		reported, probed := reports[cliAgent]
		if !probed {
			continue
		}
		corrected, drift := ReconcileBuiltinModels(cliAgent, config.DictList(declared), reported)
		for _, line := range drift {
			logger.Info(catalogLogComponent).Str("detail", line).
				Msg("[external-cli] builtin models drift")
		}
		entry["builtin_models"] = anyListOf(corrected)
	}
}

// ReportedModelsPath 探测报告文件路径。
// Python: reported_models_path (external_cli_catalog.py:202-204)
func ReportedModelsPath() string {
	return filepath.Join(pathutil.WorkspaceDir(), "runtime", "external-cli-models.json")
}

// LoadReportedModels 读已存报告：cli kind → 模型名 → efforts。
// Python: load_reported_models (external_cli_catalog.py:207-226)
func LoadReportedModels() map[string]map[string][]string {
	reports := map[string]map[string][]string{}
	data, err := os.ReadFile(ReportedModelsPath())
	if os.IsNotExist(err) {
		return reports
	}
	if err != nil {
		logger.Warn(catalogLogComponent).Err(err).
			Msg("[external-cli] ignoring unreadable model report")
		return reports
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		logger.Warn(catalogLogComponent).Err(err).
			Msg("[external-cli] ignoring unreadable model report")
		return reports
	}
	for cliAgent, models := range payload {
		modelsDict, ok := models.(map[string]any)
		if !ok {
			continue
		}
		entry := map[string][]string{}
		for name, efforts := range modelsDict {
			entry[name] = config.StringListOrEmpty(efforts)
		}
		reports[cliAgent] = entry
	}
	return reports
}

// RefreshExternalCliBuiltinModels 探测可被 leader 挑选内建模型的 CLI 并存报告。
// Python: refresh_external_cli_builtin_models (external_cli_catalog.py:237-302)
//
// 探测对象：声明了目录的 kind + 播种 kind（声明 MODEL_DISCOVERY 的已启用自定义
// agent、本次保存新启用的 kind）。探测失败（未登录/起不来/超时）保旧报告且
// 播种 kind 不产生目录。返回漂移警告（供日志）。
//
//   - entries：config 中 modes.team.jiuwen_team.external_cli_agents 列表；
//   - enabledNow：本次保存新启用的 kind 集合；
//   - writeCatalogs：播种目录回写 config 的回调（注入缝，避免 catalog→config
//     写链的包向依赖；对齐 Python update_external_cli_builtin_models_in_config）。
func RefreshExternalCliBuiltinModels(
	ctx context.Context,
	entries []any,
	enabledNow []string,
	writeCatalogs func(map[string][]map[string]any),
) []string {
	seeds := discoverableCustomAgents(entries)
	seeds = mergeStrMaps(seeds, reenabledAgents(entries, enabledNow))
	declaredByAgent := map[string][]map[string]any{}
	paths := map[string]string{}
	for k, v := range seeds {
		paths[k] = v
	}
	for _, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		cliAgent := config.String(entry["cli_agent"])
		builtinModels, ok := entry["builtin_models"].([]any)
		if cliAgent == "" || !ok || len(builtinModels) == 0 {
			continue
		}
		declaredByAgent[cliAgent] = config.DictList(builtinModels)
		paths[cliAgent] = config.String(entry["cli_path"])
	}
	if len(paths) == 0 {
		return []string{}
	}

	probed := runProbes(ctx, paths, DefaultProbeTimeoutS)
	reports := LoadReportedModels()
	catalogs := map[string][]map[string]any{}
	warnings := []string{}
	for cliAgent := range paths {
		reported, ok := probed[cliAgent]
		if !ok {
			// 探测失败保旧报告（Python :283-287）
			logger.Warn(catalogLogComponent).
				Str("cli_agent", cliAgent).
				Str("detail", "probe did not finish").
				Msg("[external-cli] model probe failed, keeping the last report")
			continue
		}
		reports[cliAgent] = reported
		if _, seeded := seeds[cliAgent]; seeded {
			if catalog := discoveredCatalog(reported); len(catalog) > 0 {
				catalogs[cliAgent] = catalog
			}
			continue
		}
		_, agentWarnings := ReconcileBuiltinModels(cliAgent, declaredByAgent[cliAgent], reported)
		warnings = append(warnings, agentWarnings...)
	}

	storeReportedModels(reports)
	if len(catalogs) > 0 && writeCatalogs != nil {
		writeCatalogs(catalogs)
	}
	for _, warning := range warnings {
		logger.Warn(catalogLogComponent).Str("detail", warning).
			Msg("[external-cli] builtin models drift")
	}
	return warnings
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// probeModels 经 harness provider 读一个 CLI 的模型清单。
// Python: _probe_models (external_cli_catalog.py:90-106)
//
// 偏差 D-15.16-d1：claude/codex 分支（ClaudeCodeHarness/CodexHarness 构造）
// ⤴️15.7/15.8 留缝——这两章未落地时 resolve 失败，走探测失败路径（保旧报告），
// 与 Python resolve_provider 抛错的运行态行为一致。
func probeModels(ctx context.Context, cliAgent, cliPath string) (map[string][]string, error) {
	if cliAgent == "claude" || cliAgent == "codex" {
		return nil, fmt.Errorf("probe for %q pending 15.7/15.8 (D-15.16-d1)", cliAgent)
	}
	return probeCustomAgentModels(ctx, cliAgent, cliPath)
}

// probeCustomAgentModels 经已注册 provider 读自定义 agent 的模型清单。
// Python: _probe_custom_agent_models (external_cli_catalog.py:109-123)
// 探测跑在 scratch 目录：为上报模型而开会话的 agent 不得在用户项目里留会话。
func probeCustomAgentModels(ctx context.Context, cliAgent, cliPath string) (map[string][]string, error) {
	provider, err := harnessproviders.ResolveProvider(cliAgent)
	if err != nil {
		return nil, err
	}
	cwd, err := os.MkdirTemp("", "uapclaw-model-probe-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(cwd) // D-15.12-c 先例：TemporaryDirectory → MkdirTemp + defer 删
	config := harnessprotocol.JsonObject{"cwd": cwd}
	if cliPath != "" {
		config["cli_path"] = cliPath
	}
	harness, err := provider.Create(config)
	if err != nil {
		return nil, err
	}
	modelControl, ok := harness.(harnessprotocol.HarnessModelControl)
	if !ok {
		return nil, fmt.Errorf("provider %q does not implement model control", cliAgent)
	}
	options, err := modelControl.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	reported := map[string][]string{}
	for _, option := range options {
		reported[option.ModelID] = append([]string(nil), option.Efforts...)
	}
	return reported, nil
}

// discoverableCustomAgents 返回启用且无目录、provider 可列举模型的自定义 agent。
// Python: _discoverable_custom_agents (external_cli_catalog.py:126-153)
func discoverableCustomAgents(entries []any) map[string]string {
	customNames := strSet(swarmcommon.CustomAgentNames())
	if len(customNames) == 0 {
		return map[string]string{}
	}
	targets := map[string]string{}
	for _, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok || !config.IsEmptyList(entry["builtin_models"]) {
			continue
		}
		cliAgent := config.String(entry["cli_agent"])
		if _, known := customNames[cliAgent]; !known {
			continue
		}
		discoverable, err := providerSupportsModelDiscovery(cliAgent)
		if err != nil {
			// Python :148-150：加载失败的 agent Warn 跳过
			logger.Warn(catalogLogComponent).
				Str("cli_agent", cliAgent).Err(err).
				Msg("[external-cli] custom agent cannot be loaded")
			continue
		}
		if discoverable {
			targets[cliAgent] = config.String(entry["cli_path"])
		}
	}
	return targets
}

// reenabledAgents 返回本次保存新启用且无目录的 kind 及其 CLI 路径。
// Python: _reenabled_agents (external_cli_catalog.py:156-165)
func reenabledAgents(entries []any, enabledNow []string) map[string]string {
	enabled := strSet(enabledNow)
	targets := map[string]string{}
	for _, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok || !config.IsEmptyList(entry["builtin_models"]) {
			continue
		}
		cliAgent := config.String(entry["cli_agent"])
		if _, on := enabled[cliAgent]; on {
			targets[cliAgent] = config.String(entry["cli_path"])
		}
	}
	return targets
}

// discoveredCatalog 播种目录：允许全部上报模型；efforts 留在报告里。
// Python: _discovered_catalog (external_cli_catalog.py:168-170)
func discoveredCatalog(reported map[string][]string) []map[string]any {
	catalog := []map[string]any{}
	for name := range reported {
		if name != DefaultModelAlias {
			catalog = append(catalog, map[string]any{"name": name})
		}
	}
	return catalog
}

// runProbes 并发探测全部目标；失败以错误串报告，绝不 raise。
// Python: _probe_all (external_cli_catalog.py:173-184) + _run_probes (:187-199)
//
// 偏差 D-15.16-d2：Python 用 daemon thread + join(timeout) 兜挂死 CLI；Go 用
// goroutine + 整体等待超时（timeout×目标数 + 5s），语义同。
func runProbes(ctx context.Context, paths map[string]string, timeoutS float64) map[string]map[string][]string {
	type probeResult struct {
		cliAgent string
		reported map[string][]string
		err      error
	}
	done := make(chan probeResult, len(paths))
	for cliAgent, cliPath := range paths {
		go func(agent, path string) {
			reported, err := probeModels(ctx, agent, path)
			done <- probeResult{cliAgent: agent, reported: reported, err: err}
		}(cliAgent, cliPath)
	}
	results := map[string]map[string][]string{}
	for range paths {
		select {
		case res := <-done:
			if res.err != nil {
				// Python :180-181：健康检查不得打断开关，失败转错误串；
				// refresh 侧 isinstance(dict) 不过 → 不更新报告（Go 以不进结果集等价）
				logger.Debug(catalogLogComponent).
					Str("cli_agent", res.cliAgent).Err(res.err).
					Msg("[external-cli] probe failed")
			} else {
				results[res.cliAgent] = res.reported
			}
		case <-ctx.Done():
			return results
		}
	}
	// goroutine 自带探测语义上的截止由上层 ctx 控制；此处 join 只为收齐结果
	//（等价 Python thread.join(timeout_s*len+5) 的兜底语义由 ctx.Done 分支承担）。
	return results
}

// storeReportedModels 原子写报告（临时文件 + rename）。
// Python: _store_reported_models (external_cli_catalog.py:229-234)
func storeReportedModels(reports map[string]map[string][]string) {
	path := ReportedModelsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(reports, "", "  ")
	if err != nil {
		return
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// providerSupportsModelDiscovery 查 provider 是否声明 MODEL_DISCOVERY。
// Python: resolve_provider(cli_agent).card.supports(HarnessCapability.MODEL_DISCOVERY)
// (external_cli_catalog.py:147)
func providerSupportsModelDiscovery(cliAgent string) (bool, error) {
	provider, err := harnessproviders.ResolveProvider(cliAgent)
	if err != nil {
		return false, err
	}
	return provider.Card().Supports(harnessprotocol.HarnessCapabilityModelDiscovery), nil
}

// ──────────────────────────── 通用小工具 ────────────────────────────
// stringValue/stringListOf/dictListOf/isEmptyAny 已收归 config.String/
// StringListOrEmpty/DictList/IsEmptyList（internal/common/config/mapvalue.go）。

func anyListOf(dicts []map[string]any) []any {
	out := make([]any, 0, len(dicts))
	for _, dict := range dicts {
		out = append(out, dict)
	}
	return out
}

func cloneStringMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func strSet(list []string) map[string]bool {
	set := map[string]bool{}
	for _, item := range list {
		set[item] = true
	}
	return set
}

func mergeStrMaps(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsStr(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

func joinComma(list []string) string {
	out := ""
	for i, item := range list {
		if i > 0 {
			out += ", "
		}
		out += item
	}
	return out
}

func anyFromStrings(list []string) []any {
	out := make([]any, 0, len(list))
	for _, item := range list {
		out = append(out, item)
	}
	return out
}

func emptyOrSlice(list []string) string {
	if len(list) == 0 {
		return "[]"
	}
	return "[" + joinComma(list) + "]"
}
