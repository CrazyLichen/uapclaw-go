package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// useTempWorkspace 把数据目录指向临时目录（会话/配置文件都落在这里）。
func useTempWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", dir)
	workspace.SetUserHome(workspace.UserHomeDir())
	return dir
}

// ensureConfigFile 在临时工作区落一份空的 config.yaml，供写回类 handler 使用。
func ensureConfigFile(t *testing.T) string {
	t.Helper()
	path := workspace.ConfigFile()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o644))
	return path
}

// writeSessionDir 在会话目录下写入一个会话（meta 为 nil 时不写 metadata.json）。
func writeSessionDir(t *testing.T, sessionID string, meta map[string]any) {
	t.Helper()
	dir := filepath.Join(workspace.AgentSessionsDir(), sessionID)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	if meta == nil {
		return
	}
	data, err := json.Marshal(meta)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644))
}

// ──────────────────────────── memory.forbidden 禁用记忆配置 ────────────────────────────

// TestMemoryForbiddenGet_无配置回空对象 config.yaml 缺失时落默认空对象而非报错。
func TestMemoryForbiddenGet_无配置回空对象(t *testing.T) {
	useTempWorkspace(t)

	payload, err := callHandler(memoryForbiddenGet(newTestChannel()), nil, "")
	require.NoError(t, err)
	assert.NotNil(t, payload)
}

// TestMemoryForbiddenSet_写入成功 写回 config.yaml 并回 {"ok": true}。
func TestMemoryForbiddenSet_写入成功(t *testing.T) {
	useTempWorkspace(t)
	ensureConfigFile(t)

	payload, err := callHandler(memoryForbiddenSet(newTestChannel()), map[string]any{
		"enabled":     true,
		"description": map[string]any{"zh": "不要记忆"},
	}, "")
	require.NoError(t, err)
	assert.Equal(t, true, payload["ok"])

	// 读回校验：get 必须拿到刚写入的字段
	got, getErr := callHandler(memoryForbiddenGet(newTestChannel()), nil, "")
	require.NoError(t, getErr)
	assert.Equal(t, true, got["enabled"])
}

// TestMemoryForbiddenSet_params为空 BAD_REQUEST。
func TestMemoryForbiddenSet_params为空(t *testing.T) {
	useTempWorkspace(t)

	_, err := callHandler(memoryForbiddenSet(newTestChannel()), nil, "")
	require.Error(t, err)
	assert.Equal(t, WsErrBadRequest, errorCode(err))
}

// ──────────────────────────── session.list 会话列表 ────────────────────────────

// TestSessionList_分页与过滤 覆盖分页参数解析（int/float64）、clamp、
// heartbeat_ 前缀过滤、缺 metadata 与坏 JSON 兜底、按 last_message_at 降序。
func TestSessionList_分页与过滤(t *testing.T) {
	useTempWorkspace(t)

	writeSessionDir(t, "sess_old", map[string]any{"session_id": "sess_old", "last_message_at": 100.0})
	writeSessionDir(t, "sess_new", map[string]any{"session_id": "sess_new", "last_message_at": 300.0})
	writeSessionDir(t, "sess_mid", map[string]any{"session_id": "sess_mid", "last_message_at": 200})
	writeSessionDir(t, "heartbeat_x", map[string]any{"session_id": "heartbeat_x", "last_message_at": 999.0})
	writeSessionDir(t, "sess_nometa", nil)
	// 坏 JSON：解析失败也必须以 session_id 兜底入列
	badDir := filepath.Join(workspace.AgentSessionsDir(), "sess_badjson")
	require.NoError(t, os.MkdirAll(badDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(badDir, "metadata.json"), []byte("{oops"), 0o644))
	// 非目录条目必须跳过
	require.NoError(t, os.WriteFile(filepath.Join(workspace.AgentSessionsDir(), "README"), []byte("x"), 0o644))

	// limit=2：只回两条，且按 last_message_at 降序（heartbeat_ 已过滤）
	payload, err := callHandler(sessionList(newTestChannel()), map[string]any{
		"limit":  float64(2),
		"offset": float64(0),
	}, "")
	require.NoError(t, err)
	assert.Equal(t, 5, payload["total"], "heartbeat_ 会话不参与计数")
	sessions, _ := payload["sessions"].([]map[string]any)
	require.Len(t, sessions, 2)
	assert.Equal(t, "sess_new", sessions[0]["session_id"])
	assert.Equal(t, "sess_mid", sessions[1]["session_id"])

	// limit=0 会被 clamp 到 1；offset 为负数 clamp 到 0
	payload, err = callHandler(sessionList(newTestChannel()), map[string]any{
		"limit":  0,
		"offset": -5,
	}, "")
	require.NoError(t, err)
	assert.Equal(t, 1, payload["limit"])
	assert.Equal(t, 0, payload["offset"])
	sessions, _ = payload["sessions"].([]map[string]any)
	assert.Len(t, sessions, 1)

	// limit 超上限 → clamp 到 200；offset 超总数 → clamp 到 total（结果为空）
	payload, err = callHandler(sessionList(newTestChannel()), map[string]any{
		"limit":  1000,
		"offset": 999,
	}, "")
	require.NoError(t, err)
	assert.Equal(t, 200, payload["limit"])
	assert.Equal(t, 5, payload["offset"])
	sessions, _ = payload["sessions"].([]map[string]any)
	assert.Empty(t, sessions)

	// 缺 metadata / 坏 JSON 的目录以 session_id 兜底
	payload, err = callHandler(sessionList(newTestChannel()), map[string]any{"limit": 200}, "")
	require.NoError(t, err)
	byID := map[string]map[string]any{}
	for _, s := range payload["sessions"].([]map[string]any) {
		byID[s["session_id"].(string)] = s
	}
	assert.Contains(t, byID, "sess_nometa")
	assert.Contains(t, byID, "sess_badjson")
	assert.NotContains(t, byID, "heartbeat_x")
}

