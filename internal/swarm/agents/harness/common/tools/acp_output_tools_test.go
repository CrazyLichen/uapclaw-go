package tools

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// newAcpTestManager 构造独立 pending 的测试管理器。
func newAcpTestManager() *AcpOutputManager {
	m := &AcpOutputManager{
		pending: make(map[string]*AcpOutputRequest),
	}
	return m
}

// TestGetAcpOutputManager_单例 验证单例语义。
func TestGetAcpOutputManager_单例(t *testing.T) {
	first := GetAcpOutputManager()
	second := GetAcpOutputManager()
	if first != second {
		t.Error("GetAcpOutputManager 应返回同一实例")
	}
}

// TestAcpOutputManager_Complete唤醒 验证完成投递与重复完成拒绝。
func TestAcpOutputManager_Complete唤醒(t *testing.T) {
	m := newAcpTestManager()
	req := &AcpOutputRequest{
		JsonrpcID: "1",
		Method:    "fs/read_text_file",
		Done:      make(chan AcpOutputResult, 1),
		RequestID: "acp_out_test",
	}
	m.AddPendingRequest(req)

	if !m.CompleteJsonrpcResponse("1", map[string]any{"result": map[string]any{"content": "x"}}) {
		t.Error("首次完成应成功")
	}
	select {
	case res := <-req.Done:
		if res.Err != nil {
			t.Errorf("不应有错误: %v", res.Err)
		}
	case <-time.After(time.Second):
		t.Fatal("Done 未唤醒")
	}
	// 重复完成返回 false（已从 pending 移除）
	if m.CompleteJsonrpcResponse("1", map[string]any{}) {
		t.Error("重复完成应返回 false")
	}
}

// TestAcpOutputManager_Complete空ID 空白/nil ID 返回 false。
func TestAcpOutputManager_Complete空ID(t *testing.T) {
	m := newAcpTestManager()
	if m.CompleteJsonrpcResponse("", map[string]any{}) {
		t.Error("空 ID 应返回 false")
	}
	if m.CompleteJsonrpcResponse(nil, map[string]any{}) {
		t.Error("nil ID 应返回 false")
	}
	// 数字 ID 归一化
	req := &AcpOutputRequest{JsonrpcID: "7", Method: "m", Done: make(chan AcpOutputResult, 1)}
	m.AddPendingRequest(req)
	if !m.CompleteJsonrpcResponse(float64(7), map[string]any{}) {
		t.Error("float64(7) 应归一化命中")
	}
	if res := <-req.Done; res.Err != nil {
		t.Errorf("res = %+v", res)
	}
}

// TestAcpOutputManager_Complete未知ID 未知 ID 返回 false。
func TestAcpOutputManager_Complete未知ID(t *testing.T) {
	m := newAcpTestManager()
	if m.CompleteJsonrpcResponse("nope", map[string]any{}) {
		t.Error("未知 ID 应返回 false")
	}
}

// TestAcpOutputManager_Fail唤醒 验证失败投递。
func TestAcpOutputManager_Fail唤醒(t *testing.T) {
	m := newAcpTestManager()
	req := &AcpOutputRequest{
		JsonrpcID: "2", Method: "m", Done: make(chan AcpOutputResult, 1),
	}
	m.AddPendingRequest(req)
	if !m.FailJsonrpcResponse("2", errors.New("boom")) {
		t.Error("Fail 应成功")
	}
	res := <-req.Done
	if res.Err == nil || res.Err.Error() != "boom" {
		t.Errorf("err = %v", res.Err)
	}
	// 重复失败返回 false
	if m.FailJsonrpcResponse("2", errors.New("again")) {
		t.Error("重复 Fail 应返回 false")
	}
	// 未知 ID
	if m.FailJsonrpcResponse("nope", errors.New("x")) {
		t.Error("未知 ID Fail 应返回 false")
	}
	// 空 ID
	if m.FailJsonrpcResponse("", errors.New("x")) {
		t.Error("空 ID Fail 应返回 false")
	}
}

// TestAcpOutputManager_ResetState 重置运行态。
func TestAcpOutputManager_ResetState(t *testing.T) {
	m := newAcpTestManager()
	req := &AcpOutputRequest{JsonrpcID: "1", Method: "m", Done: make(chan AcpOutputResult, 1)}
	m.AddPendingRequest(req)
	m.ResetState()
	if len(m.pendingSnapshot()) != 0 {
		t.Error("ResetState 应清空 pending")
	}
}

