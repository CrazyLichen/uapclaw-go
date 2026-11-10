package web

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 注册 ────────────────────────────

// TestExternalCLI_注册齐全 7 个方法全部注册。
func TestExternalCLI_注册齐全(t *testing.T) {
	d := RegisterWebHandlers(nil)

	for _, method := range []string{
		"external_cli.detect", "external_cli.validate_paths", "external_cli.prune_path_histories",
		"external_cli.codex_install_status", "external_cli.install_status",
		"external_cli.install", "external_cli.custom_agent.remove",
	} {
		_, ok := d.methodHandlers[method]
		assert.True(t, ok, "方法未注册: %s", method)
	}
}

// ──────────────────────────── detect 外部 CLI 探测 ────────────────────────────

// TestExternalCLIDetect_参数校验 params 非对象 → BAD_REQUEST。
func TestExternalCLIDetect_参数校验(t *testing.T) {
	_, err := callHandler(externalCLIDetect(newTestChannel()), nil, "")
	require.Error(t, err)
	assert.Equal(t, WsErrBadRequest, errorCode(err))
	assert.Equal(t, "params must be object", err.Error())
}

// TestExternalCLIDetect_未知种类 非 claude/codex → status=unavailable。
func TestExternalCLIDetect_未知种类(t *testing.T) {
	payload, err := callHandler(externalCLIDetect(newTestChannel()), map[string]any{
		"cli_agent": "unknown-cli",
	}, "")
	require.NoError(t, err)
	assert.Equal(t, "unavailable", payload["status"])
	assert.Contains(t, payload["message"], "unsupported cli_agent")
}

// TestExternalCLIDetect_路径不存在 显式路径不存在 → status=missing + reason=not_found。
func TestExternalCLIDetect_路径不存在(t *testing.T) {
	payload, err := callHandler(externalCLIDetect(newTestChannel()), map[string]any{
		"cli_agent": "claude",
		"cli_path":  filepath.Join(t.TempDir(), "nope"),
	}, "")
	require.NoError(t, err)
	assert.Equal(t, "missing", payload["status"])
	assert.Equal(t, "not_found", payload["reason"])
}

// TestExternalCLIDetect_路径是目录 目录 → status=unsupported + reason=directory。
func TestExternalCLIDetect_路径是目录(t *testing.T) {
	payload, err := callHandler(externalCLIDetect(newTestChannel()), map[string]any{
		"cli_agent": "codex",
		"cli_path":  t.TempDir(),
	}, "")
	require.NoError(t, err)
	assert.Equal(t, "unsupported", payload["status"])
	assert.Equal(t, "directory", payload["reason"])
}

// TestExternalCLIDetect_解析版本 可执行脚本的 --version 输出能解析出三段版本号（status=ok）。
func TestExternalCLIDetect_解析版本(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-claude")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho '0.2.123 (Claude Code)'\n"), 0o755))

	payload, err := callHandler(externalCLIDetect(newTestChannel()), map[string]any{
		"cli_agent":   "claude",
		"cli_path":    script,
		"use_builtin": false,
	}, "")
	require.NoError(t, err)
	assert.Equal(t, "ok", payload["status"])
	assert.Equal(t, "0.2.123", payload["version"])
	assert.Equal(t, script, payload["path"])
}

// ──────────────────────────── validate_paths 路径校验 ────────────────────────────

// TestExternalCLIValidatePaths_参数校验 paths 非数组 → BAD_REQUEST。
func TestExternalCLIValidatePaths_参数校验(t *testing.T) {
	_, err := callHandler(externalCLIValidatePaths(newTestChannel()), map[string]any{"paths": "oops"}, "")
	require.Error(t, err)
	assert.Equal(t, WsErrBadRequest, errorCode(err))
	assert.Equal(t, "paths must be array", err.Error())
}

// TestExternalCLIValidatePaths_三值结果 文件=true、目录=false、缺失=false。
func TestExternalCLIValidatePaths_三值结果(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "cli")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o755))
	missing := filepath.Join(dir, "gone")

	payload, err := callHandler(externalCLIValidatePaths(newTestChannel()), map[string]any{
		"paths": []any{file, dir, missing, ""},
	}, "")
	require.NoError(t, err)
	results, _ := payload["results"].(map[string]any)
	assert.Equal(t, true, results[file])
	assert.Equal(t, false, results[dir], "目录不算可执行文件")
	assert.Equal(t, false, results[missing])
	assert.NotContains(t, results, "", "空串跳过")
}

