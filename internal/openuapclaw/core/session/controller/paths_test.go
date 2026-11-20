package controller

import "testing"

func TestSessionPaths_AgentDir(t *testing.T) {
	p := SessionPaths{}
	got := p.AgentDir("/data", "agent1")
	want := "/data/agent1"
	if got != want {
		t.Errorf("AgentDir() = %q, want %q", got, want)
	}
}

func TestSessionPaths_SessionsDir(t *testing.T) {
	p := SessionPaths{}
	got := p.SessionsDir("/data", "agent1")
	want := "/data/agent1/sessions"
	if got != want {
		t.Errorf("SessionsDir() = %q, want %q", got, want)
	}
}

func TestSessionPaths_MetaFile(t *testing.T) {
	p := SessionPaths{}
	got := p.MetaFile("/data", "agent1")
	want := "/data/agent1/sessions/sessions.json"
	if got != want {
		t.Errorf("MetaFile() = %q, want %q", got, want)
	}
}

func TestSessionPaths_SessionDir(t *testing.T) {
	p := SessionPaths{}
	got := p.SessionDir("/data", "agent1", "sess1")
	want := "/data/agent1/sessions/sess1"
	if got != want {
		t.Errorf("SessionDir() = %q, want %q", got, want)
	}
}

func TestSessionPaths_StateFile(t *testing.T) {
	p := SessionPaths{}
	got := p.StateFile("/data/agent1/sessions/sess1")
	want := "/data/agent1/sessions/sess1/state.data"
	if got != want {
		t.Errorf("StateFile() = %q, want %q", got, want)
	}
}

func TestSessionPaths_DownstreamsDir(t *testing.T) {
	p := SessionPaths{}
	got := p.DownstreamsDir("/data/agent1/sessions/sess1")
	want := "/data/agent1/sessions/sess1/downstreams"
	if got != want {
		t.Errorf("DownstreamsDir() = %q, want %q", got, want)
	}
}

func TestSessionPaths_LinkFile(t *testing.T) {
	p := SessionPaths{}
	got := p.LinkFile("/data/agent1/sessions/sess1", "agent2", "sess2")
	want := "/data/agent1/sessions/sess1/downstreams/agent2_sess2.link"
	if got != want {
		t.Errorf("LinkFile() = %q, want %q", got, want)
	}
}
