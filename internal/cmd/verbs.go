package cmd

// AllowedVerbs is the canonical set of leaf-command verbs. Every leaf
// command's first token must appear here. Adding an entry requires a
// one-line justification in the PR description per the developer guide.
//
// Keep alphabetised.
var AllowedVerbs = map[string]struct{}{
	"create":    {},
	"delete":    {},
	"disable":   {},
	"edit":      {},
	"enable":    {},
	"exec":      {},
	"get":       {},
	"init":      {},
	"install":   {},
	"list":      {},
	"login":     {},
	"logout":    {},
	"open":      {},
	"run":       {},
	"set":       {},
	"show":      {},
	"status":    {},
	"sync":      {},
	"uninstall": {},
	"update":    {},
}

// IsAllowedVerb reports whether v is in the verb allow-list.
func IsAllowedVerb(v string) bool {
	_, ok := AllowedVerbs[v]
	return ok
}
