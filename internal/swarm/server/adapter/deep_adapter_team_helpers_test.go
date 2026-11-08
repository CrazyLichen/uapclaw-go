package adapter

import (
	"math"
	"testing"
	"time"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	coreevolution "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	evolutionlogic "github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter/evolution/logic"
)

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestTeamSpecTeamName 验证从各形态的 teamSpec 提取团队名。
func TestTeamSpecTeamName(t *testing.T) {
	tests := []struct {
		name string
		spec any
		want string
	}{
		{"nil 返回空", nil, ""},
		{"指针形态取 TeamName", &atschema.TeamAgentSpec{TeamName: "team-a"}, "team-a"},
		{"实现 GetTeamName 的兼容形态", teamNameHolderStub("team-b"), "team-b"},
		{"无法识别的形态返回空", 42, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := teamSpecTeamName(tt.spec); got != tt.want {
				t.Fatalf("teamSpecTeamName = %q, 期望 %q", got, tt.want)
			}
		})
	}
}

// teamNameHolderStub 实现兼容分支要求的 GetTeamName 接口。
type teamNameHolderStub string

// GetTeamName 返回预设团队名。
func (s teamNameHolderStub) GetTeamName() string { return string(s) }

// TestResolveEvolutionEventTimeoutSec 验证超时解析的非法值与宽限累加。
func TestResolveEvolutionEventTimeoutSec(t *testing.T) {
	const fallback = 99.0
	grace := evolutionlogic.TeamEvolutionEventTimeoutGraceSec

	t.Run("rail 为 nil 返回 fallback", func(t *testing.T) {
		if got := resolveEvolutionEventTimeoutSec(nil, fallback); got != fallback {
			t.Fatalf("应返回 fallback，实际 %v", got)
		}
	})

	t.Run("非法超时（<=0）返回 fallback", func(t *testing.T) {
		rail := newTeamSkillRailWithTimeout(t, 0)
		if got := resolveEvolutionEventTimeoutSec(rail, fallback); got != fallback {
			t.Fatalf("超时为 0 应回退 fallback，实际 %v", got)
		}
	})

	t.Run("NaN 返回 fallback", func(t *testing.T) {
		rail := newTeamSkillRailWithTimeout(t, math.NaN())
		if got := resolveEvolutionEventTimeoutSec(rail, fallback); got != fallback {
			t.Fatalf("NaN 应回退 fallback，实际 %v", got)
		}
	})

	t.Run("Inf 返回 fallback", func(t *testing.T) {
		rail := newTeamSkillRailWithTimeout(t, math.Inf(1))
		if got := resolveEvolutionEventTimeoutSec(rail, fallback); got != fallback {
			t.Fatalf("Inf 应回退 fallback，实际 %v", got)
		}
	})

	t.Run("合法值加宽限", func(t *testing.T) {
		rail := newTeamSkillRailWithTimeout(t, 10)
		if got := resolveEvolutionEventTimeoutSec(rail, fallback); got != 10+grace {
			t.Fatalf("应为 10+宽限(%v)，实际 %v", grace, got)
		}
	})
}

// newTeamSkillRailWithTimeout 构造指定演进超时的 TeamSkillEvolutionRail。
func newTeamSkillRailWithTimeout(t *testing.T, timeout float64) *coreevolution.TeamSkillEvolutionRail {
	t.Helper()
	return coreevolution.NewTeamSkillEvolutionRail(
		[]string{t.TempDir()},
		usableTestModel(),
		"m",
		"cn",
		coreevolution.WithTeamSkillEvolutionTimeout(timeout),
	)
}

// TestSerializeValueAny 验证 datetime 序列化为 RFC3339，其它原样。
func TestSerializeValueAny(t *testing.T) {
	ts := time.Date(2026, 5, 28, 14, 30, 5, 0, time.FixedZone("CST", 8*3600))
	got := serializeValueAny(ts)
	s, ok := got.(string)
	if !ok {
		t.Fatalf("time.Time 应转为字符串，实际 %T", got)
	}
	if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
		t.Fatalf("应为 RFC3339 格式，实际 %q: %v", s, err)
	}

	if got := serializeValueAny(42); got != 42 {
		t.Fatalf("非时间值应原样返回，实际 %v", got)
	}
}

// TestSerializeDictChunkRecursive 验证嵌套 map/list 的递归序列化。
func TestSerializeDictChunkRecursive(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	input := map[string]any{
		"when": ts,
		"nested": map[string]any{
			"list": []any{ts, "plain"},
		},
		"num": 7,
	}
	got, ok := serializeDictChunkRecursive(input).(map[string]any)
	if !ok {
		t.Fatalf("应返回 map，实际 %T", got)
	}
	if _, isStr := got["when"].(string); !isStr {
		t.Fatalf("顶层时间应转为字符串，实际 %T", got["when"])
	}
	nested, _ := got["nested"].(map[string]any)
	list, _ := nested["list"].([]any)
	if len(list) != 2 {
		t.Fatalf("嵌套列表长度应为 2，实际 %v", list)
	}
	if _, isStr := list[0].(string); !isStr {
		t.Fatalf("嵌套列表内时间应转为字符串，实际 %T", list[0])
	}
	if list[1] != "plain" {
		t.Fatalf("普通值应保留，实际 %v", list[1])
	}
	if got["num"] != 7 {
		t.Fatalf("数值应保留，实际 %v", got["num"])
	}
}

// TestParseInteractionPayloadUtils 验证交互载荷解析。
func TestParseInteractionPayloadUtils(t *testing.T) {
	t.Run("空列表返回 nil", func(t *testing.T) {
		// 无激活确认命中时下沉到 ConvertInteractionsToAskUserQuestion，
		// 空输入下该函数返回 nil——按实际行为断言。
		if got := parseInteractionPayloadUtils(nil); got != nil {
			t.Fatalf("空输入应返回 nil，实际 %v", got)
		}
	})

	t.Run("非 map 元素被跳过且不 panic", func(t *testing.T) {
		assertNotPanics(t, func() { parseInteractionPayloadUtils([]any{"not-a-map", 42}) })
	})
}

// assertNotPanics 断言函数执行期间不发生 panic。
func assertNotPanics(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("不应 panic，实际 %v", r)
		}
	}()
	fn()
}

// TestStrFromPayloadKey 验证安全取字符串。
func TestStrFromPayloadKey(t *testing.T) {
	payload := map[string]any{"k": "v", "n": 1}
	if got := strFromPayloadKey(payload, "k"); got != "v" {
		t.Fatalf("应取到 v，实际 %q", got)
	}
	if got := strFromPayloadKey(payload, "n"); got != "" {
		t.Fatalf("非字符串应返回空串，实际 %q", got)
	}
	if got := strFromPayloadKey(payload, "missing"); got != "" {
		t.Fatalf("缺失键应返回空串，实际 %q", got)
	}
}
