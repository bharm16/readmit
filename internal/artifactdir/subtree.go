package artifactdir

import "strings"

// Subtree selects bytes already retained by one bounded Read. Nested readers
// can bind their verified identities to that exact outer snapshot even if the
// source directory is replaced while they read it. It performs no filesystem IO.
// The caller must treat both maps and their shared byte slices as immutable.
func Subtree(files map[string][]byte, prefix string) map[string][]byte {
	selected := map[string][]byte{}
	prefix = strings.TrimSuffix(prefix, "/") + "/"
	for name, raw := range files {
		if strings.HasPrefix(name, prefix) {
			selected[strings.TrimPrefix(name, prefix)] = raw
		}
	}
	return selected
}
func MatchesSubtree(files map[string][]byte, prefix, schema, identity string) bool {
	selected := Subtree(files, prefix)
	return identity != "" && strings.TrimSpace(string(selected["identity.sha256"])) == identity && Identity(schema, selected) == identity
}
