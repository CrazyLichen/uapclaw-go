package evaluator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installSwebenchRunFake 替换 swebenchRun（Python monkeypatch subprocess.run 等价）。
func installSwebenchRunFake(t *testing.T, fake *fakeTBRun) {
	t.Helper()
	orig := swebenchRun
	swebenchRun = func(command []string, check bool, timeoutSec int, cwd string, inputText string) (tbRunResult, error) {
		name := command[0]
		args := command[1:]
		fake.calls = append(fake.calls, tbRunCall{Name: name, Args: args, TimeoutSec: float64(timeoutSec), Input: []byte(inputText)})
		if len(fake.steps) > 0 {
			step := fake.steps[0]
			fake.steps = fake.steps[1:]
			return step.res, step.err
		}
		return fake.defRes.res, fake.defRes.err
	}
	t.Cleanup(func() { swebenchRun = orig })
}

// callText 拼接一次调用的完整命令文本。
func callText(call tbRunCall) string {
	return call.Name + " " + strings.Join(call.Args, " ")
}

func TestSwebenchImageName_转换规则(t *testing.T) {
	got := SwebenchImageName("django__django-11039", "swebench", "latest")
	if got != "swebench/sweb.eval.x86_64.django_1776_django-11039:latest" {
		t.Fatalf("image = %q", got)
	}
	// 大写 instance_id 小写化。
	got = SwebenchImageName("Django__Django-11039", "ns", "t1")
	if got != "ns/sweb.eval.x86_64.django_1776_django-11039:t1" {
		t.Fatalf("image = %q", got)
	}
	// SWEBENCH_ARCH=arm64 分支（含大小写不敏感）。
	t.Setenv("SWEBENCH_ARCH", "ARM64")
	got = SwebenchImageName("django__django-11039", "swebench", "latest")
	if got != "swebench/sweb.eval.arm64.django_1776_django-11039:latest" {
		t.Fatalf("arm64 image = %q", got)
	}
}

func TestSwebenchIsTransientNetworkFailure_逐模式命中(t *testing.T) {
	for _, pattern := range swebenchTransientNetworkFailurePatterns {
		if !swebenchIsTransientNetworkFailure("prefix " + pattern + " suffix") {
			t.Fatalf("pattern must hit: %q", pattern)
		}
	}
	if swebenchIsTransientNetworkFailure("all good here") {
		t.Fatal("plain output must not be transient")
	}
}

func TestSwebenchInfrastructureFailureReason_步骤序(t *testing.T) {
	// 非零退出。
	reason, err := swebenchInfrastructureFailureReason(swebenchFailureReasonParams{
		completed: tbRunResult{ExitCode: 2},
	})
	if err != nil || reason != "official harness exited with code 2" {
		t.Fatalf("exit-code reason = %q err=%v", reason, err)
	}
	// 无聚合报告。
	reason, _ = swebenchInfrastructureFailureReason(swebenchFailureReasonParams{
		completed: tbRunResult{ExitCode: 0}, report: map[string]any{},
	})
	if reason != "official aggregate report is missing" {
		t.Fatalf("missing report reason = %q", reason)
	}
	// error_ids 命中。
	reason, _ = swebenchInfrastructureFailureReason(swebenchFailureReasonParams{
		completed:      tbRunResult{ExitCode: 0},
		report:         map[string]any{"error_ids": []any{"i1"}},
		instanceReport: map[string]any{},
		instanceID:     "i1",
	})
	if reason != "official report classified the instance as an error" {
		t.Fatalf("error_ids reason = %q", reason)
	}
	// incomplete_ids 命中。
	reason, _ = swebenchInfrastructureFailureReason(swebenchFailureReasonParams{
		completed:  tbRunResult{ExitCode: 0},
		report:     map[string]any{"incomplete_ids": []any{"i1"}},
		instanceID: "i1",
	})
	if reason != "official report classified the instance as incomplete" {
		t.Fatalf("incomplete reason = %q", reason)
	}
	// empty_patch 合法放行。
	reason, _ = swebenchInfrastructureFailureReason(swebenchFailureReasonParams{
		completed:  tbRunResult{ExitCode: 0},
		report:     map[string]any{"empty_patch_ids": []any{"i1"}},
		instanceID: "i1",
	})
	if reason != "" {
		t.Fatalf("empty_patch must pass through, got %q", reason)
	}
	// 无 per-instance 报告。
	reason, _ = swebenchInfrastructureFailureReason(swebenchFailureReasonParams{
		completed:  tbRunResult{ExitCode: 0},
		report:     map[string]any{"resolved_ids": []any{}},
		instanceID: "i1",
	})
	if reason != "official per-instance report is missing" {
		t.Fatalf("missing instance report reason = %q", reason)
	}
}

