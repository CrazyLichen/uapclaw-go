package personalcontext

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// directoryCapacityDefaults 对齐 Python config.py:41-42 默认值
func TestDirectoryCapacityDefaults(t *testing.T) {
	pages, subdirs := directoryCapacityDefaults()
	if pages != 20 || subdirs != 20 {
		t.Errorf("capacity = (%d, %d), want (20, 20)", pages, subdirs)
	}
}

// isUsableModelEntry/modelEntryID（host_api.py:78-106）
func TestModelEntryHelpers(t *testing.T) {
	usable := map[string]any{
		"model_client_config": map[string]any{"model_name": "qwen-max", "client_provider": "openai"},
		"model_config_obj":    map[string]any{"model": "x"},
	}
	if !isUsableModelEntry(usable) {
		t.Errorf("可用条目判定失败")
	}
	if got := modelEntryID(usable); got != "openai:qwen-max" {
		t.Errorf("modelEntryID = %q, want openai:qwen-max（host_api.py:105-106 provider:model_name）", got)
	}
	// model_name 为空的占位条目不可用（host_api.py:78-87 注释）
	placeholder := map[string]any{
		"model_client_config": map[string]any{"model_name": ""},
		"model_config_obj":    map[string]any{},
	}
	if isUsableModelEntry(placeholder) {
		t.Errorf("占位条目不应可用")
	}
	if isUsableModelEntry(nil) {
		t.Errorf("nil 条目不应可用")
	}
}

// firstUsableModel/modelIndexIsUsable（host_api.py:121-146）
func TestFirstUsableModel(t *testing.T) {
	bad := map[string]any{
		"model_client_config": map[string]any{"model_name": ""},
		"model_config_obj":    map[string]any{},
	}
	good := map[string]any{
		"model_client_config": map[string]any{"model_name": "m1"},
		"model_config_obj":    map[string]any{},
	}
	models := []map[string]any{bad, good}
	idx, id := firstUsableModel(models)
	if idx != 1 || id == "" {
		t.Errorf("firstUsableModel = (%d, %q), want (1, 非空)", idx, id)
	}
	if firstUsableModelIndex([]map[string]any{bad}) != -1 {
		t.Errorf("无可用模型应返回 -1")
	}
	if modelIndexIsUsable(models, float64(0)) != -1 {
		t.Errorf("下标 0 指向坏条目应不可用")
	}
	if modelIndexIsUsable(models, float64(1)) != 1 {
		t.Errorf("下标 1 应可用")
	}
	if modelIndexIsUsable(models, float64(5)) != -1 {
		t.Errorf("越界下标不可用")
	}
}

// reconcileModelSelection 四分支（host_api.py:149-178）
func TestReconcileModelSelection(t *testing.T) {
	good := map[string]any{
		"model_client_config": map[string]any{"model_name": "m1", "client_provider": "p"},
		"model_config_obj":    map[string]any{},
	}
	models := []map[string]any{good}
	id := modelEntryID(good)

	// 分支 1：model_id 可用 → 刷新 model_index
	stored := map[string]any{"model_id": id, "model_index": float64(9)}
	reconcileModelSelection(stored, models)
	if stored["model_index"] != 0 {
		t.Errorf("分支1: model_index = %v, want 0", stored["model_index"])
	}

	// 分支 2：旧 YAML 只有 model_index → 补写 model_id
	stored = map[string]any{"model_index": float64(0)}
	reconcileModelSelection(stored, models)
	if stored["model_id"] != id {
		t.Errorf("分支2: model_id = %v, want %q", stored["model_id"], id)
	}

	// 分支 3：双失效 → 回落第一个可用
	stored = map[string]any{"model_id": "gone", "model_index": float64(7)}
	reconcileModelSelection(stored, models)
	if stored["model_index"] != 0 || stored["model_id"] != id {
		t.Errorf("分支3: %v / %v", stored["model_index"], stored["model_id"])
	}

	// 分支 4：无可用模型 + balanced → 降级 rules
	stored = map[string]any{"model_id": "gone", "strategy_profile": "balanced"}
	reconcileModelSelection(stored, nil)
	if stored["model_index"] != nil || stored["model_id"] != nil || stored["strategy_profile"] != "rules" {
		t.Errorf("分支4: %v", stored)
	}
}