// TestAcpOutputManager_Send未注入回调报错 验证守卫。
func TestAcpOutputManager_Send未注入回调报错(t *testing.T) {
	m := newAcpTestManager()
	_, err := m.SendJsonrpcRequest(context.Background(), "fs/read_text_file",
		map[string]any{"path": "/x"})
	if err == nil || !strings.Contains(err.Error(), "send_push callback not set") {
		t.Errorf("应报回调未注入，实际 %v", err)
	}
}

// TestAcpOutputManager_Send推送与完成 验证推送结构与响应回流。
func TestAcpOutputManager_Send推送与完成(t *testing.T) {
	m := newAcpTestManager()
	var mu sync.Mutex
	var pushed map[string]any
	m.SetSendPushFunc(func(_ context.Context, msg map[string]any) error {
		mu.Lock()
		pushed = msg
		mu.Unlock()
		return nil
	})
	go func() {
		time.Sleep(50 * time.Millisecond)
		// 首个请求 jsonrpcID 恒为 "1"
		_ = m.CompleteJsonrpcResponse("1", map[string]any{"result": map[string]any{"content": "data"}})
	}()
	resp, err := m.SendJsonrpcRequest(context.Background(), "fs/read_text_file",
		map[string]any{"path": "/x"}, WithAcpSessionID("sess-9"))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if resp["result"] == nil {
		t.Errorf("resp = %v", resp)
	}
	// 推送结构断言
	mu.Lock()
	defer mu.Unlock()
	if pushed == nil {
		t.Fatal("推送未发生")
	}
	if pushed["response_kind"] != "acp.output_request" {
		t.Errorf("response_kind = %v", pushed["response_kind"])
	}
	if pushed["channel_id"] != "acp" {
		t.Errorf("channel_id = %v", pushed["channel_id"])
	}
	if pushed["payload"] == nil || pushed["body"] == nil {
		t.Error("channel_id/payload 双 key 兼容应存在")
	}
	body, _ := pushed["body"].(map[string]any)
	if body == nil || body["method"] != "fs/read_text_file" {
		t.Errorf("body = %v", pushed["body"])
	}
	if body["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v", body["jsonrpc"])
	}
	// sessionID 合并进 params
	params, _ := body["params"].(map[string]any)
	if params == nil || params["sessionId"] != "sess-9" || params["path"] != "/x" {
		t.Errorf("params = %v", body["params"])
	}
	if pushed["request_id"] == nil || !strings.HasPrefix(pushed["request_id"].(string), "acp_out_") {
		t.Errorf("request_id = %v", pushed["request_id"])
	}
	if pushed["jsonrpc_id"] != "1" {
		t.Errorf("jsonrpc_id = %v", pushed["jsonrpc_id"])
	}
	if pushed["session_id"] != "sess-9" {
		t.Errorf("session_id = %v", pushed["session_id"])
	}
	// server_push 标识在 metadata 嵌套层（Python metadata={E2A_WIRE_SERVER_PUSH_KEY: True}）
	md, _ := pushed["metadata"].(map[string]any)
	if md == nil || md["_jiuwenswarm_server_push"] != true {
		t.Errorf("metadata server_push 标识 = %v", pushed["metadata"])
	}
}

// TestAcpOutputManager_Send超时清理 验证超时分支与 pending 清理。
func TestAcpOutputManager_Send超时清理(t *testing.T) {
	m := newAcpTestManager()
	m.SetSendPushFunc(func(_ context.Context, _ map[string]any) error { return nil })
	_, err := m.SendJsonrpcRequest(context.Background(), "fs/read_text_file",
		map[string]any{"path": "/x"}, WithAcpTimeout(100*time.Millisecond))
	if err == nil {
		t.Error("应超时")
	}
	var timeErr *AcpOutputTimeoutError
	if !errors.As(err, &timeErr) {
		t.Errorf("超时错误应为可判别类型，实际 %T: %v", err, err)
	}
	if len(m.pendingSnapshot()) != 0 {
		t.Error("超时后 pending 应清空")
	}
}

// TestAcpOutputManager_Send推送失败回滚 验证失败回滚。
func TestAcpOutputManager_Send推送失败回滚(t *testing.T) {
	m := newAcpTestManager()
	m.SetSendPushFunc(func(_ context.Context, _ map[string]any) error {
		return errors.New("push failed")
	})
	_, err := m.SendJsonrpcRequest(context.Background(), "m", nil)
	if err == nil || !strings.Contains(err.Error(), "Failed to send ACP output request") {
		t.Errorf("推送失败应报错，实际 %v", err)
	}
	if len(m.pendingSnapshot()) != 0 {
		t.Error("失败后 pending 应回滚")
	}
}