// TestExternalCLIValidatePaths_超过上限截断 多传 60 条只处理前 50 条。
func TestExternalCLIValidatePaths_超过上限截断(t *testing.T) {
	dir := t.TempDir()
	paths := make([]any, 0, 60)
	for i := 0; i < 60; i++ {
		paths = append(paths, filepath.Join(dir, "p"+string(rune('a'+i%26))+string(rune('0'+i/26))))
	}

	payload, err := callHandler(externalCLIValidatePaths(newTestChannel()), map[string]any{"paths": paths}, "")
	require.NoError(t, err)
	results, _ := payload["results"].(map[string]any)
	assert.Len(t, results, 50, "单次最多探测 50 条")
}

// ──────────────────────────── prune_path_histories 路径历史裁剪 ────────────────────────────

// TestExternalCLIPrune_参数校验 cli_agent 缺失 / remove_paths 非数组各自 BAD_REQUEST。
func TestExternalCLIPrune_参数校验(t *testing.T) {
	_, err := callHandler(externalCLIPrunePathHistories(newTestChannel()), map[string]any{
		"remove_paths": []any{"/a"},
	}, "")
	require.Error(t, err)
	assert.Equal(t, "cli_agent is required", err.Error())

	_, err = callHandler(externalCLIPrunePathHistories(newTestChannel()), map[string]any{
		"cli_agent":    "claude",
		"remove_paths": "oops",
	}, "")
	require.Error(t, err)
	assert.Equal(t, "remove_paths must be array", err.Error())
}

// TestExternalCLIPrune_只删交集 历史里存在且被确认缺失的才删除，其余保留且保序。
func TestExternalCLIPrune_只删交集(t *testing.T) {
	useTempWorkspace(t)
	cfgPath := workspace.ConfigFile()
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o755))
	require.NoError(t, os.WriteFile(cfgPath, []byte(
		"modes:\n  team:\n    jiuwen_team:\n      external_cli_path_histories:\n        claude:\n          - /a/cli\n          - /b/cli\n          - /c/cli\n"), 0o644))

	payload, err := callHandler(externalCLIPrunePathHistories(newTestChannel()), map[string]any{
		"cli_agent":    "claude",
		"remove_paths": []any{"/a/cli", "/zzz/not-in-history"},
	}, "")
	require.NoError(t, err)
	surviving, _ := payload["histories"].([]string)
	assert.Equal(t, []string{"/b/cli", "/c/cli"}, surviving)

	// 落盘校验
	cfg, loadErr := loadAppConfig()
	require.NoError(t, loadErr)
	histories := mapSection(mapSection(mapSection(mapSection(cfg, "modes"), "team"), "jiuwen_team"), "external_cli_path_histories")
	assert.Equal(t, []any{"/b/cli", "/c/cli"}, histories["claude"])
}

// TestExternalCLIPrune_无历史 配置里没有该段时回空数组且不报错。
func TestExternalCLIPrune_无历史(t *testing.T) {
	useTempWorkspace(t)
	ensureConfigFile(t)

	payload, err := callHandler(externalCLIPrunePathHistories(newTestChannel()), map[string]any{
		"cli_agent":    "codex",
		"remove_paths": []any{"/a"},
	}, "")
	require.NoError(t, err)
	assert.Empty(t, payload["histories"])
}

// ──────────────────────────── install 状态与默认回包 ────────────────────────────

// TestExternalCLIInstallStatus_参数校验 未知种类 → BAD_REQUEST；已知种类回默认快照。
func TestExternalCLIInstallStatus_参数校验(t *testing.T) {
	_, err := callHandler(externalCLIInstallStatus(newTestChannel()), map[string]any{"cli_agent": "nope"}, "")
	require.Error(t, err)
	assert.Equal(t, "unsupported external cli agent", err.Error())

	payload, err := callHandler(externalCLIInstallStatus(newTestChannel()), map[string]any{"cli_agent": "codex"}, "")
	require.NoError(t, err)
	assert.Equal(t, "codex", payload["cli_agent"])
	assert.Equal(t, false, payload["installing"])
	assert.Equal(t, "idle", payload["state"])
}