// defaultStrategyAndModel / initialStoredConfig / unconfiguredProjection（host_api.py:181-225）
func TestDefaultStrategyAndInitialConfig(t *testing.T) {
	strategy, idx, id := defaultStrategyAndModel(nil)
	if strategy != "rules" || idx != nil || id != nil {
		t.Errorf("无模型默认 rules: (%v, %v, %v)", strategy, idx, id)
	}
	stored := initialStoredConfig(nil, true)
	if stored["master_enabled"] != true || stored["agent_use_enabled"] != false {
		t.Errorf("initialStoredConfig 开关不符: %v", stored)
	}
	if _, ok := stored["fetch_services"].([]any); !ok {
		t.Errorf("fetch_services 应为空列表")
	}
	proj := unconfiguredProjection(nil)
	if proj["configured"] != false || proj["master_enabled"] != false {
		t.Errorf("未配置投影不符: %v", proj)
	}
}

// validateServiceIntegers（host_api.py:280-291）
func TestValidateServiceIntegers(t *testing.T) {
	if err := validateServiceIntegers(map[string]any{"interval_seconds": float64(30)}); err != nil {
		t.Errorf("正整数应通过: %v", err)
	}
	if err := validateServiceIntegers(map[string]any{}); err != nil {
		t.Errorf("缺字段应跳过: %v", err)
	}
	if err := validateServiceIntegers(map[string]any{"max_items_per_run": nil}); err != nil {
		t.Errorf("max_items_per_run=None 应跳过: %v", err)
	}
	if err := validateServiceIntegers(map[string]any{"interval_seconds": float64(0)}); err == nil {
		t.Errorf("0 应拒绝")
	}
	if err := validateServiceIntegers(map[string]any{"interval_seconds": 1.5}); err == nil {
		t.Errorf("非整数应拒绝")
	} else if err.Reason != "positive_integer" {
		t.Errorf("reason = %q", err.Reason)
	}
	if err := validateServiceIntegers(map[string]any{"interval_seconds": "30"}); err == nil {
		t.Errorf("字符串应拒绝")
	}
}

// rejectSymlinkChain（host_api.py:293-305）
func TestRejectSymlinkChain(t *testing.T) {
	dir := t.TempDir()
	if err := rejectSymlinkChain(filepath.Join(dir, "a", "b.yaml")); err != nil {
		t.Errorf("正常路径应通过: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skip("无法创建 symlink")
	}
	err := rejectSymlinkChain(filepath.Join(link, "b.yaml"))
	if err == nil || err.Reason != "config_file_error" || err.StatusName != "CONTEXT_PROACTIVE_FILE_EXECUTION_ERROR" {
		t.Errorf("symlink 链应拒绝: %v", err)
	}
}

// serializeConfig 4MiB 上限（host_api.py:308-326）
func TestSerializeConfig(t *testing.T) {
	payload, err := serializeConfig(map[string]any{"a": 1})
	if err != nil || payload == nil {
		t.Errorf("正常序列化失败: %v", err)
	}
	big := strings.Repeat("x", maxConfigBytes+1)
	_, err = serializeConfig(map[string]any{"big": big})
	if err == nil || !strings.Contains(err.Msg, "4 MiB") {
		t.Errorf("超限应拒绝: %v", err)
	}
}

// publishYaml 原子发布链（host_api.py:328-392）
func TestPublishYaml原子发布(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", configFilename)
	payload, _ := serializeConfig(map[string]any{"master_enabled": true})
	if err := publishYaml(path, payload); err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("权限 = %v, want 600", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("临时文件应清理，残留 %d 项", len(entries))
	}
}

// readYaml：缺文件 None / 非对象拒绝 / 目录拒绝 / 往返一致（host_api.py:395-441）
func TestReadYaml(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, configFilename)

	stored, err := readYaml(path)
	if err != nil || stored != nil {
		t.Errorf("缺文件应 (nil, nil): %v %v", stored, err)
	}

	payload, _ := serializeConfig(map[string]any{"master_enabled": true, "fetch_services": []any{}})
	if err := publishYaml(path, payload); err != nil {
		t.Fatal(err)
	}
	stored, err = readYaml(path)
	if err != nil || stored == nil || stored["master_enabled"] != true {
		t.Errorf("往返失败: %v %v", stored, err)
	}

	_ = os.WriteFile(filepath.Join(dir, "scalar.yaml"), []byte("- 1\n- 2\n"), 0o600)
	if _, err = readYaml(filepath.Join(dir, "scalar.yaml")); err == nil || !strings.Contains(err.Msg, "object") {
		t.Errorf("非对象 YAML 应拒绝: %v", err)
	}

	if _, err = readYaml(dir); err == nil || !strings.Contains(err.Msg, "not a file") {
		t.Errorf("目录应拒绝: %v", err)
	}
}

