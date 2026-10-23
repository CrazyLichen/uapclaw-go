package routing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// setupInteractionsEnv 隔离 interactions 目录环境（memoryrpc 先例）
func setupInteractionsEnv(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmp)
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)
	return tmp
}

// newTestPI 构造测试用完整 PendingInteraction（15 字段全填）
func newTestPI(id string) *PendingInteraction {
	return &PendingInteraction{
		InteractionID:    id,
		Mode:             "group",
		OriginChannelID:  "feishu_enterprise:app1",
		OriginSessionID:  "sess-1",
		OriginContent:    "帮我订会议室",
		OriginSenderName: "张三",
		OriginSenderID:   "ou-zhangsan",
		Question:         "需要哪个楼层？",
		TargetUserID:     "ou-zhangsan",
		TargetUserName:   "张三",
		OriginMetadata:   map[string]any{"api_key": "sk-secret", "Token": "tok", "AUTHORIZATION": "Bearer x", "keep": "v"},
		PrincipalID:      "ou-principal",
		Status:           "pending",
		CreatedAt:        1700000000.0,
		UpdatedAt:        1700000000.0,
	}
}

// TestSave_落盘与敏感键过滤 Save 写文件内容断言（api_key/Token/AUTHORIZATION 大小写三态剔除）
func TestSave_落盘与敏感键过滤(t *testing.T) {
	setupInteractionsEnv(t)

	pi := newTestPI("gpq_sess-1_ou-zhangsan")
	pi.Save()

	// 文件应存在
	fp := filepath.Join(pathutil.AgentInteractionsDir(), "gpq_sess-1_ou-zhangsan.json")
	raw, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("save 后文件应存在: %v", err)
	}

	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("应为合法 JSON: %v", err)
	}

	// 敏感键剔除（小写比较三键）
	meta, _ := data["origin_metadata"].(map[string]any)
	if meta == nil {
		t.Fatalf("origin_metadata 应为 map: %v", data["origin_metadata"])
	}
	for _, k := range []string{"api_key", "Token", "AUTHORIZATION"} {
		if _, ok := meta[k]; ok {
			t.Errorf("敏感键 %q 应被剔除", k)
		}
	}
	if meta["keep"] != "v" {
		t.Errorf("非敏感键 keep 应保留, 实际 %v", meta["keep"])
	}

	// updated_at 被刷新为 now（>= 保存前时刻）
	updatedAt, _ := data["updated_at"].(float64)
	if updatedAt < float64(time.Now().Unix())-2 {
		t.Errorf("updated_at 应刷新为当前时间, 实际 %v", updatedAt)
	}

	// 15 字段全量断言
	if data["interaction_id"] != "gpq_sess-1_ou-zhangsan" || data["mode"] != "group" ||
		data["status"] != "pending" || data["question"] != "需要哪个楼层？" {
		t.Errorf("核心字段不匹配: %+v", data)
	}

	// created_at 零值时补 now（dataclass default_factory 对齐）
	pi2 := newTestPI("gpq_zero_time")
	pi2.CreatedAt = 0
	pi2.UpdatedAt = 0
	pi2.Save()
	raw2, _ := os.ReadFile(filepath.Join(pathutil.AgentInteractionsDir(), "gpq_zero_time.json"))
	var data2 map[string]any
	_ = json.Unmarshal(raw2, &data2)
	createdAt, _ := data2["created_at"].(float64)
	if createdAt < float64(time.Now().Unix())-2 {
		t.Errorf("created_at 零值应补当前时间, 实际 %v", createdAt)
	}
}

// TestSave_失败仅Warn 目录路径被同名文件占据 → MkdirAll 失败不 panic
func TestSave_失败仅Warn(t *testing.T) {
	tmp := setupInteractionsEnv(t)
	// 用同名文件占据 interactions 目录路径
	dir := filepath.Join(tmp, "agent", "workspace", "interactions")
	_ = os.MkdirAll(filepath.Dir(dir), 0o755)
	if err := os.WriteFile(dir, []byte("occupied"), 0o644); err != nil {
		t.Fatalf("预置失败: %v", err)
	}

	pi := newTestPI("gpq_any")
	pi.Save() // 不 panic 即通过
}

// TestRemove_存在与不存在 存在删除 + 不存在静默
func TestRemove_存在与不存在(t *testing.T) {
	setupInteractionsEnv(t)

	pi := newTestPI("gpq_rm_1")
	pi.Save()
	pi.Remove()

	fp := filepath.Join(pathutil.AgentInteractionsDir(), "gpq_rm_1.json")
	if _, err := os.Stat(fp); !os.IsNotExist(err) {
		t.Errorf("remove 后文件应不存在")
	}

	// 不存在时静默（不 panic 不报错）
	pi2 := newTestPI("gpq_rm_missing")
	pi2.Remove()
}
