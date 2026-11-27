package server

import (
	"encoding/json"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// makeCatalogReq 构造设备目录请求。
func makeCatalogReq(method schema.ReqMethod, params map[string]any) *schema.AgentRequest {
	raw, _ := json.Marshal(params)
	return schema.NewAgentRequest("req-cat", "web", method, raw)
}

func TestHandlePackageCatalogRequest_安装拒绝(t *testing.T) {
	for _, method := range []schema.ReqMethod{
		schema.ReqMethodAgentTemplatesInstall,
		schema.ReqMethodAgentGroupsInstall,
		schema.ReqMethodPluginPackagesInstall,
	} {
		resp := HandlePackageCatalogRequest(makeCatalogReq(method, map[string]any{"id": "x"}))
		if resp.OK {
			t.Fatalf("%s 应拒绝", method)
		}
		if resp.Payload["code"] != "MARKET_UNAVAILABLE" {
			t.Fatalf("%s 应 MARKET_UNAVAILABLE: %+v", method, resp.Payload)
		}
	}
}

func TestHandlePackageCatalogRequest_未知方法(t *testing.T) {
	resp := HandlePackageCatalogRequest(makeCatalogReq("agent_templates.bogus", map[string]any{}))
	if resp.OK || resp.Payload["code"] != "BAD_REQUEST" {
		t.Fatalf("未知方法应 BAD_REQUEST: %+v", resp.Payload)
	}
}

func TestHandlePackageCatalogRequest_idName别名(t *testing.T) {
	// name 别名兼容（Python :2984-2986）：未知包统一 NOT_FOUND 语义（error 文案含 name）
	resp := HandlePackageCatalogRequest(makeCatalogReq(schema.ReqMethodAgentTemplatesShow,
		map[string]any{"name": "ghost_pkg"}))
	if resp.OK {
		t.Fatal("未知包应失败")
	}
	if !stringsContains(resp.Payload["error"], "ghost_pkg") {
		t.Fatalf("错误信息应含包名: %+v", resp.Payload)
	}
}

// stringsContains 测试助手。
func stringsContains(haystack any, needle string) bool {
	s, _ := haystack.(string)
	return len(needle) > 0 && len(s) >= len(needle) && (s == needle ||
		len(s) > 0 && indexOfStr(s, needle) >= 0)
}

func indexOfStr(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