// normalizeProviderCredentials（host_api.py:505-538）
func TestNormalizeProviderCredentials(t *testing.T) {
	out, err := normalizeProviderCredentials(nil)
	if err != nil || len(out) != 0 {
		t.Errorf("None 应返回空: %v %v", out, err)
	}
	out, err = normalizeProviderCredentials(map[string]any{
		"github":  map[string]any{"token": " t1 "},
		"gitcode": map[string]any{"pat": "p1"},
	})
	if err != nil || out["github"]["token"] != "t1" || out["gitcode"]["pat"] != "p1" {
		t.Errorf("归一化失败: %v %v", out, err)
	}
	if _, err = normalizeProviderCredentials(map[string]any{"rss_feed": map[string]any{"token": "x"}}); err == nil {
		t.Errorf("非仓库 provider 应拒绝")
	}
	if _, err = normalizeProviderCredentials(map[string]any{"github": map[string]any{"pat": "x"}}); err == nil {
		t.Errorf("字段名错误应拒绝（github 用 token）")
	}
	if _, err = normalizeProviderCredentials(map[string]any{"github": map[string]any{"token": "  "}}); err == nil {
		t.Errorf("空白凭据应拒绝")
	}
	if _, err = normalizeProviderCredentials("bad"); err == nil {
		t.Errorf("非 dict 应拒绝")
	}
}

// projectService/projectStoredConfig（host_api.py:541-560）
func TestProjectStoredConfig(t *testing.T) {
	stored := map[string]any{
		"collection_enabled":   true,
		"agent_use_enabled":    false,
		"provider_credentials": map[string]any{"github": map[string]any{"token": "t"}},
		"fetch_services": []any{
			map[string]any{"service_id": "s1", "credentials": map[string]any{"token": "t"}},
		},
	}
	projected, err := projectStoredConfig(stored)
	if err != nil {
		t.Fatal(err)
	}
	if projected["master_enabled"] != true {
		t.Errorf("缺 master_enabled 应按子开关补齐: %v", projected["master_enabled"])
	}
	if _, present := projected["provider_credentials"]; present {
		t.Errorf("投影应剔除 provider_credentials")
	}
	svc := projected["fetch_services"].([]any)[0].(map[string]any)
	if _, present := svc["credentials"]; present {
		t.Errorf("投影 service 应剔除 credentials")
	}
	// fetch_services 含非 dict 应拒绝（host_api.py:555-558）
	bad := map[string]any{"fetch_services": []any{"x"}}
	if _, err = projectStoredConfig(bad); err == nil {
		t.Errorf("坏 services 应拒绝")
	}
}

