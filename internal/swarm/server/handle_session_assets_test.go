package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// newSessionAssetsTestEnv 建临时会话根（对齐 Python 测试对 get_agent_sessions_dir 的 monkeypatch）。
func newSessionAssetsTestEnv(t *testing.T) *AgentServer {
	t.Helper()
	sessionAssetsBaseOverride = t.TempDir()
	t.Cleanup(func() { sessionAssetsBaseOverride = "" })
	return &AgentServer{}
}

// newSessionAssetsReq 构造 session.assets.* 请求。
func newSessionAssetsReq(method schema.ReqMethod, params map[string]any) *schema.AgentRequest {
	raw, _ := json.Marshal(params)
	return schema.NewAgentRequest("req-test", "web", method, raw)
}

// toAnyItems 转 []any。
func toAnyItems(items []map[string]any) []any {
	out := make([]any, 0, len(items))
	for _, m := range items {
		out = append(out, m)
	}
	return out
}

func TestValidateSessionAssetName(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr string
	}{
		{"  Oat  Serum ", "Oat Serum", ""},
		{"", "", "INVALID_NAME"},
		{"a@b", "", "INVALID_NAME"},
		{"a\nb", "", "INVALID_NAME"},
		{strings.Repeat("长", 61), "", "INVALID_NAME"},
	}
	for _, c := range cases {
		got, err := validateSessionAssetName(c.in)
		if c.wantErr != "" {
			se, ok := err.(*SessionAssetError)
			if !ok || se.Code != c.wantErr {
				t.Fatalf("validate(%q) err=%v want %s", c.in, err, c.wantErr)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Fatalf("validate(%q) = %q, %v want %q", c.in, got, err, c.want)
		}
	}
}

func TestSessionAssetsStateFileFor_非法sessionID(t *testing.T) {
	newSessionAssetsTestEnv(t)
	for _, sid := range []string{"", "../escape", "a/b", strings.Repeat("x", 129)} {
		if _, err := sessionAssetsStateFileFor(sid); err == nil {
			t.Fatalf("sid=%q 应拒绝", sid)
		}
	}
	if _, err := sessionAssetsStateFileFor("s1"); err != nil {
		t.Fatalf("合法 sid 报错: %v", err)
	}
}

