package ui

import "path/filepath"

// MatchesMaskPattern reports whether key matches any of the configured
// --mask-keys glob patterns (filepath.Match syntax: *, ?, [...]). Matching
// keys are never decoded or rendered at all -- see ValueView.Masked --
// which is a stronger guarantee than the default "masked behind a reveal
// click" behavior every other value gets: a mask-keys match never reaches
// the client in the first place, revealable or not.
func MatchesMaskPattern(key string, patterns []string) bool {
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, key); ok {
			return true
		}
	}
	return false
}
