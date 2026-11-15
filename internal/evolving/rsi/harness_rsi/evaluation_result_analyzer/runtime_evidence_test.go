package evaluationresultanalyzer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRedactValue_净化分支 对照 Python _redact。
// Python: runtime_evidence.py:50-61
func TestRedactValue_净化分支(t *testing.T) {
	// ① 敏感键 → [REDACTED]
	in := map[string]any{
		"api_key":       "sk-123",
		"authorization": "Bearer abc",
		"access_token":  "tok",
		"x-token":       "tok",
		"password":      "p",
		"X-Api-Key":     "k", // re.I
		"model_config":  map[string]any{"nested": "v"},
		"prompt":        "text",
		"normal_field":  "keep",
	}
	out := redactValue(in).(map[string]any)
	for _, key := range []string{"api_key", "authorization", "access_token", "x-token", "password", "X-Api-Key", "model_config", "prompt"} {
		if out[key] != "[REDACTED]" {
			t.Fatalf("键 %s 应 REDACTED, got %v", key, out[key])
		}
	}
	if out["normal_field"] != "keep" {
		t.Fatal("普通键应保留")
	}

	// ② 文本脱敏：bearer/token= 不动前缀
	if got := redactValue("Bearer abc123 def"); got != "Bearer [REDACTED] def" {
		t.Fatalf("bearer 文本不符: %q", got)
	}
	if got := redactValue("api_key=sk-abc123"); got != "api_key=[REDACTED]" {
		t.Fatalf("api_key 文本不符: %q", got)
	}
	if got := redactValue("token: 'secret-val'"); got != "token: [REDACTED]" {
		t.Fatalf("token 引号值不符: %q", got)
	}
	if got := redactValue("key sk-abcd12345678 tail"); got != "key [REDACTED] tail" {
		t.Fatalf("sk- 前缀不符: %q", got)
	}
	if got := redactValue("plain text"); got != "plain text" {
		t.Fatalf("普通文本应保留: %q", got)
	}

	// ③ 载荷文本整条省略
	if got := redactValue(`"prompt": "big content"`); got != "[Structured prompt/model payload omitted]" {
		t.Fatalf("载荷省略不符: %q", got)
	}

	// ④ 列表截断 50 + truncated_items 标记
	list := make([]any, 55)
	for i := range list {
		list[i] = i
	}
	outList := redactValue(list).([]any)
	if len(outList) != 51 {
		t.Fatalf("应保留 50 项 + 截断标记, got %d", len(outList))
	}
	if marker, ok := outList[50].(map[string]any); !ok || marker["truncated_items"] != 5 {
		t.Fatalf("截断标记不符: %v", outList[50])
	}
}

// TestProjectValue_白名单投影 对照 Python _project。
// Python: runtime_evidence.py:64-69
func TestProjectValue_白名单投影(t *testing.T) {
	fields := map[string]bool{"keep": true, "nested": true, "list": true}
	in := map[string]any{
		"keep":   "v",
		"drop":   "x",
		"nested": map[string]any{"keep": 1, "drop": 2},
		"list":   []any{map[string]any{"keep": "a", "drop": "b"}, "plain"},
	}
	out := projectValue(in, fields).(map[string]any)
	if _, ok := out["drop"]; ok {
		t.Fatal("白名单外键应剔除")
	}
	nested := out["nested"].(map[string]any)
	if _, ok := nested["drop"]; ok {
		t.Fatal("嵌套白名单外键应剔除")
	}
	if nested["keep"] != 1 {
		t.Fatal("嵌套白名单键应保留")
	}
	list := out["list"].([]any)
	if list[0].(map[string]any)["keep"] != "a" {
		t.Fatalf("列表内投影不符: %v", list[0])
	}
	if list[1] != "plain" {
		t.Fatalf("标量经 redact 应保留: %v", list[1])
	}
}

