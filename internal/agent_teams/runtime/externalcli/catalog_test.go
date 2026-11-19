package externalcli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	harnessproviders "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
)

// CatalogSuite external_cli_catalog 对拍与探测编排测试。
// Python 对照：external_cli_catalog.py 各分支逐条覆盖。
type CatalogSuite struct {
	suite.Suite
}

func TestCatalogSuite(t *testing.T) {
	suite.Run(t, new(CatalogSuite))
}

func (s *CatalogSuite) TestReconcileBuiltinModels() {
	declared := []map[string]any{
		{"name": "opus", "efforts": []any{"low"}, "default_effort": "high"}, // high 不在上报集 → 删
		{"name": "gone"},   // CLI 不再提供 → 剔除
		{"name": "sonnet"}, // 未声明 efforts → 上报值补入
	}
	reported := map[string][]string{"opus": {"low"}, "sonnet": {"low"}, "new": {"mid"}}
	corrected, warnings := ReconcileBuiltinModels("claude", declared, reported)
	s.Require().Len(corrected, 2) // gone 被剔除
	s.Equal("opus", corrected[0]["name"])
	s.Equal([]any{"low"}, corrected[0]["efforts"]) // 往返形态 []any（config map 承载）
	_, hasDefault := corrected[0]["default_effort"]
	s.False(hasDefault) // default_effort 不在 efforts → 删
	// 警告序（Python :45-87 顺序）：opus default dropped、removed gone、sonnet efforts、added new
	s.Require().Len(warnings, 4)
	s.Contains(warnings[0], "default_effort 'high' is not supported")
	s.Contains(warnings[1], "removed 'gone'")
	s.Contains(warnings[2], "'sonnet' efforts [] -> [low]")
	s.Contains(warnings[3], "the CLI also offers new")
}

func (s *CatalogSuite) TestReconcileBuiltinModels空Efforts删键() {
	declared := []map[string]any{{"name": "m1", "efforts": []any{"low"}}}
	reported := map[string][]string{"m1": {}}
	corrected, warnings := ReconcileBuiltinModels("claude", declared, reported)
	s.Require().Len(corrected, 1)
	_, has := corrected[0]["efforts"]
	s.False(has) // 上报空 efforts → 删键（Python :74-75）
	s.Require().Len(warnings, 1)
	s.Contains(warnings[0], "[]")
}

func (s *CatalogSuite) TestDiscoveredCatalog滤Default别名() {
	catalog := discoveredCatalog(map[string][]string{"default": {}, "m1": {"low"}})
	s.Require().Len(catalog, 1)
	s.Equal("m1", catalog[0]["name"])
}

// fakeDiscoveryProvider 声明 MODEL_DISCOVERY 的 fake provider（注册缝注入）。
type fakeDiscoveryProvider struct {
	reported map[string][]string
}

func (f *fakeDiscoveryProvider) Card() harnessprotocol.HarnessCard {
	return harnessprotocol.HarnessCard{
		Name: "fake-discovery",
		Capabilities: map[harnessprotocol.HarnessCapability]bool{
			harnessprotocol.HarnessCapabilityModelDiscovery: true,
		},
	}
}

func (f *fakeDiscoveryProvider) Create(config harnessprotocol.JsonObject) (harnessprotocol.HarnessProtocol, error) {
	return &fakeDiscoveryHarness{reported: f.reported}, nil
}

type fakeDiscoveryHarness struct {
	reported map[string][]string
}

func (h *fakeDiscoveryHarness) Card() harnessprotocol.HarnessCard {
	return harnessprotocol.HarnessCard{}
}
func (h *fakeDiscoveryHarness) State() harnessprotocol.HarnessState {
	return harnessprotocol.HarnessStateIdle
}
func (h *fakeDiscoveryHarness) ProviderSessionID() *string { return nil }
func (h *fakeDiscoveryHarness) EventBufferConfig() harnessprotocol.EventBufferConfig {
	return harnessprotocol.EventBufferConfig{}
}
func (h *fakeDiscoveryHarness) Start(context.Context, harnessprotocol.HarnessContext) error {
	return nil
}
func (h *fakeDiscoveryHarness) Stop(context.Context) error { return nil }
func (h *fakeDiscoveryHarness) Events() harnessprotocol.HarnessEventCursor {
	return nil
}
func (h *fakeDiscoveryHarness) TurnEvents(_ *string) harnessprotocol.HarnessEventCursor {
	return nil
}
func (h *fakeDiscoveryHarness) Send(context.Context, harnessprotocol.HarnessInput, harnessprotocol.DeliveryMode) (harnessprotocol.SendReceipt, error) {
	return harnessprotocol.SendReceipt{}, nil
}
func (h *fakeDiscoveryHarness) Abort(context.Context, harnessprotocol.AbortMode) error { return nil }
func (h *fakeDiscoveryHarness) Pause(context.Context) error                            { return nil }
func (h *fakeDiscoveryHarness) Resume(context.Context, *harnessprotocol.HarnessInput) error {
	return nil
}
func (h *fakeDiscoveryHarness) ExportCheckpoint(context.Context) (*harnessprotocol.HarnessCheckpoint, error) {
	return nil, nil
}

