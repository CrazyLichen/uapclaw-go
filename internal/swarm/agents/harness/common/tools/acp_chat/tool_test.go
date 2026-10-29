package acpchat

import (
	"context"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// TestLoadProfile_无acpAgents配置 验证缺失映射报错。
func TestLoadProfile_无acpAgents配置(t *testing.T) {
	_, err := loadProfile(map[string]any{}, "x")
	if err == nil || !strings.Contains(err.Error(), "config has no acp_agents mapping") {
		t.Errorf("err = %v", err)
	}
	// nil configBase 空配置基线
	_, err = loadProfile(nil, "x")
	if err == nil || !strings.Contains(err.Error(), "config has no acp_agents mapping") {
		t.Errorf("nil err = %v", err)
	}
	// 非 dict
	_, err = loadProfile(map[string]any{"acp_agents": "bad"}, "x")
	if err == nil || !strings.Contains(err.Error(), "config has no acp_agents mapping") {
		t.Errorf("非 dict err = %v", err)
	}
}

// TestLoadProfile_未知profile 验证未知 profile 报错。
func TestLoadProfile_未知profile(t *testing.T) {
	_, err := loadProfile(map[string]any{"acp_agents": map[string]any{}}, "nope")
	if err == nil || !strings.Contains(err.Error(), "unknown acp_agents profile") {
		t.Errorf("err = %v", err)
	}
}

// TestLoadProfile_成功 验证 trim 键名与规格提取。
func TestLoadProfile_成功(t *testing.T) {
	spec, err := loadProfile(map[string]any{
		"acp_agents": map[string]any{
			"codex": map[string]any{"command": "codex"},
		},
	}, " codex ")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if spec["command"] != "codex" {
		t.Errorf("spec = %v", spec)
	}
}

// TestBuildClientSpec_校验链 验证 command/args/env/cwd 解析。
func TestBuildClientSpec_校验链(t *testing.T) {
	// command 空
	_, err := buildClientSpec(map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "non-empty command") {
		t.Errorf("command 空 err = %v", err)
	}
	// command 非字符串
	_, err = buildClientSpec(map[string]any{"command": 123})
	if err == nil || !strings.Contains(err.Error(), "non-empty command") {
		t.Errorf("command 非字符串 err = %v", err)
	}
	// args 非列表
	_, err = buildClientSpec(map[string]any{"command": "x", "args": "not-list"})
	if err == nil || !strings.Contains(err.Error(), "must be a list") {
		t.Errorf("args err = %v", err)
	}
	// args 合法（元素经 str() 转换）
	cs, err := buildClientSpec(map[string]any{
		"command": "x", "args": []any{"--flag", 42},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(cs.args) != 2 || cs.args[1] != "42" {
		t.Errorf("args = %v", cs.args)
	}
	// env 非映射
	_, err = buildClientSpec(map[string]any{"command": "x", "env": "bad"})
	if err == nil || !strings.Contains(err.Error(), "must be a map") {
		t.Errorf("env err = %v", err)
	}
	// cwd 空白归空
	cs, err = buildClientSpec(map[string]any{"command": "x", "cwd": "   "})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if cs.cwd != "" {
		t.Errorf("cwd = %q", cs.cwd)
	}
	// cwd 非字符串归空
	cs, err = buildClientSpec(map[string]any{"command": "x", "cwd": 9})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if cs.cwd != "" {
		t.Errorf("cwd 非字符串 = %q", cs.cwd)
	}
	// env 合法带入
	cs, err = buildClientSpec(map[string]any{
		"command": "x", "env": map[string]any{"K": "V"},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if cs.env["K"] != "V" {
		t.Errorf("env = %v", cs.env)
	}
}

// TestGetOrCreateClient_真实脚本 验证创建/复用/new_session 重启。
func TestGetOrCreateClient_真实脚本(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("testdata shell 脚本不支持 Windows")
	}
	resetClientsForTest()
	defer resetClientsForTest()
	cfg := map[string]any{
		"acp_agents": map[string]any{
			"peer": map[string]any{
				// 相对路径（go test cwd=包目录）：
				// acp_chat →(5 层 ..)→ internal/swarm → acp/testdata/acp_peer.sh
				"command": "../../../../../acp/testdata/acp_peer.sh",
			},
		},
	}
	ctx := context.Background()
	c1, err := getOrCreateClient(ctx, cfg, "peer", false)
	if err != nil {
		t.Fatalf("首次创建: %v", err)
	}
	// 复用
	c2, err := getOrCreateClient(ctx, cfg, "peer", false)
	if err != nil {
		t.Fatalf("复用: %v", err)
	}
	if c1 != c2 {
		t.Error("同 profile 应复用 client")
	}
	// new_session 重启
	c3, err := getOrCreateClient(ctx, cfg, "peer", true)
	if err != nil {
		t.Fatalf("重启: %v", err)
	}
	if c3 == c1 {
		t.Error("new_session 应产生新 client")
	}
	_ = c3.Close()
}

// TestGetOrCreateClient_未知profile报错 验证错误传播。
func TestGetOrCreateClient_未知profile报错(t *testing.T) {
	resetClientsForTest()
	defer resetClientsForTest()
	_, err := getOrCreateClient(context.Background(), map[string]any{}, "nope", false)
	if err == nil || !strings.Contains(err.Error(), "acp_agents") {
		t.Errorf("err = %v", err)
	}
}

// TestGetOrCreateClient_空command报错 验证规格校验。
func TestGetOrCreateClient_空command报错(t *testing.T) {
	resetClientsForTest()
	defer resetClientsForTest()
	cfg := map[string]any{
		"acp_agents": map[string]any{
			"bad": map[string]any{"args": []any{}},
		},
	}
	_, err := getOrCreateClient(context.Background(), cfg, "bad", false)
	if err == nil || !strings.Contains(err.Error(), "non-empty command") {
		t.Errorf("err = %v", err)
	}
}

// TestAcpChatTool_Invoke空白校验 验证 agent/message 必填。
func TestAcpChatTool_Invoke空白校验(t *testing.T) {
	resetClientsForTest()
	defer resetClientsForTest()
	tk := NewAcpChatTool(map[string]any{})
	res, err := tk.invoke(context.Background(), map[string]any{"agent": "", "message": "x"})
	if err != nil {
		t.Fatalf("不应抛错: %v", err)
	}
	if res["result"] != "[ERROR] agent (profile name) is required." {
		t.Errorf("res = %v", res)
	}
	res, _ = tk.invoke(context.Background(), map[string]any{"agent": "a", "message": "  "})
	if res["result"] != "[ERROR] message is empty." {
		t.Errorf("res = %v", res)
	}
}

// TestAcpChatTool_Invoke错误转文本 验证异常兜底文案。
func TestAcpChatTool_Invoke错误转文本(t *testing.T) {
	resetClientsForTest()
	defer resetClientsForTest()
	// 未知 profile → [ERROR] acp_chat failed: ...
	tk := NewAcpChatTool(map[string]any{"acp_agents": map[string]any{}})
	res, err := tk.invoke(context.Background(), map[string]any{"agent": "nope", "message": "hi"})
	if err != nil {
		t.Fatalf("不应抛错: %v", err)
	}
	s, _ := res["result"].(string)
	if !strings.HasPrefix(s, "[ERROR] acp_chat failed:") {
		t.Errorf("res = %v", res)
	}
}

// TestAcpChatTool_Invoke真实对话 验证完整链路文本返回。
func TestAcpChatTool_Invoke真实对话(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("testdata shell 脚本不支持 Windows")
	}
	resetClientsForTest()
	defer resetClientsForTest()
	cfg := map[string]any{
		"acp_agents": map[string]any{
			"peer": map[string]any{
				"command": "../../../../../acp/testdata/acp_peer.sh",
			},
		},
	}
	tk := NewAcpChatTool(cfg)
	res, err := tk.invoke(context.Background(), map[string]any{"agent": "peer", "message": "hi"})
	if err != nil {
		t.Fatalf("不应抛错: %v", err)
	}
	if res["result"] != "peer-reply" {
		t.Errorf("res = %v", res)
	}
}

// TestAcpChatTool_Invoke空响应 验证空响应兜底文案。
func TestAcpChatTool_Invoke空响应(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("testdata shell 脚本不支持 Windows")
	}
	resetClientsForTest()
	defer resetClientsForTest()
	// session/prompt 回空 result（无 update 文本 → resultTextFallback("")）
	dir := t.TempDir()
	script := dir + "/empty_reply_peer.sh"
	body := `#!/bin/sh
while IFS= read -r line; do
    method=$(printf '%s' "$line" | sed -n 's/.*"method":"\([^"]*\)".*/\1/p')
    rid=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
    case "$method" in
        initialize) printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1}}\n' "$rid" ;;
        session/new) printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"e-sess"}}\n' "$rid" ;;
        *) printf '{"jsonrpc":"2.0","id":%s,"result":null}\n' "$rid" ;;
    esac
done
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{
		"acp_agents": map[string]any{
			"empty": map[string]any{"command": script},
		},
	}
	tk := NewAcpChatTool(cfg)
	res, err := tk.invoke(context.Background(), map[string]any{"agent": "empty", "message": "hi"})
	if err != nil {
		t.Fatalf("不应抛错: %v", err)
	}
	if res["result"] != "(empty response from external ACP agent)" {
		t.Errorf("res = %v", res)
	}
}

// TestProfileLock并发互斥 验证同 profile 串行。
// 契约：acquireProfileLock 返回锁对象 → 手动 Lock → releaseProfileLock 解锁。
func TestProfileLock并发互斥(t *testing.T) {
	var order []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l := acquireProfileLock("concurrent-test")
			l.mu.Lock()
			defer releaseProfileLock("concurrent-test", l)
			mu.Lock()
			order = append(order, "in")
			mu.Unlock()
			mu.Lock()
			order = append(order, "out")
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(order) != 10 {
		t.Fatalf("order = %v", order)
	}
	// in/out 交替（互斥生效：每个 in 之后必须紧跟 out 才允许下一个 in）
	for i, s := range order {
		want := "in"
		if i%2 == 1 {
			want = "out"
		}
		if s != want {
			t.Fatalf("order = %v（位置 %d 应为 %s）", order, i, want)
		}
	}
}

// TestGetTools_卡与参数 验证工具卡构造。
func TestGetTools_卡与参数(t *testing.T) {
	tk := NewAcpChatTool(map[string]any{})
	tools := tk.GetTools()
	if len(tools) != 1 {
		t.Fatalf("应 1 个工具，实际 %d", len(tools))
	}
	if tools[0].Card().Name != "acp_chat" {
		t.Errorf("name = %s", tools[0].Card().Name)
	}
	if !strings.Contains(tools[0].Card().Description, "external ACP-compatible agent subprocess") {
		t.Errorf("description = %s", tools[0].Card().Description)
	}
	params := tools[0].Card().InputParams
	if len(params) != 3 {
		t.Fatalf("应 3 参数，实际 %d", len(params))
	}
	if params[0].Name != "agent" || !params[0].Required {
		t.Errorf("params[0] = %+v", params[0])
	}
	if params[2].Name != "new_session" {
		t.Errorf("params[2] = %+v", params[2])
	}
}