// prepareStoredConfig 主链（host_api.py:609-634）
func TestPrepareStoredConfig(t *testing.T) {
	good := map[string]any{
		"model_client_config": map[string]any{"model_name": "m1", "client_provider": "p"},
		"model_config_obj":    map[string]any{},
	}
	stored := map[string]any{
		"collection_enabled":   true,
		"model_index":          float64(0),
		"provider_credentials": map[string]any{"github": map[string]any{"token": "t"}},
		"fetch_services":       []any{},
	}
	out, err := prepareStoredConfig(stored, []map[string]any{good})
	if err != nil {
		t.Fatal(err)
	}
	if out["master_enabled"] != true {
		t.Errorf("master_enabled 应补齐: %v", out["master_enabled"])
	}
	if _, present := out["model_client"]; present {
		t.Errorf("落盘配置不应携带 model_client（host_api.py:624）")
	}
	if _, present := out["provider_credentials"]; !present {
		t.Errorf("落盘配置应回填 provider_credentials（host_api.py:633）")
	}
	if out["model_index"] != float64(0) {
		t.Errorf("model_index 应保留: %v", out["model_index"])
	}
	if _, err = prepareStoredConfig(map[string]any{"provider_credentials": map[string]any{"xx": map[string]any{"token": "t"}}, "fetch_services": []any{}}, nil); err == nil {
		t.Errorf("未知 provider 凭据应拒绝")
	}
}

// repositoryAuthorizationResult（host_api.py:637-651）
func TestRepositoryAuthorizationResult(t *testing.T) {
	out := repositoryAuthorizationResult("github", "not_authorized", nil, "")
	if out["provider"] != "github" || out["state"] != "not_authorized" || out["account"] != nil || out["error"] != nil {
		t.Errorf("投影不符: %v", out)
	}
	if out["verification_url"] != nil || out["expires_at"] != nil {
		t.Errorf("verification_url/expires_at 应为 None")
	}
}

// resolveModelReference（host_api.py:470-502）
func TestResolveModelReference(t *testing.T) {
	entry := map[string]any{
		"model_client_config": map[string]any{"model_name": "qwen", "keep": "v"},
		"model_config_obj":    map[string]any{"temperature": 0.7},
	}
	client, request, err := resolveModelReference([]map[string]any{entry}, float64(0))
	if err != nil {
		t.Fatal(err)
	}
	if client["model_name"] != nil {
		t.Errorf("model_name 应从 client 弹出（host_api.py:495）")
	}
	if client["max_retries"] != personalContextModelMaxRetries {
		t.Errorf("max_retries 应注入（host_api.py:500）")
	}
	if request["model"] != "qwen" {
		t.Errorf("request.model 应设为 model_name（host_api.py:501）")
	}
	if _, _, err = resolveModelReference([]map[string]any{entry}, float64(5)); err == nil || err.Reason != "model_invalid" {
		t.Errorf("越界应 model_invalid: %v", err)
	}
	if _, _, err = resolveModelReference(nil, -1); err == nil || err.Reason != "parameter_invalid" {
		t.Errorf("负数应 parameter_invalid")
	}
}

// configsEquivalent 语义比较（host_api.py:595-606）
func TestConfigsEquivalent(t *testing.T) {
	left := map[string]any{"a": 1, "model_client": map[string]any{"client_id": "x"}}
	right := map[string]any{"a": 1, "model_client": map[string]any{"client_id": "y"}}
	if !configsEquivalent(left, right) {
		t.Errorf("client_id 剔除后应等价（host_api.py:589-591）")
	}
	right2 := map[string]any{"a": 2}
	if configsEquivalent(left, right2) {
		t.Errorf("不同配置不应等价")
	}
}

// validateRepositoryPAT D-24.3-4 空态挂账（host_api.py:654-755 ⤴️ 24.3b）
func TestValidateRepositoryPAT挂账(t *testing.T) {
	if _, err := validateRepositoryPAT(testCtx(), "github", "tok"); err == nil || err.Reason != "authorization_failed" {
		t.Errorf("网络验证应挂账: %v", err)
	}
	if _, err := validateRepositoryPATForWrite(testCtx(), "github", "tok"); err == nil || err.Reason != "credentials_invalid" {
		t.Errorf("写路径验证应挂账: %v", err)
	}
}

// asHostError 已是 HostError 时原样返回（host_api.py:268-277）
func TestAsHostError(t *testing.T) {
	original := &HostError{Code: "154001", Msg: "keep"}
	if got := asHostError(original, "wrapped", "", ""); got != original {
		t.Errorf("HostError 应原样返回")
	}
	got := asHostError(os.ErrNotExist, "wrapped", "", "config_file_error")
	if got.Msg != "wrapped" || got.Reason != "config_file_error" {
		t.Errorf("包装失败: %v", got)
	}
}

