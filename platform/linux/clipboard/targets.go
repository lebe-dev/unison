package clipboard

import "strings"

// Names of the X11 targets and atoms used by the clipboard protocol. Atoms are
// interned by name, so the constants are the protocol names.
const (
	atomCLIPBOARD     = "CLIPBOARD"
	atomTARGETS       = "TARGETS"
	atomTIMESTAMP     = "TIMESTAMP"
	atomINTEGER       = "INTEGER"
	atomUTF8String    = "UTF8_STRING"
	atomTextPlain     = "text/plain"
	atomTextPlainUTF8 = "text/plain;charset=utf-8"
	atomTextHTML      = "text/html"
	atomHTMLCharset   = "text/html;charset="
	atomSTRING        = "STRING"
	atomTEXT          = "TEXT"
	atomINCR          = "INCR"
	atomMULTIPLE      = "MULTIPLE"
	atomATOMPair      = "ATOM_PAIR"
	atomATOMType      = "ATOM"
	atomSelectionProp = "UNISON_CLIPBOARD_SELECTION"
	atomScratchProp   = "UNISON_CLIPBOARD_SCRATCH"
)

// atom is an X11 atom identifier.
type atom uint32

// parseAtomList reads the payload of a TARGETS or ATOM_PAIR property: a list of
// 32 bit atom identifiers in little endian order. A trailing partial value is
// ignored.
func parseAtomList(data []byte) []atom {
	atoms := make([]atom, 0, len(data)/4)
	for i := 0; i+4 <= len(data); i += 4 {
		value := uint32(data[i]) | uint32(data[i+1])<<8 | uint32(data[i+2])<<16 | uint32(data[i+3])<<24
		atoms = append(atoms, atom(value))
	}
	return atoms
}

// targetList is the list of target names a selection owner advertises.
type targetList []string

// has reports whether the owner advertises the target.
func (t targetList) has(name string) bool {
	for _, target := range t {
		if target == name {
			return true
		}
	}
	return false
}

// pickPlain returns the preferred plain text target and whether its payload is
// latin-1 encoded.
func (t targetList) pickPlain() (name string, latin1 bool) {
	for _, candidate := range []string{atomUTF8String, atomTextPlainUTF8, atomTextPlain} {
		if t.has(candidate) {
			return candidate, false
		}
	}
	for _, candidate := range []string{atomSTRING, atomTEXT} {
		if t.has(candidate) {
			return candidate, true
		}
	}
	return "", false
}

// pickHTML returns the HTML target, allowing a charset suffix.
func (t targetList) pickHTML() string {
	for _, target := range t {
		if target == atomTextHTML || strings.HasPrefix(target, atomHTMLCharset) {
			return target
		}
	}
	return ""
}
