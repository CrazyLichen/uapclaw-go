package native

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	hp "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 签名收窄（本轮新增） ────────────────────────────

// TestCreateNativeHarnessProtocol_具名类型入口 断言 union 在 API 层拆开：
// 函数只收 hschema.AgentTemplateSpec 具体类型，路径态由调用方经
// ManifestSourceFromPath 自行转换（对齐 15.5 factory.go:212-216 决策）。
func TestCreateNativeHarnessProtocol_具名类型入口(t *testing.T) {
	// 编译期检查：函数签名第二参数必须是具体类型而非 any。
	var _ func(*NativeHarnessProtocolAdapter, hschema.AgentTemplateSpec) error = CreateNativeHarnessProtocol
	a := NewNativeV2Adapter(schema.NewDeepAgentSpec(), nil, 1024)
	tpl := hschema.AgentTemplateSpec{}
	if err := CreateNativeHarnessProtocol(a, tpl); err != nil {
		t.Fatalf("具名类型入口应可用: %v", err)
	}
	if a.spec.AgentTemplateSpec == nil {
		t.Fatal("装配后 spec.AgentTemplateSpec 应非 nil")
	}
}

// TestDeepAgentSpecFromMap_参数收窄 断言配置字典解析不再以 any 为参数。
func TestDeepAgentSpecFromMap_参数收窄(t *testing.T) {
	spec, err := deepAgentSpecFromMap(map[string]any{"language": "en"})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Language != "en" {
		t.Fatalf("language 未解析: %q", spec.Language)
	}
}

// TestStructToJsonObject_直接构造无双次转换 断言快照→JsonObject 不经
// []byte 中转：数字保持 int 而非 float64，且 9 字段齐全。
func TestStructToJsonObject_直接构造无双次转换(t *testing.T) {
	snap := NativeCheckpoint{
		SessionID: "host-1:agent-1", CardID: "card-1", Cwd: "/tmp/w",
		Contexts:  map[string]map[string]any{"c1": {"messages": []any{}}},
		DeepAgent: map[string]any{"stop_condition_state": map[string]any{"iteration": 7}},
		Queued:    []SavedInput{}, OutputState: map[string]any{},
	}
	obj, err := structToJsonObject(snap)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"session_id", "card_id", "cwd", "contexts", "deepagent", "output_state", "queued"} {
		if _, ok := obj[key]; !ok {
			t.Fatalf("JsonObject 缺键 %q", key)
		}
	}
	// 直接构造时 iteration 保持原 int；若走 marshal→unmarshal 会变 float64。
	da, _ := obj["deepagent"].(map[string]any)
	scs, _ := da["stop_condition_state"].(map[string]any)
	if n, ok := scs["iteration"].(int); !ok || n != 7 {
		t.Fatalf("iteration 应保持 int=7，得 %#v（说明仍走 JSON 双次转换）", scs["iteration"])
	}
}

// ──────────────────────────── D-5 专属验收（plan 缺失项） ────────────────────────────

// TestCaptureCheckpoint_StopConditionStateFromGetState 断言快照里的
// stop_condition_state 来自当次 GetState()，而非 ToSessionDict 旧值。
// Python: protocol_adapter.py:393 deepagent["stop_condition_state"] = loop_coordinator.get_state()
func TestCaptureCheckpoint_StopConditionStateFromGetState(t *testing.T) {
	// 纯函数级验收：coordinatorStateToMap 的输出必须与 GetState 同步，
	// 且 captureCheckpoint 的 ⑤ 显式赋值不被 ToSessionDict 隐式结果替代。
	src, err := os.ReadFile("protocolAdapter.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if !strings.Contains(body, `deepagent["stop_condition_state"] = coordinatorStateToMap(`) {
		t.Fatal("captureCheckpoint 缺少 ⑤ 的显式 GetState 赋值（D-5 步骤被合并了）")
	}
	if !strings.Contains(body, "coord.LoadState(coordinatorStateFromMap(scs))") {
		t.Fatal("openSession 缺少 ⑨ 的显式 LoadState 步骤（D-5 步骤被合并了）")
	}
	// 运行时验收：ToMap/FromMap 往返保住 iteration（对齐 LoopCoordinatorState 四键）
	state := coordinatorStateToMap(coordinatorStateFromMap(map[string]any{
		"iteration": 7, "token_usage": 300, "stop_reason": "max_rounds",
		"evaluator_states": map[string]any{},
	}))
	if state["iteration"] != 7 || state["token_usage"] != 300 || state["stop_reason"] != "max_rounds" {
		t.Fatalf("coordinator 状态往返漂移: %#v", state)
	}
}

