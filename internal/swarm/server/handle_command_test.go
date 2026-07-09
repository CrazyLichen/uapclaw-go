package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestHandleCommandStatus 验证 command.status 返回诊断信息。
func TestHandleCommandStatus(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodCommandStatus, json.RawMessage(`{}`))

	resp, err := s.handleCommandStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("handleCommandStatus 返回错误: %v", err)
	}
	if !resp.OK {
		t.Error("resp.OK 应为 true")
	}
	if resp.Payload == nil {
		t.Fatal("payload 不应为 nil")
	}
	if _, ok := resp.Payload["version"]; !ok {
		t.Error("payload 应包含 version")
	}
	if _, ok := resp.Payload["config_dir"]; !ok {
		t.Error("payload 应包含 config_dir")
	}
}

// TestHandleCommandCompact 验证 command.compact 返回 compressed=false。
func TestHandleCommandCompact(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodCommandCompact, json.RawMessage(`{}`))

	resp, err := s.handleCommandCompact(context.Background(), req)
	if err != nil {
		t.Fatalf("handleCommandCompact 返回错误: %v", err)
	}
	if !resp.OK {
		t.Error("resp.OK 应为 true")
	}
	if compressed, ok := resp.Payload["compressed"]; !ok || compressed != false {
		t.Errorf("payload.compressed 应为 false, 实际: %v", compressed)
	}
}

// TestHandleCommandContext 验证 command.context 返回 usage=0, limit=0。
func TestHandleCommandContext(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodCommandContext, json.RawMessage(`{}`))

	resp, err := s.handleCommandContext(context.Background(), req)
	if err != nil {
		t.Fatalf("handleCommandContext 返回错误: %v", err)
	}
	if usage, ok := resp.Payload["usage"]; !ok || usage != 0 {
		t.Errorf("payload.usage 应为 0, 实际: %v", usage)
	}
	if limit, ok := resp.Payload["limit"]; !ok || limit != 0 {
		t.Errorf("payload.limit 应为 0, 实际: %v", limit)
	}
}

// TestHandleCommandModel 验证 command.model 返回 ok=true。
func TestHandleCommandModel(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodCommandModel,
		json.RawMessage(`{"action": "get", "model": "qwen-max"}`))

	resp, err := s.handleCommandModel(context.Background(), req)
	if err != nil {
		t.Fatalf("handleCommandModel 返回错误: %v", err)
	}
	if ok := resp.Payload["ok"]; ok != true {
		t.Errorf("payload.ok 应为 true, 实际: %v", ok)
	}
}

// TestHandleCommandSandbox 验证 command.sandbox 返回 NOT_IMPLEMENTED。
func TestHandleCommandSandbox(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodCommandSandbox, json.RawMessage(`{}`))

	resp, err := s.handleCommandSandbox(context.Background(), req)
	if err != nil {
		t.Fatalf("handleCommandSandbox 返回错误: %v", err)
	}
	if resp.OK {
		t.Error("resp.OK 应为 false（NOT_IMPLEMENTED）")
	}
}

// TestHandleCommandAddDir 验证 command.add_dir 返回 ok=true。
func TestHandleCommandAddDir(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodCommandAddDir,
		json.RawMessage(`{"dir": "/tmp/test"}`))

	resp, err := s.handleCommandAddDir(context.Background(), req)
	if err != nil {
		t.Fatalf("handleCommandAddDir 返回错误: %v", err)
	}
	if ok := resp.Payload["ok"]; ok != true {
		t.Errorf("payload.ok 应为 true, 实际: %v", ok)
	}
}