// TestReadExecutionContext_状态机 对照 Python read_execution_context 全分支。
// Python: runtime_evidence.py:80-126
func TestReadExecutionContext_状态机(t *testing.T) {
	dir := t.TempDir()

	// ① 可用文件：白名单投影 + available
	if err := os.WriteFile(filepath.Join(dir, "runtime_config.json"),
		[]byte(`{"max_iterations": 5, "secret_key": "v", "other": 1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "termination.json"),
		[]byte(`{"termination_reason": "done", "secret": "x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ReadExecutionContext(dir, map[string]any{}, "recorded-reason")
	rc := got["runtime_config"].(map[string]any)
	if rc["status"] != "available" {
		t.Fatalf("runtime_config 应 available: %v", rc)
	}
	facts := rc["facts"].(map[string]any)
	if facts["max_iterations"] != float64(5) {
		t.Fatalf("白名单字段应保留: %v", facts)
	}
	if _, ok := facts["secret_key"]; ok {
		t.Fatal("白名单外键应剔除")
	}
	if _, ok := facts["other"]; ok {
		t.Fatal("白名单外键 other 应剔除")
	}
	if rc["source"] != "runtime_config.json" {
		t.Fatalf("provenance 应为相对路径, got %v", rc["source"])
	}
	// termination 记录回填仅在缺 termination_reason 时生效；此例已有值不覆盖
	et := got["execution_termination"].(map[string]any)
	if et["facts"].(map[string]any)["termination_reason"] != "done" {
		t.Fatalf("termination_reason 不符: %v", et)
	}

	// ② 记录回填：termination 缺字段时补 recorded 值 + partial 状态 + provenance 后缀
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, "termination.json"), []byte(`{"status": "ok"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got2 := ReadExecutionContext(dir2, map[string]any{}, "recorded-reason")
	et2 := got2["execution_termination"].(map[string]any)
	// Python: status = "partial" if status != "available" else status（投影成功时保持 available）
	if et2["status"] != "available" {
		t.Fatalf("投影成功应保持 available, got %v", et2["status"])
	}
	if et2["facts"].(map[string]any)["termination_reason"] != "recorded-reason" {
		t.Fatalf("记录回填不符: %v", et2["facts"])
	}
	if !strings.HasSuffix(et2["source"].(string), "; termination_reason from recorded case response/metadata") {
		t.Fatalf("provenance 后缀不符: %v", et2["source"])
	}

	// ③ 内联 dict（无 path/file 键）→ 投影 available
	dir3 := t.TempDir()
	got3 := ReadExecutionContext(dir3, map[string]any{
		"runtime_config": map[string]any{"max_iterations": 3, "junk": 1},
	}, "")
	rc3 := got3["runtime_config"].(map[string]any)
	if rc3["status"] != "available" || rc3["facts"].(map[string]any)["max_iterations"] != 3 {
		t.Fatalf("内联投影不符: %v", rc3)
	}

	// ④ 内联 dict 带 path 键但文件缺失 → unknown（不投影）
	dir4 := t.TempDir()
	got4 := ReadExecutionContext(dir4, map[string]any{
		"runtime_config": map[string]any{"path": "missing.json", "max_iterations": 3},
	}, "")
	if got4["runtime_config"].(map[string]any)["status"] != "unknown" {
		t.Fatalf("带 path 的内联不应投影: %v", got4["runtime_config"])
	}

	// ⑤ 非 dict JSON → invalid
	dir5 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir5, "runtime_config.json"), []byte(`[1,2]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got5 := ReadExecutionContext(dir5, map[string]any{}, ""); got5["runtime_config"].(map[string]any)["status"] != "invalid" {
		t.Fatalf("非 dict JSON 应 invalid: %v", got5["runtime_config"])
	}

	// ⑥ 超大文件 → oversized
	dir6 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir6, "runtime_config.json"), []byte(strings.Repeat("x", 70000)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got6 := ReadExecutionContext(dir6, map[string]any{}, ""); got6["runtime_config"].(map[string]any)["status"] != "oversized" {
		t.Fatalf("超大文件应 oversized: %v", got6["runtime_config"])
	}

	// ⑦ 逃逸路径 → outside_case_rejected
	dir7 := t.TempDir()
	got7 := ReadExecutionContext(dir7, map[string]any{
		"runtime_config": map[string]any{"path": "../outside.json"},
	}, "")
	rc7 := got7["runtime_config"].(map[string]any)
	if rc7["source"] != "outside_case_rejected" || rc7["status"] != "unknown" {
		t.Fatalf("逃逸路径应拒绝: %v", rc7)
	}
}

// TestPrepareRuntimeEvidence_清单与日志拷贝 对照 Python prepare_runtime_evidence 主流程。
// Python: runtime_evidence.py:129-193
func TestPrepareRuntimeEvidence_清单与日志拷贝(t *testing.T) {
	caseDir := t.TempDir()
	runtimeDir := t.TempDir()

	// 准备案例本地日志
	logsDir := filepath.Join(caseDir, "logs")
	if err := os.Mkdir(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "inv.jsonl"), []byte(
		`{"agent_id":"a1","role":"planner","level":"ERROR","message":"Traceback in step"}
{"level":"INFO","message":"normal"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "runtime_config.json"),
		[]byte(`{"max_iterations": 4}`), 0o644); err != nil {
		t.Fatal(err)
	}

	manifest, evErr := PrepareRuntimeEvidence(caseDir, map[string]any{}, runtimeDir, "")
	if evErr != nil {
		t.Fatal(evErr)
	}

	// ① manifest 结构
	if manifest["scope"] == nil || manifest["copy_policy"] == nil {
		t.Fatal("manifest 头部字段缺失")
	}
	structured := manifest["structured"].(map[string]any)
	rc := structured["runtime_config"].(map[string]any)
	if rc["status"] != "available" || rc["path"] != "runtime_evidence/runtime_config.json" {
		t.Fatalf("structured 登记不符: %v", rc)
	}
	// logs：rglob 到 1 个文件
	logs := manifest["logs"].([]any)
	if len(logs) != 1 {
		t.Fatalf("应登记 1 个日志, got %d", len(logs))
	}
	row := logs[0].(map[string]any)
	if row["status"] != "available" || row["records_copied"] != 2 {
		t.Fatalf("日志拷贝不符: %v", row)
	}
	if row["source_line"] != nil {
		// source_line 在 record 内，row 上没有——确认 row 有 path
	}
	if row["path"] != "runtime_evidence/invocation_001.jsonl" {
		t.Fatalf("拷贝路径不符: %v", row["path"])
	}
	if manifest["log_files_omitted"] != 0 {
		t.Fatalf("无省略应 0, got %v", manifest["log_files_omitted"])
	}
	// termination.json 缺失 → Python 同样登记 missing
	if missing := manifest["missing"].([]any); len(missing) != 1 || missing[0] != "execution_termination" {
		t.Fatalf("termination 缺失应登记, got %v", missing)
	}

	// ② 拷贝产物：净化后记录可解析，身份被提取
	copied, err := os.ReadFile(filepath.Join(runtimeDir, "runtime_evidence", "invocation_001.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(copied)), "\n")
	if len(lines) != 2 {
		t.Fatalf("应拷贝 2 条记录, got %d", len(lines))
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["source_line"] != float64(1) || rec["source_byte_offset"] != float64(0) {
		t.Fatalf("记录元数据不符: %v", rec)
	}
	event := rec["event"].(map[string]any)
	if event["agent_id"] != "a1" {
		t.Fatalf("事件字段不符: %v", event)
	}
	// initial_search：ERROR + Traceback 命中，INFO 无关键词被滤
	search := manifest["initial_search"].(map[string]any)
	if search["hits"] != 1 {
		t.Fatalf("initial_search 应 1 命中, got %v", search)
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, "runtime_log_search.json")); err != nil {
		t.Fatal("runtime_log_search.json 应落盘")
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, "evidence_manifest.json")); err != nil {
		t.Fatal("evidence_manifest.json 应落盘")
	}
}

// TestPrepareRuntimeEvidence_声明路径与越界 对照 Python 声明式 runtime_logs 分支。
// Python: runtime_evidence.py:152-167, 170-174
func TestPrepareRuntimeEvidence_声明路径与越界(t *testing.T) {
	caseDir := t.TempDir()
	runtimeDir := t.TempDir()

	// 声明一个案例内日志 + 一个越界路径 + 一个目录
	logsDir := filepath.Join(caseDir, "sub", "inner")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "a.jsonl"), []byte(`{"level":"ERROR","message":"Traceback"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// 越界文件（案例外）
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "b.jsonl"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	manifest, evErr := PrepareRuntimeEvidence(caseDir, map[string]any{
		"runtime_logs": map[string]any{
			"paths": []any{"sub/inner", outside},
		},
	}, runtimeDir, "")
	if evErr != nil {
		t.Fatal(evErr)
	}

	logs := manifest["logs"].([]any)
	// 声明 2 项：目录展开为 1 文件 + 越界 1 项（保留行但状态 unknown）
	if len(logs) != 2 {
		t.Fatalf("应登记 2 行, got %d: %v", len(logs), manifest["logs"])
	}
	first := logs[0].(map[string]any)
	if first["status"] != "available" {
		t.Fatalf("目录展开的日志应可用: %v", first)
	}
	second := logs[1].(map[string]any)
	// Python: source=None → row["source"] = "outside_case_rejected"
	if second["source"] != "outside_case_rejected" || second["status"] != "unknown" {
		t.Fatalf("越界路径应标签化且 unknown: %v", second)
	}

	// 无日志场景 → missing 登记 case_scoped_runtime_logs
	caseDir2 := t.TempDir()
	runtimeDir2 := t.TempDir()
	manifest2, evErr2 := PrepareRuntimeEvidence(caseDir2, map[string]any{}, runtimeDir2, "")
	if evErr2 != nil {
		t.Fatal(evErr2)
	}
	if logs2 := manifest2["logs"].([]any); len(logs2) == 0 {
		missing := manifest2["missing"].([]any)
		found := false
		for _, m := range missing {
			if m == "case_scoped_runtime_logs" {
				found = true
			}
		}
		if !found {
			t.Fatalf("无日志应登记缺失, got %v", missing)
		}
	}
}

// TestCopyLog_超预算尾先头后 对照 Python _copy_log 窗口策略。
// Python: runtime_evidence.py:197-293
func TestCopyLog_超预算尾先头后(t *testing.T) {
	dir := t.TempDir()
	// 构造 3 行日志，每行 ~100 字节；预算 250（头 125 尾 125）
	var sb strings.Builder
	for i := 1; i <= 3; i++ {
		sb.WriteString(fmt.Sprintf("{\"n\": %d, \"pad\": \"%s\"}\n", i, strings.Repeat("p", 80)))
	}
	source := filepath.Join(dir, "big.jsonl")
	if err := os.WriteFile(source, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	sourceSize := len(sb.String())
	target := filepath.Join(dir, "out.jsonl")
	budget := 250

	result, err := copyLog(source, target, budget, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if result["source_bytes"] != sourceSize {
		t.Fatalf("source_bytes 不符: %v", result["source_bytes"])
	}
	if result["truncated"] != true || result["scan_complete"] != false {
		t.Fatalf("超预算应 truncated: %v", result)
	}
	ranges := result["source_ranges"].([]map[string]any)
	if len(ranges) != 2 {
		t.Fatalf("应有 2 个窗口, got %d", len(ranges))
	}
	// 尾窗口在前：byte_start = size - tail
	tailStart := sourceSize - (budget - budget/2)
	if ranges[0]["byte_start"] != tailStart {
		t.Fatalf("尾窗口应先出现, got %v want %d", ranges[0]["byte_start"], tailStart)
	}
	if ranges[1]["byte_start"] != 0 {
		t.Fatalf("头窗口其次, got %v", ranges[1]["byte_start"])
	}

	// 尾窗口行不对齐时跳过对齐字节（行尾从 123 行中段开始）
	// alignment_bytes_omitted 记录在首窗口
	if ranges[0]["alignment_bytes_omitted"] == nil {
		t.Fatal("对齐字节应记录")
	}

	// 空文件：available 且 scan_complete
	emptySrc := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(emptySrc, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	res2, err := copyLog(emptySrc, filepath.Join(dir, "out2.jsonl"), 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	if res2["status"] != "available" || res2["scan_complete"] != true {
		t.Fatalf("空文件应 available+complete: %v", res2)
	}
}
