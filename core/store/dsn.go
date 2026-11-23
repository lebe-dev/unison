package store

import (
	"net/url"
	"runtime"
	"strings"
)

// fileDSN returns the data source name of a database file: a file URI, so a
// path with spaces, percent signs, non-ASCII characters or a drive letter stays
// unambiguous.
func fileDSN(path string, query url.Values) string {
	host, rest := fileURIParts(path)
	return fileURI(host, rest, query)
}

// fileURI builds a file URI from its parts and the query of the driver.
func fileURI(host, path string, query url.Values) string {
	u := url.URL{Scheme: "file", Host: host, Path: path, RawQuery: query.Encode()}
	return u.String()
}

// fileURIParts splits a native path into the host and the path of a file URI.
func fileURIParts(path string) (host, rest string) {
	if runtime.GOOS != "windows" {
		return "", path
	}
	return windowsURIParts(path)
}

// windowsURIParts splits a Windows path into the host and the path of a file
// URI. A drive path gets a leading slash and the server of a UNC path becomes
// the host.
func windowsURIParts(path string) (host, rest string) {
	path = strings.ReplaceAll(path, `\`, "/")
	if unc, ok := strings.CutPrefix(path, "//"); ok {
		host, rest, _ = strings.Cut(unc, "/")
		return host, "/" + rest
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return "", path
}