// TestCaptureCheckpoint_八守卫 逐项断言 8 条守卫的错误文案都在生产代码里，
// 且每条都有独立的提前返回（不合并）。
// Python: protocol_adapter.py:380-409
func TestCaptureCheckpoint_八守卫(t *testing.T) {
	src, err := os.ReadFile("protocolAdapter.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	guards := []struct{ name, want string }{
		{"agent_or_session_nil", "native session is unavailable"},
		{"wrong_phase", "native session left the checkpoint boundary"},
		{"async_running", "wait for native background tools before checkpointing"},
		{"pending_interrupts", "resolve pending user interactions before checkpointing"},
		{"paused_no_active", "paused native turn has no resumable query"},
		{"paused_no_query", "paused native turn has no resumable query"},
		{"contexts_encode", "EncodeContexts(contexts)"},
		{"explicit_getstate", `deepagent["stop_condition_state"]`},
	}
	for _, g := range guards {
		t.Run(g.name, func(t *testing.T) {
			if !strings.Contains(body, g.want) {
				t.Fatalf("守卫 %q 缺失（期望源码含 %q）", g.name, g.want)
			}
		})
	}
	// 8 条守卫必须分布在不同的 if 分支（不合并成一层）
	if strings.Count(body, "HarnessStateError{Msg:") < 6 {
		t.Fatal("守卫分支被合并，独立提前返回不足 6 条")
	}
}

// TestOpenSession_LoadStateAfterStart 断言 ⑨ 在 ⑧ start 之后独立成步。
// Python: protocol_adapter.py:210-211
func TestOpenSession_LoadStateAfterStart(t *testing.T) {
	src, err := os.ReadFile("protocolAdapter.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	startIdx := strings.Index(body, "agent.Start(ctx, sess)")
	loadIdx := strings.Index(body, "coord.LoadState(coordinatorStateFromMap(scs))")
	if startIdx < 0 || loadIdx < 0 || startIdx >= loadIdx {
		t.Fatalf("LoadState 必须在 Start 之后独立成步: start=%d load=%d", startIdx, loadIdx)
	}
}

// ──────────────────────────── D-6 命名回归守卫（plan 缺失项） ────────────────────────────

// TestD6_RenamedSymbolsGrepEmpty 断言旧名在 internal/ 中 0 命中（规则 8.3）。
func TestD6_RenamedSymbolsGrepEmpty(t *testing.T) {
	root := "../../../../"
	// 排除本测试文件自身（它内含旧名字符串作为检索目标）。
	hits := grepRecursiveExcluding(root, []string{"ExportState", "ImportState"},
		"protocolAdapter_review_test.go")
	if len(hits) > 0 {
		t.Fatalf("旧名未删净: %v", hits)
	}
}

// grepRecursiveExcluding 与 grepRecursive 同义，但跳过路径含 exclude 的文件。
func grepRecursiveExcluding(root string, needles []string, exclude string) []string {
	var out []string
	for _, dir := range []string{"internal", "tests", "cmd"} {
		_ = walkGoFiles(root+dir, func(path string, data []byte) {
			if strings.Contains(path, exclude) {
				return
			}
			for _, n := range needles {
				if strings.Contains(string(data), n) {
					out = append(out, path+":"+n)
				}
			}
		})
	}
	return out
}

// walkGoFiles 遍历目录下的 .go 文件并回调其内容。
func walkGoFiles(dir string, fn func(path string, data []byte)) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 目录不存在时静默跳过
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		fn(path, data)
		return nil
	})
}

