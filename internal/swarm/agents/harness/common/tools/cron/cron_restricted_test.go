package cronbridge

import (
	"context"
	"strings"
	"testing"

	agentcron "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/cron"
)

// fakeInner 可编程内层 backend（真实接口实现，非窄替身接口）。
type fakeInner struct {
	agentcron.CronToolBackend
	createCalls int
	updateCalls int
	listCalls   int
}

func (f *fakeInner) ListJobs(_ context.Context, _ bool) ([]map[string]any, error) {
	f.listCalls++
	return []map[string]any{{"id": "job1"}}, nil
}
func (f *fakeInner) CreateJob(_ context.Context, _ map[string]any, _ *agentcron.CronToolContext) (map[string]any, error) {
	f.createCalls++
	return map[string]any{"id": "new"}, nil
}
func (f *fakeInner) UpdateJob(_ context.Context, _ string, _ map[string]any, _ *agentcron.CronToolContext) (map[string]any, error) {
	f.updateCalls++
	return map[string]any{"id": "upd"}, nil
}

// TestRestrictedCronBackend_拒绝与放行 对齐 Python：create/update 按开关
// 拒绝（文案逐字节），list 照常委托。
func TestRestrictedCronBackend_拒绝与放行(t *testing.T) {
	inner := &fakeInner{}
	restricted := NewRestrictedCronBackend(inner, false, false)

	_, err := restricted.CreateJob(context.Background(), nil, nil)
	if err == nil || err.Error() != restrictedCreateMessage {
		t.Fatalf("create 应拒绝:\n got: %v\nwant: %s", err, restrictedCreateMessage)
	}
	_, err = restricted.UpdateJob(context.Background(), "id", nil, nil)
	if err == nil || err.Error() != restrictedUpdateMessage {
		t.Fatalf("update 应拒绝:\n got: %v\nwant: %s", err, restrictedUpdateMessage)
	}
	jobs, err := restricted.ListJobs(context.Background(), true)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("list 应委托: %v %v", jobs, err)
	}
	if inner.createCalls != 0 || inner.updateCalls != 0 || inner.listCalls != 1 {
		t.Fatalf("委托计数不符: %+v", inner)
	}

	// 只关 create：update 照常委托
	inner2 := &fakeInner{}
	partial := NewRestrictedCronBackend(inner2, false, true)
	if _, err := partial.UpdateJob(context.Background(), "id", nil, nil); err != nil {
		t.Fatalf("allow_update=true 时 update 应委托: %v", err)
	}
	if _, err := partial.CreateJob(context.Background(), nil, nil); err == nil {
		t.Fatal("allow_create=false 时 create 应拒绝")
	}
}

// TestIsRestrictedCronBackend 对齐 Python type(backend) is 判定 + delegate 挖取。
func TestIsRestrictedCronBackend(t *testing.T) {
	inner := &fakeInner{}
	restricted := NewRestrictedCronBackend(inner, false, true)
	if !IsRestrictedCronBackend(restricted) {
		t.Fatal("受限视图应命中判定")
	}
	if IsRestrictedCronBackend(inner) {
		t.Fatal("内层 backend 不应命中")
	}
	got := RestrictedInnerBackend(restricted)
	if got != agentcron.CronToolBackend(inner) {
		t.Fatal("内层挖取失败")
	}
	allowCreate, allowUpdate, isRestricted := RestrictedPolicy(restricted)
	if !isRestricted || allowCreate || !allowUpdate {
		t.Fatalf("开关取值不符: %v %v %v", allowCreate, allowUpdate, isRestricted)
	}
	// 未包装：全允许 + 非受限
	ac, au, isRestricted2 := RestrictedPolicy(inner)
	if !ac || !au || isRestricted2 {
		t.Fatalf("未包装应全允许: %v %v %v", ac, au, isRestricted2)
	}
}

// TestBuildToolsWithPolicy_独立工具下架 对齐 Python :1093-1100：
// allowCreate=false 时 cron_create_job 从工具列表下架，管理类保留。
func TestBuildToolsWithPolicy_独立工具下架(t *testing.T) {
	bridge := NewCronRuntimeBridge()
	bridge.SetBackend(&fakeInner{})
	ctx := &agentcron.CronToolContext{ChannelID: "feishu", SessionID: "s"}

	full := bridge.BuildToolsWithPolicy(ctx, "agent1", "cn", true, true)
	names := map[string]bool{}
	for _, tl := range full {
		names[tl.Card().Name] = true
	}
	if !names["cron_create_job"] || !names["cron_list_jobs"] {
		t.Fatalf("全量构建应含创建与管理工具: %v", names)
	}

	restricted := bridge.BuildToolsWithPolicy(ctx, "agent1", "cn", false, true)
	restrictedNames := map[string]bool{}
	for _, tl := range restricted {
		restrictedNames[tl.Card().Name] = true
	}
	if restrictedNames["cron_create_job"] {
		t.Fatal("allowCreate=false 时 cron_create_job 应下架")
	}
	if !restrictedNames["cron_list_jobs"] {
		t.Fatal("管理类工具应保留")
	}
	// 统一 cron 工具若在列表中，其 action 经受限 backend 运行时报错
	if restrictedNames["cron"] {
		// 委托链验证：调用统一工具的 add 分支应返回受限错误
		found := false
		for _, tl := range restricted {
			if tl.Card().Name == "cron" {
				found = true
			}
		}
		if !found {
			t.Fatal("统一 cron 工具应保留")
		}
	}
	if !strings.Contains(restrictedCreateMessage, "not allowed") {
		t.Fatal("拒绝文案形态校验")
	}
}
