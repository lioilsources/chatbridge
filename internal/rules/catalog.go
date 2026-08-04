// Package rules turns chat messages into intents and decides whether they may
// run. The action catalog is pure data from the config — adding an action never
// requires code changes here (plan §2).
package rules

import (
	"strings"

	"github.com/lioilsources/chatbridge/internal/config"
)

// Catalog resolves a command alias to its action.
type Catalog struct {
	byAlias map[string]*config.Action
}

// NewCatalog indexes every alias of every action. Aliases are matched
// case-insensitively; config validation already guarantees they are unique.
func NewCatalog(actions []config.Action) *Catalog {
	c := &Catalog{byAlias: make(map[string]*config.Action, len(actions)*2)}
	for i := range actions {
		a := &actions[i]
		for _, alias := range a.Aliases {
			c.byAlias[normalize(alias)] = a
		}
	}
	return c
}

// Lookup returns the action for an exact command token, or false if unknown.
func (c *Catalog) Lookup(command string) (*config.Action, bool) {
	a, ok := c.byAlias[normalize(command)]
	return a, ok
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
