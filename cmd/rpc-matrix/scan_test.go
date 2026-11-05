package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile 在临时目录写一个文件（测试助手）。
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestScanFrontend_识别包装器调用 验证前端 RPC 方法名提取。
func TestScanFrontend_识别包装器调用(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "src", "api.ts"),
		"export const a = () => call('models.list', {});\n"+
			"export const b = () => request('session.pin', {});\n"+
			"const NOT_RPC = 'not.a.method';\n")

	sites, err := scanFrontend(root)
	if err != nil {
		t.Fatalf("scanFrontend: %v", err)
	}
	got := map[string]bool{}
	for _, s := range sites {
		got[s.Method] = true
	}
	if !got["models.list"] || !got["session.pin"] {
		t.Fatalf("未识别到 RPC 方法，实际：%v", got)
	}
	if got["not.a.method"] {
		t.Fatalf("把普通字符串误判为 RPC：%v", got)
	}
}

// TestScanGo_识别注册点 验证 Register / RegisterLocalHandler 两种写法。
func TestScanGo_识别注册点(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "web", "handlers.go"),
		"func f() {\n"+
			"\td.Register(\"config.get\", h1)\n"+
			"\trs.RegisterLocalHandler(\"/tui\", \"session.delete\", h2)\n"+
			"}\n")

	sites, err := scanGo(root)
	if err != nil {
		t.Fatalf("scanGo: %v", err)
	}
	got := map[string]bool{}
	for _, s := range sites {
		got[s.Method] = true
	}
	if !got["config.get"] || !got["session.delete"] {
		t.Fatalf("未识别到注册点，实际：%v", got)
	}
}

// TestScanUpstream_识别直接与间接注册 验证 register_method 与助手封装两种写法。
func TestScanUpstream_识别直接与间接注册(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "gateway", "h.py"),
		"    channel.register_method(\"models.get\", _models_get)\n"+
			"    def _register_perm(method_name, rm):\n"+
			"        channel.register_method(method_name, _h)\n"+
			"    _register_perm(\"permissions.rules.create\", _PermReq.CREATE)\n")

	src, err := loadUpstreamSource(root)
	if err != nil {
		t.Fatalf("loadUpstreamSource: %v", err)
	}
	sites, err := scanUpstream(root, detectRegistrars(src))
	if err != nil {
		t.Fatalf("scanUpstream: %v", err)
	}
	got := map[string]bool{}
	handlers := map[string]string{}
	for _, s := range sites {
		got[s.Method] = true
		handlers[s.Method] = s.Handler
	}
	if !got["models.get"] || !got["permissions.rules.create"] {
		t.Fatalf("未识别到上游注册，实际：%v", got)
	}
	if handlers["models.get"] != "_models_get" {
		t.Fatalf("handler 符号未提取：%q", handlers["models.get"])
	}
}

// TestBuildMatrix_三态归类 验证一致/漂移/缺三种状态。
func TestBuildMatrix_三态归类(t *testing.T) {
	rows := buildMatrix(
		map[string][]string{"models.list": {"a.ts:1"}, "project.list": {"b.ts:2"}},
		map[string][]string{"models.list": {"web.go:9"}, "harness.list": {"web.go:10"}},
		map[string][]string{"models.list": {"h.py:3"}, "project.list": {"h.py:4"}},
	)
	byMethod := map[string]string{}
	for _, r := range rows {
		byMethod[r.Method] = r.Status
	}
	if byMethod["models.list"] != statusOK {
		t.Fatalf("models.list 应为 %s，实际 %s", statusOK, byMethod["models.list"])
	}
	if byMethod["project.list"] != statusMissing {
		t.Fatalf("project.list 应为 %s，实际 %s", statusMissing, byMethod["project.list"])
	}
	if byMethod["harness.list"] != statusDrift {
		t.Fatalf("harness.list 应为 %s，实际 %s", statusDrift, byMethod["harness.list"])
	}
}

// TestMissing_只统计缺的行 验证 -verify 的判据集合。
func TestMissing_只统计缺的行(t *testing.T) {
	rows := []matrixRow{
		{Method: "a.b", Status: statusOK},
		{Method: "c.d", Status: statusMissing},
		{Method: "e.f", Status: statusDrift},
	}
	got := missing(rows)
	if len(got) != 1 || got[0].Method != "c.d" {
		t.Fatalf("missing 应为 [c.d]，实际 %v", got)
	}
}

