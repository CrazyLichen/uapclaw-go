package lifecycle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupDataRoot 建独立的 agent 数据根并注入环境变量。
func setupDataRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", root)
	return filepath.Join(root, "agent")
}

// TestValidateID 白名单五类判据。
func TestValidateID(t *testing.T) {
	valid := []string{"sess-1", "abc", "_x", "a.b.c", strings.Repeat("a", 79)}
	for _, value := range valid {
		if _, err := ValidateID(value); err != nil {
			t.Errorf("ValidateID(%q) 应通过: %v", value, err)
		}
	}

	cases := []struct {
		name  string
		value string
		code  string
	}{
		{"空串", "", "BAD_REQUEST"},
		{"首尾空白", " abc", "BAD_REQUEST"},
		{"双点", "a..b", "BAD_REQUEST"},
		{"点结尾", "abc.", "BAD_REQUEST"},
		{"空格结尾", "abc ", "BAD_REQUEST"},
		{"正斜杠", "a/b", "BAD_REQUEST"},
		{"反斜杠", `a\b`, "BAD_REQUEST"},
		{"冒号", "a:b", "BAD_REQUEST"},
		{"星号", "a*b", "BAD_REQUEST"},
		{"控制字符", "a\x01b", "BAD_REQUEST"},
		{"CON 保留名", "CON", "BAD_REQUEST"},
		{"com1 保留名小写", "com1", "BAD_REQUEST"},
		{"CON.txt 保留名前缀", "CON.txt", "BAD_REQUEST"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateID(tc.value)
			if err == nil {
				t.Fatalf("ValidateID(%q) 应报错", tc.value)
			}
			lifecycleErr, ok := err.(*LifecycleError)
			if !ok {
				t.Fatalf("应为 LifecycleError，实际 %T", err)
			}
			if lifecycleErr.Code != tc.code {
				t.Errorf("code = %q, want %q", lifecycleErr.Code, tc.code)
			}
		})
	}
}

// TestResourcePath 资源路径与非法 kind。
func TestResourcePath(t *testing.T) {
	setupDataRoot(t)
	path, err := ResourcePath("session", "s1")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if filepath.Base(path) != "session_s1.json" {
		t.Errorf("文件名不符: %q", filepath.Base(path))
	}
	if filepath.Base(filepath.Dir(path)) != "resources" {
		t.Errorf("父目录应为 resources，实际 %q", filepath.Base(filepath.Dir(path)))
	}
	if _, err := ResourcePath("unknown", "s1"); err == nil {
		t.Error("非法 kind 应报错")
	}
	if _, err := ResourcePath("session", "../etc"); err == nil {
		t.Error("非法 ID 应报错")
	}
}