// TestSessionList_会话目录不可读 目录位置被文件占用 → INTERNAL_ERROR。
func TestSessionList_会话目录不可读(t *testing.T) {
	useTempWorkspace(t)

	sessionsDir := workspace.AgentSessionsDir()
	require.NoError(t, os.MkdirAll(filepath.Dir(sessionsDir), 0o755))
	require.NoError(t, os.WriteFile(sessionsDir, []byte("not a dir"), 0o644))

	_, err := callHandler(sessionList(newTestChannel()), nil, "")
	require.Error(t, err)
	assert.Equal(t, WsErrInternalError, errorCode(err))
}

// ──────────────────────────── session.create 会话创建 ────────────────────────────

// TestSessionCreate_写入可选字段 channel_id/user_id/title/mode 一并落 metadata.json。
func TestSessionCreate_写入可选字段(t *testing.T) {
	useTempWorkspace(t)

	payload, err := callHandler(sessionCreate(newTestChannel()), map[string]any{
		"session_id": "sess_meta",
		"channel_id": "web",
		"user_id":    "u1",
		"title":      "标题",
		"mode":       "BUILD",
	}, "")
	require.NoError(t, err)
	assert.Equal(t, "sess_meta", payload["session_id"])

	data, readErr := os.ReadFile(filepath.Join(workspace.AgentSessionsDir(), "sess_meta", "metadata.json"))
	require.NoError(t, readErr)
	var meta map[string]any
	require.NoError(t, json.Unmarshal(data, &meta))
	assert.Equal(t, "web", meta["channel_id"])
	assert.Equal(t, "u1", meta["user_id"])
	assert.Equal(t, "标题", meta["title"])
	assert.Equal(t, "BUILD", meta["mode"])
	assert.Contains(t, meta, "created_at")
}

// TestSessionCreate_建目录失败 目录位置被文件占用 → INTERNAL_ERROR。
func TestSessionCreate_建目录失败(t *testing.T) {
	useTempWorkspace(t)

	sessionsDir := workspace.AgentSessionsDir()
	require.NoError(t, os.MkdirAll(filepath.Dir(sessionsDir), 0o755))
	require.NoError(t, os.WriteFile(sessionsDir, []byte("not a dir"), 0o644))

	_, err := callHandler(sessionCreate(newTestChannel()), map[string]any{"session_id": "s1"}, "")
	require.Error(t, err)
	assert.Equal(t, WsErrInternalError, errorCode(err))
}

// ──────────────────────────── history.get 历史读取 ────────────────────────────

