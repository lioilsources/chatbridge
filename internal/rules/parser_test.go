package rules

import (
	"testing"

	"github.com/lioilsources/chatbridge/internal/config"
)

func testCatalog() *Catalog {
	return NewCatalog([]config.Action{
		{Name: "chicken", Aliases: []string{"!kure", "!Chicken"}, Type: "spawn", TTL: 60},
		{Name: "rain", Aliases: []string{"!rain"}, Type: "weather"},
	})
}

func TestParse(t *testing.T) {
	c := testCatalog()
	cases := []struct {
		msg  string
		want string // "" = expect no match
	}{
		{"!kure", "chicken"},
		{"!KURE", "chicken"},         // case-insensitive
		{"  !rain  ", "rain"},        // trimmed
		{"!rain now please", "rain"}, // first token only
		{"!chicken", "chicken"},      // second alias
		{"hello world", ""},          // unknown
		{"", ""},                     // empty
		{"   ", ""},                  // whitespace only
	}
	for _, tc := range cases {
		got, ok := c.Parse(tc.msg)
		if tc.want == "" {
			if ok {
				t.Errorf("Parse(%q) matched %q, want no match", tc.msg, got.Name)
			}
			continue
		}
		if !ok || got.Name != tc.want {
			t.Errorf("Parse(%q) = (%v, %v), want action %q", tc.msg, got, ok, tc.want)
		}
	}
}

func TestHashUser(t *testing.T) {
	if HashUser("Streamer") != HashUser("streamer") {
		t.Error("HashUser should be case-insensitive")
	}
	if h := HashUser("streamer"); h == "streamer" || h == "Streamer" {
		t.Error("HashUser must not return the raw nick")
	}
	if HashUser("alice") == HashUser("bob") {
		t.Error("distinct nicks must hash differently")
	}
}
