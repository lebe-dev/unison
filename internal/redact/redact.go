// Package redact removes the personal data that must not leave the device.
package redact

import "regexp"

// pathPattern matches absolute paths of common system directories and Windows
// drive paths. The first token has no spaces; a following token continues the
// path only while it carries a separator, so prose after a path stays. The
// leading character is kept, so a path inside a URL is not touched when no
// separator precedes it.
var pathPattern = regexp.MustCompile(
	`(?i)(^|[\s"'(<]|\[)((?:/(?:Users|home|root|tmp|private|var|opt|mnt|data|storage)/|[A-Za-z]:[\\/])[^\s"'<>|:)\]},;]*(?:[ \t]+[^\s"'<>|:)\]},;]*[\\/][^\s"'<>|:)\]},;]*)*)`,
)

// Paths replaces the absolute paths of a text with "[path]".
func Paths(text string) string {
	return pathPattern.ReplaceAllString(text, "${1}[path]")
}