func testCtx() context.Context { return context.Background() }

// ─────────────── Task 3：Host API 主体（Python host_api.py:758-2061 空态行为） ───────────────

// newTestHost 测试用 Host（home 指向 TempDir，cfgGo nil；注入一个可用默认模型）
func newTestHost(t *testing.T) *PersonalContextHostAPI {
	t.Helper()
	setTestModels(t)
	return NewPersonalContextHostAPI(t.TempDir(), nil)
}

// testModel 可用模型条目（model_client_config.model_name 非空，host_api.py:78-87）
func testModel() map[string]any {
	return map[string]any{
		"model_client_config": map[string]any{"model_name": "test-model", "client_provider": "test"},
		"model_config_obj":    map[string]any{"model": "test-model"},
	}
}

// setTestModels 注入默认模型测试桩
func setTestModels(t *testing.T) {
	t.Helper()
	original := defaultModelsFn
	defaultModelsFn = func() []map[string]any { return []map[string]any{testModel()} }
	t.Cleanup(func() { defaultModelsFn = original })
}

// setMasterEnabled 首开：初始配置落盘 + 级联子开关（host_api.py:1240-1267）
func TestSetMasterEnabled_首开落盘(t *testing.T) {
	h := newTestHost(t)
	out, err := h.SetMasterEnabled(true)
	if err != nil {
		t.Fatal(err)
	}
	if out["master_enabled"] != true || out["collection_enabled"] != true || out["agent_use_enabled"] != true {
		t.Errorf("总开关应级联: %v", out)
	}
	stored, herr := readYaml(h.configPath)
	if herr != nil || stored == nil || stored["master_enabled"] != true {
		t.Errorf("yaml 应落盘: %v %v", stored, herr)
	}
	if h.GetStatus()["state"] != "CONFIGURED" {
		t.Errorf("状态应 CONFIGURED（空转态，D-24.3-5）: %v", h.GetStatus()["state"])
	}
}

// setMasterEnabled 关闭：全部子开关关闭（host_api.py:1259-1261）
func TestSetMasterEnabled_关闭级联(t *testing.T) {
	h := newTestHost(t)
	if _, err := h.SetMasterEnabled(true); err != nil {
		t.Fatal(err)
	}
	out, err := h.SetMasterEnabled(false)
	if err != nil {
		t.Fatal(err)
	}
	if out["master_enabled"] != false || out["collection_enabled"] != false || out["agent_use_enabled"] != false {
		t.Errorf("关闭应级联: %v", out)
	}
}

// setCollectionEnabled 首开 false 返回未配置投影（host_api.py:1169-1171）
func TestSetCollectionEnabled_未配置关(t *testing.T) {
	h := newTestHost(t)
	out, err := h.SetCollectionEnabled(false)
	if err != nil || out["configured"] != false {
		t.Errorf("未配置关闭应返回未配置投影: %v %v", out, err)
	}
}

// setAgentUseEnabled 未配置应拒绝（host_api.py:1216-1219）
func TestSetAgentUseEnabled_未配置拒绝(t *testing.T) {
	h := newTestHost(t)
	if _, err := h.SetAgentUseEnabled(true); err == nil || err.Reason != "not_configured" {
		t.Errorf("应 not_configured: %v", err)
	}
}

// patchRuntimeConfig 未知字段拒绝 + 正常修补（host_api.py:1100-1130）
func TestPatchRuntimeConfig(t *testing.T) {
	h := newTestHost(t)
	if _, err := h.SetMasterEnabled(true); err != nil {
		t.Fatal(err)
	}
	if _, err := h.PatchRuntimeConfig(map[string]any{"bogus": 1}); err == nil || err.Reason != "parameter_invalid" {
		t.Errorf("未知字段应拒绝: %v", err)
	}
	out, err := h.PatchRuntimeConfig(map[string]any{"strategy_profile": "balanced"})
	if err != nil || out["strategy_profile"] != "balanced" {
		t.Errorf("修补失败: %v %v", out, err)
	}
}

