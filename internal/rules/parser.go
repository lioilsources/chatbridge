package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/lioilsources/chatbridge/internal/config"
)

// Parse resolves a chat message to an action using the first whitespace token
// as the command. Unknown or empty messages return false — the caller must
// silently drop them (the bridge never writes back to chat).
func (c *Catalog) Parse(message string) (*config.Action, bool) {
	fields := strings.Fields(message)
	if len(fields) == 0 {
		return nil, false
	}
	return c.Lookup(fields[0])
}

// HashUser derives a stable, opaque key from a chat nick. We key cooldowns by
// this hash so raw nicks are never stored (plan §2).
func HashUser(nick string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(nick))))
	return hex.EncodeToString(sum[:8]) // 64-bit prefix — ample for a map key
}