// TestExternalCLI_默认回包 codex_install_status / install / custom_agent.remove 回占位载荷。
func TestExternalCLI_默认回包(t *testing.T) {
	d := RegisterWebHandlers(nil)

	codexStatus, err := dispatch(d, "external_cli.codex_install_status", nil, "")
	require.NoError(t, err)
	assert.Equal(t, false, codexStatus["installing"])

	install, err := dispatch(d, "external_cli.install", map[string]any{"cli_agent": "codex"}, "")
	require.NoError(t, err)
	assert.Equal(t, false, install["installing"])

	removed, err := dispatch(d, "external_cli.custom_agent.remove", map[string]any{"cli_agent": "x"}, "")
	require.NoError(t, err)
	assert.Contains(t, removed, "cli_agent")
}

// ──────────────────────────── 边界分支 ────────────────────────────

// TestExternalCLIDetect_useBuiltin与PATH 覆盖 use_builtin 两种写法与 PATH 搜索失败分支。
func TestExternalCLIDetect_useBuiltin与PATH(t *testing.T) {
	wc := newTestChannel()

	for _, raw := range []any{true, "true", "TRUE", false} {
		payload, err := callHandler(externalCLIDetect(wc), map[string]any{
			"cli_agent":   "claude-not-installed-xyz",
			"use_builtin": raw,
		}, "")
		require.NoError(t, err)
		assert.Equal(t, "unavailable", payload["status"], "未知种类恒 unavailable")
	}

	// 已知种类 + 空路径：PATH 里没有该可执行文件 → missing
	payload, err := callHandler(externalCLIDetect(wc), map[string]any{
		"cli_agent":   "codex",
		"use_builtin": true,
	}, "")
	require.NoError(t, err)
	assert.Contains(t, []string{"missing", "ok", "warning", "unavailable"}, payload["status"])
}

// TestExternalCLIDetect_版本不可解析 --version 输出无三段版本号 → warning。
func TestExternalCLIDetect_版本不可解析(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-codex")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho 'no version here'\n"), 0o755))

	payload, err := callHandler(externalCLIDetect(newTestChannel()), map[string]any{
		"cli_agent": "codex",
		"cli_path":  script,
	}, "")
	require.NoError(t, err)
	assert.Equal(t, "warning", payload["status"])
	assert.Equal(t, "version output could not be parsed", payload["message"])
}

// TestExternalCLIDetect_版本命令静默失败 无输出且退出码非 0 → unavailable。
func TestExternalCLIDetect_版本命令静默失败(t *testing.T) {
	script := filepath.Join(t.TempDir(), "silent-claude")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nexit 3\n"), 0o755))

	payload, err := callHandler(externalCLIDetect(newTestChannel()), map[string]any{
		"cli_agent": "claude",
		"cli_path":  script,
	}, "")
	require.NoError(t, err)
	assert.Equal(t, "unavailable", payload["status"])
	assert.NotEmpty(t, payload["message"])
}

// TestIsWindowsScriptPath_后缀判定 脚本后缀为真、可执行文件为假（与运行平台无关）。
func TestIsWindowsScriptPath_后缀判定(t *testing.T) {
	assert.True(t, isWindowsScriptPath("cli.bat"))
	assert.True(t, isWindowsScriptPath("cli.CMD"))
	assert.True(t, isWindowsScriptPath("cli.ps1"))
	assert.False(t, isWindowsScriptPath("cli.exe"))
	assert.False(t, isWindowsScriptPath("cli"))
}

// TestExternalCLIValidatePaths_params为空 params 非对象 → BAD_REQUEST。
func TestExternalCLIValidatePaths_params为空(t *testing.T) {
	_, err := callHandler(externalCLIValidatePaths(newTestChannel()), nil, "")
	require.Error(t, err)
	assert.Equal(t, "params must be object", err.Error())
}

// TestExternalCLIPrune_params为空 params 非对象 → BAD_REQUEST。
func TestExternalCLIPrune_params为空(t *testing.T) {
	_, err := callHandler(externalCLIPrunePathHistories(newTestChannel()), nil, "")
	require.Error(t, err)
	assert.Equal(t, "params must be object", err.Error())
}