// TestAcpOutputManager_SendCtx取消 ctx 取消分支。
func TestAcpOutputManager_SendCtx取消(t *testing.T) {
	m := newAcpTestManager()
	m.SetSendPushFunc(func(_ context.Context, _ map[string]any) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := m.SendJsonrpcRequest(ctx, "m", nil)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("ctx 取消应传播，实际 %v", err)
	}
	if len(m.pendingSnapshot()) != 0 {
		t.Error("取消后 pending 应清空")
	}
}

// TestAcpOutputManager_Send失败唤醒 Fail 传播。
func TestAcpOutputManager_Send失败唤醒(t *testing.T) {
	m := newAcpTestManager()
	m.SetSendPushFunc(func(_ context.Context, _ map[string]any) error { return nil })
	go func() {
		time.Sleep(30 * time.Millisecond)
		_ = m.FailJsonrpcResponse("1", errors.New("peer gone"))
	}()
	_, err := m.SendJsonrpcRequest(context.Background(), "m", nil)
	if err == nil || err.Error() != "peer gone" {
		t.Errorf("err = %v", err)
	}
}

// TestNormalizeJsonrpcID ID 归一化各类型。
func TestNormalizeJsonrpcID(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{" 7 ", "7"},
		{float64(7), "7"},
		{float64(7.5), "7.5"},
		{int(9), "9"},
		{int64(11), "11"},
		{nil, ""},
		{true, ""},
	}
	for _, tc := range cases {
		if got := normalizeJsonrpcID(tc.in); got != tc.want {
			t.Errorf("normalizeJsonrpcID(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestReadTextFile_错误响应 error 响应转 AcpOutputError。
func TestReadTextFile_错误响应(t *testing.T) {
	m := newAcpTestManager()
	m.SetSendPushFunc(func(_ context.Context, _ map[string]any) error { return nil })
	go func() {
		time.Sleep(30 * time.Millisecond)
		_ = m.CompleteJsonrpcResponse("1", map[string]any{
			"error": map[string]any{"code": float64(-32000), "message": "denied"},
		})
	}()
	_, err := readTextFile(context.Background(), m, "/x", nil, nil, "")
	if err == nil {
		t.Fatal("error 响应应转为 AcpOutputError")
	}
	var aoe *AcpOutputError
	if !errors.As(err, &aoe) {
		t.Fatalf("应为 AcpOutputError，实际 %T", err)
	}
	if aoe.Code != -32000 || aoe.Message != "denied" || aoe.Method != "fs/read_text_file" {
		t.Errorf("aoe = %+v", aoe)
	}
}

// TestReadTextFile_成功响应 result 提取。
func TestReadTextFile_成功响应(t *testing.T) {
	m := newAcpTestManager()
	m.SetSendPushFunc(func(_ context.Context, _ map[string]any) error { return nil })
	go func() {
		time.Sleep(30 * time.Millisecond)
		_ = m.CompleteJsonrpcResponse("1", map[string]any{
			"result": map[string]any{"content": "hello"},
		})
	}()
	res, err := readTextFile(context.Background(), m, "/x", 2, 5, "sess")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res["content"] != "hello" {
		t.Errorf("res = %v", res)
	}
}

// TestWaitForTerminalExit_软超时 超时返回轮询结构。
func TestWaitForTerminalExit_软超时(t *testing.T) {
	m := newAcpTestManager()
	m.SetSendPushFunc(func(_ context.Context, _ map[string]any) error { return nil })
	// 不 Complete → 必超时（用短超时注入）
	res, err := waitForTerminalExit(context.Background(), m, "t1", "", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("软超时不应报错: %v", err)
	}
	if res["timedOut"] != true || res["running"] != true || res["shouldRetry"] != true {
		t.Errorf("软超时结构 = %v", res)
	}
	if res["exitCode"] != nil {
		t.Errorf("exitCode 应为 nil: %v", res["exitCode"])
	}
	if res["signal"] != nil {
		t.Errorf("signal 应为 nil: %v", res["signal"])
	}
}

// TestWaitForTerminalExit_完成归一化 exitCode 非空 → running=false。
func TestWaitForTerminalExit_完成归一化(t *testing.T) {
	m := newAcpTestManager()
	m.SetSendPushFunc(func(_ context.Context, _ map[string]any) error { return nil })
	go func() {
		time.Sleep(30 * time.Millisecond)
		_ = m.CompleteJsonrpcResponse("1", map[string]any{
			"result": map[string]any{"exitCode": float64(0)},
		})
	}()
	res, err := waitForTerminalExit(context.Background(), m, "t1", "", 5*time.Second)
	if err != nil {
		t.Fatalf("完成: %v", err)
	}
	if res["running"] != false {
		t.Errorf("exitCode 非空 → running=false，实际 %v", res)
	}
	if res["shouldRetry"] != false {
		t.Errorf("shouldRetry = %v", res["shouldRetry"])
	}
	if res["timedOut"] != false {
		t.Errorf("timedOut 默认 false: %v", res["timedOut"])
	}
}

// TestWaitForTerminalExit_running显式保留 显式 running=true 时不覆写。
func TestWaitForTerminalExit_running显式保留(t *testing.T) {
	m := newAcpTestManager()
	m.SetSendPushFunc(func(_ context.Context, _ map[string]any) error { return nil })
	go func() {
		time.Sleep(30 * time.Millisecond)
		_ = m.CompleteJsonrpcResponse("1", map[string]any{
			"result": map[string]any{"exitCode": float64(1), "running": true},
		})
	}()
	res, err := waitForTerminalExit(context.Background(), m, "t1", "", 5*time.Second)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res["running"] != true {
		t.Errorf("显式 running=true 应保留: %v", res)
	}
	if res["shouldRetry"] != true {
		t.Errorf("running=true → shouldRetry=true: %v", res)
	}
}

// TestWaitForTerminalExit_错误响应 error 响应仍转 AcpOutputError。
func TestWaitForTerminalExit_错误响应(t *testing.T) {
	m := newAcpTestManager()
	m.SetSendPushFunc(func(_ context.Context, _ map[string]any) error { return nil })
	go func() {
		time.Sleep(30 * time.Millisecond)
		_ = m.CompleteJsonrpcResponse("1", map[string]any{
			"error": map[string]any{"code": float64(-9), "message": "no term"},
		})
	}()
	_, err := waitForTerminalExit(context.Background(), m, "t1", "", 5*time.Second)
	var aoe *AcpOutputError
	if !errors.As(err, &aoe) || aoe.Method != "terminal/wait_for_exit" {
		t.Errorf("应为 wait_for_exit 的 AcpOutputError，实际 %v", err)
	}
}

// TestWaitForTerminalExit_软超时env覆盖 wait 超时 env 变量。
func TestWaitForTerminalExit_软超时env覆盖(t *testing.T) {
	t.Setenv("ACP_WAIT_FOR_EXIT_TIMEOUT_SECONDS", "0.2")
	m := newAcpTestManager()
	m.SetSendPushFunc(func(_ context.Context, _ map[string]any) error { return nil })
	res, err := waitForTerminalExit(context.Background(), m, "t1", "", acpWaitForExitTimeout)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res["timedOut"] != true {
		t.Errorf("env 覆盖超时应软超时: %v", res)
	}
}

// TestGetAcpOutputTools_六工具绑定session 工具卡与闭包绑定。
func TestGetAcpOutputTools_六工具绑定session(t *testing.T) {
	tools := GetAcpOutputTools("sess-1", "req-1")
	if len(tools) != 6 {
		t.Fatalf("应有 6 个工具，实际 %d", len(tools))
	}
	names := []string{"read_text_file", "write_text_file", "create_terminal",
		"read_terminal_output", "wait_for_terminal_exit", "release_terminal"}
	for i, want := range names {
		if tools[i].Card().Name != want {
			t.Errorf("工具[%d] = %s，want %s", i, tools[i].Card().Name, want)
		}
	}
	// 卡片描述逐字复制验证（Python L492-495）
	if !strings.Contains(tools[0].Card().Description, "[ACP] 通过 IDE 读取用户本地文件内容") {
		t.Errorf("read_text_file 描述应含 [ACP] 前缀: %s", tools[0].Card().Description)
	}
	if !strings.Contains(tools[1].Card().Description, "[ACP] 通过 IDE 写入文件到用户本地") {
		t.Errorf("write_text_file 描述: %s", tools[1].Card().Description)
	}
	if !strings.Contains(tools[2].Card().Description, "[ACP] 通过 IDE 创建终端并执行命令") {
		t.Errorf("create_terminal 描述: %s", tools[2].Card().Description)
	}
	if !strings.Contains(tools[4].Card().Description, "应继续使用同一个 terminal_id 再次调用本工具轮询") {
		t.Errorf("wait_for_terminal_exit 描述: %s", tools[4].Card().Description)
	}
	if tools[5].Card().Description != "释放终端资源（命令完成后必须调用）。" {
		t.Errorf("release_terminal 描述: %s", tools[5].Card().Description)
	}
	// 参数 schema
	params := tools[0].Card().InputParams
	if params == nil {
		t.Fatal("read_text_file 应有参数")
	}
	found := map[string]bool{}
	for _, p := range params {
		found[p.Name] = true
	}
	if !found["path"] || !found["offset"] || !found["limit"] {
		t.Errorf("read_text_file 参数 = %v", found)
	}
}

// TestGetAcpOutputTools_闭包调用sessionID透传 闭包绑定 session 经 push 验证。
func TestGetAcpOutputTools_闭包调用sessionID透传(t *testing.T) {
	mgr := GetAcpOutputManager()
	mgr.ResetState()
	defer mgr.ResetState()
	var mu sync.Mutex
	var gotBody map[string]any
	mgr.SetSendPushFunc(func(_ context.Context, msg map[string]any) error {
		if body, ok := msg["body"].(map[string]any); ok {
			mu.Lock()
			gotBody = body
			mu.Unlock()
		}
		return nil
	})
	tools := GetAcpOutputTools("bound-sess", "req-1")
	// read_text_file 闭包调用
	go func() {
		time.Sleep(30 * time.Millisecond)
		_ = mgr.CompleteJsonrpcResponse("1", map[string]any{"result": map[string]any{}})
	}()
	res, err := tools[0].Invoke(context.Background(), map[string]any{
		"path": "/tmp/x", "offset": float64(2), "limit": float64(3),
	})
	if err != nil {
		t.Fatalf("闭包调用: %v", err)
	}
	if _, ok := res["result"]; !ok {
		t.Errorf("res = %v", res)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotBody == nil {
		t.Fatal("推送未发生")
	}
	params, _ := gotBody["params"].(map[string]any)
	if params == nil || params["sessionId"] != "bound-sess" {
		t.Errorf("闭包应绑定 session: %v", gotBody["params"])
	}
	if params["path"] != "/tmp/x" || params["offset"] != float64(2) {
		t.Errorf("参数透传 = %v", params)
	}
}

// TestWriteTextFile等其余四工具 四工具参数透传。
func TestWriteTextFile等其余四工具(t *testing.T) {
	m := newAcpTestManager()
	m.SetSendPushFunc(func(_ context.Context, _ map[string]any) error { return nil })
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = m.CompleteJsonrpcResponse("1", map[string]any{"result": map[string]any{}})
	}()
	res, err := writeTextFile(context.Background(), m, "/y", "content-1", "s")
	if err != nil {
		t.Fatalf("writeTextFile: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("空 result 应为空 map: %v", res)
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = m.CompleteJsonrpcResponse("2", map[string]any{"result": map[string]any{"terminalId": "t"}})
	}()
	res, err = createTerminal(context.Background(), m, "ls -la", "/tmp", "s")
	if err != nil {
		t.Fatalf("createTerminal: %v", err)
	}
	if res["terminalId"] != "t" {
		t.Errorf("res = %v", res)
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = m.CompleteJsonrpcResponse("3", map[string]any{"result": map[string]any{"output": "out"}})
	}()
	res, err = readTerminalOutput(context.Background(), m, "t1", "s")
	if err != nil {
		t.Fatalf("readTerminalOutput: %v", err)
	}
	if res["output"] != "out" {
		t.Errorf("res = %v", res)
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = m.CompleteJsonrpcResponse("4", map[string]any{"result": map[string]any{}})
	}()
	res, err = releaseTerminal(context.Background(), m, "t1", "s")
	if err != nil {
		t.Fatalf("releaseTerminal: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("释放终端空 result 应为空 map: %v", res)
	}
}

// TestAcpOutputError_Error 错误文案格式。
func TestAcpOutputError_Error(t *testing.T) {
	e := &AcpOutputError{Method: "fs/read_text_file", Code: -32000, Message: "denied"}
	if got := e.Error(); got != "[fs/read_text_file] error -32000: denied" {
		t.Errorf("Error() = %q", got)
	}
}
