package cmd

import "strings"

// anyTagged reports whether at least one guest in list carries tags. The
// TAGS column in `ct list`/`qm list` is rendered only when this is true:
// on a typical cluster most guests are untagged, so an always-on column
// would be near-empty whitespace pushed onto everyone's output. Generic
// over the guest type because api.Container and api.VM are deliberately
// separate structs (see AGENTS.md on keeping the ct/qm trees parallel)
// and this is the one bit of list rendering they'd otherwise duplicate.
func anyTagged[T any](list []T, tags func(T) []string) bool {
	for _, item := range list {
		if len(tags(item)) > 0 {
			return true
		}
	}
	return false
}

// joinTags renders one guest's tags as a single table cell. Comma-joined
// rather than semicolon-joined (Proxmox's own wire separator) because a
// comma reads better in a terminal table; NormalizeTags accepts either on
// the way back in.
func joinTags(tags []string) string {
	return strings.Join(tags, ",")
}
