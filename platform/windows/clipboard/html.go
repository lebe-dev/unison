package clipboard

import (
	"bytes"
	"fmt"
	"strings"
)

// The CF_HTML clipboard format carries an HTML document as plain bytes with a
// header of byte offsets, because the clipboard has no length prefix. Offsets
// are fixed width, so the header length does not depend on their values.
const (
	// cfHTMLHeaderFormat is the header of a CF_HTML payload.
	cfHTMLHeaderFormat = "Version:1.0\r\n" +
		"StartHTML:%010d\r\n" +
		"EndHTML:%010d\r\n" +
		"StartFragment:%010d\r\n" +
		"EndFragment:%010d\r\n"
	// cfHTMLPrefix opens the document before the fragment.
	cfHTMLPrefix = "<html><body>\r\n<!--StartFragment-->"
	// cfHTMLSuffix closes the document after the fragment.
	cfHTMLSuffix = "<!--EndFragment-->\r\n</body></html>"
)

// maxOffset is the largest byte offset the builder allows and the parser
// accepts, which keeps offsets inside the fixed header width and an int.
const maxOffset = 1<<31 - 1

// maxOffsetDigits is the widest offset the parser reads.
const maxOffsetDigits = 10

// cfHTMLKeys are the header fields that carry an offset.
var cfHTMLKeys = map[string]bool{
	"starthtml":     true,
	"endhtml":       true,
	"startfragment": true,
	"endfragment":   true,
}

// cfHTMLMarkers are the header fields that carry no offset.
var cfHTMLMarkers = map[string]bool{
	"version":        true,
	"startselection": true,
	"endselection":   true,
	"sourceurl":      true,
}

// cfHTMLHeader is the offset part of a CF_HTML payload. Offsets the header does
// not carry are negative.
type cfHTMLHeader struct {
	startHTML     int
	endHTML       int
	startFragment int
	endFragment   int
}

// buildCFHTML wraps an HTML fragment into a CF_HTML document.
func buildCFHTML(fragment []byte) ([]byte, error) {
	// The offsets are filled after the header length is known, which the fixed
	// width makes independent of their values.
	headerLen := len(fmt.Sprintf(cfHTMLHeaderFormat, 0, 0, 0, 0))
	startFragment := headerLen + len(cfHTMLPrefix)
	endFragment := startFragment + len(fragment)
	endHTML := endFragment + len(cfHTMLSuffix)
	if endHTML > maxOffset {
		return nil, fmt.Errorf("clipboard: html of %d bytes does not fit a CF_HTML payload", len(fragment))
	}
	document := make([]byte, 0, endHTML)
	document = fmt.Appendf(document, cfHTMLHeaderFormat, headerLen, endHTML, startFragment, endFragment)
	document = append(document, cfHTMLPrefix...)
	document = append(document, fragment...)
	document = append(document, cfHTMLSuffix...)
	return document, nil
}

// fragmentOf returns the HTML fragment of a CF_HTML payload. It falls back to
// the document range and then to the payload after the header.
func fragmentOf(data []byte) []byte {
	header, body := parseCFHTMLHeader(data)
	if start, end, ok := header.fragment(len(data)); ok {
		return data[start:end]
	}
	if start, end, ok := header.document(len(data)); ok {
		return data[start:end]
	}
	return data[body:]
}

// fragment returns the range of the fragment when the header carries a usable
// one.
func (h cfHTMLHeader) fragment(size int) (int, int, bool) {
	return validRange(h.startFragment, h.endFragment, size)
}

// document returns the range of the document when the header carries a usable
// one.
func (h cfHTMLHeader) document(size int) (int, int, bool) {
	return validRange(h.startHTML, h.endHTML, size)
}

// validRange reports whether start and end describe a non-empty range inside a
// payload of the given size.
func validRange(start, end, size int) (int, int, bool) {
	if start < 0 || start >= end || end > size {
		return 0, 0, false
	}
	return start, end, true
}

// parseCFHTMLHeader parses the header of a CF_HTML payload. It returns the
// offsets and the position where the payload after the header starts.
func parseCFHTMLHeader(data []byte) (cfHTMLHeader, int) {
	header := cfHTMLHeader{startHTML: -1, endHTML: -1, startFragment: -1, endFragment: -1}
	fields := map[string]*int{
		"starthtml":     &header.startHTML,
		"endhtml":       &header.endHTML,
		"startfragment": &header.startFragment,
		"endfragment":   &header.endFragment,
	}
	for pos := 0; pos < len(data); {
		line := bytes.IndexByte(data[pos:], '\n')
		if line < 0 {
			return header, pos
		}
		name, value, ok := splitHeaderLine(data[pos : pos+line])
		if !ok {
			return header, pos
		}
		switch {
		case cfHTMLMarkers[name]:
			// The field carries no offset.
		case cfHTMLKeys[name]:
			offset, ok := parseOffset(value)
			if !ok {
				return header, pos
			}
			*fields[name] = offset
		default:
			return header, pos
		}
		pos += line + 1
	}
	return header, len(data)
}

// splitHeaderLine splits a header line into a lowercase field name and its
// value.
func splitHeaderLine(line []byte) (string, []byte, bool) {
	key, value, found := bytes.Cut(line, []byte{':'})
	if !found {
		return "", nil, false
	}
	name := strings.ToLower(strings.TrimSpace(string(key)))
	if name == "" {
		return "", nil, false
	}
	return name, value, true
}

// parseOffset parses a decimal byte offset from a header value. Padding is
// optional, so values of any width up to maxOffset are accepted.
func parseOffset(value []byte) (int, bool) {
	value = bytes.TrimSpace(value)
	if len(value) == 0 || len(value) > maxOffsetDigits {
		return 0, false
	}
	offset := 0
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return 0, false
		}
		offset = offset*10 + int(digit-'0')
	}
	if offset > maxOffset {
		return 0, false
	}
	return offset, true
}
