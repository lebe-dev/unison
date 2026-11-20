package clipboard

import "bytes"

// PasswordManagerHint is the X11 target password managers set on secret
// content. Klipper introduced it and KeePassXC, Bitwarden, KWalletManager,
// Firefox and wl-copy with --sensitive set it.
const PasswordManagerHint = "x-kde-passwordManagerHint"

// PasswordManagerHintMIME is the MIME shaped form of the hint that has been
// proposed for toolkits that reject names without a slash.
//
//nolint:gosec // this is a clipboard target name, not a credential.
const PasswordManagerHintMIME = "text/x-kde-passwordManagerHint"

// Sensitive reports whether the advertised targets mark the content as secret.
// The hint exists only for content that must stay out of clipboard history and
// sync, so its presence is enough; the value cannot be read from every owner.
func Sensitive(targets []string) bool {
	for _, target := range targets {
		if target == PasswordManagerHint || target == PasswordManagerHintMIME {
			return true
		}
	}
	return false
}

// SensitiveHint reports whether the payload of the password manager hint marks
// the content as secret.
func SensitiveHint(payload []byte) bool {
	value := bytes.ToLower(bytes.TrimSpace(bytes.Trim(payload, "\x00")))
	return bytes.Equal(value, []byte("secret")) || bytes.Equal(value, []byte("alwayssecret"))
}
