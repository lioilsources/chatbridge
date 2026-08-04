package config

import (
	"strings"
	"testing"
)

// minimal is the smallest config that must validate: everything else defaults.
const minimal = `
server:
  admin_token: "secret"
twitch:
  channel: "somestreamer"
actions:
  - name: chicken
    aliases: ["!kure"]
    type: spawn
    params: { entity: "mobs:chicken", count: 1 }
    ttl: 60
`

func TestParseSeedsDefaults(t *testing.T) {
	cfg, err := Parse([]byte(minimal))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Server.Addr != ":8093" {
		t.Errorf("server.addr default = %q, want :8093", cfg.Server.Addr)
	}
	if cfg.Limits.QueueSize != 500 {
		t.Errorf("queue_size default = %d, want 500", cfg.Limits.QueueSize)
	}
	if cfg.Limits.MaxLiveEntities != 40 {
		t.Errorf("max_live_entities default = %d, want 40", cfg.Limits.MaxLiveEntities)
	}
	if !cfg.Mode.AutoSwitch {
		t.Error("mode.auto_switch default = false, want true")
	}
}

func TestParseOverridesDefaults(t *testing.T) {
	cfg, err := Parse([]byte(minimal + "\nlimits:\n  queue_size: 42\nmode:\n  auto_switch: false\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Limits.QueueSize != 42 {
		t.Errorf("queue_size = %d, want 42 (override)", cfg.Limits.QueueSize)
	}
	if cfg.Mode.AutoSwitch {
		t.Error("auto_switch = true, want false (explicit override)")
	}
	// unrelated default must survive a partial limits block
	if cfg.Limits.MaxLiveEntities != 40 {
		t.Errorf("max_live_entities = %d, want 40 (untouched default)", cfg.Limits.MaxLiveEntities)
	}
}

func TestExampleFileIsValid(t *testing.T) {
	cfg, err := Load("../../config.example.yaml")
	if err != nil {
		t.Fatalf("example config must be valid: %v", err)
	}
	if len(cfg.Actions) == 0 {
		t.Error("example config should ship with some actions")
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"missing channel", `{server: {admin_token: x}}`, "channel is required"},
		{"missing admin token", `{twitch: {channel: s}}`, "admin_token must be set"},
		{"bad action type", `
server: {admin_token: x}
twitch: {channel: s}
actions:
  - {name: boom, aliases: ["!boom"], type: explode}`, "invalid type"},
		{"zero queue size", `
server: {admin_token: x}
twitch: {channel: s}
limits: {queue_size: 0}`, "queue_size must be > 0"},
		{"no aliases", `
server: {admin_token: x}
twitch: {channel: s}
actions:
  - {name: chicken, type: spawn}`, "at least one alias"},
		{"duplicate alias", `
server: {admin_token: x}
twitch: {channel: s}
actions:
  - {name: a, aliases: ["!x"], type: spawn}
  - {name: b, aliases: ["!x"], type: weather}`, "already used"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}
