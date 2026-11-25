package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	workflow "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow"
)

// TestIntegration_HTTPRequestComponent_真链 ExecuteSingleComponent 十步全链：
// config 静态 URL → 三步链 → 输出信封贯通（对齐 8.18 llm 集成套件惯例）。
// 注：单组件直调的 inputs 走 commit_user_inputs 进 session state
// （Python _workflow.py:78-79），组件 inputs 形参为空 map——与 Python
// vertex.call(None) 语义一致，占位符 inputs 注入在 InvokeFn 层直测（见单测）。
func TestIntegration_HTTPRequestComponent_真链(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"echo": r.URL.Path})
	}))
	defer srv.Close()

	rp := NewHttpRequestParamConfig()
	rp.URL = srv.URL + "/hello"
	rp.Timeout = 5
	comp := NewHTTPRequestComponent(&HttpComponentConfig{RequestParams: rp})

	sess := session.NewSession()
	out, err := workflow.ExecuteSingleComponent(context.Background(), "http-comp", *sess, comp,
		map[string]any{"path": "hello"}, nil, nil, nil)
	if err != nil {
		t.Fatalf("单组件直调失败: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["statusCode"] != 200 || m["ok"] != true {
		t.Fatalf("输出信封漂移: %v", out)
	}
	body := m["body"].(map[string]any)
	if body["echo"] != "/hello" {
		t.Fatalf("响应体未贯通: %v", body)
	}
}

// TestIntegration_HTTPRequestComponent_inputs占位符 InvokeFn 层 inputs 注入：
// URL 占位符由 inputs 解析（Python process_inputs(inputs) 直接调用语义）
func TestIntegration_HTTPRequestComponent_inputs占位符(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"echo": r.URL.Path})
	}))
	defer srv.Close()

	rp := NewHttpRequestParamConfig()
	rp.URL = srv.URL + "/{{path}}"
	rp.Timeout = 5
	e := NewHTTPRequestExecutable(&HttpComponentConfig{RequestParams: rp})

	out, err := e.InvokeFn(context.Background(), map[string]any{"path": "inject"}, nil, nil)
	if err != nil {
		t.Fatalf("invoke 失败: %v", err)
	}
	body := out["body"].(map[string]any)
	if body["echo"] != "/inject" {
		t.Fatalf("inputs 占位符未贯通: %v", out)
	}
}
