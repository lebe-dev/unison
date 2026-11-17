package clip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// withChunk inserts an ancillary chunk right after the IHDR chunk of a PNG.
func withChunk(t *testing.T, data []byte, kind string, body []byte) []byte {
	t.Helper()

	const ihdrEnd = 8 + 4 + 4 + 13 + 4
	if len(data) < ihdrEnd || string(data[12:16]) != "IHDR" {
		t.Fatal("the test image does not start with IHDR")
	}
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(body)))
	chunk = append(chunk, kind...)
	chunk = append(chunk, body...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))

	out := append([]byte(nil), data[:ihdrEnd]...)
	out = append(out, chunk...)
	return append(out, data[ihdrEnd:]...)
}

// TestSanitizeImageReencodesPNG checks that a PNG of a peer never reaches the
// clipboard byte for byte: chunks the decoder ignores are dropped.
func TestSanitizeImageReencodesPNG(t *testing.T) {
	marker := []byte("payload for a platform decoder")
	data := withChunk(t, imageBytes(t, "png", 16, 8), "zzZz", marker)

	out, err := sanitizeImage(data)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	if bytes.Contains(out, marker) {
		t.Fatal("an unknown chunk survived the sanitizing")
	}
	decoded, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode the result: %v", err)
	}
	if got := decoded.Bounds().Size(); got != image.Pt(16, 8) {
		t.Fatalf("result is %v, want 16x8", got)
	}
	r, g, b, a := decoded.At(3, 2).RGBA()
	if r>>8 != 3*8 || g>>8 != 2*8 || b>>8 != 0x40 || a>>8 != 0xff {
		t.Fatalf("pixel (3,2) is %d %d %d %d", r>>8, g>>8, b>>8, a>>8)
	}
}

// TestSanitizeImageAcceptsJPEG checks that a JPEG of a peer becomes PNG.
func TestSanitizeImageAcceptsJPEG(t *testing.T) {
	out, err := sanitizeImage(imageBytes(t, "jpeg", 12, 10))
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode the result: %v", err)
	}
	if cfg.Width != 12 || cfg.Height != 10 {
		t.Fatalf("result is %dx%d, want 12x10", cfg.Width, cfg.Height)
	}
}

// TestSanitizeImageRefusesOtherFormats checks that the decoders of TIFF and
// BMP never see data of a peer.
func TestSanitizeImageRefusesOtherFormats(t *testing.T) {
	for _, format := range []string{"tiff", "bmp"} {
		t.Run(format, func(t *testing.T) {
			_, err := sanitizeImage(imageBytes(t, format, 4, 4))
			if !errors.Is(err, ErrImageFormat) {
				t.Fatalf("got %v, want ErrImageFormat", err)
			}
		})
	}
	if _, err := sanitizeImage([]byte("<svg xmlns='http://www.w3.org/2000/svg'/>")); !errors.Is(err, ErrImageFormat) {
		t.Fatalf("svg: got %v, want ErrImageFormat", err)
	}
}

// TestSanitizeImageRefusesLargeImages checks the bounds before decoding.
func TestSanitizeImageRefusesLargeImages(t *testing.T) {
	// The header claims a huge image, the pixel data is never read.
	header := imageBytes(t, "png", 1, 1)
	binary.BigEndian.PutUint32(header[16:20], MaxImageSide+1)
	binary.BigEndian.PutUint32(header[29:33], crc32.ChecksumIEEE(header[12:29]))

	if _, err := sanitizeImage(header); err == nil {
		t.Fatal("an image above the side limit was accepted")
	}
}

// TestSanitizeImageRefusesTruncatedData checks that a broken stream is an
// error, not a partial image.
func TestSanitizeImageRefusesTruncatedData(t *testing.T) {
	data := imageBytes(t, "png", 32, 32)
	if _, err := sanitizeImage(data[:len(data)/2]); err == nil {
		t.Fatal("a truncated image was accepted")
	}
}

// TestThumbnailScalesDown checks the size and the layout of the pixels.
func TestThumbnailScalesDown(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 400, 200))
	for x := range 400 {
		for y := range 200 {
			source.SetNRGBA(x, y, color.NRGBA{R: 0x20, G: 0x80, B: 0xc0, A: 0xff})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, source); err != nil {
		t.Fatalf("encode: %v", err)
	}

	thumb, err := Thumbnail(buffer.Bytes(), 100)
	if err != nil {
		t.Fatalf("thumbnail: %v", err)
	}
	if thumb.Width != 100 || thumb.Height != 50 {
		t.Fatalf("thumbnail is %dx%d, want 100x50", thumb.Width, thumb.Height)
	}
	if len(thumb.RGBA) != 100*50*4 {
		t.Fatalf("thumbnail holds %d bytes, want %d", len(thumb.RGBA), 100*50*4)
	}
	if got := thumb.RGBA[:4]; !bytes.Equal(got, []byte{0x20, 0x80, 0xc0, 0xff}) {
		t.Fatalf("first pixel is %v", got)
	}
}

// TestThumbnailKeepsSmallImages checks that an image is never scaled up.
func TestThumbnailKeepsSmallImages(t *testing.T) {
	thumb, err := Thumbnail(imageBytes(t, "png", 10, 6), 100)
	if err != nil {
		t.Fatalf("thumbnail: %v", err)
	}
	if thumb.Width != 10 || thumb.Height != 6 {
		t.Fatalf("thumbnail is %dx%d, want 10x6", thumb.Width, thumb.Height)
	}
}

// TestThumbnailRefusesOtherFormats checks that a thumbnail uses the same
// decoders as the clipboard.
func TestThumbnailRefusesOtherFormats(t *testing.T) {
	if _, err := Thumbnail(imageBytes(t, "tiff", 4, 4), 100); !errors.Is(err, ErrImageFormat) {
		t.Fatalf("got %v, want ErrImageFormat", err)
	}
}