func (h *fakeDiscoveryHarness) SetModel(context.Context, harnessprotocol.ModelSelection) error {
	return nil
}

func (h *fakeDiscoveryHarness) ListModels(context.Context) ([]harnessprotocol.ModelOption, error) {
	options := []harnessprotocol.ModelOption{}
	for modelID, efforts := range h.reported {
		options = append(options, harnessprotocol.ModelOption{ModelID: modelID, Efforts: efforts})
	}
	return options, nil
}

// reportRoot 注入报告目录（WorkspaceDir 缓存进程级，走显式路径的测试注入缝：
// store/load 以 ReportedModelsPath 为参的包装在此不可行，故直接用 chdir 语义的
// 环境变量 + 独立临时目录唯一化文件名避免并发互扰）。
var reportMu sync.Mutex

func (s *CatalogSuite) withReport(report map[string]map[string][]string) func() {
	reportMu.Lock()
	_ = os.MkdirAll(filepath.Dir(ReportedModelsPath()), 0o755)
	data := "{}"
	if report != nil {
		b, err := json.MarshalIndent(report, "", "  ")
		s.Require().NoError(err)
		data = string(b)
	}
	s.Require().NoError(os.WriteFile(ReportedModelsPath(), []byte(data), 0o644))
	return reportMu.Unlock
}

func (s *CatalogSuite) TestRefresh探测失败保旧报告() {
	defer s.withReport(map[string]map[string][]string{"kept": {"old": {"low"}}})()
	// 注册两个 fake：一个成功、一个每次 Create 报错
	harnessproviders.RegisterBuiltinProvider("probe-ok",
		func() (harnessprotocol.HarnessProvider, error) {
			return &fakeDiscoveryProvider{reported: map[string][]string{"m1": {"low"}}}, nil
		})
	defer harnessproviders.UnregisterHarnessProvider("probe-ok")
	harnessproviders.RegisterBuiltinProvider("probe-fail",
		func() (harnessprotocol.HarnessProvider, error) {
			return nil, context.DeadlineExceeded
		})
	defer harnessproviders.UnregisterHarnessProvider("probe-fail")

	entries := []any{
		map[string]any{"cli_agent": "probe-ok"},
		map[string]any{"cli_agent": "probe-fail", "builtin_models": []any{map[string]any{"name": "old"}}},
	}
	var catalogs map[string][]map[string]any
	warnings := RefreshExternalCliBuiltinModels(context.Background(), entries,
		[]string{"probe-ok", "probe-fail"}, func(c map[string][]map[string]any) { catalogs = c })

	reports := LoadReportedModels()
	s.Equal(map[string][]string{"m1": {"low"}}, reports["probe-ok"]) // 成功者更新
	s.Equal(map[string][]string{"old": {"low"}}, reports["kept"])    // 失败者保旧报告
	s.Nil(reports["probe-fail"])                                     // 失败者不进报告
	s.Empty(warnings)                                                // probe-ok 是播种 kind，无目录对拍
	s.Require().NotNil(catalogs)
	s.Equal("m1", catalogs["probe-ok"][0]["name"]) // 播种目录回写回调收到
}

func (s *CatalogSuite) TestApplyReportedModelsInPlace() {
	defer s.withReport(map[string]map[string][]string{"dsh": {"m1": {"low"}}})()
	// apply 只修正不新增（reconcile 语义）：gone 被剔除、m1 的 efforts 由上报补入
	entries := []any{
		map[string]any{"cli_agent": "dsh", "builtin_models": []any{
			map[string]any{"name": "gone"}, map[string]any{"name": "m1"},
		}},
		map[string]any{"cli_agent": "unprobed", "builtin_models": []any{map[string]any{"name": "keep"}}},
	}
	ApplyReportedModels(entries)
	dsh := entries[0].(map[string]any)
	corrected := dsh["builtin_models"].([]any)
	s.Require().Len(corrected, 1)
	s.Equal("m1", corrected[0].(map[string]any)["name"]) // in-place 修正
	unprobed := entries[1].(map[string]any)
	s.Equal("keep", unprobed["builtin_models"].([]any)[0].(map[string]any)["name"]) // 未探测过保原样
}

func (s *CatalogSuite) TestLoadReportedModels损坏归空() {
	s.Require().NoError(os.MkdirAll(filepath.Dir(ReportedModelsPath()), 0o755))
	s.Require().NoError(os.WriteFile(ReportedModelsPath(), []byte("{bad"), 0o644))
	s.Empty(LoadReportedModels())
}
