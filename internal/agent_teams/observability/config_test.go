package observability

import (
	"testing"
)

func TestDefaultObservabilityConfig(t *testing.T) {
	cfg := DefaultObservabilityConfig()
	if !cfg.Enabled {
		t.Error("期望 Enabled=true")
	}
	if cfg.ServiceName != "openjiuwen-agent-teams" {
		t.Errorf("期望 ServiceName=openjiuwen-agent-teams，实际 %s", cfg.ServiceName)
	}
	if cfg.Exporter != "otlp_grpc" {
		t.Errorf("期望 Exporter=otlp_grpc，实际 %s", cfg.Exporter)
	}
	if cfg.Endpoint != "http://localhost:4317" {
		t.Errorf("期望 Endpoint=http://localhost:4317，实际 %s", cfg.Endpoint)
	}
	if cfg.SampleRate != 1.0 {
		t.Errorf("期望 SampleRate=1.0，实际 %f", cfg.SampleRate)
	}
	if cfg.AttributeValueMaxLength != 8192 {
		t.Errorf("期望 AttributeValueMaxLength=8192，实际 %d", cfg.AttributeValueMaxLength)
	}
	if cfg.ExportTimeoutMs != 5000 {
		t.Errorf("期望 ExportTimeoutMs=5000，实际 %d", cfg.ExportTimeoutMs)
	}
}

func TestObservabilityConfig_自定义值(t *testing.T) {
	cfg := &ObservabilityConfig{
		Enabled:       false,
		ServiceName:   "test-service",
		Exporter:      "console",
		Endpoint:      "http://localhost:4318",
		SampleRate:    0.5,
		RedactPrompts: true,
	}
	if cfg.Enabled {
		t.Error("期望 Enabled=false")
	}
	if cfg.ServiceName != "test-service" {
		t.Errorf("期望 ServiceName=test-service，实际 %s", cfg.ServiceName)
	}
	if !cfg.RedactPrompts {
		t.Error("期望 RedactPrompts=true")
	}
}