// TestReadWriteJSON 原子写读回一致，且不残留临时文件。
func TestReadWriteJSON(t *testing.T) {
	root := setupDataRoot(t)
	path := filepath.Join(root, "lifecycle", "resources", "session_s1.json")
	if err := AtomicJSON(path, map[string]any{"a": "b"}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	value, err := ReadJSON(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if value["a"] != "b" {
		t.Errorf("读回不符: %v", value)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Errorf("不应残留临时文件: %s", entry.Name())
		}
	}
}

// TestReadJSON_不存在与非法 不存在返回空，非法内容报 CONFLICT。
func TestReadJSON_不存在与非法(t *testing.T) {
	root := setupDataRoot(t)
	missing := filepath.Join(root, "nope.json")
	value, err := ReadJSON(missing)
	if err != nil || len(value) != 0 {
		t.Errorf("不存在应返回空 map，实际 %v / %v", value, err)
	}

	broken := filepath.Join(root, "broken.json")
	if err := os.MkdirAll(filepath.Dir(broken), 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(broken, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	if _, err := ReadJSON(broken); err == nil {
		t.Error("非法内容应报错")
	} else if lifecycleErr, ok := err.(*LifecycleError); !ok || lifecycleErr.Code != "CONFLICT" {
		t.Errorf("应为 CONFLICT，实际 %v", err)
	}
}

// TestBegin_新建操作 状态与代次正确。
func TestBegin_新建操作(t *testing.T) {
	setupDataRoot(t)
	operation, err := Begin("session", "s1", "delete", true)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if operation["kind"] != "delete" {
		t.Errorf("kind 不符: %v", operation["kind"])
	}
	if operation["status"] != OperationStatusRunning {
		t.Errorf("status 不符: %v", operation["status"])
	}
	if operation["phase"] != phaseBlock {
		t.Errorf("blockExecution 为真时 phase 应为 block，实际 %v", operation["phase"])
	}
	value := State("session", "s1")
	if asInt(value["generation"]) != 1 {
		t.Errorf("generation 应为 1，实际 %v", value["generation"])
	}
	if blocked, _ := value["blocked"].(bool); !blocked {
		t.Error("blockExecution 为真时 blocked 应为真")
	}
}

// TestBegin_同 kind 幂等 重复 begin 返回同一操作。
func TestBegin_同kind幂等(t *testing.T) {
	setupDataRoot(t)
	first, err := Begin("session", "s1", "delete", true)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	second, err := Begin("session", "s1", "delete", true)
	if err != nil {
		t.Fatalf("幂等重入不应报错: %v", err)
	}
	if first["operation_id"] != second["operation_id"] {
		t.Error("同 kind 重入应返回同一操作")
	}
}

// TestBegin_不同kind冲突 未完成操作冲突时报 OPERATION_IN_PROGRESS。
func TestBegin_不同kind冲突(t *testing.T) {
	setupDataRoot(t)
	if _, err := Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("首次不应报错: %v", err)
	}
	_, err := Begin("session", "s1", "archive", true)
	if err == nil {
		t.Fatal("不同 kind 应冲突")
	}
	if lifecycleErr, ok := err.(*LifecycleError); !ok || lifecycleErr.Code != "OPERATION_IN_PROGRESS" {
		t.Errorf("应为 OPERATION_IN_PROGRESS，实际 %v", err)
	}
}

// TestBegin_已删除后 删除幂等，其他动作 NOT_FOUND。
func TestBegin_已删除后(t *testing.T) {
	setupDataRoot(t)
	if _, err := Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if err := Complete("session", "s1", false, true, nil); err != nil {
		t.Fatalf("完成不应报错: %v", err)
	}
	if _, err := Begin("session", "s1", "delete", true); err != nil {
		t.Errorf("删除已完成后再删应幂等返回，实际 %v", err)
	}
	_, err := Begin("session", "s1", "archive", true)
	if err == nil {
		t.Fatal("已删除后归档应报错")
	}
	if lifecycleErr, ok := err.(*LifecycleError); !ok || lifecycleErr.Code != "NOT_FOUND" {
		t.Errorf("应为 NOT_FOUND，实际 %v", err)
	}
}

// TestClaimOperation 换主时递增代次。
func TestClaimOperation(t *testing.T) {
	setupDataRoot(t)
	if _, err := Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	first, err := ClaimOperation("session", "s1", "owner-a")
	if err != nil {
		t.Fatalf("认领不应报错: %v", err)
	}
	if first["owner_id"] != "owner-a" {
		t.Errorf("owner 不符: %v", first["owner_id"])
	}
	before := asInt(State("session", "s1")["generation"])

	second, err := ClaimOperation("session", "s1", "owner-b")
	if err != nil {
		t.Fatalf("换主不应报错: %v", err)
	}
	if second["owner_id"] != "owner-b" {
		t.Errorf("owner 应切换: %v", second["owner_id"])
	}
	after := asInt(State("session", "s1")["generation"])
	if after != before+1 {
		t.Errorf("换主应递增代次: %d → %d", before, after)
	}
}

// TestRenewOperation 续租与释放。
func TestRenewOperation(t *testing.T) {
	setupDataRoot(t)
	if _, err := Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if _, err := ClaimOperation("session", "s1", "owner-a"); err != nil {
		t.Fatalf("认领失败: %v", err)
	}
	if err := RenewOperation("session", "s1", "owner-a", false); err != nil {
		t.Fatalf("续租不应报错: %v", err)
	}
	operation, _ := State("session", "s1")["operation"].(map[string]any)
	if asFloat(operation["lease_expires_at"]) <= 0 {
		t.Error("续租后应有到期时刻")
	}
	if err := RenewOperation("session", "s1", "owner-a", true); err != nil {
		t.Fatalf("释放不应报错: %v", err)
	}
	operation, _ = State("session", "s1")["operation"].(map[string]any)
	if asFloat(operation["lease_expires_at"]) != 0 {
		t.Error("释放后到期时刻应清零")
	}
	// 非归属者续租无效
	if err := RenewOperation("session", "s1", "owner-x", false); err != nil {
		t.Fatalf("非归属者续租不应报错: %v", err)
	}
}

// TestUpdate 更新操作字段。
func TestUpdate(t *testing.T) {
	setupDataRoot(t)
	if _, err := Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	operation, err := Update("session", "s1", map[string]any{"phase": "cleanup"})
	if err != nil {
		t.Fatalf("更新不应报错: %v", err)
	}
	if operation["phase"] != "cleanup" {
		t.Errorf("phase 未写回: %v", operation["phase"])
	}
}

// TestComplete 完成后的可见性。
func TestComplete(t *testing.T) {
	setupDataRoot(t)
	if _, err := Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if err := Complete("session", "s1", false, true, map[string]any{"ok": true}); err != nil {
		t.Fatalf("完成不应报错: %v", err)
	}
	value := State("session", "s1")
	if asBoolField(value, "deleted") != true {
		t.Error("deleted 应落盘")
	}
	if asBoolField(value, "blocked") != true {
		t.Error("删除后 blocked 应为真")
	}
	operation, _ := value["operation"].(map[string]any)
	if operation["status"] != OperationStatusCompleted {
		t.Errorf("status 应为 completed，实际 %v", operation["status"])
	}
}

// TestLogicallyArchived 归档中的判定。
func TestLogicallyArchived(t *testing.T) {
	if LogicallyArchived(map[string]any{}) {
		t.Error("无操作不应判为归档中")
	}
	if !LogicallyArchived(map[string]any{
		"operation": map[string]any{"kind": "archive", "status": "running"},
	}) {
		t.Error("未完成归档应判为已逻辑归档")
	}
	if LogicallyArchived(map[string]any{
		"operation": map[string]any{"kind": "archive", "status": "completed"},
	}) {
		t.Error("已完成归档不再判为归档中")
	}
}

// TestMarkFinalizing 收尾阶段字段。
func TestMarkFinalizing(t *testing.T) {
	setupDataRoot(t)
	if _, err := Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	operation, err := MarkFinalizing("session", "s1", true, nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if operation["phase"] != phaseFinalizing {
		t.Errorf("phase 应为 finalizing，实际 %v", operation["phase"])
	}
	if asBoolField(operation, "stop_pending") != true {
		t.Error("stop_pending 应为真")
	}
	value := State("session", "s1")
	if asBoolField(value, "deleted") != true {
		t.Error("deleted 应落盘")
	}
}

// TestAbandonFailedOperation 失败操作可被放弃。
func TestAbandonFailedOperation(t *testing.T) {
	setupDataRoot(t)
	if _, err := Begin("session", "s1", "archive", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if _, err := Update("session", "s1", map[string]any{"status": OperationStatusFailed}); err != nil {
		t.Fatalf("更新失败状态出错: %v", err)
	}
	abandoned, err := AbandonFailedOperation("session", "s1", "archive")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !abandoned {
		t.Error("失败操作应可放弃")
	}
	// 未完成操作不可放弃
	if _, err := Begin("session", "s2", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	abandoned, err = AbandonFailedOperation("session", "s2", "delete")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if abandoned {
		t.Error("running 操作不应被放弃")
	}
}

// TestSetVisibility 可见性改写。
func TestSetVisibility(t *testing.T) {
	setupDataRoot(t)
	if err := SetVisibility("session", "s1", true, nil, false); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	value := State("session", "s1")
	if asBoolField(value, "blocked") != true {
		t.Error("blocked 应为真")
	}
	if asBoolField(value, "write_blocked") != true {
		t.Error("write_blocked 应跟随 blocked")
	}
	custom := false
	if err := SetVisibility("session", "s1", false, &custom, false); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	value = State("session", "s1")
	if asBoolField(value, "blocked") != false {
		t.Error("blocked 应为假")
	}
	if asBoolField(value, "deleted") != false {
		t.Error("解除阻断后 deleted 应清零")
	}
}

// TestSessionDeletePending 删除中判定。
func TestSessionDeletePending(t *testing.T) {
	setupDataRoot(t)
	if SessionDeletePending("s1") {
		t.Error("无操作时不应为删除中")
	}
	if _, err := Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !SessionDeletePending("s1") {
		t.Error("未完成删除应为删除中")
	}
	if err := Complete("session", "s1", false, true, nil); err != nil {
		t.Fatalf("完成不应报错: %v", err)
	}
	if SessionDeletePending("s1") {
		t.Error("完成后不应为删除中")
	}
}

// TestResourceLock_可重入 同协程重复获取不阻塞。
func TestResourceLock_可重入(t *testing.T) {
	setupDataRoot(t)
	release, err := ResourceLock("session", "s1")
	if err != nil {
		t.Fatalf("加锁失败: %v", err)
	}
	innerRelease, err := ResourceLock("session", "s1")
	if err != nil {
		t.Fatalf("可重入加锁失败: %v", err)
	}
	innerRelease()
	release()
}

// TestSaveLocked_双写 资源文件与操作日志都落盘。
func TestSaveLocked_双写(t *testing.T) {
	setupDataRoot(t)
	if _, err := Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	operation, _ := State("session", "s1")["operation"].(map[string]any)
	operationID, _ := operation["operation_id"].(string)
	if operationID == "" {
		t.Fatal("操作应有 ID")
	}
	if !pathExists(operationLogPath(operationID)) {
		t.Errorf("操作日志应落盘: %s", operationLogPath(operationID))
	}
}

// TestGuard 准入判定三分支。
func TestGuard(t *testing.T) {
	setupDataRoot(t)
	if err := Guard("s1", ""); err != nil {
		t.Errorf("无操作应放行: %v", err)
	}
	if _, err := Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	err := Guard("s1", "")
	if err == nil {
		t.Fatal("操作进行中应拒绝")
	}
	if lifecycleErr, ok := err.(*LifecycleError); !ok || lifecycleErr.Code != "OPERATION_IN_PROGRESS" {
		t.Errorf("应为 OPERATION_IN_PROGRESS，实际 %v", err)
	}
	if err := Complete("session", "s1", false, true, nil); err != nil {
		t.Fatalf("完成不应报错: %v", err)
	}
	err = Guard("s1", "")
	if err == nil {
		t.Fatal("已删除应拒绝")
	}
	if lifecycleErr, ok := err.(*LifecycleError); !ok || lifecycleErr.Code != "NOT_FOUND" {
		t.Errorf("应为 NOT_FOUND，实际 %v", err)
	}
}

// TestWriteGuard 写准入：写围栏与代次。
func TestWriteGuard(t *testing.T) {
	setupDataRoot(t)
	if err := WriteGuard("s1", nil); err != nil {
		t.Errorf("无围栏应放行: %v", err)
	}
	if _, err := Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if err := FenceWrites("session", "s1"); err != nil {
		t.Fatalf("加围栏不应报错: %v", err)
	}
	if err := WriteGuard("s1", nil); err == nil {
		t.Error("写围栏后应拒绝")
	}
}

// TestWriteGuard_代次 过期代次被拒。
func TestWriteGuard_代次(t *testing.T) {
	setupDataRoot(t)
	if _, err := Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	current := asInt(State("session", "s1")["generation"])
	if err := WriteGuard("s1", &current); err != nil {
		t.Errorf("当前代次应放行: %v", err)
	}
	stale := current - 5
	if err := WriteGuard("s1", &stale); err == nil {
		t.Error("过期代次应拒绝")
	}
}

// TestParseIDs 批量解析。
func TestParseIDs(t *testing.T) {
	ids, err := ParseIDs(map[string]any{"session_ids": []any{"a", "b", "a"}}, false)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("应去重为 2 条，实际 %d", len(ids))
	}
	ids, err = ParseIDs(map[string]any{"session_id": "a"}, true)
	if err != nil || len(ids) != 1 || ids[0] != "a" {
		t.Errorf("delete 单条解析不符: %v / %v", ids, err)
	}
	if _, err := ParseIDs(map[string]any{}, false); err == nil {
		t.Error("无 ID 应报错")
	}
	if _, err := ParseIDs(map[string]any{"session_ids": []any{"../x"}}, false); err == nil {
		t.Error("非法 ID 应报错")
	}
	tooMany := make([]any, 101)
	for i := range tooMany {
		tooMany[i] = "s"
	}
	if _, err := ParseIDs(map[string]any{"session_ids": tooMany}, false); err == nil {
		t.Error("超过 100 条应报错")
	}
}

// TestPage 过滤排序分页。
func TestPage(t *testing.T) {
	items := []map[string]any{
		{"session_id": "a", "title": "Alpha", "archived_at": 1.0, "work_mode": "work"},
		{"session_id": "b", "title": "Beta", "archived_at": 3.0, "work_mode": "code"},
		{"session_id": "c", "title": "Alpha Two", "archived_at": 2.0, "work_mode": "work"},
	}
	result, err := Page(items, map[string]any{"limit": float64(2)}, "sessions", 100)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	selected, _ := result["sessions"].([]map[string]any)
	if len(selected) != 2 {
		t.Fatalf("应返回 2 条，实际 %d", len(selected))
	}
	// archived_at 倒序：b(3) 在前
	if selected[0]["session_id"] != "b" {
		t.Errorf("应按 archived_at 倒序，首条应为 b，实际 %v", selected[0]["session_id"])
	}
	if result["total"] != 3 {
		t.Errorf("total 应为 3，实际 %v", result["total"])
	}
	if result["has_more"] != true {
		t.Error("应有下一页")
	}

	// work_mode 过滤
	result, err = Page(items, map[string]any{"work_mode": "work"}, "sessions", 100)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	selected, _ = result["sessions"].([]map[string]any)
	if len(selected) != 2 {
		t.Errorf("work 模式应 2 条，实际 %d", len(selected))
	}

	// keyword 过滤（大小写不敏感）
	result, err = Page(items, map[string]any{"keyword": "alpha"}, "sessions", 100)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	selected, _ = result["sessions"].([]map[string]any)
	if len(selected) != 2 {
		t.Errorf("keyword alpha 应 2 条，实际 %d", len(selected))
	}
}

// TestPage_参数校验 非法分页与过滤参数。
func TestPage_参数校验(t *testing.T) {
	items := []map[string]any{}
	if _, err := Page(items, map[string]any{"limit": float64(0)}, "sessions", 100); err == nil {
		t.Error("limit=0 应报错")
	}
	if _, err := Page(items, map[string]any{"limit": float64(200)}, "sessions", 100); err == nil {
		t.Error("limit 超上限应报错")
	}
	if _, err := Page(items, map[string]any{"offset": float64(-1)}, "sessions", 100); err == nil {
		t.Error("offset 负值应报错")
	}
	if _, err := Page(items, map[string]any{"work_mode": "bad"}, "sessions", 100); err == nil {
		t.Error("非法 work_mode 应报错")
	}
	if _, err := Page(items, map[string]any{"keyword": 42}, "sessions", 100); err == nil {
		t.Error("非字符串 keyword 应报错")
	}
}

// TestAsPageInt 三种输入形态。
func TestAsPageInt(t *testing.T) {
	cases := []struct {
		value  any
		want   int
		wantOK bool
	}{
		{nil, 20, true},
		{float64(5), 5, true},
		{float64(5.5), 0, false},
		{"7", 7, true},
		{"-3", -3, true},
		{"abc", 0, false},
		{true, 0, false},
	}
	for _, tc := range cases {
		got, ok := asPageInt(tc.value, 20)
		if ok != tc.wantOK || (ok && got != tc.want) {
			t.Errorf("asPageInt(%v) = (%d,%v), want (%d,%v)", tc.value, got, ok, tc.want, tc.wantOK)
		}
	}
}

// TestProjectIDFor 项目推断三级回退。
func TestProjectIDFor(t *testing.T) {
	if got := ProjectIDFor(map[string]any{"project_id": "p1"}, nil); got != "p1" {
		t.Errorf("直取 project_id，实际 %q", got)
	}
	if got := ProjectIDFor(map[string]any{"work_mode": "code"}, nil); got != "default_code" {
		t.Errorf("code 模式应落 default_code，实际 %q", got)
	}
	if got := ProjectIDFor(map[string]any{}, nil); got != "default" {
		t.Errorf("空元数据应落 default，实际 %q", got)
	}
	if got := ProjectIDFor(nil, nil); got != "default" {
		t.Errorf("nil 应落 default，实际 %q", got)
	}
}

// TestNormalizePathForMatch 路径规范化。
func TestNormalizePathForMatch(t *testing.T) {
	if got := NormalizePathForMatch(`C:\Work\Proj\`); got != "c:/work/proj" {
		t.Errorf("反斜杠路径规范化不符: %q", got)
	}
	if got := NormalizePathForMatch("/a/b/../c"); got != "/a/c" {
		t.Errorf("应 Clean 掉 ..: %q", got)
	}
}

// TestSetProjectSource 未注入与已注入两种形态。
func TestSetProjectSource(t *testing.T) {
	SetProjectSource(nil)
	if projects := ListProjects(); len(projects) != 0 {
		t.Errorf("未注入应返回空列表，实际 %d", len(projects))
	}

	SetProjectSource(fakeProjectSource{records: []ProjectRecord{
		{ProjectID: "p1", ProjectDir: "/work/a", WorkMode: "work"},
	}})
	projects := ListProjects()
	if len(projects) != 1 || projects[0].ProjectID != "p1" {
		t.Errorf("注入后应返回 1 条，实际 %v", projects)
	}
	lookup, err := BuildProjectLookup()
	if err != nil {
		t.Fatalf("构建反查表不应报错: %v", err)
	}
	if len(lookup.ByDirectory) != 1 {
		t.Errorf("反查表应有 1 个目录，实际 %d", len(lookup.ByDirectory))
	}
	if lookup.WorkModes["p1"] != "work" {
		t.Errorf("工作模式应记录，实际 %q", lookup.WorkModes["p1"])
	}
	SetProjectSource(nil)
}

// fakeProjectSource 测试用项目存储。
type fakeProjectSource struct {
	// records 预设项目
	records []ProjectRecord
}

// ListProjects 返回预设项目。
func (f fakeProjectSource) ListProjects() ([]ProjectRecord, error) { return f.records, nil }

// asBoolField 读布尔字段（测试辅助）。
func asBoolField(value map[string]any, key string) bool {
	boolean, _ := value[key].(bool)
	return boolean
}

// TestSessionPaths 活动与归档目录。
func TestSessionPaths(t *testing.T) {
	root := setupDataRoot(t)
	sessionsRoot := filepath.Join(root, "sessions")
	active, archived, err := SessionPaths("s1", sessionsRoot)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if active != filepath.Join(sessionsRoot, "s1") {
		t.Errorf("活动目录不符: %q", active)
	}
	if archived != filepath.Join(filepath.Dir(sessionsRoot), "sessions_archived", "s1") {
		t.Errorf("归档目录不符: %q", archived)
	}
}

// TestSessionPaths_双存冲突 同一 ID 同时在两区时报冲突。
func TestSessionPaths_双存冲突(t *testing.T) {
	root := setupDataRoot(t)
	sessionsRoot := filepath.Join(root, "sessions")
	for _, path := range []string{
		filepath.Join(sessionsRoot, "s1"),
		filepath.Join(filepath.Dir(sessionsRoot), "sessions_archived", "s1"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("建目录失败: %v", err)
		}
	}
	_, _, err := SessionPaths("s1", sessionsRoot)
	if err == nil {
		t.Fatal("双存应报冲突")
	}
	if lifecycleErr, ok := err.(*LifecycleError); !ok || lifecycleErr.Code != "SESSION_ID_CONFLICT" {
		t.Errorf("应为 SESSION_ID_CONFLICT，实际 %v", err)
	}
}

// TestSessionPaths_符号链接 符号链接扩大范围时拒绝。
func TestSessionPaths_符号链接(t *testing.T) {
	root := setupDataRoot(t)
	sessionsRoot := filepath.Join(root, "sessions")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(sessionsRoot, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("建外部目录失败: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(sessionsRoot, "s1")); err != nil {
		t.Skip("平台不支持符号链接")
	}
	_, _, err := SessionPaths("s1", sessionsRoot)
	if err == nil {
		t.Fatal("符号链接应被拒绝")
	}
}

// TestResolveSession 归档优先与不存在。
func TestResolveSession(t *testing.T) {
	root := setupDataRoot(t)
	sessionsRoot := filepath.Join(root, "sessions")
	active := filepath.Join(sessionsRoot, "s1")
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	resolved, err := ResolveSession("s1", true, sessionsRoot)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if resolved != active {
		t.Errorf("应解析到活动目录: %q", resolved)
	}
	if _, err := ResolveSession("nope", true, sessionsRoot); err == nil {
		t.Error("不存在应报 NOT_FOUND")
	}
	notExist, err := ResolveSession("nope", false, sessionsRoot)
	if err != nil {
		t.Fatalf("mustExist=false 不应报错: %v", err)
	}
	if notExist == "" {
		t.Error("应返回活动区路径")
	}
}

// TestRawMetadata 读元数据（含锁）。
func TestRawMetadata(t *testing.T) {
	root := setupDataRoot(t)
	sessionDir := filepath.Join(root, "sessions", "s1")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	meta := map[string]any{"session_id": "s1", "title": "T"}
	if err := AtomicJSON(filepath.Join(sessionDir, "metadata.json"), meta); err != nil {
		t.Fatalf("写元数据失败: %v", err)
	}
	got, err := RawMetadata("s1")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if got["title"] != "T" {
		t.Errorf("元数据不符: %v", got)
	}
}

// TestClaimReleaseRuntime 运行时归属登记与释放。
func TestClaimReleaseRuntime(t *testing.T) {
	setupDataRoot(t)
	if err := ClaimRuntime("s1"); err != nil {
		t.Fatalf("认领不应报错: %v", err)
	}
	owner, _ := State("session", "s1")["runtime_owner"].(map[string]any)
	if asInt(owner["pid"]) != os.Getpid() {
		t.Errorf("归属 pid 不符: %v", owner["pid"])
	}
	if err := ReleaseRuntime("s1"); err != nil {
		t.Fatalf("释放不应报错: %v", err)
	}
	if _, exists := State("session", "s1")["runtime_owner"]; exists {
		t.Error("释放后不应再有 runtime_owner")
	}
}

// TestAssertRuntimeOwner_本进程 同进程直接放行。
func TestAssertRuntimeOwner_本进程(t *testing.T) {
	setupDataRoot(t)
	if err := ClaimRuntime("s1"); err != nil {
		t.Fatalf("认领不应报错: %v", err)
	}
	if err := AssertRuntimeOwner("s1"); err != nil {
		t.Errorf("本进程应放行: %v", err)
	}
}

// TestVisible 归档后不可见。
func TestVisible(t *testing.T) {
	root := setupDataRoot(t)
	if !Visible(map[string]any{"session_id": "s1"}) {
		t.Error("正常会话应可见")
	}
	archived := filepath.Join(root, "sessions_archived", "s1")
	if err := os.MkdirAll(archived, 0o755); err != nil {
		t.Fatalf("建归档目录失败: %v", err)
	}
	if Visible(map[string]any{"session_id": "s1"}) {
		t.Error("已归档会话应不可见")
	}
	if !Visible(map[string]any{}) {
		t.Error("无 session_id 时应可见")
	}
}

// TestProjection 投影字段。
func TestProjection(t *testing.T) {
	setupDataRoot(t)
	if _, err := Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	result := Projection("session", "s1", "", nil, nil, nil)
	if result["execution_blocked"] != true {
		t.Error("操作进行中应阻断执行")
	}
	if result["lifecycle_operation"] == nil {
		t.Error("应有 lifecycle_operation")
	}

	// 完成后投影不含操作
	if err := Complete("session", "s1", false, false, nil); err != nil {
		t.Fatalf("完成不应报错: %v", err)
	}
	result = Projection("session", "s1", "", nil, nil, nil)
	if result["lifecycle_operation"] != nil {
		t.Error("完成后不应有 lifecycle_operation")
	}
}

// TestProjection_归档目录阻断 归档目录存在时阻断。
func TestProjection_归档目录阻断(t *testing.T) {
	root := setupDataRoot(t)
	archived := filepath.Join(root, "sessions_archived", "s1")
	if err := os.MkdirAll(archived, 0o755); err != nil {
		t.Fatalf("建归档目录失败: %v", err)
	}
	result := Projection("session", "s1", "", nil, nil, nil)
	if result["execution_blocked"] != true {
		t.Error("归档目录存在时应阻断")
	}
}

// TestEventSnapshots 事件快照。
func TestEventSnapshots(t *testing.T) {
	setupDataRoot(t)
	if _, err := Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	snapshots, err := EventSnapshots()
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if len(snapshots) == 0 {
		t.Fatal("应有快照")
	}
	found := false
	for _, snapshot := range snapshots {
		if snapshot["event"] == "session.lifecycle.updated" {
			found = true
			payload, _ := snapshot["payload"].(map[string]any)
			if payload["session_id"] != "s1" {
				t.Errorf("payload 应含 session_id，实际 %v", payload)
			}
		}
	}
	if !found {
		t.Error("应含 session.lifecycle.updated 事件")
	}
}

// TestEventSnapshots_空目录 无目录时返回空。
func TestEventSnapshots_空目录(t *testing.T) {
	setupDataRoot(t)
	snapshots, err := EventSnapshots()
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if len(snapshots) != 0 {
		t.Errorf("空目录应返回 0 条，实际 %d", len(snapshots))
	}
}

// TestFenceWrites_project级联 project 围栏级联到成员会话。
func TestFenceWrites_project级联(t *testing.T) {
	root := setupDataRoot(t)
	sessionsRoot := filepath.Join(root, "sessions")
	sessionDir := filepath.Join(sessionsRoot, "s1")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := AtomicJSON(filepath.Join(sessionDir, "metadata.json"),
		map[string]any{"session_id": "s1", "project_id": "p1"}); err != nil {
		t.Fatalf("写元数据失败: %v", err)
	}
	if _, err := Begin("session", "s1", "", true); err != nil {
		t.Logf("session begin 返回: %v", err)
	}
	before := asInt(State("session", "s1")["generation"])

	if err := FenceWrites("project", "p1"); err != nil {
		t.Fatalf("项目围栏不应报错: %v", err)
	}
	if asBoolField(State("project", "p1"), "write_blocked") != true {
		t.Error("项目应被加写围栏")
	}
	after := asInt(State("session", "s1")["generation"])
	if after <= before {
		t.Errorf("级联应递增会话代次: %d → %d", before, after)
	}
}

// TestGetAgentDirs 数据根与会话根。
func TestGetAgentDirs(t *testing.T) {
	root := setupDataRoot(t)
	if got := GetAgentRootDir(); got != root {
		t.Errorf("数据根不符: %q", got)
	}
	if got := GetAgentSessionsDir(); got != filepath.Join(root, "sessions") {
		t.Errorf("会话根不符: %q", got)
	}
}

// TestGetAgentRootDir_无环变 HOME 兜底到用户目录。
func TestGetAgentRootDir_无环变(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", "")
	got := GetAgentRootDir()
	if got == "" {
		t.Fatal("应有兜底路径")
	}
	if !strings.Contains(got, "agent") {
		t.Errorf("应含 agent 段: %q", got)
	}
}

// TestLogicallyDeleted 删除标记。
func TestLogicallyDeleted(t *testing.T) {
	if LogicallyDeleted(map[string]any{}) {
		t.Error("空值不应判为已删")
	}
	if !LogicallyDeleted(map[string]any{"deleted": true}) {
		t.Error("deleted=true 应判为已删")
	}
}

// TestAssertRuntimeOwner_他进程已死 归属进程不存在时放行。
func TestAssertRuntimeOwner_他进程已死(t *testing.T) {
	setupDataRoot(t)
	// 直接构造一个不存在的 pid 作为归属
	if _, err := Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	value := State("session", "s1")
	value["runtime_owner"] = map[string]any{"pid": 999999, "started_at": 0.0}
	SaveLocked("session", "s1", value)

	if err := AssertRuntimeOwner("s1"); err != nil {
		t.Errorf("归属进程已死应放行: %v", err)
	}
}

// TestAsFloat 数值转换。
func TestAsFloat(t *testing.T) {
	cases := []struct {
		value any
		want  float64
	}{
		{float64(1.5), 1.5},
		{int(2), 2.0},
		{int64(3), 3.0},
		{"x", 0},
		{nil, 0},
	}
	for _, tc := range cases {
		if got := asFloat(tc.value); got != tc.want {
			t.Errorf("asFloat(%v) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

// TestAbandonFailedOperation_非session 非 session kind 也走通。
func TestAbandonFailedOperation_非session(t *testing.T) {
	setupDataRoot(t)
	if _, err := Begin("project", "p1", "archive", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if _, err := Update("project", "p1", map[string]any{"status": OperationStatusFailed}); err != nil {
		t.Fatalf("更新失败状态出错: %v", err)
	}
	abandoned, err := AbandonFailedOperation("project", "p1", "archive")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !abandoned {
		t.Error("失败项目操作应可放弃")
	}
}

// TestComplete_归档 归档完成后的可见性。
func TestComplete_归档(t *testing.T) {
	setupDataRoot(t)
	if _, err := Begin("session", "s1", "archive", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if err := Complete("session", "s1", true, false, map[string]any{"moved": true}); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	value := State("session", "s1")
	if asBoolField(value, "blocked") != true {
		t.Error("归档后应阻断")
	}
	if asBoolField(value, "deleted") != false {
		t.Error("归档不是删除")
	}
}

// TestMarkFinalizing_未开始 无操作时返回空。
func TestMarkFinalizing_未开始(t *testing.T) {
	setupDataRoot(t)
	operation, err := MarkFinalizing("session", "nope", true, nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if len(operation) != 0 {
		t.Errorf("无操作应返回空 map，实际 %v", operation)
	}
}
