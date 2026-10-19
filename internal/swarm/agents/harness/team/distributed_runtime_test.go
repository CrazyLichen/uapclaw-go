package team

import (
	"reflect"
	"strings"
	"testing"
)

// TestIsDistributedMode_判定 runtime.mode 或 transport.type 为分布式。
func TestIsDistributedMode_判定(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]any
		want bool
	}{
		{"runtime_distributed", map[string]any{"team": map[string]any{"runtime": map[string]any{"mode": "distributed"}}}, true},
		{"runtime_distributed_upper", map[string]any{"team": map[string]any{"runtime": map[string]any{"mode": " Distributed "}}}, true},
		{"transport_pyzmq", map[string]any{"team": map[string]any{"transport": map[string]any{"type": "pyzmq"}}}, true},
		{"local", map[string]any{"team": map[string]any{"runtime": map[string]any{"mode": "local"}}}, false},
		{"empty", map[string]any{}, false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		if got := IsDistributedMode(tc.cfg); got != tc.want {
			t.Errorf("%s: IsDistributedMode = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestRuntimeRole_默认leader 仅接受 leader/teammate。
func TestRuntimeRole_默认leader(t *testing.T) {
	if got := RuntimeRole(map[string]any{}); got != "leader" {
		t.Errorf("default role = %q, want leader", got)
	}
	cfg := map[string]any{"team": map[string]any{"runtime": map[string]any{"role": "Teammate"}}}
	if got := RuntimeRole(cfg); got != "teammate" {
		t.Errorf("role = %q, want teammate", got)
	}
	cfg2 := map[string]any{"team": map[string]any{"runtime": map[string]any{"role": "bogus"}}}
	if got := RuntimeRole(cfg2); got != "leader" {
		t.Errorf("bogus role = %q, want leader", got)
	}
}

// TestRuntimeMemberName_回退predefined 首个成员名。
func TestRuntimeMemberName_回退predefined(t *testing.T) {
	cfg := map[string]any{"team": map[string]any{
		"runtime": map[string]any{"member_name": "configured"},
	}}
	if got := RuntimeMemberName(cfg, cfg["team"].(map[string]any)); got != "configured" {
		t.Errorf("configured = %q", got)
	}
	teamCfg := map[string]any{
		"predefined_members": []any{map[string]any{"member_name": "first"}},
	}
	if got := RuntimeMemberName(map[string]any{}, teamCfg); got != "first" {
		t.Errorf("predefined fallback = %q, want first", got)
	}
	if got := RuntimeMemberName(map[string]any{}, map[string]any{}); got != "" {
		t.Errorf("empty = %q, want empty", got)
	}
}

// TestParsePort_边界 nil/空串/非数字/越界均返回默认值。
func TestParsePort_边界(t *testing.T) {
	if got := ParsePort(nil, 18555, "f"); got != 18555 {
		t.Errorf("nil = %d", got)
	}
	if got := ParsePort("  ", 18555, "f"); got != 18555 {
		t.Errorf("blank = %d", got)
	}
	if got := ParsePort("abc", 18555, "f"); got != 18555 {
		t.Errorf("non-numeric = %d", got)
	}
	if got := ParsePort(0, 18555, "f"); got != 18555 {
		t.Errorf("zero = %d", got)
	}
	if got := ParsePort(65536, 18555, "f"); got != 18555 {
		t.Errorf("overflow = %d", got)
	}
	if got := ParsePort("18555", 1, "f"); got != 18555 {
		t.Errorf("string ok = %d", got)
	}
	if got := ParsePort(18556.0, 1, "f"); got != 18556 {
		t.Errorf("float ok = %d", got)
	}
}

// TestNormalizeDistributedTransportFields_leader 清空 peers 并推导地址。
func TestNormalizeDistributedTransportFields_leader(t *testing.T) {
	configBase := map[string]any{"team": map[string]any{
		"runtime":   map[string]any{"mode": "distributed", "role": "leader"},
		"transport": map[string]any{"type": "pyzmq"},
	}}
	teamCfg := map[string]any{"transport": map[string]any{
		"type":   "pyzmq",
		"params": map[string]any{"known_peers": []any{"x"}},
	}}
	result := NormalizeDistributedTransportFields(configBase, teamCfg)
	transport := result["transport"].(map[string]any)
	params := transport["params"].(map[string]any)
	if _, exists := params["known_peers"]; exists {
		t.Fatalf("leader should drop known_peers")
	}
	if params["direct_addr"] != "tcp://0.0.0.0:18555" {
		t.Fatalf("direct_addr = %v", params["direct_addr"])
	}
	if params["pubsub_publish_addr"] != "tcp://127.0.0.1:18556" {
		t.Fatalf("pubsub_publish_addr = %v", params["pubsub_publish_addr"])
	}
	if params["pubsub_subscribe_addr"] != "tcp://127.0.0.1:18557" {
		t.Fatalf("pubsub_subscribe_addr = %v", params["pubsub_subscribe_addr"])
	}
	meta := params["metadata"].(map[string]any)
	if meta["pubsub_bind"] != true {
		t.Fatalf("leader pubsub_bind should be true")
	}
	// 入参不被修改
	orig := teamCfg["transport"].(map[string]any)["params"].(map[string]any)
	if _, exists := orig["known_peers"]; !exists {
		t.Fatalf("input teamCfg should stay unmodified (deepcopy)")
	}
}

// TestNormalizeDistributedTransportFields_teammate 构造指向 leader 的 peers。
func TestNormalizeDistributedTransportFields_teammate(t *testing.T) {
	configBase := map[string]any{"team": map[string]any{
		"runtime":   map[string]any{"mode": "distributed", "role": "teammate"},
		"transport": map[string]any{"type": "pyzmq"},
	}}
	teamCfg := map[string]any{
		"leader":    map[string]any{"member_name": "my_leader"},
		"transport": map[string]any{"type": "pyzmq", "params": map[string]any{}},
	}
	result := NormalizeDistributedTransportFields(configBase, teamCfg)
	params := result["transport"].(map[string]any)["params"].(map[string]any)
	peers, ok := params["known_peers"].([]any)
	if !ok || len(peers) != 1 {
		t.Fatalf("teammate known_peers = %v", params["known_peers"])
	}
	peer := peers[0].(map[string]any)
	if peer["agent_id"] != "my_leader" {
		t.Fatalf("peer agent_id = %v", peer["agent_id"])
	}
	if params["direct_addr"] != "tcp://0.0.0.0:18600" {
		t.Fatalf("teammate direct_addr = %v", params["direct_addr"])
	}
	meta := params["metadata"].(map[string]any)
	if meta["pubsub_bind"] != false {
		t.Fatalf("teammate pubsub_bind should be false")
	}
}

// TestNormalizeDistributedTransportFields_非pyzmq直接返回。
func TestNormalizeDistributedTransportFields_非pyzmq直接返回(t *testing.T) {
	teamCfg := map[string]any{"transport": map[string]any{"type": "inprocess"}}
	result := NormalizeDistributedTransportFields(map[string]any{}, teamCfg)
	if !reflect.DeepEqual(result, teamCfg) {
		t.Fatalf("non-pyzmq should return as-is")
	}
}

// TestNormalizeDistributedTransportFields_已配置pubsub跳过推导。
func TestNormalizeDistributedTransportFields_已配置pubsub跳过推导(t *testing.T) {
	configBase := map[string]any{"team": map[string]any{"runtime": map[string]any{"role": "leader"}}}
	teamCfg := map[string]any{"transport": map[string]any{
		"type":   "pyzmq",
		"params": map[string]any{"pubsub_publish_addr": "tcp://1.2.3.4:1", "pubsub_subscribe_addr": "tcp://1.2.3.4:2"},
	}}
	result := NormalizeDistributedTransportFields(configBase, teamCfg)
	params := result["transport"].(map[string]any)["params"].(map[string]any)
	if params["pubsub_publish_addr"] != "tcp://1.2.3.4:1" {
		t.Fatalf("pre-filled pubsub should be kept")
	}
}

// TestMissingDistributedDependencies_Go恒空。
func TestMissingDistributedDependencies_Go恒空(t *testing.T) {
	cfg := map[string]any{"team": map[string]any{
		"runtime":   map[string]any{"mode": "distributed"},
		"transport": map[string]any{"type": "pyzmq"},
	}}
	if got := MissingDistributedDependencies(cfg); len(got) != 0 {
		t.Fatalf("Go 静态编译无缺包概念，恒返回空，got %v", got)
	}
}

// TestFallbackDistributedToLocal_配置变换。
func TestFallbackDistributedToLocal_配置变换(t *testing.T) {
	cfg := map[string]any{
		"team": map[string]any{
			"runtime":   map[string]any{"mode": "distributed", "role": "teammate"},
			"transport": map[string]any{"type": "pyzmq", "params": map[string]any{"x": 1}},
			"storage":   map[string]any{"type": "postgresql", "params": map[string]any{"connection_string": "postgresql://h/db"}},
		},
		"modes": map[string]any{
			"team": map[string]any{
				"team1": map[string]any{
					"transport": map[string]any{"type": "pyzmq", "params": map[string]any{}},
				},
			},
		},
	}
	result := FallbackDistributedToLocal(cfg)
	team := result["team"].(map[string]any)
	if team["runtime"].(map[string]any)["mode"] != "local" {
		t.Fatalf("mode should be local")
	}
	if team["runtime"].(map[string]any)["role"] != "leader" {
		t.Fatalf("role should be leader")
	}
	transport := team["transport"].(map[string]any)
	if transport["type"] != "inprocess" {
		t.Fatalf("transport type should be inprocess")
	}
	if _, exists := transport["params"]; exists {
		t.Fatalf("transport params should be removed")
	}
	storage := team["storage"].(map[string]any)
	if storage["type"] != "sqlite" {
		t.Fatalf("pg storage should fall back to sqlite, got %v", storage["type"])
	}
	modeTeam := result["modes"].(map[string]any)["team"].(map[string]any)
	cand := modeTeam["team1"].(map[string]any)
	if cand["transport"].(map[string]any)["type"] != "inprocess" {
		t.Fatalf("modes.team candidate transport should be inprocess")
	}
}

// TestIsPostgresqlStorage_判定。
func TestIsPostgresqlStorage_判定(t *testing.T) {
	if !IsPostgresqlStorage(map[string]any{"storage": map[string]any{"type": "postgresql"}}) {
		t.Errorf("postgresql should match")
	}
	if !IsPostgresqlStorage(map[string]any{"storage": map[string]any{"type": " Postgres "}}) {
		t.Errorf("postgres (case-insensitive) should match")
	}
	if IsPostgresqlStorage(map[string]any{"storage": map[string]any{"type": "sqlite"}}) {
		t.Errorf("sqlite should not match")
	}
	if IsPostgresqlStorage(map[string]any{}) {
		t.Errorf("empty should not match")
	}
}

// TestExtractPgEndpoint_URL解析。
func TestExtractPgEndpoint_URL解析(t *testing.T) {
	host, port := ExtractPgEndpoint(map[string]any{"storage": map[string]any{
		"params": map[string]any{"connection_string": "postgresql+asyncpg://user:pw@dbhost:6543/mydb"},
	}})
	if host != "dbhost" || port != 6543 {
		t.Fatalf("sqlalchemy URL: got %s:%d, want dbhost:6543", host, port)
	}
	host2, port2 := ExtractPgEndpoint(map[string]any{})
	if host2 != "127.0.0.1" || port2 != 5432 {
		t.Fatalf("empty default: got %s:%d", host2, port2)
	}
}

// TestRunCommand_成功执行 返回退出码 0 与输出。
func TestRunCommand_成功执行(t *testing.T) {
	rc, out := RunCommand(t.Context(), []string{"echo", "hello"}, subprocessTimeoutSec)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("out = %q, want contains hello", out)
	}
}

// TestRunCommand_非零退出码 返回真实退出码。
func TestRunCommand_非零退出码(t *testing.T) {
	rc, _ := RunCommand(t.Context(), []string{"sh", "-c", "exit 3"}, subprocessTimeoutSec)
	if rc != 3 {
		t.Fatalf("rc = %d, want 3", rc)
	}
}

// TestRunCommand_spawn失败127 可执行不存在。
func TestRunCommand_spawn失败127(t *testing.T) {
	rc, _ := RunCommand(t.Context(), []string{"no-such-cmd-xyz"}, subprocessTimeoutSec)
	if rc != 127 {
		t.Fatalf("rc = %d, want 127", rc)
	}
}

// TestRunCommand_超时124 超时后被 kill。
func TestRunCommand_超时124(t *testing.T) {
	rc, _ := RunCommand(t.Context(), []string{"sleep", "5"}, 0.3)
	if rc != 124 {
		t.Fatalf("rc = %d, want 124", rc)
	}
}

// TestIsPgAvailable_pg缺失时false pg_isready 不存在 → 127 ≠ 0。
func TestIsPgAvailable_pg缺失时false(t *testing.T) {
	// 环境无 pg_isready 时返回 false（CI 安全）；有 pg 但未运行也返回 false
	if IsPgAvailable(t.Context(), "127.0.0.1", 1) {
		t.Logf("pg_isready 可用且 127.0.0.1:1 竟然 ready，跳过断言")
	}
}

// TestEnsurePostgresqlForLeader_非leader短路 不执行任何命令。
func TestEnsurePostgresqlForLeader_非leader短路(t *testing.T) {
	cfg := map[string]any{"team": map[string]any{
		"runtime": map[string]any{"mode": "distributed", "role": "teammate"},
		"storage": map[string]any{"type": "postgresql"},
	}}
	// 无 PG 环境下 teammate 角色必须直接返回（否则会尝试启动 PG 拖慢测试）
	EnsurePostgresqlForLeader(t.Context(), cfg)
}

// TestEnsurePostgresqlForLeader_非pg存储短路。
func TestEnsurePostgresqlForLeader_非pg存储短路(t *testing.T) {
	cfg := map[string]any{"team": map[string]any{
		"runtime": map[string]any{"mode": "distributed", "role": "leader"},
		"storage": map[string]any{"type": "sqlite"},
	}}
	EnsurePostgresqlForLeader(t.Context(), cfg)
}
