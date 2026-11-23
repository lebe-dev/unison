// Package tracking removes advertising and campaign parameters from URLs.
package tracking

import (
	"net/url"
	"regexp"
	"strings"
)

// textURL matches an http or https URL inside text. The match ends at a space
// or at a character that ends a link in plain text and HTML.
var textURL = regexp.MustCompile(`(?i)https?://[^\s<>"'` + "`" + `]+`)

// attribute matches an href or src attribute of an HTML tag.
var attribute = regexp.MustCompile(`(?i)\s(?:href|src)\s*=\s*(?:"[^"]*"|'[^']*')`)

// stripExact lists the parameters that are removed by name. The names are
// lowercase and compared without case.
var stripExact = map[string]struct{}{
	"_ga":              {},
	"_gl":              {},
	"_ke":              {},
	"_kx":              {},
	"_openstat":        {},
	"_pin_unauth":      {},
	"dclid":            {},
	"epik":             {},
	"fb_action_ids":    {},
	"fb_action_types":  {},
	"fb_ref":           {},
	"fb_source":        {},
	"fbclid":           {},
	"gad_campaignid":   {},
	"gad_source":       {},
	"gbraid":           {},
	"gclid":            {},
	"gclsrc":           {},
	"hsctatracking":    {},
	"icid":             {},
	"igsh":             {},
	"igshid":           {},
	"li_fat_id":        {},
	"mc_cid":           {},
	"mc_eid":           {},
	"mkt_tok":          {},
	"msclkid":          {},
	"nr_email_referer": {},
	"pin_unauth":       {},
	"rb_clickid":       {},
	"ref_src":          {},
	"ref_url":          {},
	"sc_campaign":      {},
	"sc_channel":       {},
	"sc_cid":           {},
	"sc_content":       {},
	"sc_country":       {},
	"sc_creative":      {},
	"sc_geo":           {},
	"sc_medium":        {},
	"sc_outcome":       {},
	"spm":              {},
	"srsltid":          {},
	"trk":              {},
	"trkcampaign":      {},
	"ttclid":           {},
	"twclid":           {},
	"wbraid":           {},
	"wickedid":         {},
	"wt_mc":            {},
	"wt_zmc":           {},
	"yclid":            {},
	"ymclid":           {},
	"ysclid":           {},
}

// stripPrefixes lists parameter prefixes that mark campaign parameters of a
// known analytics system.
var stripPrefixes = []string{
	"__hs",
	"_hs",
	"at_",
	"ga_",
	"hsa_",
	"matomo_",
	"mtm_",
	"oly_",
	"piwik_",
	"pk_",
	"trk_",
	"utm_",
	"vero_",
}

// StripText removes the tracking parameters of every http and https URL in
// text. The text around the URLs is kept as it is.
func StripText(text string) string {
	if !strings.Contains(text, "://") {
		return text
	}
	return textURL.ReplaceAllStringFunc(text, func(match string) string {
		cleaned, trimmed := trimTrailing(match)
		if cleaned, changed := StripURL(cleaned); changed {
			return cleaned + trimmed
		}
		return match
	})
}

// StripHTML removes the tracking parameters from the URLs of an HTML fragment:
// the href and src attributes and the visible text.
func StripHTML(html string) string {
	if !strings.Contains(html, "://") {
		return html
	}
	stripped := attribute.ReplaceAllStringFunc(html, func(attr string) string {
		quote := strings.IndexAny(attr, `"'`)
		if quote < 0 {
			return attr
		}
		value := attr[quote+1 : len(attr)-1]
		escaped := strings.Contains(value, "&amp;")
		if escaped {
			value = strings.ReplaceAll(value, "&amp;", "&")
		}
		cleaned, changed := StripURL(value)
		if !changed {
			return attr
		}
		if escaped {
			cleaned = strings.ReplaceAll(cleaned, "&", "&amp;")
		}
		return attr[:quote+1] + cleaned + attr[len(attr)-1:]
	})
	return StripText(stripped)
}

// StripURL removes the tracking parameters of one URL. It reports whether the
// URL changed. Other parameters, their order and the encoding are kept.
func StripURL(raw string) (string, bool) {
	if !isHTTP(raw) {
		return raw, false
	}

	rest, fragment := raw, ""
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		rest, fragment = rest[:i], rest[i:]
	}
	base, query, ok := strings.Cut(rest, "?")
	if !ok {
		return raw, false
	}
	cleaned, changed := stripQuery(query)
	if !changed {
		return raw, false
	}
	if cleaned == "" {
		return base + fragment, true
	}
	return base + "?" + cleaned + fragment, true
}

// stripQuery removes the tracking parameters of a raw query string.
func stripQuery(query string) (string, bool) {
	segments := strings.Split(query, "&")
	kept := make([]string, 0, len(segments))
	changed := false
	for _, segment := range segments {
		if segment == "" {
			continue
		}
		key := segment
		if i := strings.IndexByte(segment, '='); i >= 0 {
			key = segment[:i]
		}
		if isTracking(keyOf(key)) {
			changed = true
			continue
		}
		kept = append(kept, segment)
	}
	if !changed {
		return query, false
	}
	return strings.Join(kept, "&"), true
}

// keyOf decodes one parameter name of a query string.
func keyOf(raw string) string {
	key, err := url.QueryUnescape(raw)
	if err != nil {
		return raw
	}
	return key
}

// isTracking reports whether a parameter is a campaign or advertising one.
func isTracking(key string) bool {
	name := strings.ToLower(key)
	if _, ok := stripExact[name]; ok {
		return true
	}
	for _, prefix := range stripPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// isHTTP reports whether raw is an absolute http or https URL.
func isHTTP(raw string) bool {
	lower := strings.ToLower(raw)
	var rest string
	switch {
	case strings.HasPrefix(lower, "https://"):
		rest = raw[len("https://"):]
	case strings.HasPrefix(lower, "http://"):
		rest = raw[len("http://"):]
	default:
		return false
	}
	host, _, _ := strings.Cut(rest, "/")
	if i := strings.IndexByte(host, '?'); i >= 0 {
		host = host[:i]
	}
	if i := strings.IndexByte(host, '#'); i >= 0 {
		host = host[:i]
	}
	return host != ""
}

// trimTrailing splits the punctuation that ends a sentence rather than the
// URL. A closing bracket stays when the URL opens one.
func trimTrailing(match string) (string, string) {
	end := len(match)
	for end > 0 {
		switch match[end-1] {
		case '.', ',', ';', ':', '!', '?', '\'', '"':
			end--
		case ')':
			if strings.Contains(match[:end], "(") {
				return match[:end], match[end:]
			}
			end--
		case ']':
			if strings.Contains(match[:end], "[") {
				return match[:end], match[end:]
			}
			end--
		default:
			return match[:end], match[end:]
		}
	}
	return "", match
}