func TestSwebenchInfrastructureFailureReason_配置模式表合并序(t *testing.T) {
	// 默认表命中：numpy 2.0 类环境故障。
	reason, _ := swebenchInfrastructureFailureReason(swebenchFailureReasonParams{
		completed:      tbRunResult{ExitCode: 0},
		report:         map[string]any{"resolved_ids": []any{"i1"}},
		instanceReport: map[string]any{"x": 1},
		testOutput:     "Traceback: AttributeError: `np.Inf` was removed in the NumPy 2.0 release",
	})
	if reason != "configured infrastructure failure matched: AttributeError: `np.Inf` was removed in the NumPy 2.0 release" {
		t.Fatalf("default pattern reason = %q", reason)
	}
	// 配置表在默认表之后合并命中。
	reason, _ = swebenchInfrastructureFailureReason(swebenchFailureReasonParams{
		completed:      tbRunResult{ExitCode: 0},
		report:         map[string]any{"resolved_ids": []any{"i1"}},
		instanceReport: map[string]any{"x": 1},
		testOutput:     "custom-environment-broken",
		config:         map[string]any{"infrastructure_failure_patterns": []any{"custom-environment-broken"}},
	})
	if reason != "configured infrastructure failure matched: custom-environment-broken" {
		t.Fatalf("raw pattern reason = %q", reason)
	}
	// 非 string 列表报错。
	_, err := swebenchInfrastructureFailureReason(swebenchFailureReasonParams{
		completed:      tbRunResult{ExitCode: 0},
		report:         map[string]any{"resolved_ids": []any{"i1"}},
		instanceReport: map[string]any{"x": 1},
		config:         map[string]any{"infrastructure_failure_patterns": []any{123}},
	})
	if err == nil || !strings.Contains(err.Error(), "must be a list of strings") {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestSwebenchBoundedTestOutputExcerpt_边界(t *testing.T) {
	if got := swebenchBoundedTestOutputExcerpt(" short "); got != "short" {
		t.Fatalf("short must strip+pass: %q", got)
	}
	exact := strings.Repeat("a", swebenchTestOutputExcerptChars)
	if got := swebenchBoundedTestOutputExcerpt(exact); got != exact {
		t.Fatal("exact boundary must pass unchanged")
	}
	long := strings.Repeat("a", swebenchTestOutputExcerptHeadChars) +
		strings.Repeat("b", 100) +
		strings.Repeat("c", swebenchTestOutputExcerptChars-swebenchTestOutputExcerptHeadChars)
	got := swebenchBoundedTestOutputExcerpt(long)
	omitted := len(long) - swebenchTestOutputExcerptChars
	want := strings.Repeat("a", swebenchTestOutputExcerptHeadChars) +
		"\n... " + itoa(omitted) + " characters omitted ...\n" +
		strings.Repeat("c", swebenchTestOutputExcerptChars-swebenchTestOutputExcerptHeadChars)
	if got != want {
		t.Fatalf("excerpt mismatch: %q", got)
	}
}

// itoa 简单 int→string。
func itoa(v int) string {
	return json.Number(intToString(v)).String()
}

func intToString(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func TestSwebenchBoundedInfrastructureFailureDetail_拼接与尾截(t *testing.T) {
	got := swebenchBoundedInfrastructureFailureDetail(
		tbRunResult{Stdout: "  out  \n", Stderr: "\t err \n"}, "")
	if got != "out\nerr" {
		t.Fatalf("detail = %q", got)
	}
	if swebenchBoundedInfrastructureFailureDetail(tbRunResult{}, "") != "" {
		t.Fatal("empty detail must be empty")
	}
	long := strings.Repeat("x", 3000)
	got = swebenchBoundedInfrastructureFailureDetail(tbRunResult{Stdout: long}, "")
	if len(got) != 2000 {
		t.Fatalf("tail truncation len = %d, want 2000", len(got))
	}
}

func TestSwebenchSafeID(t *testing.T) {
	if got := swebenchSafeID("Repo__Name-1"); got != "repo--name-1" {
		t.Fatalf("safe id = %q", got)
	}
	long := swebenchSafeID(strings.Repeat("a", 60))
	if len(long) != 48 {
		t.Fatalf("safe id len = %d", len(long))
	}
	if swebenchSafeID("///") != "" {
		t.Fatal("all-unsafe id must strip to empty")
	}
}

func TestSwebenchConfig_解析(t *testing.T) {
	if _, err := swebenchConfig(map[string]any{"swebench": "nope"}); err == nil {
		t.Fatal("non-dict swebench must error")
	}
	if _, err := swebenchConfig(map[string]any{}); err == nil {
		t.Fatal("missing swebench must error")
	}
	cfg, err := swebenchConfig(map[string]any{"swebench": map[string]any{"k": 1}})
	if err != nil || cfg["k"] != 1 {
		t.Fatalf("dict swebench must pass: %v %v", cfg, err)
	}
}

func TestSwebenchImageCoordinates_ach摘要tag(t *testing.T) {
	ns, tag, err := swebenchImageCoordinates(map[string]any{
		"verifier_setup_commands": []any{"pip install x"},
	})
	if err != nil || ns != "swebench" {
		t.Fatalf("ns/tag = %s/%s err=%v", ns, tag, err)
	}
	if !strings.HasPrefix(tag, "ach-") || len(tag) != len("ach-")+12 {
		t.Fatalf("digest tag = %q", tag)
	}
	// 空 setup → latest。
	_, tag, _ = swebenchImageCoordinates(map[string]any{})
	if tag != "latest" {
		t.Fatalf("default tag = %q", tag)
	}
	// 非法 setup 类型报错。
	if _, _, err := swebenchImageCoordinates(map[string]any{
		"verifier_setup_commands": "pip install x",
	}); err == nil {
		t.Fatal("non-list setup commands must error")
	}
}

func TestSwebenchConfiguredImage_不一致报错(t *testing.T) {
	expected := SwebenchImageName("i1", "swebench", "latest")
	if _, err := swebenchConfiguredImage(map[string]any{
		"docker_image": "some/other:tag",
	}, "i1"); err == nil || !strings.Contains(err.Error(), expected) {
		t.Fatalf("mismatch must error with expected image: %v", err)
	}
	got, err := swebenchConfiguredImage(map[string]any{}, "i1")
	if err != nil || got != expected {
		t.Fatalf("default image = %q err=%v", got, err)
	}
}

func TestSwebenchEnsureConfiguredImage_命中即返回(t *testing.T) {
	fake := &fakeTBRun{steps: []tbRunResultErr{
		{res: tbRunResult{ExitCode: 0}}, // image inspect 命中
	}}
	installSwebenchRunFake(t, fake)
	if err := swebenchEnsureConfiguredImage(map[string]any{}, "i1", "img:1", 60); err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("inspect hit must short-circuit, calls = %d", len(fake.calls))
	}
}

func TestSwebenchEnsureConfiguredImage_无setup直接pull(t *testing.T) {
	fake := &fakeTBRun{steps: []tbRunResultErr{
		{res: tbRunResult{ExitCode: 1}}, // inspect miss
		{res: tbRunResult{ExitCode: 0}}, // pull
	}}
	installSwebenchRunFake(t, fake)
	if err := swebenchEnsureConfiguredImage(map[string]any{}, "i1", "img:1", 60); err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 2 || !strings.Contains(callText(fake.calls[1]), "docker pull img:1") {
		t.Fatalf("pull call mismatch: %s", callText(fake.calls[1]))
	}
}

func TestSwebenchEnsureConfiguredImage_setup构建(t *testing.T) {
	fake := &fakeTBRun{steps: []tbRunResultErr{
		{res: tbRunResult{ExitCode: 1}}, // configured inspect miss
		{res: tbRunResult{ExitCode: 0}}, // base inspect hit
		{res: tbRunResult{ExitCode: 0}}, // build
	}}
	installSwebenchRunFake(t, fake)
	err := swebenchEnsureConfiguredImage(map[string]any{
		"verifier_setup_commands": []any{"pip install x"},
		"base_docker_image":       "base:1",
	}, "i1", "img:1", 60)
	if err != nil {
		t.Fatal(err)
	}
	build := fake.calls[2]
	if !strings.Contains(callText(build), "docker build --pull=false --provenance=false -t img:1 -") {
		t.Fatalf("build args mismatch: %s", callText(build))
	}
	if !strings.Contains(string(build.Input), "FROM base:1\nRUN pip install x\n") {
		t.Fatalf("dockerfile mismatch: %q", string(build.Input))
	}
}

func TestPrepareSwebenchWorkspace_调用序(t *testing.T) {
	fake := &fakeTBRun{steps: []tbRunResultErr{
		{res: tbRunResult{ExitCode: 0}}, // configured image inspect 命中
		{res: tbRunResult{ExitCode: 0}}, // create
		{res: tbRunResult{ExitCode: 0}}, // start
		{res: tbRunResult{Stdout: `[["bin/py", "/opt/x"], ["../escape", "/t"]]`, ExitCode: 0}}, // detach symlinks：第二个条目应触发不安全路径报错
	}}
	installSwebenchRunFake(t, fake)
	ws := t.TempDir()
	_, err := PrepareSwebenchWorkspace(PrepareSwebenchParams{
		Case: map[string]any{
			"case_id":  "case-1",
			"swebench": map[string]any{"instance_id": "django__django-11039"},
		},
		WorkspaceDir: ws,
		TimeoutSec:   100,
	})
	// 不安全符号链接路径必须在 detach 阶段报错（Python L147-148 守卫）。
	if err == nil || !strings.Contains(err.Error(), "unsafe SWE-bench workspace symlink path") {
		t.Fatalf("expected unsafe symlink error, got %v", err)
	}
	// finally rm -f 必须执行。
	last := fake.calls[len(fake.calls)-1]
	if !strings.Contains(callText(last), "docker rm -f ach-sweb-prepare-django--django-11039-") {
		t.Fatalf("finally rm mismatch: %s", callText(last))
	}
}

func TestPrepareSwebenchWorkspace_正常链路(t *testing.T) {
	fake := &fakeTBRun{steps: []tbRunResultErr{
		{res: tbRunResult{ExitCode: 0}},                                   // inspect
		{res: tbRunResult{ExitCode: 0}},                                   // create
		{res: tbRunResult{ExitCode: 0}},                                   // start
		{res: tbRunResult{Stdout: `[["bin/py", "/opt/x"]]`, ExitCode: 0}}, // detach symlinks
		{res: tbRunResult{ExitCode: 0}},                                   // docker cp
		{res: tbRunResult{ExitCode: 0}},                                   // restore symlink ln -s
		{res: tbRunResult{ExitCode: 0}},                                   // git config ×4
		{res: tbRunResult{ExitCode: 0}},                                   // （walk 无依赖文件，无缓存动作）
	}}
	installSwebenchRunFake(t, fake)
	ws := t.TempDir()
	image, err := PrepareSwebenchWorkspace(PrepareSwebenchParams{
		Case: map[string]any{
			"case_id":  "case-1",
			"swebench": map[string]any{"instance_id": "django__django-11039"},
		},
		WorkspaceDir: ws,
		TimeoutSec:   100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if image != "swebench/sweb.eval.x86_64.django_1776_django-11039:latest" {
		t.Fatalf("image = %q", image)
	}
	texts := make([]string, 0, len(fake.calls))
	for _, call := range fake.calls {
		texts = append(texts, callText(call))
	}
	joined := strings.Join(texts, "\n")
	// create 带 bind mount。
	if !strings.Contains(joined, "create --name ach-sweb-prepare-django--django-11039-") ||
		!strings.Contains(joined, "--mount type=bind,source="+ws+",target=/ach-workspace") ||
		!strings.Contains(joined, "sleep infinity") {
		t.Fatalf("create call mismatch:\n%s", joined)
	}
	// docker cp。
	if !strings.Contains(joined, "docker cp ach-sweb-prepare-django--django-11039-") ||
		!strings.Contains(joined, ":/testbed/.") {
		t.Fatalf("docker cp missing:\n%s", joined)
	}
	// restore symlink 容器内 ln -s。
	if !strings.Contains(joined, "ln -s -- /opt/x /ach-workspace/bin/py") {
		t.Fatalf("symlink restore missing:\n%s", joined)
	}
	// git config 四条。
	for _, kv := range []string{"core.autocrlf false", "core.eol lf", "core.safecrlf false", "core.filemode false"} {
		if !strings.Contains(joined, "git -C "+ws+" config "+kv) {
			t.Fatalf("git config %q missing:\n%s", kv, joined)
		}
	}
}

func TestPrepareSwebenchWorkspace_缺instanceID(t *testing.T) {
	installSwebenchRunFake(t, &fakeTBRun{})
	_, err := PrepareSwebenchWorkspace(PrepareSwebenchParams{
		Case:         map[string]any{"swebench": map[string]any{}},
		WorkspaceDir: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "swebench.instance_id is required") {
		t.Fatalf("expected instance_id error, got %v", err)
	}
}

func TestCollectModelPatch_失败报SWEbenchInfrastructureError(t *testing.T) {
	fake := &fakeTBRun{defRes: tbRunResultErr{res: tbRunResult{Stderr: "not a repo", ExitCode: 128}}}
	installSwebenchRunFake(t, fake)
	_, err := CollectModelPatch(t.TempDir())
	if err == nil {
		t.Fatal("nonzero exit must error")
	}
	var infra *SWEbenchInfrastructureError
	if !asInfraError(err, &infra) {
		t.Fatalf("must be SWEbenchInfrastructureError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "failed to collect SWE-bench patch: not a repo") {
		t.Fatalf("message mismatch: %v", err)
	}
}

// asInfraError errors.As 包装。
func asInfraError(err error, target **SWEbenchInfrastructureError) bool {
	for err != nil {
		if e, ok := err.(*SWEbenchInfrastructureError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestCollectModelPatch_成功(t *testing.T) {
	fake := &fakeTBRun{defRes: tbRunResultErr{res: tbRunResult{Stdout: "diff --git\n", ExitCode: 0}}}
	installSwebenchRunFake(t, fake)
	patch, err := CollectModelPatch(t.TempDir())
	if err != nil || patch != "diff --git\n" {
		t.Fatalf("patch = %q err=%v", patch, err)
	}
}

func TestSwebenchRunOfficialCommandWithRetries_重试参数校验(t *testing.T) {
	installSwebenchRunFake(t, &fakeTBRun{})
	_, _, err := swebenchRunOfficialCommandWithRetries(officialRunContext{
		config: map[string]any{"infrastructure_retry_attempts": 6},
	})
	if err == nil || !strings.Contains(err.Error(), "must be between 1 and 5") {
		t.Fatalf("attempts bound: %v", err)
	}
	_, _, err = swebenchRunOfficialCommandWithRetries(officialRunContext{
		config: map[string]any{"infrastructure_retry_delay_sec": 61},
	})
	if err == nil || !strings.Contains(err.Error(), "must be between 0 and 60") {
		t.Fatalf("delay bound: %v", err)
	}
}

func TestSwebenchRunOfficialCommandWithRetries_瞬态重试后成功(t *testing.T) {
	fake := &fakeTBRun{steps: []tbRunResultErr{
		{res: tbRunResult{Stdout: "requests.exceptions.ConnectionError boom", ExitCode: 1}},
		{res: tbRunResult{ExitCode: 0}},
	}}
	installSwebenchRunFake(t, fake)
	testOutput := t.TempDir()
	if err := os.WriteFile(filepath.Join(testOutput, "t.txt"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	completed, attempts, err := swebenchRunOfficialCommandWithRetries(officialRunContext{
		command:        []string{"echo", "hi"},
		timeoutSec:     10,
		verifierDir:    t.TempDir(),
		runID:          "r1",
		reportPath:     filepath.Join(testOutput, "r.json"),
		testOutputPath: filepath.Join(testOutput, "t.txt"),
		config:         map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if completed.ExitCode != 0 {
		t.Fatalf("exit = %d", completed.ExitCode)
	}
}

func TestSwebenchRunOfficialCommandWithRetries_非瞬态不重试(t *testing.T) {
	fake := &fakeTBRun{steps: []tbRunResultErr{
		{res: tbRunResult{Stdout: "hard failure", ExitCode: 1}},
	}}
	installSwebenchRunFake(t, fake)
	dir := t.TempDir()
	tf := filepath.Join(dir, "t.txt")
	if err := os.WriteFile(tf, []byte("boom"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, attempts, err := swebenchRunOfficialCommandWithRetries(officialRunContext{
		command: []string{"echo"}, timeoutSec: 10, verifierDir: dir, runID: "r",
		reportPath: filepath.Join(dir, "r.json"), testOutputPath: tf,
		config: map[string]any{},
	})
	if err != nil || attempts != 1 {
		t.Fatalf("attempts = %d err=%v, want 1 (no retry for non-transient)", attempts, err)
	}
}

func TestSwebenchReadJSON_ReadText_缺省语义(t *testing.T) {
	if got := swebenchReadJSON(filepath.Join(t.TempDir(), "missing.json")); len(got) != 0 {
		t.Fatalf("missing json must be empty map: %v", got)
	}
	if got := swebenchReadText(filepath.Join(t.TempDir(), "missing.txt")); got != "" {
		t.Fatalf("missing text must be empty: %q", got)
	}
	// 非 dict JSON → 空。
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "arr.json"), []byte("[1,2]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := swebenchReadJSON(filepath.Join(dir, "arr.json")); len(got) != 0 {
		t.Fatalf("non-dict json must be empty map: %v", got)
	}
}

func TestSwebenchDependencyCacheDir_绝对路径校验(t *testing.T) {
	t.Setenv(swebenchDependencyCacheRootEnv, "relative/path")
	if _, err := swebenchDependencyCacheDir(map[string]any{}); err == nil ||
		!strings.Contains(err.Error(), "must be an absolute path") {
		t.Fatalf("relative cache root must error: %v", err)
	}
	t.Setenv(swebenchDependencyCacheRootEnv, t.TempDir())
	got, err := swebenchDependencyCacheDir(map[string]any{
		"repo": "django/django", "base_commit": "abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, filepath.Join("django-django", "abc123")) {
		t.Fatalf("cache dir = %q", got)
	}
}

func TestSwebenchDownloadDependencyManifest_重试与404(t *testing.T) {
	// 404 → nil。
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	content, err := swebenchDownloadDependencyManifest(server.URL)
	if err != nil || content != nil {
		t.Fatalf("404 must return nil content: %v %v", content, err)
	}
	// 500 → 503 → 200 成功（可重试状态码重试）。
	statuses := []int{500, 503, 200}
	bodies := []string{"", "", "data"}
	calls = 0
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := calls
		calls++
		if i < len(statuses) {
			w.WriteHeader(statuses[i])
			_, _ = w.Write([]byte(bodies[i]))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte("late"))
	}))
	defer server2.Close()
	content, err = swebenchDownloadDependencyManifest(server2.URL)
	if err != nil {
		t.Fatalf("retryable statuses must eventually succeed: %v", err)
	}
	// 400 非可重试 → 立即报错。
	server3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
	}))
	defer server3.Close()
	if _, err := swebenchDownloadDependencyManifest(server3.URL); err == nil {
		t.Fatal("400 must raise immediately")
	}
}

func TestSwebenchCacheOfficialDependencyFiles_遴选与缓存(t *testing.T) {
	t.Setenv(swebenchDependencyCacheRootEnv, t.TempDir())
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "req"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"requirements.txt", "environment.yml", "random.txt", "keep.py"} {
		path := filepath.Join(ws, name)
		if name == "random.txt" {
			path = filepath.Join(ws, "req", name)
		}
		if err := os.WriteFile(path, []byte("dep==1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	target, err := swebenchCacheOfficialDependencyFiles(map[string]any{
		"repo": "django/django", "base_commit": "abc", "environment_setup_commit": "abc",
	}, ws)
	if err != nil {
		t.Fatal(err)
	}
	if target == "" {
		t.Fatal("cache target must be returned")
	}
	for _, rel := range []string{"requirements.txt", "environment.yml"} {
		if _, statErr := os.Stat(filepath.Join(target, rel)); statErr != nil {
			t.Fatalf("cached file missing: %s (%v)", rel, statErr)
		}
	}
	if _, statErr := os.Stat(filepath.Join(target, "req", "random.txt")); statErr == nil {
		t.Fatal("non-requirement .txt must be skipped")
	}
	// 二次进入 version=2 短路。
	again, err := swebenchCacheOfficialDependencyFiles(map[string]any{
		"repo": "django/django", "base_commit": "abc", "environment_setup_commit": "abc",
	}, ws)
	if err != nil || again != target {
		t.Fatalf("version=2 short circuit: %q %v", again, err)
	}
}

func TestSwebenchOfficialCommand_非Windows前缀(t *testing.T) {
	command, err := swebenchOfficialCommand(swebenchOfficialCommandParams{
		config:          map[string]any{"split": "test"},
		datasetPath:     "/tmp/ds.json",
		predictionsPath: "/tmp/p.jsonl",
		instanceID:      "i1",
		runID:           "ach_i1_abcd1234",
		timeoutSec:      1800,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command, "\x00")
	for _, want := range []string{
		"-m\x00swebench.harness.run_evaluation",
		"-d\x00/tmp/ds.json",
		"-s\x00test",
		"-i\x00i1",
		"-t\x001800",
		"--cache_level\x00instance",
		"--clean\x00false",
		"--namespace\x00swebench",
		"--instance_image_tag\x00latest",
		"env\x00PYTHONPATH=",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("command missing %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "wsl") {
		t.Fatalf("non-windows must not wrap wsl: %s", joined)
	}
	// Python [*prefix, *base_args]：prefix 在最前，第 3 位是解释器。
	if command[0] != "env" || !strings.HasPrefix(command[1], "PYTHONPATH=") || command[2] != "python" {
		t.Fatalf("prefix mismatch: %v", command[:3])
	}
}

func TestSwebenchOfficialCommand_WSL分支(t *testing.T) {
	orig := swebenchOsName
	swebenchOsName = "windows"
	t.Cleanup(func() { swebenchOsName = orig })
	fake := &fakeTBRun{defRes: tbRunResultErr{res: tbRunResult{Stdout: "/mnt/c/ds.json\n", ExitCode: 0}}}
	installSwebenchRunFake(t, fake)
	command, err := swebenchOfficialCommand(swebenchOfficialCommandParams{
		config:          map[string]any{},
		datasetPath:     `C:\data\ds.json`,
		predictionsPath: `C:\data\p.jsonl`,
		instanceID:      "i1",
		runID:           "r1",
		timeoutSec:      1800,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command, "\x00")
	if !strings.HasPrefix(joined, "wsl\x00-d\x00"+swebenchDefaultWslDistro+"\x00--") {
		t.Fatalf("wsl prefix mismatch: %s", joined)
	}
	if !strings.Contains(joined, "python3") {
		t.Fatalf("wsl python default mismatch: %s", joined)
	}
	// Windows 盘符路径直接翻译为 /mnt/c/...（不触发 wslpath）。
	if !strings.Contains(joined, "/mnt/c/data/ds.json") {
		t.Fatalf("drive translation missing: %s", joined)
	}
}

func TestSwebenchWslPath_盘符与wslpath分支(t *testing.T) {
	fake := &fakeTBRun{defRes: tbRunResultErr{res: tbRunResult{Stdout: "/mnt/x/thing\n", ExitCode: 0}}}
	installSwebenchRunFake(t, fake)
	got, err := swebenchWslPath(`D:\a\b`, "Ubuntu-24.04")
	if err != nil || got != "/mnt/d/a/b" {
		t.Fatalf("drive translation = %q err=%v", got, err)
	}
	got, err = swebenchWslPath("/usr/local/x", "Ubuntu-24.04")
	if err != nil || got != "/mnt/x/thing" {
		t.Fatalf("wslpath branch = %q err=%v", got, err)
	}
	if !strings.Contains(callText(fake.calls[0]), "wslpath -a /usr/local/x") {
		t.Fatalf("wslpath call mismatch: %s", callText(fake.calls[0]))
	}
}

func TestRunOfficialSwebenchEvaluation_空patch分支(t *testing.T) {
	installSwebenchRunFake(t, &fakeTBRun{})
	outDir := t.TempDir()
	ds := filepath.Join(t.TempDir(), "ds.json")
	if err := os.WriteFile(ds, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := RunOfficialSwebenchEvaluation(RunOfficialParams{
		Case: map[string]any{
			"case_id":  "case-1",
			"swebench": map[string]any{"instance_id": "i__x-1", "official_dataset_path": ds},
		},
		ModelPatch: "   ",
		OutputDir:  outDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["passed"] != false || result["empty_patch"] != true || result["score"] != 0.0 {
		t.Fatalf("empty patch result mismatch: %#v", result)
	}
	if result["reason"] != "official SWE-bench evaluation received an empty model patch" {
		t.Fatalf("reason mismatch: %v", result["reason"])
	}
	// 报告与日志落盘。
	if _, statErr := os.Stat(result["report_path"].(string)); statErr != nil {
		t.Fatalf("report missing: %v", statErr)
	}
	if _, statErr := os.Stat(result["stdout_path"].(string)); statErr != nil {
		t.Fatalf("stdout log missing: %v", statErr)
	}
}

func TestRunOfficialSwebenchEvaluation_数据集缺失(t *testing.T) {
	installSwebenchRunFake(t, &fakeTBRun{})
	_, err := RunOfficialSwebenchEvaluation(RunOfficialParams{
		Case: map[string]any{
			"swebench": map[string]any{
				"instance_id": "i1", "official_dataset_path": "/nonexistent/ds.json",
			},
		},
		ModelPatch: "diff",
		OutputDir:  t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "official dataset not found") {
		t.Fatalf("dataset missing must error: %v", err)
	}
}

func TestRunOfficialSwebenchEvaluation_成功resolved(t *testing.T) {
	dir := t.TempDir()
	ds := filepath.Join(dir, "ds.json")
	if err := os.WriteFile(ds, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	runIDHolder := struct{ v string }{}
	// 官方命令是第 3 次调用（inspect/pull 不走——official command 直接执行）；
	// 报告文件路径含 run_id（运行时生成），用 fake 副作用落盘。
	fake := &fakeTBRun{steps: []tbRunResultErr{
		{res: tbRunResult{Stdout: "", ExitCode: 0}}, // official harness exit 0
	}}
	installSwebenchRunFake(t, fake)
	// 用自定义 fake wrapper：执行时写入 report/instance_report/test_output。
	orig := swebenchRun
	swebenchRun = func(command []string, check bool, timeoutSec int, cwd string, inputText string) (tbRunResult, error) {
		res, err := orig(command, check, timeoutSec, cwd, inputText)
		// official harness 调用：-m swebench.harness.run_evaluation
		if isOfficialHarnessCommand(command) {
			runID := ""
			for i, arg := range command {
				if arg == "-id" {
					runID = command[i+1]
				}
			}
			runIDHolder.v = runID
			verifierDir := filepath.Join(absPath(outDir), "verifier", "swebench")
			report := map[string]any{
				"resolved_ids":    []any{"i__x-1"},
				"empty_patch_ids": []any{},
			}
			raw, _ := json.MarshalIndent(report, "", "  ")
			_ = os.WriteFile(filepath.Join(verifierDir, "openjiuwen_single_harness."+runID+".json"),
				append(raw, '\n'), 0o644)
			instanceDir := filepath.Join(verifierDir, "logs", "run_evaluation", runID,
				"openjiuwen_single_harness", "i__x-1")
			_ = os.MkdirAll(instanceDir, 0o755)
			_ = os.WriteFile(filepath.Join(instanceDir, "report.json"),
				[]byte(`{"resolved": true}`), 0o644)
			_ = os.WriteFile(filepath.Join(instanceDir, "test_output.txt"),
				[]byte("PASSED tests/test_x.py"), 0o644)
		}
		return res, err
	}
	t.Cleanup(func() { swebenchRun = orig })
	result, err := RunOfficialSwebenchEvaluation(RunOfficialParams{
		Case: map[string]any{
			"case_id":  "case-1",
			"swebench": map[string]any{"instance_id": "i__x-1", "official_dataset_path": ds},
		},
		ModelPatch: "diff --git a/x b/x\n",
		OutputDir:  outDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["passed"] != true || result["score"] != 1.0 || result["empty_patch"] != false {
		t.Fatalf("resolved result mismatch: %#v", result)
	}
	if result["official_attempts"] != 1 {
		t.Fatalf("attempts = %v", result["official_attempts"])
	}
	if result["test_output_excerpt"] != "PASSED tests/test_x.py" {
		t.Fatalf("excerpt mismatch: %v", result["test_output_excerpt"])
	}
}

func TestRunOfficialSwebenchEvaluation_未resolved(t *testing.T) {
	dir := t.TempDir()
	ds := filepath.Join(dir, "ds.json")
	if err := os.WriteFile(ds, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	fake := &fakeTBRun{steps: []tbRunResultErr{{res: tbRunResult{ExitCode: 0}}}}
	installSwebenchRunFake(t, fake)
	orig := swebenchRun
	swebenchRun = func(command []string, check bool, timeoutSec int, cwd string, inputText string) (tbRunResult, error) {
		res, err := orig(command, check, timeoutSec, cwd, inputText)
		if isOfficialHarnessCommand(command) {
			runID := ""
			for i, arg := range command {
				if arg == "-id" {
					runID = command[i+1]
				}
			}
			verifierDir := filepath.Join(absPath(outDir), "verifier", "swebench")
			raw, _ := json.MarshalIndent(map[string]any{
				"resolved_ids": []any{}, "unresolved_ids": []any{"i__x-1"},
			}, "", "  ")
			_ = os.WriteFile(filepath.Join(verifierDir, "openjiuwen_single_harness."+runID+".json"),
				append(raw, '\n'), 0o644)
			instanceDir := filepath.Join(verifierDir, "logs", "run_evaluation", runID,
				"openjiuwen_single_harness", "i__x-1")
			_ = os.MkdirAll(instanceDir, 0o755)
			_ = os.WriteFile(filepath.Join(instanceDir, "report.json"),
				[]byte(`{"resolved": false}`), 0o644)
		}
		return res, err
	}
	t.Cleanup(func() { swebenchRun = orig })
	result, err := RunOfficialSwebenchEvaluation(RunOfficialParams{
		Case: map[string]any{
			"swebench": map[string]any{"instance_id": "i__x-1", "official_dataset_path": ds},
		},
		ModelPatch: "diff",
		OutputDir:  outDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["passed"] != false || result["score"] != 0.0 {
		t.Fatalf("unresolved result mismatch: %#v", result)
	}
	if result["reason"] != "official SWE-bench evaluation did not resolve the instance" {
		t.Fatalf("reason mismatch: %v", result["reason"])
	}
}

func TestRunOfficialSwebenchEvaluation_基建失败抛SWEbenchInfrastructureError(t *testing.T) {
	dir := t.TempDir()
	ds := filepath.Join(dir, "ds.json")
	if err := os.WriteFile(ds, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	installSwebenchRunFake(t, &fakeTBRun{defRes: tbRunResultErr{res: tbRunResult{ExitCode: 1}}})
	_, err := RunOfficialSwebenchEvaluation(RunOfficialParams{
		Case: map[string]any{
			"swebench": map[string]any{"instance_id": "i1", "official_dataset_path": ds},
		},
		ModelPatch: "diff",
		OutputDir:  outDir,
	})
	var infra *SWEbenchInfrastructureError
	if !asInfraError(err, &infra) {
		t.Fatalf("must be SWEbenchInfrastructureError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "official harness exited with code 1") {
		t.Fatalf("reason mismatch: %v", err)
	}
}

func TestSwebenchVerifierBridge_已注册(t *testing.T) {
	// init 时经 RegisterSwebenchVerifier 注册真身（spec §5 保留缝）；真身行为由
	// CollectModelPatch/RunOfficialSwebenchEvaluation 各自用例覆盖，这里验证
	// 编译期接口兜底 + 注册副作用不 panic。
	_ = swebenchVerifierBridge{}
}

func TestSwebenchOfficialSupportDir_解包(t *testing.T) {
	dir := swebenchOfficialSupportDir()
	if info, err := os.Stat(filepath.Join(dir, "sitecustomize.py")); err != nil || info.IsDir() {
		t.Fatalf("sitecustomize.py must be materialized: %v", err)
	}
}

// isOfficialHarnessCommand 判断是否官方 harness 调用（非 Windows 前缀 env/PYTHONPATH/
// python + -m swebench.harness.run_evaluation）。
func isOfficialHarnessCommand(command []string) bool {
	for i, arg := range command {
		if arg == "-m" && i+1 < len(command) && command[i+1] == "swebench.harness.run_evaluation" {
			return true
		}
	}
	return false
}
