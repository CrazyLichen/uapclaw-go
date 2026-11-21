// Package testhelpers - turn_invariants.go 提供单轮事件序不变量断言助手。
//
// 对齐 Python tests/system_tests/harness_providers/_contract.py:104-113
// assert_turn_invariants：provider 无关的协议不变量（15.17 共享断言面）。
package testhelpers

import (
	"testing"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
)

// AssertTurnInvariants 校验单轮事件序不变量。
// Python: _contract.py:104-113 assert_turn_invariants
// ① 恰一个 TurnLifecycleEvent STARTED；
// ② 恰一个非 STARTED 终态；
// ③ 全部事件 turn_id == turnID（turn 视图不串轮）；
// ④ sequence 单调且不重复；
// ⑤ 每条事件 host_session_id 与 agent_id 非空。
func AssertTurnInvariants(t *testing.T, events []harnessprotocol.HarnessEvent, turnID string) {
	t.Helper()
	started := 0
	terminal := 0
	terminalKinds := []harnessprotocol.TurnEventKind{}
	sequences := make([]int, 0, len(events))
	for _, event := range events {
		if event.TurnID != turnID {
			t.Fatalf("turn 视图泄漏其他轮次事件: %s != %s", event.TurnID, turnID)
		}
		if event.HostSessionID == "" || event.AgentID == "" {
			t.Fatalf("事件信封缺 host_session_id/agent_id: seq=%d", event.Sequence)
		}
		sequences = append(sequences, event.Sequence)
		if payload, ok := event.Event.(harnessprotocol.TurnLifecycleEvent); ok {
			if payload.Kind == harnessprotocol.TurnEventKindStarted {
				started++
			} else {
				terminal++
				terminalKinds = append(terminalKinds, payload.Kind)
			}
		}
	}
	if started != 1 {
		t.Fatalf("期望恰一个 STARTED，实得 %d", started)
	}
	if terminal != 1 {
		t.Fatalf("期望恰一个终态事件，实得 %d %v", terminal, terminalKinds)
	}
	for index := 1; index < len(sequences); index++ {
		if sequences[index] <= sequences[index-1] {
			t.Fatalf("sequence 必须单调不重复: %v", sequences)
		}
	}
}