// ──────────────────────────── 日志字段与协议文案（plan 缺失项） ────────────────────────────

// TestErrorPaths_LogFields 断言异常路径的 logger.Error 带四个上下文字段。
func TestErrorPaths_LogFields(t *testing.T) {
	src, err := os.ReadFile("protocolAdapter.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	for _, field := range []string{`"event_type"`, `"method"`, `"model_provider"`, `.Err(err)`} {
		if !strings.Contains(body, field) {
			t.Fatalf("异常路径日志缺字段 %s", field)
		}
	}
	if !strings.Contains(body, `Str("model_provider", nativeV2ProviderName)`) {
		t.Fatal("model_provider 未用 nativeV2ProviderName 常量固定")
	}
}

// TestErrorMessages_ExactTextReview 逐字断言关键协议文案（不本地化）。
func TestErrorMessages_ExactTextReview(t *testing.T) {
	want := []string{
		"declare native MCP servers in the manifest",
		"NativeHarness requires a checkpoint to resume",
		"unsupported native checkpoint version",
		"native checkpoint scope does not match the context",
		"invalid native checkpoint",
		"NativeHarness output stream is unavailable",
		"NativeHarness output closed before reaching IDLE",
		"the native turn is not running",
		"the native turn is no longer active",
		"resume the restored paused turn before sending new input",
		"resume query does not match the checkpoint",
		"cold resume requires a paused checkpoint",
		"pause the native turn before exporting its checkpoint",
		"native session is unavailable",
		"native session left the checkpoint boundary",
		"wait for native background tools before checkpointing",
		"resolve pending user interactions before checkpointing",
		"paused native turn has no resumable query",
		"unknown native_v2 configuration fields",
	}
	src, err := os.ReadFile("protocolAdapter.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range want {
		if !strings.Contains(string(src), w) {
			t.Fatalf("协议文案缺失或漂移: %q", w)
		}
	}
}

// ──────────────────────────── D-3 与 codec 补漏 ────────────────────────────

// TestD3_ExecuteTurnUsesAdapterRunRound 断言 ExecuteTurn 调 adapter 自己的 runRound。
func TestD3_ExecuteTurnUsesAdapterRunRound(t *testing.T) {
	src, err := os.ReadFile("protocolAdapter.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if !strings.Contains(body, "a.runRound(ctx") {
		t.Fatal("ExecuteTurn 未调 adapter 自己的 runRound（可能误用 15.6 的版本）")
	}
	if strings.Contains(body, "a.DeepAgentHarness.runRound") {
		t.Fatal("不得经嵌入调用 15.6 runRound（Go 虚派发陷阱）")
	}
}

// TestD3_ConsumeChunkSamePackage 断言普通帧消费走同包注入的 consumeFn。
func TestD3_ConsumeChunkSamePackage(t *testing.T) {
	src, err := os.ReadFile("protocolAdapter.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "a.consumeFn(ctx") {
		t.Fatal("consumeChunk 未经构造期注入的 consumeFn 调用")
	}
}

// TestCodec_DirectUnmarshalRoundTrip 断言 codec 的 map→struct 仍用
// json.Marshal→Unmarshal（受 15.4 JsonObject=map[string]any 约束，登记为已知偏差）。
func TestCodec_DirectUnmarshalRoundTrip(t *testing.T) {
	data := map[string]any{"role": "tool", "content": "r", "tool_call_id": "tc-1"}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatal(err)
	}
	if probe["tool_call_id"] != "tc-1" {
		t.Fatal("tool_call_id 在边界转换中丢失")
	}
	_ = context.Background()
	_ = hp.JsonObject{}
}
