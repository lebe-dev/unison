package clip

import (
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/tracking"
)

// stripTracking removes the advertising parameters of the URLs in the text
// representations of content.
func (p *Pipeline) cleanTracking(content ports.Content) ports.Content {
	if !p.StripTracking() {
		return content
	}
	cleaned := ports.Content{ItemID: content.ItemID}
	cleaned.Representations = make([]ports.Representation, len(content.Representations))
	for i, rep := range content.Representations {
		cleaned.Representations[i] = rep
		cleaned.Representations[i].Data = p.stripTrackingText(rep.MIME, rep.Data)
	}
	return cleaned
}

// stripTrackingText removes the advertising parameters of the URLs of one text
// representation.
func (p *Pipeline) stripTrackingText(mime string, data []byte) []byte {
	if !p.StripTracking() {
		return data
	}
	switch mime {
	case MIMETextPlain:
		return []byte(tracking.StripText(string(data)))
	case MIMETextHTML:
		return []byte(tracking.StripHTML(string(data)))
	default:
		return data
	}
}