// selectModel：非配置态拒绝 / 越界 model_invalid / 正常双写（host_api.py:1132-1160）
func TestSelectModel(t *testing.T) {
	h := newTestHost(t)
	if _, err := h.SelectModel(float64(0)); err == nil || err.Reason != "not_configured" {
		t.Errorf("未配置应拒绝: %v", err)
	}
	if _, err := h.SetMasterEnabled(true); err != nil {
		t.Fatal(err)
	}
	if _, err := h.SelectModel(float64(99)); err == nil || err.Reason != "model_invalid" {
		t.Errorf("越界应 model_invalid: %v", err)
	}
	out, err := h.SelectModel(float64(0))
	if err != nil || out["model_index"] != float64(0) || out["model_id"] != "test:test-model" {
		t.Errorf("正常选择失败: %v %v", out, err)
	}
}

// getRuntimeConfig 未配置投影 / 配置后投影（host_api.py:1092-1098）
func TestGetRuntimeConfig(t *testing.T) {
	h := newTestHost(t)
	out, err := h.GetRuntimeConfig()
	if err != nil || out["configured"] != false {
		t.Errorf("未配置投影失败: %v %v", out, err)
	}
	if _, err = h.SetMasterEnabled(true); err != nil {
		t.Fatal(err)
	}
	out, err = h.GetRuntimeConfig()
	if err != nil || out["collection_enabled"] != true {
		t.Errorf("配置后应返回投影（configured 键属 get_overview，get_runtime_config 无此键，Python host_api.py:1092-1098）: %v %v", out, err)
	}
}

// fetch services CRUD：rss 正常建 / github 未授权拒 / 重复拒 / 删除 / patch（host_api.py:1301-1600）
func TestFetchServicesCRUD(t *testing.T) {
	h := newTestHost(t)
	if _, err := h.SetMasterEnabled(true); err != nil {
		t.Fatal(err)
	}
	rss := map[string]any{"service_id": "rss-1", "provider": "rss_feed", "source": map[string]any{"feed_url": "https://example.com/feed"}}
	created, err := h.CreateFetchService(rss)
	if err != nil || created["service_id"] != "rss-1" {
		t.Fatalf("rss 建源失败: %v %v", created, err)
	}
	if _, hasCreds := created["credentials"]; hasCreds {
		t.Errorf("投影不应带 credentials")
	}
	gh := map[string]any{"service_id": "gh-1", "provider": "github", "source": map[string]any{"owner": "o", "repo": "r"}, "credentials": map[string]any{"token": "x"}}
	if _, err = h.CreateFetchService(gh); err == nil || err.Reason != "credentials_invalid" {
		t.Errorf("github 自带凭据应拒（host_api.py:1353-1357）: %v", err)
	}
	gh2 := map[string]any{"service_id": "gh-1", "provider": "github", "source": map[string]any{"owner": "o", "repo": "r"}}
	if _, err = h.CreateFetchService(gh2); err == nil || err.Reason != "authorization_required" {
		t.Errorf("github 未授权应拒（host_api.py:1364-1368）: %v", err)
	}
	if _, err = h.CreateFetchService(rss); err == nil || err.Reason != "duplicate_service" {
		t.Errorf("重复 id 应拒: %v", err)
	}
	list, err := h.ListFetchServices()
	if err != nil || len(list) != 1 {
		t.Errorf("列表应 1 项: %v %v", list, err)
	}
	patched, err := h.PatchFetchService("rss-1", map[string]any{"interval_seconds": float64(600)})
	if err != nil || patched["interval_seconds"] != float64(600) {
		t.Errorf("patch 失败: %v %v", patched, err)
	}
	if _, err = h.PatchFetchService("nope", map[string]any{}); err == nil || err.Reason != "unknown_service" {
		t.Errorf("未知源 patch 应拒: %v", err)
	}
	if err = h.DeleteFetchService("rss-1"); err != nil {
		t.Errorf("删除失败: %v", err)
	}
	if err = h.DeleteFetchService("rss-1"); err == nil || err.Reason != "unknown_service" {
		t.Errorf("重复删除应拒: %v", err)
	}
	if err = h.DeleteFetchService(" "); err == nil || err.Reason != "parameter_invalid" {
		t.Errorf("空白 id 应拒: %v", err)
	}
}