// TestSnakeToCamel 验证上游常量到本仓标识符的转换。
func TestSnakeToCamel(t *testing.T) {
	cases := map[string]string{
		"SESSION_GET_METADATA":    "SessionGetMetadata",
		"HARNESS_PACKAGES_EXPORT": "HarnessPackagesExport",
		"RECOMMENDATION":          "Recommendation",
	}
	for in, want := range cases {
		if got := snakeToCamel(in); got != want {
			t.Errorf("snakeToCamel(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestClassifyDownstream_四种形态 验证 local / agent:ok / const-only / missing 判定。
func TestClassifyDownstream_四种形态(t *testing.T) {
	src := &upstreamSource{bodies: map[string]string{
		"_local": "async def _local(ws, req_id, params, session_id):\n    payload = build()\n",
		"_ok":    "async def _ok(ws, req_id, params, session_id):\n    await proxy_unary_request(req_method=ReqMethod.SESSION_RENAME)\n",
		"_const": "async def _const(ws, req_id, params, session_id):\n    await proxy_unary_request(req_method=ReqMethod.HARNESS_PACKAGES_EXPORT)\n",
		"_miss":  "async def _miss(ws, req_id, params, session_id):\n    await proxy_unary_request(req_method=ReqMethod.PROJECT_GIT_STATUS)\n",
		"_rec":   "async def _rec(ws, req_id, params, session_id):\n    await _forward_recommendation(ws, req_id, params, session_id, action='get')\n",
	}}
	caps := &goCapabilities{
		constants:   map[string]bool{"ReqMethodSessionRename": true, "ReqMethodHarnessPackagesExport": true},
		serverCases: map[string]bool{"ReqMethodSessionRename": true},
	}

	cases := []struct {
		method  string
		handler string
		want    string
	}{
		{"local.thing", "_local", downLocal},
		{"session.rename", "_ok", downAgentOK},
		{"harness.export", "_const", downAgentConst},
		{"project.git.status", "_miss", downAgentMissing},
		{"topic_starters.get", "_rec", downAgentMissing},
		{"openai_account.auth.status", "_local", downDep},
	}
	for _, c := range cases {
		got, note := classifyDownstream(c.method, upstreamRef{Handler: c.handler}, nil, src, caps)
		if got != c.want {
			t.Errorf("%s 下游状态 = %s（%s），want %s", c.method, got, note, c.want)
		}
	}

	// 间接注册：方法名与常量都在调用处，按常量直接判定。
	got, note := classifyDownstream("permissions.rules.get", upstreamRef{Token: "PERMISSIONS_RULES_GET"}, nil, src, caps)
	if got != downAgentMissing {
		t.Errorf("间接注册应按常量判定为 agent:missing，实际 %s（%s）", got, note)
	}
	// config 代理族归 local（单用户本地执行）。
	got, note = classifyDownstream("models.get", upstreamRef{Token: "MODELS_GET"}, map[string]bool{"models.get": true}, src, caps)
	if got != downLocal {
		t.Errorf("config 代理族应为 local，实际 %s（%s）", got, note)
	}
}

// TestRenderGaps_只列非本地缺口 验证缺口清单分组。
func TestRenderGaps_只列非本地缺口(t *testing.T) {
	rows := []matrixRow{
		{Method: "a.local", Status: statusMissing, Down: downstream{Status: downLocal}},
		{Method: "b.proxy", Status: statusMissing, Down: downstream{Status: downAgentMissing, Note: "代理 X"}},
		{Method: "c.ok", Status: statusOK, Down: downstream{Status: downAgentOK}},
	}
	out := renderGaps(rows)
	if want := "b.proxy"; !containsStr(out, want) {
		t.Fatalf("缺口清单应包含 %s", want)
	}
	if containsStr(out, "a.local") {
		t.Fatalf("local 缺口不应进下游清单")
	}
	if containsStr(out, "c.ok") {
		t.Fatalf("已覆盖方法不应进下游清单")
	}
}

// TestInScope_只保留本层承诺范围 验证门禁口径按下游状态过滤。
func TestInScope_只保留本层承诺范围(t *testing.T) {
	rows := []matrixRow{
		{Method: "a.local", Down: downstream{Status: downLocal}},
		{Method: "b.ok", Down: downstream{Status: downAgentOK}},
		{Method: "c.proxy", Down: downstream{Status: downAgentMissing}},
		{Method: "d.dep", Down: downstream{Status: downDep}},
	}
	got := inScope(rows)
	if len(got) != 2 {
		t.Fatalf("inScope 应保留 2 条，实际 %d", len(got))
	}
	for _, r := range got {
		if r.Down.Status != downLocal && r.Down.Status != downAgentOK {
			t.Fatalf("inScope 混入下游缺口：%s", r.Method)
		}
	}
}

// containsStr 判断子串是否出现（测试助手）。
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
