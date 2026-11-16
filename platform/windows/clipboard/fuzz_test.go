package clipboard

import (
	"image"
	"image/color"
	"testing"
)

// FuzzCFHTML checks that an arbitrary CF_HTML payload is either refused or
// yields a fragment inside the payload.
func FuzzCFHTML(f *testing.F) {
	document, err := buildCFHTML([]byte("<b>copied</b>"))
	if err != nil {
		f.Fatalf("build a document: %v", err)
	}
	f.Add(document)
	f.Add([]byte{})
	f.Add([]byte("Version:1.0\r\nStartHTML:0000000040\r\nEndHTML:0000000001\r\n"))
	f.Add([]byte("StartFragment:0000000010\r\nEndFragment:"))
	f.Add([]byte("Fake:1\r\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		fragment := fragmentOf(data)
		if len(fragment) > len(data) {
			t.Fatalf("a fragment of %d bytes exceeds the payload of %d bytes", len(fragment), len(data))
		}

		header, body := parseCFHTMLHeader(data)
		if body < 0 || body > len(data) {
			t.Fatalf("the header ends at %d of %d bytes", body, len(data))
		}
		for _, rangeOf := range []func(int) (int, int, bool){header.fragment, header.document} {
			start, end, ok := rangeOf(len(data))
			if !ok {
				continue
			}
			if start < 0 || start >= end || end > len(data) {
				t.Fatalf("accepted the range %d..%d of %d bytes", start, end, len(data))
			}
		}
	})
}

// FuzzDIB checks that an arbitrary device independent bitmap is either refused
// or turns into a PNG that the reader accepts.
func FuzzDIB(f *testing.F) {
	img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	img.Set(0, 0, color.NRGBA{R: 0xFF, G: 0x10, B: 0x20, A: 0xFF})
	payload, err := encodeDIB(img)
	if err != nil {
		f.Fatalf("encode a bitmap: %v", err)
	}
	f.Add(payload)
	f.Add([]byte{})
	f.Add([]byte{40, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 24, 0})

	f.Fuzz(func(t *testing.T, data []byte) {
		png, err := dibToPNG(data)
		if err != nil {
			return
		}
		if err := checkPNG(png); err != nil {
			t.Fatalf("the reader refuses the PNG it produced: %v", err)
		}
	})
}

// FuzzHDROP checks that an arbitrary file list is either refused or describes
// paths inside the limits.
func FuzzHDROP(f *testing.F) {
	payload, err := encodeDropFiles([]string{"/tmp/report.pdf", "C:\\Users\\me\\shot.png"})
	if err != nil {
		f.Fatalf("encode a file list: %v", err)
	}
	f.Add(payload)
	f.Add([]byte{})
	f.Add([]byte{20, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	f.Add([]byte{20, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0x2f, 0, 0, 0})

	f.Fuzz(func(t *testing.T, data []byte) {
		paths, err := decodeDropFiles(data)
		if err != nil {
			return
		}
		if len(paths) > maxDropFiles {
			t.Fatalf("accepted %d paths", len(paths))
		}
		for _, path := range paths {
			if path == "" {
				t.Fatalf("accepted an empty path")
			}
		}
	})
}