// setFetchServiceEnabled：正常置位往返（host_api.py:1628-1674）
func TestSetFetchServiceEnabled(t *testing.T) {
	h := newTestHost(t)
	if _, err := h.SetMasterEnabled(true); err != nil {
		t.Fatal(err)
	}
	rss := map[string]any{"service_id": "rss-1", "provider": "rss_feed", "source": map[string]any{"feed_url": "https://e.com/f"}}
	if _, err := h.CreateFetchService(rss); err != nil {
		t.Fatal(err)
	}
	if err := h.SetFetchServiceEnabled("rss-1", false); err != nil {
		t.Errorf("置位失败: %v", err)
	}
	list, _ := h.ListFetchServices()
	if list[0]["enabled"] != false {
		t.Errorf("enabled 应落盘: %v", list[0])
	}
	if err := h.SetFetchServiceEnabled("nope", true); err == nil || err.Reason != "unknown_service" {
		t.Errorf("未知源应拒: %v", err)
	}
}

// run 类空态：run_fetch state_invalid / stop_run ok / get_run_status 空（host_api.py:1602-1706）
func TestRun类空态(t *testing.T) {
	h := newTestHost(t)
	if _, err := h.RunFetch(nil); err == nil || err.Reason != "not_configured" {
		t.Errorf("未配置 run 应 not_configured: %v", err)
	}
	if _, err := h.SetMasterEnabled(true); err != nil {
		t.Fatal(err)
	}
	// master 开启后 collection_enabled=true，Core 缺席 → Python 走 Core 非 RUNNING 错误（无 reason 覆盖）
	if _, err := h.RunFetch(nil); err == nil || err.Reason != "state_invalid" {
		t.Errorf("运行时未就绪 run 应 state_invalid: %v", err)
	}
	// 关闭采集后补 collection_disabled 分支（host_api.py:1690-1691）
	if _, err := h.SetCollectionEnabled(false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.RunFetch(nil); err == nil || err.Reason != "collection_disabled" {
		t.Errorf("采集关闭 run 应 collection_disabled: %v", err)
	}
	out, err := h.StopFetchRun("x")
	if err != nil || out["ok"] != true {
		t.Errorf("stop_run 空态应 ok: %v %v", out, err)
	}
	out2, err := h.GetFetchRunStatus(nil, nil)
	if err != nil || len(out2["services"].([]any)) != 0 {
		t.Errorf("get_run_status 应空: %v %v", out2, err)
	}
	if _, err = h.GetFetchRunStatus("", nil); err == nil {
		t.Errorf("空 service_id 应拒")
	}
}

// context 查询族空态：统一 state_invalid（host_api.py:1708-1741）
func TestContext查询族空态(t *testing.T) {
	h := newTestHost(t)
	for name, fn := range map[string]func() *HostError{
		"get_graph":      func() *HostError { _, e := h.GetGraph(nil, 3); return e },
		"get_tree":       func() *HostError { _, e := h.GetTree(nil, 3); return e },
		"search":         func() *HostError { _, e := h.SearchGraph("q"); return e },
		"get_graph_page": func() *HostError { _, e := h.GetGraphPage("n"); return e },
		"get_source":     func() *HostError { _, e := h.GetSource("s"); return e },
	} {
		err := fn()
		if err == nil || err.Reason != "state_invalid" {
			t.Errorf("%s 空态应 state_invalid: %v", name, err)
		}
	}
}

// 授权族空态（host_api.py:1743-1957，D-24.3-4）
func Test授权族空态(t *testing.T) {
	h := newTestHost(t)
	if _, err := h.GetAuthorizationStatus(" "); err == nil {
		t.Errorf("空 provider 应拒")
	}
	out, err := h.GetAuthorizationStatus("github")
	if err != nil || out["state"] != "not_authorized" {
		t.Errorf("未配置 github 应 not_authorized: %v %v", out, err)
	}
	if _, err = h.GetAuthorizationStatus("rss_feed"); err == nil || err.Reason != "authorization_unsupported" {
		t.Errorf("rss_feed 应不支持授权: %v", err)
	}
	if _, err = h.GetAuthorizationStatus("feishu"); err == nil || err.Reason != "not_configured" {
		t.Errorf("未配置 feishu 应 not_configured: %v", err)
	}
	if _, err = h.AuthorizeProvider("github", map[string]any{"pat": "x"}, false); err == nil {
		t.Errorf("github 字段名错应拒（须 token）")
	}
	if _, err = h.AuthorizeProvider("github", map[string]any{"token": " "}, false); err == nil {
		t.Errorf("空白 token 应拒")
	}
	out, err = h.AuthorizeProvider("github", map[string]any{"token": "good"}, false)
	if err != nil || out["state"] != "not_authorized" {
		t.Errorf("D-24.3-4：表单通过后应返回未授权空态: %v %v", out, err)
	}
	if _, err = h.AuthorizeProvider("feishu", map[string]any{"token": "x"}, false); err == nil {
		t.Errorf("feishu 不应接受 credentials")
	}
	if _, err = h.AuthorizeProvider("rss_feed", nil, false); err == nil {
		t.Errorf("rss_feed 应拒绝授权")
	}
}

// start/isRuntimeEnabled/stop 生命周期（host_api.py:1959-2034）
func TestStart生命周期(t *testing.T) {
	h := newTestHost(t)
	if err := h.Start(); err != nil {
		t.Fatalf("无配置 start 应无错返回: %v", err)
	}
	if h.GetStatus()["state"] != "CREATED" {
		t.Errorf("无配置状态 CREATED: %v", h.GetStatus())
	}
	if _, err := h.SetMasterEnabled(true); err != nil {
		t.Fatal(err)
	}
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	if h.GetStatus()["state"] != "RUNNING" {
		t.Errorf("开启后 start 应 RUNNING（空转态标记，D-24.3-5）: %v", h.GetStatus())
	}
	if !h.IsRuntimeEnabled() {
		t.Errorf("agent_use_enabled 应 true")
	}
	if err := h.Stop(0); err == nil || err.StatusName != "CONTEXT_PROACTIVE_RUNTIME_TIMEOUT" {
		t.Errorf("超时 0 应拒: %v", err)
	}
	if err := h.Stop(1); err != nil {
		t.Errorf("stop 失败: %v", err)
	}
	if h.GetStatus()["state"] != "CONFIGURED" {
		t.Errorf("stop 后应 CONFIGURED")
	}
}

// configure + 重启加载：yaml 持久化往返（host_api.py:802-809 + 1959-1979）
func TestConfigure与重启加载(t *testing.T) {
	home := t.TempDir()
	h := NewPersonalContextHostAPI(home, nil)
	stored := map[string]any{
		"master_enabled": true, "collection_enabled": true, "agent_use_enabled": false,
		"strategy_profile": "rules", "fetch_services": []any{},
		"provider_credentials": map[string]any{},
	}
	if err := h.Configure(stored); err != nil {
		t.Fatalf("configure 失败: %v", err)
	}
	// 新实例模拟重启：从 yaml 加载
	h2 := NewPersonalContextHostAPI(home, nil)
	if err := h2.Start(); err != nil {
		t.Fatal(err)
	}
	out, err := h2.GetRuntimeConfig()
	if err != nil || out["collection_enabled"] != true {
		t.Errorf("重启加载失败: %v %v", out, err)
	}
}

// get_overview 结构（host_api.py:1074-1090）
func TestGetOverview(t *testing.T) {
	h := newTestHost(t)
	out, err := h.GetOverview()
	if err != nil || out["configured"] != false {
		t.Errorf("未配置 overview: %v %v", out, err)
	}
	status := out["status"].(map[string]any)
	if status["state"] != "CREATED" {
		t.Errorf("overview status.state 应 CREATED: %v", status)
	}
}
