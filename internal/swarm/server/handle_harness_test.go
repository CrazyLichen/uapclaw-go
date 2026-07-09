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

// TestHandleHarnessPackagesGet 验证 harness.packages.get 返回空列表。
func TestHandleHarnessPackagesGet(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodHarnessPackagesGet, json.RawMessage(`{}`))

	resp, err := s.handleHarnessPackagesGet(context.Background(), req)
	if err != nil {
		t.Fatalf("handleHarnessPackagesGet 返回错误: %v", err)
	}
	if _, ok := resp.Payload["packages"]; !ok {
		t.Error("payload 应包含 packages")
	}
}

// TestHandleScheduleList 验证 schedule.list 返回空列表。
func TestHandleScheduleList(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodScheduleList, json.RawMessage(`{}`))

	resp, err := s.handleScheduleList(context.Background(), req)
	if err != nil {
		t.Fatalf("handleScheduleList 返回错误: %v", err)
	}
	if _, ok := resp.Payload["tasks"]; !ok {
		t.Error("payload 应包含 tasks")
	}
}

// TestHandleScheduleCreate 验证 schedule.create 返回 NOT_IMPLEMENTED。
func TestHandleScheduleCreate(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodScheduleCreate, json.RawMessage(`{}`))

	resp, err := s.handleScheduleCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("handleScheduleCreate 返回错误: %v", err)
	}
	if resp.OK {
		t.Error("resp.OK 应为 false（NOT_IMPLEMENTED）")
	}
}

// TestHandleScheduleCheckConfig 验证 schedule.check_config 返回空配置。
func TestHandleScheduleCheckConfig(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodScheduleCheckConfig, json.RawMessage(`{}`))

	resp, err := s.handleScheduleCheckConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("handleScheduleCheckConfig 返回错误: %v", err)
	}
	if _, ok := resp.Payload["config"]; !ok {
		t.Error("payload 应包含 config")
	}
}
