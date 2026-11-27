package repositories

import (
	"encoding/json"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// makeProjectReq 构造项目控制面请求。
func makeProjectReq(method schema.ReqMethod, params map[string]any) *schema.AgentRequest {
	raw, _ := json.Marshal(params)
	return schema.NewAgentRequest("req-test", "web", method, raw)
}

func TestHandleProjectRequest_列表与不支持方法(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	resp := HandleProjectRequest(makeProjectReq(schema.ReqMethodProjectList, map[string]any{}))
	if !resp.OK || resp.Payload == nil || resp.Payload["projects"] == nil {
		t.Fatalf("project.list 失败: %+v", resp)
	}
	if _, ok := resp.Payload["projects"].([]map[string]any); !ok {
		t.Fatalf("projects 应为数组: %+v", resp.Payload["projects"])
	}
	// create/rename/git 等执行平面方法不在控制面集合 → unsupported
	for _, m := range []schema.ReqMethod{
		"project.git.status", "project.create", "project.rename",
	} {
		req := schema.ReqMethod(m)
		bad := HandleProjectRequest(makeProjectReq(req, map[string]any{}))
		if bad.OK || bad.Payload["code"] != "BAD_REQUEST" {
			t.Fatalf("%s 应 unsupported BAD_REQUEST: %+v", m, bad.Payload)
		}
	}
}
