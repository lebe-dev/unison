package clip

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"strings"
	"testing"
)

// marker is planted in the metadata of broken images. A result that still
// carries it passed bytes of the peer through.
const marker = "UNISON-PEER-BYTES"

// expect is what sanitizing a broken image must end with.
type expect int

const (
	// expectError requires the image to be refused.
	expectError expect = iota
	// expectImage requires a clean PNG of the size in the header.
	expectImage
	// expectEither accepts both, as long as an accepted image is clean.
	expectEither
)

// brokenImage is one damaged input.
type brokenImage struct {
	name   string
	data   []byte
	expect expect
}

// sampleImage returns a small image with a gradient.
func sampleImage(width, height int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for x := range width {
		for y := range height {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 7), G: uint8(y * 11), B: 0x55, A: 0xff})
		}
	}
	return img
}

// encodedPNG returns a valid PNG of the given size.
func encodedPNG(width, height int) []byte {
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, sampleImage(width, height)); err != nil {
		panic(err)
	}
	return buffer.Bytes()
}

// encodedGIF returns a valid GIF of the given size.
func encodedGIF(width, height int) []byte {
	var buffer bytes.Buffer
	if err := gif.Encode(&buffer, sampleImage(width, height), nil); err != nil {
		panic(err)
	}
	return buffer.Bytes()
}

// pngChunk builds one PNG chunk with a valid checksum.
func pngChunk(kind string, body []byte) []byte {
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(body))) //nolint:gosec // Test chunks are small.
	chunk = append(chunk, kind...)
	chunk = append(chunk, body...)
	return binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
}

// pngAfterIHDR inserts raw bytes right after the IHDR chunk.
func pngAfterIHDR(data, extra []byte) []byte {
	const ihdrEnd = 33
	out := append([]byte(nil), data[:ihdrEnd]...)
	out = append(out, extra...)
	return append(out, data[ihdrEnd:]...)
}

// pngIHDR rewrites the fields of the IHDR chunk and fixes its checksum.
func pngIHDR(data []byte, change func(ihdr []byte)) []byte {
	out := append([]byte(nil), data...)
	change(out[16:29])
	binary.BigEndian.PutUint32(out[29:33], crc32.ChecksumIEEE(out[12:29]))
	return out
}

// pngIDAT replaces the body of the first IDAT chunk.
func pngIDAT(data, body []byte) []byte {
	start := bytes.Index(data, []byte("IDAT")) - 4
	length := int(binary.BigEndian.Uint32(data[start : start+4]))
	end := start + 12 + length
	out := append([]byte(nil), data[:start]...)
	out = append(out, pngChunk("IDAT", body)...)
	return append(out, data[end:]...)
}

// jpegSegment builds one JPEG marker segment with a declared length.
func jpegSegment(kind byte, length uint16, body []byte) []byte {
	segment := []byte{0xff, kind}
	segment = binary.BigEndian.AppendUint16(segment, length)
	return append(segment, body...)
}

// jpegApp1 builds an APP1 segment with a correct length.
func jpegApp1(body []byte) []byte {
	return jpegSegment(0xe1, uint16(len(body)+2), body) //nolint:gosec // Test segments are small.
}

// exif returns an APP1 body with the EXIF header and a TIFF structure.
func exif(tiff []byte) []byte {
	return append([]byte("Exif\x00\x00"), tiff...)
}

// tiffLE builds a little endian TIFF header with the offset of the first IFD
// and the bytes that follow it.
func tiffLE(ifdOffset uint32, rest []byte) []byte {
	out := []byte("II*\x00")
	out = binary.LittleEndian.AppendUint32(out, ifdOffset)
	return append(out, rest...)
}

// ifdEntry builds one little endian IFD entry.
func ifdEntry(tag, kind uint16, count, value uint32) []byte {
	out := binary.LittleEndian.AppendUint16(nil, tag)
	out = binary.LittleEndian.AppendUint16(out, kind)
	out = binary.LittleEndian.AppendUint32(out, count)
	return binary.LittleEndian.AppendUint32(out, value)
}