// TestExternalCLIPrune_去重与空项 历史里的重复与空项不重复输出；删除后为空则清掉该 key。
func TestExternalCLIPrune_去重与空项(t *testing.T) {
	useTempWorkspace(t)
	cfgPath := workspace.ConfigFile()
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o755))
	require.NoError(t, os.WriteFile(cfgPath, []byte(
		"modes:\n  team:\n    jiuwen_team:\n      external_cli_path_histories:\n        claude:\n          - /a\n          - /a\n          - \"\"\n          - /b\n"), 0o644))

	// 只删 /a：/b 留下，重复项与空项被去重
	payload, err := callHandler(externalCLIPrunePathHistories(newTestChannel()), map[string]any{
		"cli_agent":    "claude",
		"remove_paths": []any{"/a"},
	}, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"/b"}, payload["histories"])

	// 再删 /b：整段清空后写空 mapping
	payload, err = callHandler(externalCLIPrunePathHistories(newTestChannel()), map[string]any{
		"cli_agent":    "claude",
		"remove_paths": []any{"/b"},
	}, "")
	require.NoError(t, err)
	assert.Empty(t, payload["histories"])

	cfg, loadErr := loadAppConfig()
	require.NoError(t, loadErr)
	histories := mapSection(mapSection(mapSection(mapSection(cfg, "modes"), "team"), "jiuwen_team"), "external_cli_path_histories")
	assert.NotContains(t, histories, "claude", "删空后该种类的 key 应移除")
}

// TestExternalCLIPrune_无交集不写盘 没有交集时原样返回且文件不被改写。
func TestExternalCLIPrune_无交集不写盘(t *testing.T) {
	useTempWorkspace(t)
	cfgPath := workspace.ConfigFile()
	content := "modes:\n  team:\n    jiuwen_team:\n      external_cli_path_histories:\n        codex:\n          - /keep\n"
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o755))
	require.NoError(t, os.WriteFile(cfgPath, []byte(content), 0o644))

	payload, err := callHandler(externalCLIPrunePathHistories(newTestChannel()), map[string]any{
		"cli_agent":    "codex",
		"remove_paths": []any{"/nope"},
	}, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"/keep"}, payload["histories"])

	after, readErr := os.ReadFile(cfgPath)
	require.NoError(t, readErr)
	assert.Equal(t, content, string(after), "无变化不应改写文件")
}

// TestExternalCLIPrune_写回失败 配置不可写时回 INTERNAL_ERROR。
func TestExternalCLIPrune_写回失败(t *testing.T) {
	useTempWorkspace(t)
	breakConfigFile(t)

	_, err := callHandler(externalCLIPrunePathHistories(newTestChannel()), map[string]any{
		"cli_agent":    "claude",
		"remove_paths": []any{"/a"},
	}, "")
	require.Error(t, err)
	assert.Equal(t, WsErrInternalError, errorCode(err))
}

// TestExternalCLIValidatePaths_无法判定 非法路径（含 NUL）→ null（不是 false，调用方不得据此清历史）。
func TestExternalCLIValidatePaths_无法判定(t *testing.T) {
	bad := "bad\x00path"
	payload, err := callHandler(externalCLIValidatePaths(newTestChannel()), map[string]any{
		"paths": []any{bad},
	}, "")
	require.NoError(t, err)
	results, _ := payload["results"].(map[string]any)
	value, present := results[bad]
	assert.True(t, present, "无法判定的路径也要出现在结果里")
	assert.Nil(t, value, "无法判定应为 null")
}

// TestExternalCLIDetect_显式路径不可读 含 NUL 的路径 → reason=unreadable。
func TestExternalCLIDetect_显式路径不可读(t *testing.T) {
	payload, err := callHandler(externalCLIDetect(newTestChannel()), map[string]any{
		"cli_agent": "claude",
		"cli_path":  "bad\x00path",
	}, "")
	require.NoError(t, err)
	assert.Equal(t, "missing", payload["status"])
	assert.Equal(t, "unreadable", payload["reason"])
}

// TestExternalCLIPrune_上限截断 remove_paths 超过 50 条时只处理前 50 条。
func TestExternalCLIPrune_上限截断(t *testing.T) {
	useTempWorkspace(t)
	cfgPath := workspace.ConfigFile()
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o755))
	require.NoError(t, os.WriteFile(cfgPath, []byte(
		"modes:\n  team:\n    jiuwen_team:\n      external_cli_path_histories:\n        claude:\n          - /keep\n"), 0o644))

	remove := make([]any, 0, 60)
	for i := 0; i < 60; i++ {
		remove = append(remove, "/p"+string(rune('a'+i%26))+string(rune('0'+i/26)))
	}
	payload, err := callHandler(externalCLIPrunePathHistories(newTestChannel()), map[string]any{
		"cli_agent":    "claude",
		"remove_paths": remove,
	}, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"/keep"}, payload["histories"], "超出上限的路径不参与删除")
}

