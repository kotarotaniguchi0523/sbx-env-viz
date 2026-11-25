//declscope:shared // keys.go holds the key helpers every file that walks fields or maps uses
package sbxenv

import "sort"

// joinKey appends key to the dotted field path parent.
func joinKey(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

// sortedKeys returns the keys of m in sorted order, for a stable output.
func sortedKeys[M ~map[string]V, V any](m M) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