// TestHistoryGet_透传cursor与limit 对齐上游 app_web_handlers.py:4165-4176 的四个可选键。
func TestHistoryGet_透传cursor与limit(t *testing.T) {
	payload, err := callHandler(historyGet(newTestChannel()), map[string]any{
		"session_id": "sid-params",
		"page_idx":   2,
		"cursor":     "c-1",
		"limit":      50,
	}, "sid-default")
	require.NoError(t, err)
	assert.Equal(t, "sid-params", payload["session_id"])
	assert.Equal(t, 2, payload["page_idx"])
	assert.Equal(t, "c-1", payload["cursor"])
	assert.Equal(t, 50, payload["limit"])
	assert.Equal(t, true, payload["accepted"])
}

// ──────────────────────────── permissions.* 权限相关接口 ────────────────────────────

// TestPermissionsOwnerScopesGet_无配置回空表 config.yaml 缺失时落空表而非报错。
func TestPermissionsOwnerScopesGet_无配置回空表(t *testing.T) {
	useTempWorkspace(t)

	payload, err := callHandler(permissionsOwnerScopesGet(newTestChannel()), nil, "")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{}, payload["owner_scopes"])
	assert.Equal(t, "", payload["deny_guidance_message"])
}

// TestPermissionsOwnerScopesSet_params为空 BAD_REQUEST。
func TestPermissionsOwnerScopesSet_params为空(t *testing.T) {
	useTempWorkspace(t)

	_, err := callHandler(permissionsOwnerScopesSet(newTestChannel()), nil, "")
	require.Error(t, err)
	assert.Equal(t, WsErrBadRequest, errorCode(err))
}

// TestPermissionsOwnerScopesSet_写入成功 回 {"ok": true} 且 get 能读回。
func TestPermissionsOwnerScopesSet_写入成功(t *testing.T) {
	useTempWorkspace(t)
	ensureConfigFile(t)

	payload, err := callHandler(permissionsOwnerScopesSet(newTestChannel()), map[string]any{
		"owner_scopes":          map[string]any{"u1": "all"},
		"deny_guidance_message": "无权限",
	}, "")
	require.NoError(t, err)
	assert.Equal(t, true, payload["ok"])

	got, getErr := callHandler(permissionsOwnerScopesGet(newTestChannel()), nil, "")
	require.NoError(t, getErr)
	scopes, _ := got["owner_scopes"].(map[string]any)
	assert.Equal(t, "all", scopes["u1"])
	assert.Equal(t, "无权限", got["deny_guidance_message"])
}

// TestPermissionsForwardToAgent_本地dispatch成功 AgentClient 未就绪时回退本地状态。
func TestPermissionsForwardToAgent_本地dispatch成功(t *testing.T) {
	useTempWorkspace(t)

	payload, err := callHandler(permissionsForwardToAgent(newTestChannel(), nil, "permissions.tools.get"), nil, "")
	require.NoError(t, err)
	assert.Contains(t, payload, "tools")
}

// TestPermissionsForwardToAgent_本地dispatch失败 未知方法 → 帧级错误 + BAD_REQUEST。
func TestPermissionsForwardToAgent_本地dispatch失败(t *testing.T) {
	useTempWorkspace(t)

	_, err := callHandler(permissionsForwardToAgent(newTestChannel(), nil, "permissions.unknown.get"), nil, "")
	require.Error(t, err)
	assert.Equal(t, WsErrBadRequest, errorCode(err))
	assert.Contains(t, err.Error(), "unknown permissions req_method")
}

// ──────────────────────────── 写回失败分支 ────────────────────────────

// breakConfigFile 把 config.yaml 的位置占成目录，让所有 yaml 写回必然失败。
// 用目录而不是只读文件：测试以 root 运行，chmod 无法阻止写入。
func breakConfigFile(t *testing.T) {
	t.Helper()
	path := workspace.ConfigFile()
	require.NoError(t, os.MkdirAll(path, 0o755))
}

// TestMemoryForbiddenGet_配置损坏 配置加载失败时落空对象而非报错（对齐 Python except 降级）。
func TestMemoryForbiddenGet_配置损坏(t *testing.T) {
	useTempWorkspace(t)
	path := workspace.ConfigFile()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("key: [unclosed"), 0o644))

	payload, err := callHandler(memoryForbiddenGet(newTestChannel()), nil, "")
	require.NoError(t, err)
	assert.NotNil(t, payload)
}