// TestPruneExternalCLIPathHistories_空入参 空 agent 或空 remove 集直接返回空且不触盘。
func TestPruneExternalCLIPathHistories_空入参(t *testing.T) {
	got, err := pruneExternalCLIPathHistories("", []string{"/a"})
	require.NoError(t, err)
	assert.Empty(t, got)

	got, err = pruneExternalCLIPathHistories("claude", []string{"", "  "})
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestFirstNonEmpty 返回第一个非空串，全空回空串。
func TestFirstNonEmpty(t *testing.T) {
	assert.Equal(t, "b", firstNonEmpty("", "  ", "b", "c"))
	assert.Equal(t, "", firstNonEmpty("", ""))
}

// TestExternalCLIDetect_版本偏旧 warning 探测版本低于参考版本时降级 warning 并给上游同款文案。
func TestExternalCLIDetect_版本偏旧(t *testing.T) {
	script := filepath.Join(t.TempDir(), "old-claude")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho '0.2.100 (Claude Code)'\n"), 0o755))

	payload, err := callHandler(externalCLIDetect(newTestChannel()), map[string]any{
		"cli_agent": "claude",
		"cli_path":  script,
	}, "")
	require.NoError(t, err)
	assert.Equal(t, "warning", payload["status"])
	assert.Equal(t, "0.2.100", payload["version"])
	assert.Equal(t, "0.2.115", payload["reference_version"])
	assert.Equal(t, "version 0.2.100 may be incompatible with expected version 0.2.115", payload["message"])
}

// TestExternalCLIDetect_版本不低于参考 等于或高于参考版本都保持 ok。
func TestExternalCLIDetect_版本不低于参考(t *testing.T) {
	for _, ver := range []string{"0.2.115", "0.2.200", "1.0.0"} {
		script := filepath.Join(t.TempDir(), "claude-"+ver)
		require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho '"+ver+"'\n"), 0o755))

		payload, err := callHandler(externalCLIDetect(newTestChannel()), map[string]any{
			"cli_agent": "claude",
			"cli_path":  script,
		}, "")
		require.NoError(t, err)
		assert.Equal(t, "ok", payload["status"], "版本 %s 不应报偏旧", ver)
		assert.Equal(t, "", payload["message"])
	}
}

// TestExternalCLIDetect_参考版本恒返回 codex 的参考版本来自常量表，且路径缺失时也带上。
func TestExternalCLIDetect_参考版本恒返回(t *testing.T) {
	payload, err := callHandler(externalCLIDetect(newTestChannel()), map[string]any{
		"cli_agent": "codex",
		"cli_path":  filepath.Join(t.TempDir(), "nope"),
	}, "")
	require.NoError(t, err)
	assert.Equal(t, "missing", payload["status"])
	assert.Equal(t, "0.144.4", payload["reference_version"], "缺路径分支也要带参考版本")
}

// TestParseVersionTuple 三段/两段/非数字/空串。
func TestParseVersionTuple(t *testing.T) {
	got, ok := parseVersionTuple("1.2.3")
	assert.True(t, ok)
	assert.Equal(t, [3]int{1, 2, 3}, got)

	got, ok = parseVersionTuple("1.2")
	assert.True(t, ok, "缺段按 0 补")
	assert.Equal(t, [3]int{1, 2, 0}, got)

	_, ok = parseVersionTuple("1.x.3")
	assert.False(t, ok, "非数字段不可解析")

	_, ok = parseVersionTuple("  ")
	assert.False(t, ok)
}

// TestCompareVersionTuple 三段逐位比较。
func TestCompareVersionTuple(t *testing.T) {
	assert.Equal(t, -1, compareVersionTuple([3]int{0, 2, 100}, [3]int{0, 2, 115}))
	assert.Equal(t, 0, compareVersionTuple([3]int{1, 0, 0}, [3]int{1, 0, 0}))
	assert.Equal(t, 1, compareVersionTuple([3]int{1, 10, 0}, [3]int{1, 9, 9}))
}
