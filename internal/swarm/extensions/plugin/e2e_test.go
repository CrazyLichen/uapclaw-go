package plugin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// e2eBinPath TestMain 预编译的 echo 插件二进制路径
var e2eBinPath string

// TestMain 预编译 testdata/echo 插件（本地 go build，非外部服务，不加 build tag）
func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "uapclaw-ext-e2e-*")
	if err != nil {
		os.Exit(1)
	}
	bin := filepath.Join(tmpDir, "ext-plugin")
	cmd := exec.Command("go", "build", "-o", bin, "./testdata/echo/")
	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	if out, err := cmd.CombinedOutput(); err != nil {
		// 无 go 工具链环境（罕见）跳过端到端
		_ = out
		_ = os.RemoveAll(tmpDir)
		e2eBinPath = ""
		os.Exit(m.Run())
	}
	e2eBinPath = bin
	code := m.Run()
	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}

// TestE2E_FullLifecycle 端到端：发现→加载→Register→事件桥→transport 回环→shutdown
func TestE2E_FullLifecycle(t *testing.T) {
	if e2eBinPath == "" {
		t.Skip("go 工具链不可用，跳过端到端")
	}

	// 准备扩展根目录：manifest + ext-plugin
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "extension.yaml"), []byte(
		"id: echo-ext\nname: 回环扩展\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatalf("写 manifest 失败: %v", err)
	}
	if err := copyFile(e2eBinPath, filepath.Join(root, "ext-plugin")); err != nil {
		t.Fatalf("复制插件失败: %v", err)
	}
	_ = os.Chmod(filepath.Join(root, "ext-plugin"), 0o755)

	// 加载（真实 go-plugin 子进程）
	pc, err := LoadPluginClient(root)
	if err != nil {
		t.Fatalf("LoadPluginClient 失败: %v", err)
	}
	defer pc.Kill()

	// Register 声明
	decls, err := pc.Register(context.Background())
	if err != nil {
		t.Fatalf("Register 失败: %v", err)
	}
	if len(decls) != 1 {
		t.Fatalf("声明数 = %d, 期望 1", len(decls))
	}
	if decls[0].Metadata.ID != "echo-ext" {
		t.Errorf("声明 ID = %q, 期望 echo-ext", decls[0].Metadata.ID)
	}
	if len(decls[0].EventSubs) != 2 {
		t.Errorf("事件订阅数 = %d, 期望 2", len(decls[0].EventSubs))
	}
	if !decls[0].ProvidesTransport {
		t.Error("应声明 ProvidesTransport")
	}
	if !decls[0].ProvidesCrypto {
		t.Error("应声明 ProvidesCrypto")
	}

	// SetExtensionDir + GetMetadata 目录设置与元数据查询
	if err := pc.SetExtensionDir(context.Background(), root); err != nil {
		t.Errorf("SetExtensionDir 失败: %v", err)
	}
	if m, err := pc.GetMetadata(context.Background()); err != nil || m.ID != "echo-ext" {
		t.Errorf("GetMetadata = %+v, err = %v", m, err)
	}

	// 事件桥：memory_blocks 写回
	result, err := pc.HandleEvent(context.Background(), "agent_server:memory_before_chat",
		map[string]any{"memory_blocks": []any{}})
	if err != nil {
		t.Fatalf("HandleEvent 失败: %v", err)
	}
	blocks, _ := result["memory_blocks"].([]any)
	if len(blocks) != 1 || blocks[0] != "来自echo插件" {
		t.Errorf("memory_blocks = %v, 期望含 echo 追加块", result["memory_blocks"])
	}

	// 事件桥：params 写回（gateway 场景）
	result2, err := pc.HandleEvent(context.Background(), "gateway:before_chat_request",
		map[string]any{"params": map[string]any{"query": "原始"}})
	if err != nil {
		t.Fatalf("HandleEvent gateway 失败: %v", err)
	}
	if p, ok := result2["params"].(map[string]any); !ok || p["echo_touched"] != true {
		t.Errorf("params = %v, 期望 echo_touched=true", result2["params"])
	}

	// Crypto RPC 往返（真实子进程 Encrypt/Decrypt）
	enc, err := pc.Encrypt(context.Background(), "topsecret")
	if err != nil {
		t.Fatalf("Encrypt 失败: %v", err)
	}
	if enc != "enc(topsecret)" {
		t.Errorf("Encrypt = %q, 期望 enc(topsecret)", enc)
	}
	dec, err := pc.Decrypt(context.Background(), enc)
	if err != nil {
		t.Fatalf("Decrypt 失败: %v", err)
	}
	if dec != "topsecret" {
		t.Errorf("Decrypt = %q, 期望 topsecret", dec)
	}

	// Transport 回环
	tr := NewGRPCPluginTransport(pc)
	payload := []byte(`{"e2e":true}`)
	if err := tr.Send(context.Background(), payload); err != nil {
		t.Fatalf("Transport Send 失败: %v", err)
	}
	recvCh, err := tr.Recv()
	if err != nil {
		t.Fatalf("Transport Recv 失败: %v", err)
	}
	select {
	case got := <-recvCh:
		if string(got) != string(payload) {
			t.Errorf("回环数据不符: %s", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("transport 回环超时")
	}

	// Shutdown RPC + Kill 优雅关闭与进程终止
	if err := pc.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown 失败: %v", err)
	}
}

// copyFile 复制文件辅助
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}