// TestConfigSet_写回失败回INTERNAL_ERROR yaml 写不进时非 BadRequest 分支回 INTERNAL_ERROR。
func TestConfigSet_写回失败回INTERNAL_ERROR(t *testing.T) {
	useTempWorkspace(t)
	breakConfigFile(t)

	_, err := callHandler(configSet(newTestChannel(), nil, nil, nil), map[string]any{
		"team": []any{},
	}, "")
	require.Error(t, err)
	assert.Equal(t, WsErrInternalError, errorCode(err))
}

// TestConfigSaveAll_config写回失败回INTERNAL_ERROR 同上，走 save_all 的 config 子载荷分支。
func TestConfigSaveAll_config写回失败回INTERNAL_ERROR(t *testing.T) {
	useTempWorkspace(t)
	breakConfigFile(t)

	_, err := callHandler(configSaveAll(newTestChannel(), nil, nil, nil), map[string]any{
		"team": []any{},
	}, "")
	require.Error(t, err)
	assert.Equal(t, WsErrInternalError, errorCode(err))
}

// TestModelsReplaceAll_写回失败回INTERNAL_ERROR models.defaults 写不进时回 INTERNAL_ERROR。
func TestModelsReplaceAll_写回失败回INTERNAL_ERROR(t *testing.T) {
	useTempWorkspace(t)
	breakConfigFile(t)

	_, err := callHandler(modelsReplaceAll(newTestChannel(), nil, nil), map[string]any{
		"models": []any{map[string]any{
			"model_name":     "qwen-max",
			"api_base":       "https://example.com/v1",
			"api_key":        "sk-x",
			"model_provider": "OpenAI",
		}},
	}, "")
	require.Error(t, err)
	assert.Equal(t, WsErrInternalError, errorCode(err))
}

// TestPermissionsOwnerScopesSet_写回失败回INTERNAL_ERROR 配置不可写时回 INTERNAL_ERROR。
func TestPermissionsOwnerScopesSet_写回失败回INTERNAL_ERROR(t *testing.T) {
	useTempWorkspace(t)
	breakConfigFile(t)

	_, err := callHandler(permissionsOwnerScopesSet(newTestChannel()), map[string]any{
		"owner_scopes": map[string]any{"u1": "all"},
	}, "")
	require.Error(t, err)
	assert.Equal(t, WsErrInternalError, errorCode(err))
}

// TestSessionList_目录不存在 首启无会话目录时回空列表（对齐 Python 目录缺失容错）。
func TestSessionList_目录不存在(t *testing.T) {
	_ = useTempWorkspace(t)

	payload, err := callHandler(sessionList(newTestChannel()), nil, "")
	require.NoError(t, err)
	assert.Equal(t, 0, payload["total"])
	assert.Empty(t, payload["sessions"])
}

// TestCronJobToggle_控制器报错非notFound ToggleJob 返回其他错误 → BAD_REQUEST。
func TestCronJobToggle_控制器报错非notFound(t *testing.T) {
	cc := &fakeCronJobController{err: errNotJobNotFound{}}

	_, err := callHandler(cronJobToggle(newTestChannel(), cc), map[string]any{
		"id": "j1", "enabled": true,
	}, "")
	require.Error(t, err)
	assert.Equal(t, WsErrBadRequest, errorCode(err))
}

// TestCronJobPreview_控制器报错非notFound PreviewJob 返回其他错误 → BAD_REQUEST。
func TestCronJobPreview_控制器报错非notFound(t *testing.T) {
	cc := &fakeCronJobController{err: errNotJobNotFound{}}

	_, err := callHandler(cronJobPreview(newTestChannel(), cc), map[string]any{"id": "j1"}, "")
	require.Error(t, err)
	assert.Equal(t, WsErrBadRequest, errorCode(err))
}

// errNotJobNotFound 任意非 "job not found" 的控制器错误。
type errNotJobNotFound struct{}

// Error 实现 error 接口。
func (errNotJobNotFound) Error() string { return "scheduler unavailable" }

// TestMemoryForbiddenGet_非对象值兜底 配置里该键不是 mapping 时回空对象（对齐 Python 类型守卫）。
func TestMemoryForbiddenGet_非对象值兜底(t *testing.T) {
	useTempWorkspace(t)
	path := workspace.ConfigFile()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("memory:\n  forbidden_memory_definition: not-a-map\n"), 0o644))

	payload, err := callHandler(memoryForbiddenGet(newTestChannel()), nil, "")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{}, payload)
}