// jpegAfterSOI inserts segments right after the start of image marker.
func jpegAfterSOI(data []byte, segments ...[]byte) []byte {
	out := append([]byte(nil), data[:2]...)
	for _, segment := range segments {
		out = append(out, segment...)
	}
	return append(out, data[2:]...)
}

// jpegSegmentOffset returns the offset of the first segment of a kind, found
// by walking the segment lengths.
func jpegSegmentOffset(data []byte, kind byte) int {
	for offset := 2; offset+4 <= len(data); {
		if data[offset] != 0xff {
			return -1
		}
		if data[offset+1] == kind {
			return offset
		}
		offset += 2 + int(binary.BigEndian.Uint16(data[offset+2:offset+4]))
	}
	return -1
}

// jpegSOF rewrites the frame header of a baseline JPEG.
func jpegSOF(t testing.TB, data []byte, change func(sof []byte)) []byte {
	t.Helper()
	offset := jpegSegmentOffset(data, 0xc0)
	if offset < 0 {
		t.Fatal("the test JPEG has no baseline frame header")
	}
	out := append([]byte(nil), data...)
	change(out[offset:])
	return out
}

// brokenImages returns the damaged inputs of the tests and the seeds of the
// fuzz target.
func brokenImages(t testing.TB) []brokenImage {
	t.Helper()

	validPNG := encodedPNG(16, 8)
	validJPEG := imageBytes(t, "jpeg", 16, 8)
	validGIF := encodedGIF(16, 8)
	planted := []byte(marker)

	selfLoop := tiffLE(8, append(append(binary.LittleEndian.AppendUint16(nil, 1),
		ifdEntry(0x8769, 4, 1, 8)...), binary.LittleEndian.AppendUint32(nil, 8)...))
	orientation := tiffLE(8, append(append(binary.LittleEndian.AppendUint16(nil, 1),
		ifdEntry(0x0112, 3, 0xffffffff, 0xdeadbeef)...), planted...))
	hugeCount := tiffLE(8, binary.LittleEndian.AppendUint16(nil, 0xffff))
	farIFD := tiffLE(0xfffffff0, planted)
	bigEndianGarbage := append([]byte("MM\x00*\xff\xff\xff\xff"), planted...)

	return []brokenImage{
		// Inputs that are not an image at all.
		{name: "empty", data: nil, expect: expectError},
		{name: "one byte", data: []byte{0x89}, expect: expectError},
		{name: "png signature only", data: pngSignature, expect: expectError},
		{name: "jpeg start only", data: []byte{0xff, 0xd8, 0xff}, expect: expectError},
		{name: "jpeg start and end", data: []byte{0xff, 0xd8, 0xff, 0xd9}, expect: expectError},
		{name: "gif signature only", data: gif89Signature, expect: expectError},
		{name: "svg", data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>x</script></svg>`), expect: expectError},
		{name: "webp", data: append([]byte("RIFF\x10\x00\x00\x00WEBPVP8 "), planted...), expect: expectError},
		{name: "png signature over jpeg", data: append(append([]byte(nil), pngSignature...), validJPEG[2:]...), expect: expectError},
		{name: "jpeg start over png", data: append([]byte{0xff, 0xd8, 0xff}, validPNG...), expect: expectError},
		{name: "gif signature over png", data: append(append([]byte(nil), gif89Signature...), validPNG...), expect: expectError},

		// JPEG with damaged EXIF metadata. The decoder skips APP1, so the
		// image is accepted and the metadata never reaches the result.
		{name: "exif with a far IFD", data: jpegAfterSOI(validJPEG, jpegApp1(exif(farIFD))), expect: expectImage},
		{name: "exif with a huge entry count", data: jpegAfterSOI(validJPEG, jpegApp1(exif(hugeCount))), expect: expectImage},
		{name: "exif with an IFD loop", data: jpegAfterSOI(validJPEG, jpegApp1(exif(selfLoop))), expect: expectImage},
		{name: "exif with a broken orientation", data: jpegAfterSOI(validJPEG, jpegApp1(exif(orientation))), expect: expectImage},
		{name: "exif with a big endian garbage header", data: jpegAfterSOI(validJPEG, jpegApp1(exif(bigEndianGarbage))), expect: expectImage},
		{name: "exif header only", data: jpegAfterSOI(validJPEG, jpegApp1([]byte("Exif\x00"))), expect: expectImage},
		{name: "many exif segments", data: jpegAfterSOI(validJPEG, bytes.Repeat(jpegApp1(exif(farIFD)), 200)), expect: expectImage},
		{name: "icc profile garbage", data: jpegAfterSOI(validJPEG, jpegSegment(0xe2, uint16(2+12+len(marker)), append([]byte("ICC_PROFILE\x00"), planted...))), expect: expectImage},
		{name: "comment", data: jpegAfterSOI(validJPEG, jpegSegment(0xfe, uint16(2+len(marker)), planted)), expect: expectImage},
		{name: "app1 length zero", data: jpegAfterSOI(validJPEG, jpegSegment(0xe1, 0, planted)), expect: expectEither},
		{name: "app1 length one", data: jpegAfterSOI(validJPEG, jpegSegment(0xe1, 1, planted)), expect: expectEither},
		{name: "app1 longer than the file", data: jpegAfterSOI(validJPEG[:40], jpegSegment(0xe1, 0xffff, planted)), expect: expectError},
		{name: "adobe segment with a bad transform", data: jpegAfterSOI(validJPEG, jpegSegment(0xee, 14, []byte("Adobe\x00\x64\x00\x00\x00\x00\x07"))), expect: expectEither},

		// JPEG with a damaged frame header.
		{name: "jpeg zero width", data: jpegSOF(t, validJPEG, func(sof []byte) { binary.BigEndian.PutUint16(sof[7:9], 0) }), expect: expectError},
		{name: "jpeg zero height", data: jpegSOF(t, validJPEG, func(sof []byte) { binary.BigEndian.PutUint16(sof[5:7], 0) }), expect: expectError},
		{name: "jpeg huge size", data: jpegSOF(t, validJPEG, func(sof []byte) {
			binary.BigEndian.PutUint16(sof[5:7], 0xffff)
			binary.BigEndian.PutUint16(sof[7:9], 0xffff)
		}), expect: expectError},
		{name: "jpeg above the pixel limit", data: jpegSOF(t, validJPEG, func(sof []byte) {
			binary.BigEndian.PutUint16(sof[5:7], MaxImageSide)
			binary.BigEndian.PutUint16(sof[7:9], MaxImageSide)
		}), expect: expectError},
		{name: "jpeg taller than its data", data: jpegSOF(t, validJPEG, func(sof []byte) { binary.BigEndian.PutUint16(sof[5:7], 4000) }), expect: expectEither},
		{name: "jpeg no components", data: jpegSOF(t, validJPEG, func(sof []byte) { sof[9] = 0 }), expect: expectError},
		{name: "jpeg 255 components", data: jpegSOF(t, validJPEG, func(sof []byte) { sof[9] = 255 }), expect: expectError},
		{name: "jpeg zero sampling", data: jpegSOF(t, validJPEG, func(sof []byte) { sof[11] = 0x00 }), expect: expectError},
		{name: "jpeg sampling above four", data: jpegSOF(t, validJPEG, func(sof []byte) { sof[11] = 0x55 }), expect: expectError},
		{name: "jpeg bad quantization table", data: jpegSOF(t, validJPEG, func(sof []byte) { sof[12] = 9 }), expect: expectError},
		{name: "jpeg cut in the header", data: validJPEG[:len(validJPEG)/4], expect: expectError},
		{name: "jpeg cut in the scan", data: validJPEG[:len(validJPEG)-len(validJPEG)/4], expect: expectEither},
		{name: "jpeg without the end", data: validJPEG[:len(validJPEG)-2], expect: expectEither},
		{name: "jpeg with a garbage scan", data: append(append([]byte(nil), validJPEG[:len(validJPEG)-40]...), bytes.Repeat([]byte{0xff, 0x00, 0xa5}, 13)...), expect: expectEither},

		// PNG with damaged chunks.
		{name: "png bad header checksum", data: func() []byte {
			out := append([]byte(nil), validPNG...)
			out[30] ^= 0xff
			return out
		}(), expect: expectError},
		{name: "png zero width", data: pngIHDR(validPNG, func(h []byte) { binary.BigEndian.PutUint32(h[0:4], 0) }), expect: expectError},
		{name: "png zero height", data: pngIHDR(validPNG, func(h []byte) { binary.BigEndian.PutUint32(h[4:8], 0) }), expect: expectError},
		{name: "png huge width", data: pngIHDR(validPNG, func(h []byte) { binary.BigEndian.PutUint32(h[0:4], 0x7fffffff) }), expect: expectError},
		{name: "png width with the sign bit", data: pngIHDR(validPNG, func(h []byte) { binary.BigEndian.PutUint32(h[0:4], 0x80000010) }), expect: expectError},
		{name: "png bit depth three", data: pngIHDR(validPNG, func(h []byte) { h[8] = 3 }), expect: expectError},
		{name: "png color type seven", data: pngIHDR(validPNG, func(h []byte) { h[9] = 7 }), expect: expectError},
		{name: "png interlace two", data: pngIHDR(validPNG, func(h []byte) { h[12] = 2 }), expect: expectError},
		{name: "png shorter than its data", data: pngIHDR(encodedPNG(16, 16), func(h []byte) { binary.BigEndian.PutUint32(h[4:8], 8) }), expect: expectError},
		{name: "png taller than its data", data: pngIHDR(validPNG, func(h []byte) { binary.BigEndian.PutUint32(h[4:8], 64) }), expect: expectError},
		{name: "png broken zlib", data: pngIDAT(validPNG, append([]byte{0x78, 0x9c}, planted...)), expect: expectError},
		{name: "png empty data", data: pngIDAT(validPNG, nil), expect: expectError},
		{name: "png cut in the data", data: validPNG[:len(validPNG)-20], expect: expectError},
		{name: "png without the end", data: validPNG[:len(validPNG)-12], expect: expectEither},
		{name: "png garbage exif", data: pngAfterIHDR(validPNG, pngChunk("eXIf", exif(farIFD))), expect: expectImage},
		{name: "png exif loop", data: pngAfterIHDR(validPNG, pngChunk("eXIf", selfLoop)), expect: expectImage},
		{name: "png garbage icc profile", data: pngAfterIHDR(validPNG, pngChunk("iCCP", append([]byte("x\x00\x00"), planted...))), expect: expectImage},
		{name: "png text without a separator", data: pngAfterIHDR(validPNG, pngChunk("tEXt", planted)), expect: expectImage},
		{name: "png compressed text garbage", data: pngAfterIHDR(validPNG, pngChunk("zTXt", append([]byte("k\x00\x00"), planted...))), expect: expectImage},
		{name: "png animation chunks", data: pngAfterIHDR(validPNG, append(pngChunk("acTL", []byte{0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0}), pngChunk("fdAT", planted)...)), expect: expectImage},
		{name: "png unknown chunk", data: pngAfterIHDR(validPNG, pngChunk("zzZz", planted)), expect: expectImage},
		{name: "png unknown critical chunk", data: pngAfterIHDR(validPNG, pngChunk("ZZZZ", planted)), expect: expectEither},
		{name: "png chunk length overflow", data: pngAfterIHDR(validPNG, append([]byte{0xff, 0xff, 0xff, 0xff}, []byte("tEXt")...)), expect: expectError},
		{name: "png chunk with a bad checksum", data: pngAfterIHDR(validPNG, func() []byte {
			chunk := pngChunk("tEXt", planted)
			chunk[len(chunk)-1] ^= 0xff
			return chunk
		}()), expect: expectEither},
		{name: "png palette for true color", data: pngAfterIHDR(validPNG, pngChunk("PLTE", bytes.Repeat([]byte{1, 2, 3}, 300))), expect: expectEither},

		// GIF with damaged headers.
		{name: "gif zero screen", data: func() []byte {
			out := append([]byte(nil), validGIF...)
			binary.LittleEndian.PutUint16(out[6:8], 0)
			binary.LittleEndian.PutUint16(out[8:10], 0)
			return out
		}(), expect: expectError},
		{name: "gif huge screen", data: func() []byte {
			out := append([]byte(nil), validGIF...)
			binary.LittleEndian.PutUint16(out[6:8], 0xffff)
			binary.LittleEndian.PutUint16(out[8:10], 0xffff)
			return out
		}(), expect: expectError},
		{name: "gif frame outside the screen", data: func() []byte {
			out := append([]byte(nil), validGIF...)
			descriptor := bytes.IndexByte(out[13:], 0x2c) + 13
			binary.LittleEndian.PutUint16(out[descriptor+1:descriptor+3], 0xfff0)
			return out
		}(), expect: expectError},
		{name: "gif bad code size", data: func() []byte {
			out := append([]byte(nil), validGIF...)
			descriptor := bytes.IndexByte(out[13:], 0x2c) + 13
			out[descriptor+10] = 12
			return out
		}(), expect: expectError},
		{name: "gif cut in the data", data: validGIF[:len(validGIF)-len(validGIF)/3], expect: expectError},
	}
}

// checkSanitized checks the outcome of sanitizing one input.
func checkSanitized(t *testing.T, out []byte, err error, want expect) {
	t.Helper()

	if err != nil {
		if strings.Contains(err.Error(), "decoder failed") {
			t.Fatalf("a decoder panicked: %v", err)
		}
		if want == expectImage {
			t.Fatalf("the image was refused: %v", err)
		}
		return
	}
	if want == expectError {
		t.Fatal("a broken image was accepted")
	}
	if !bytes.HasPrefix(out, pngSignature) {
		t.Fatal("the result is not a PNG")
	}
	if bytes.Contains(out, []byte(marker)) || bytes.Contains(out, []byte("Exif")) {
		t.Fatal("metadata of the peer reached the result")
	}
	cfg, decodeErr := png.DecodeConfig(bytes.NewReader(out))
	if decodeErr != nil {
		t.Fatalf("the result does not decode: %v", decodeErr)
	}
	if checkImageBounds(cfg) != nil {
		t.Fatalf("the result of %dx%d is outside the limits", cfg.Width, cfg.Height)
	}
	if _, decodeErr := png.Decode(bytes.NewReader(out)); decodeErr != nil {
		t.Fatalf("the pixels of the result do not decode: %v", decodeErr)
	}
}

// TestSanitizeBrokenImages feeds damaged images to the sanitizer: none may
// crash it, and an accepted one must come out as a clean PNG.
func TestSanitizeBrokenImages(t *testing.T) {
	for _, test := range brokenImages(t) {
		t.Run(test.name, func(t *testing.T) {
			out, err := sanitizeImage(test.data)
			checkSanitized(t, out, err, test.expect)
		})
	}
}

// TestThumbnailBrokenImages feeds the same images to the thumbnail decoder.
func TestThumbnailBrokenImages(t *testing.T) {
	for _, test := range brokenImages(t) {
		t.Run(test.name, func(t *testing.T) {
			thumb, err := Thumbnail(test.data, 64)
			if err != nil {
				if strings.Contains(err.Error(), "decoder failed") {
					t.Fatalf("a decoder panicked: %v", err)
				}
				if test.expect == expectImage {
					t.Fatalf("the image was refused: %v", err)
				}
				return
			}
			if test.expect == expectError {
				t.Fatal("a broken image was accepted")
			}
			if len(thumb.RGBA) != thumb.Width*thumb.Height*4 || thumb.Width > 64 || thumb.Height > 64 {
				t.Fatalf("thumbnail of %dx%d holds %d bytes", thumb.Width, thumb.Height, len(thumb.RGBA))
			}
		})
	}
}

// FuzzSanitizeImage checks that no input crashes the sanitizer and that an
// accepted one always comes out as a clean PNG within the limits.
func FuzzSanitizeImage(f *testing.F) {
	for _, test := range brokenImages(f) {
		f.Add(test.data)
	}
	f.Add(encodedPNG(3, 2))
	f.Add(encodedGIF(3, 2))

	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := sanitizeImage(data)
		checkSanitized(t, out, err, expectEither)
		if _, err := Thumbnail(data, 32); err != nil && strings.Contains(err.Error(), "decoder failed") {
			t.Fatalf("a decoder panicked in the thumbnail: %v", err)
		}
	})
}