func TestSessionAssetsRegister_去重与命名(t *testing.T) {
	s := newSessionAssetsTestEnv(t)
	root, err := sessionAssetsStateFileFor("s1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	p1 := filepath.Join(t.TempDir(), "serum.png")
	if err := os.WriteFile(p1, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p2 := strings.TrimSuffix(p1, ".png") + "-2.png"
	if err := os.WriteFile(p2, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	register := func(items []map[string]any) map[string]any {
		resp, rerr := s.HandleSessionAssetsRegister(context.Background(), newSessionAssetsReq(
			schema.ReqMethodSessionAssetsRegister,
			map[string]any{"session_id": "s1", "items": toAnyItems(items)}))
		if rerr != nil || !resp.OK {
			t.Fatalf("register err=%v resp=%+v", rerr, resp)
		}
		return resp.Payload
	}
	first := register([]map[string]any{
		{"path": p1},
		{"path": p2, "source": "generated"},
	})
	assets := first["assets"].([]any)
	if len(assets) != 2 {
		t.Fatalf("want 2 assets got %d", len(assets))
	}
	a0 := assets[0].(map[string]any)
	a1 := assets[1].(map[string]any)
	if a0["name"] != "serum" || a0["kind"] != "image" || a0["source"] != "upload" {
		t.Fatalf("asset0 = %+v", a0)
	}
	if a1["name"] != "serum-2" || a1["kind"] != "image" {
		t.Fatalf("asset1 应独立命名 serum-2/类 image（stem 即 serum-2）： %+v", a1)
	}
	// 新 client_id：新增一条
	second := register([]map[string]any{{"path": p2, "client_id": "draft-1", "name": "别的名字"}})
	if got := len(second["assets"].([]any)); got != 3 {
		t.Fatalf("新 client_id 应新增一条，got %d", got)
	}
	// 同 client_id 重试：返回既有素材不新增
	third := register([]map[string]any{{"path": p2, "client_id": "draft-1"}})
	created := third["created"].([]any)
	if created[0] == nil {
		t.Fatal("同 client_id 重试应返回既有素材")
	}
	if created[0].(map[string]any)["name"] != "别的名字" {
		t.Fatalf("幂等返回错误素材: %+v", created[0])
	}
	if got := len(third["assets"].([]any)); got != 3 {
		t.Fatalf("同 client_id 不应新增，got %d", got)
	}
}

func TestSessionAssetsRename_冲突与历史(t *testing.T) {
	s := newSessionAssetsTestEnv(t)
	p1 := filepath.Join(t.TempDir(), "one.txt")
	_ = os.WriteFile(p1, []byte("x"), 0o644)
	register := func(items []map[string]any) map[string]any {
		resp, _ := s.HandleSessionAssetsRegister(context.Background(), newSessionAssetsReq(
			schema.ReqMethodSessionAssetsRegister,
			map[string]any{"session_id": "s1", "items": toAnyItems(items)}))
		return resp.Payload
	}
	first := register([]map[string]any{{"path": p1}, {"path": p1}})
	assets := first["assets"].([]any)
	id1 := assets[0].(map[string]any)["asset_id"].(string)
	id2 := assets[1].(map[string]any)["asset_id"].(string)
	// 重命名 1 → cub
	rresp, rerr := s.HandleSessionAssetsRename(context.Background(), newSessionAssetsReq(
		schema.ReqMethodSessionAssetsRename,
		map[string]any{"session_id": "s1", "asset_id": id1, "name": "cub"}))
	if rerr != nil || !rresp.OK {
		t.Fatalf("rename err=%v", rerr)
	}
	// 2 改名 one（旧名经 history 不复活）
	if _, rerr := s.HandleSessionAssetsRename(context.Background(), newSessionAssetsReq(
		schema.ReqMethodSessionAssetsRename,
		map[string]any{"session_id": "s1", "asset_id": id2, "name": "one"})); rerr != nil {
		t.Fatalf("rename 2 err=%v", rerr)
	}
	// 再注册 stem=one 的文件应拿 one-1（one 已进 history 被占用）
	one := filepath.Join(t.TempDir(), "one.txt")
	_ = os.WriteFile(one, []byte("x"), 0o644)
	got := register([]map[string]any{{"path": one}})
	newName := got["assets"].([]any)[2].(map[string]any)["name"]
	if newName != "one-2" {
		t.Fatalf("history 占名失败: %q", newName)
	}
	// 重名冲突（错误经 ok=false 响应承载，对齐 Python AgentResponse 分支）
	if cresp, _ := s.HandleSessionAssetsRename(context.Background(), newSessionAssetsReq(
		schema.ReqMethodSessionAssetsRename,
		map[string]any{"session_id": "s1", "asset_id": id1, "name": "ONE"})); cresp.OK ||
		cresp.Payload["code"] != "NAME_CONFLICT" {
		t.Fatalf("重名改名应报 NAME_CONFLICT: %+v", cresp.Payload)
	}
	// 未知 asset_id
	if nresp, _ := s.HandleSessionAssetsRename(context.Background(), newSessionAssetsReq(
		schema.ReqMethodSessionAssetsRename,
		map[string]any{"session_id": "s1", "asset_id": "nope", "name": "x"})); nresp.OK ||
		nresp.Payload["code"] != "ASSET_NOT_FOUND" {
		t.Fatalf("未知 asset_id 应报 ASSET_NOT_FOUND: %+v", nresp.Payload)
	}
}

func TestSessionAssetsRemove_幂等(t *testing.T) {
	s := newSessionAssetsTestEnv(t)
	p1 := filepath.Join(t.TempDir(), "a.txt")
	_ = os.WriteFile(p1, []byte("x"), 0o644)
	regResp, _ := s.HandleSessionAssetsRegister(context.Background(), newSessionAssetsReq(
		schema.ReqMethodSessionAssetsRegister,
		map[string]any{"session_id": "s1", "items": toAnyItems([]map[string]any{{"path": p1}})}))
	assets := regResp.Payload["assets"].([]any)
	id := assets[0].(map[string]any)["asset_id"].(string)
	for i := 0; i < 2; i++ {
		resp, rerr := s.HandleSessionAssetsRemove(context.Background(), newSessionAssetsReq(
			schema.ReqMethodSessionAssetsRemove,
			map[string]any{"session_id": "s1", "asset_id": id}))
		if rerr != nil || !resp.OK {
			t.Fatalf("remove err=%v", rerr)
		}
		if got := len(resp.Payload["assets"].([]any)); got != 0 {
			t.Fatalf("remove 后应空，got %d", got)
		}
	}
}
