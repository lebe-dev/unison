package clipboard

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"slices"
	"testing"
)

// bitmapHeader builds a bitmap header of the given size.
func bitmapHeader(size int, width, height int32, bitCount uint16, compression, colors uint32) []byte {
	header := make([]byte, size)
	binary.LittleEndian.PutUint32(header[0:], uint32(size))
	binary.LittleEndian.PutUint32(header[4:], uint32(width))
	binary.LittleEndian.PutUint32(header[8:], uint32(height))
	binary.LittleEndian.PutUint16(header[12:], 1)
	binary.LittleEndian.PutUint16(header[14:], bitCount)
	binary.LittleEndian.PutUint32(header[16:], compression)
	binary.LittleEndian.PutUint32(header[32:], colors)
	return header
}

// setMasks stores channel masks in a bitmap header.
func setMasks(header []byte, masks ...uint32) []byte {
	for i, mask := range masks {
		binary.LittleEndian.PutUint32(header[dibMaskOffset+4*i:], mask)
	}
	return header
}

// packBGR packs pixels into a padded 24 bit bitmap row.
func packBGR(pixels ...color.NRGBA) []byte {
	row := make([]byte, (len(pixels)*3+rowAlign-1)&^(rowAlign-1))
	for i, pixel := range pixels {
		row[3*i], row[3*i+1], row[3*i+2] = pixel.B, pixel.G, pixel.R
	}
	return row
}

// packWords packs pixel words into a bitmap row.
func packWords(words ...uint32) []byte {
	row := make([]byte, 4*len(words))
	for i, word := range words {
		binary.LittleEndian.PutUint32(row[4*i:], word)
	}
	return row
}

// rowBytes joins rows into the pixel area of a bitmap.
func rowBytes(rows ...[]byte) []byte {
	return bytes.Join(rows, nil)
}

// withPixels joins a bitmap header with the bytes that follow it.
func withPixels(header, rest []byte) []byte {
	return slices.Concat(header, rest)
}

var (
	red   = color.NRGBA{R: 0xFF, A: 0xFF}
	green = color.NRGBA{G: 0xFF, A: 0xFF}
	blue  = color.NRGBA{B: 0xFF, A: 0xFF}
	white = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
)

// expectPixel checks one pixel of a decoded bitmap.
func expectPixel(t *testing.T, img *image.NRGBA, x, y int, want color.NRGBA) {
	t.Helper()
	if got := img.NRGBAAt(x, y); got != want {
		t.Fatalf("pixel (%d,%d) = %+v, want %+v", x, y, got, want)
	}
}

func TestDecodeDIB24BottomUp(t *testing.T) {
	payload := bitmapHeader(40, 2, 2, 24, biRGB, 0)
	payload = append(payload, rowBytes(packBGR(blue, white), packBGR(red, green))...)

	img, err := decodeDIB(payload)
	if err != nil {
		t.Fatalf("decodeDIB: %v", err)
	}
	if got := img.Bounds(); got != image.Rect(0, 0, 2, 2) {
		t.Fatalf("bounds = %v, want 2x2", got)
	}
	expectPixel(t, img, 0, 0, red)
	expectPixel(t, img, 1, 0, green)
	expectPixel(t, img, 0, 1, blue)
	expectPixel(t, img, 1, 1, white)
}

func TestDecodeDIB24TopDown(t *testing.T) {
	payload := bitmapHeader(40, 2, -2, 24, biRGB, 0)
	payload = append(payload, rowBytes(packBGR(red, green), packBGR(blue, white))...)

	img, err := decodeDIB(payload)
	if err != nil {
		t.Fatalf("decodeDIB: %v", err)
	}
	expectPixel(t, img, 0, 0, red)
	expectPixel(t, img, 1, 0, green)
	expectPixel(t, img, 0, 1, blue)
	expectPixel(t, img, 1, 1, white)
}

func TestDecodeDIB32WithAlpha(t *testing.T) {
	header := setMasks(bitmapHeader(dibMaxHeaderSize, 2, 1, 32, biRGB, 0), 0x00FF0000, 0x0000FF00, 0x000000FF, 0xFF000000)
	payload := withPixels(header, packWords(0x80FF0000, 0x0000FF00))

	img, err := decodeDIB(payload)
	if err != nil {
		t.Fatalf("decodeDIB: %v", err)
	}
	expectPixel(t, img, 0, 0, color.NRGBA{R: 0xFF, A: 0x80})
	expectPixel(t, img, 1, 0, color.NRGBA{G: 0xFF, A: 0})
}

func TestDecodeDIB32AllZeroAlphaIsOpaque(t *testing.T) {
	header := setMasks(bitmapHeader(dibMaxHeaderSize, 1, 1, 32, biRGB, 0), 0x00FF0000, 0x0000FF00, 0x000000FF, 0xFF000000)
	payload := withPixels(header, packWords(0x000000FF))

	img, err := decodeDIB(payload)
	if err != nil {
		t.Fatalf("decodeDIB: %v", err)
	}
	expectPixel(t, img, 0, 0, blue)
}

func TestDecodeDIB32WithoutAlphaMaskIgnoresAlphaBytes(t *testing.T) {
	header := bitmapHeader(dibMaxHeaderSize, 1, 1, 32, biRGB, 0)
	payload := withPixels(header, packWords(0x00FF0000))

	img, err := decodeDIB(payload)
	if err != nil {
		t.Fatalf("decodeDIB: %v", err)
	}
	expectPixel(t, img, 0, 0, red)
}

func TestDecodeDIB16DefaultMasks(t *testing.T) {
	header := bitmapHeader(40, 1, 1, 16, biRGB, 0)
	payload := withPixels(header, make([]byte, 4))
	binary.LittleEndian.PutUint16(payload[dibHeaderSize:], 0x7C00)

	img, err := decodeDIB(payload)
	if err != nil {
		t.Fatalf("decodeDIB: %v", err)
	}
	expectPixel(t, img, 0, 0, red)
}

func TestDecodeDIB16Bitfields(t *testing.T) {
	header := bitmapHeader(40, 1, 1, 16, biBitfields, 0)
	for _, mask := range []uint32{0xF800, 0x07E0, 0x001F} {
		header = binary.LittleEndian.AppendUint32(header, mask)
	}
	payload := withPixels(header, make([]byte, 4))
	binary.LittleEndian.PutUint16(payload[dibHeaderSize+12:], 0x07E0)

	img, err := decodeDIB(payload)
	if err != nil {
		t.Fatalf("decodeDIB: %v", err)
	}
	expectPixel(t, img, 0, 0, green)
}

func TestDecodeDIB8Palette(t *testing.T) {
	header := bitmapHeader(40, 2, 1, 8, biRGB, 2)
	palette := []byte{
		0xFF, 0x00, 0x00, 0x00,
		0x00, 0x00, 0xFF, 0x00,
	}
	payload := withPixels(header, palette)
	payload = append(payload, 1, 0, 0, 0)

	img, err := decodeDIB(payload)
	if err != nil {
		t.Fatalf("decodeDIB: %v", err)
	}
	expectPixel(t, img, 0, 0, red)
	expectPixel(t, img, 1, 0, blue)
}

func TestDecodeDIBPaletteIndexBeyondColors(t *testing.T) {
	header := bitmapHeader(40, 2, 1, 8, biRGB, 1)
	payload := withPixels(header, []byte{0xFF, 0x00, 0x00, 0x00})
	payload = append(payload, 0, 200, 0, 0)

	img, err := decodeDIB(payload)
	if err != nil {
		t.Fatalf("decodeDIB: %v", err)
	}
	expectPixel(t, img, 0, 0, blue)
	expectPixel(t, img, 1, 0, blue)
}

func TestDecodeDIB1Bit(t *testing.T) {
	header := bitmapHeader(40, 8, 1, 1, biRGB, 2)
	palette := []byte{
		0x00, 0x00, 0x00, 0x00,
		0xFF, 0xFF, 0xFF, 0x00,
	}
	payload := withPixels(header, palette)
	payload = append(payload, 0b10100000, 0, 0, 0)

	img, err := decodeDIB(payload)
	if err != nil {
		t.Fatalf("decodeDIB: %v", err)
	}
	want := []color.NRGBA{white, {A: 0xFF}, white, {A: 0xFF}, {A: 0xFF}, {A: 0xFF}, {A: 0xFF}, {A: 0xFF}}
	for x := range 8 {
		expectPixel(t, img, x, 0, want[x])
	}
}

func TestDecodeDIBEmbeddedPNG(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	source.SetNRGBA(0, 0, red)
	source.SetNRGBA(1, 0, blue)
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, source); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	payload := append(bitmapHeader(40, 2, 1, 32, biPNG, 0), buffer.Bytes()...)

	img, err := decodeDIB(payload)
	if err != nil {
		t.Fatalf("decodeDIB: %v", err)
	}
	expectPixel(t, img, 0, 0, red)
	expectPixel(t, img, 1, 0, blue)
}

func TestDecodeDIBRejectsBrokenPayloads(t *testing.T) {
	truncated := bitmapHeader(40, 2, 2, 24, biRGB, 0)
	palette := bitmapHeader(40, 2, 1, 8, biRGB, 256)

	tests := map[string][]byte{
		"empty":              nil,
		"short":              make([]byte, 20),
		"zero header":        make([]byte, 40),
		"header beyond data": bitmapHeader(40, 1, 1, 24, biRGB, 0)[:10],
		"header too large":   bitmapHeader(dibMaxHeaderSize+4, 1, 1, 24, biRGB, 0),
		"zero width":         bitmapHeader(40, 0, 1, 24, biRGB, 0),
		"zero height":        bitmapHeader(40, 1, 0, 24, biRGB, 0),
		"side limit":         bitmapHeader(40, maxImageSide+1, 1, 24, biRGB, 0),
		"pixel limit":        bitmapHeader(40, 8000, 8000, 24, biRGB, 0),
		"depth":              bitmapHeader(40, 1, 1, 7, biRGB, 0),
		"compression":        bitmapHeader(40, 1, 1, 24, 1, 0),
		"truncated pixels":   truncated,
		"truncated palette":  palette,
		"masks missing":      bitmapHeader(40, 1, 1, 16, biBitfields, 0),
		"png without stream": bitmapHeader(40, 1, 1, 32, biPNG, 0),
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			if img, err := decodeDIB(payload); err == nil {
				t.Fatalf("decodeDIB returned a %v image, want an error", img.Bounds())
			}
		})
	}
}

func TestDIBToPNG(t *testing.T) {
	payload := bitmapHeader(40, 2, 1, 24, biRGB, 0)
	payload = append(payload, packBGR(red, blue)...)

	encoded, err := dibToPNG(payload)
	if err != nil {
		t.Fatalf("dibToPNG: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	if got := img.Bounds(); got != image.Rect(0, 0, 2, 1) {
		t.Fatalf("bounds = %v, want 2x1", got)
	}
	if got := color.NRGBAModel.Convert(img.At(0, 0)); got != red {
		t.Fatalf("first pixel = %+v, want %+v", got, red)
	}
}

// pngHeader builds the PNG signature and header that declares the size.
func pngHeader(width, height uint32) []byte {
	data := make([]byte, 13)
	binary.BigEndian.PutUint32(data[0:], width)
	binary.BigEndian.PutUint32(data[4:], height)
	data[8] = 8
	data[9] = 2

	payload := []byte("\x89PNG\r\n\x1a\n")
	payload = binary.BigEndian.AppendUint32(payload, uint32(len(data)))
	payload = append(payload, "IHDR"...)
	payload = append(payload, data...)
	return binary.BigEndian.AppendUint32(payload, crc32.ChecksumIEEE(payload[12:]))
}

func TestCheckPNGDimensions(t *testing.T) {
	tests := map[string]struct {
		payload []byte
		valid   bool
	}{
		"small":       {payload: pngHeader(2, 2), valid: true},
		"side limit":  {payload: pngHeader(maxImageSide+1, 1)},
		"pixel limit": {payload: pngHeader(8000, 8000)},
		"zero width":  {payload: pngHeader(0, 1)},
		"not a png":   {payload: []byte("GIF89a")},
		"empty":       {},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := checkPNG(test.payload)
			if test.valid && err != nil {
				t.Fatalf("checkPNG: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("checkPNG accepted the payload")
			}
		})
	}
}

func TestEncodeDIBRoundTrip(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	pixels := []color.NRGBA{red, green, blue, white, {R: 0x10, G: 0x20, B: 0x30, A: 0xFF}, {G: 0x80, A: 0xFF}}
	for i, pixel := range pixels {
		source.SetNRGBA(i%3, i/3, pixel)
	}

	payload, err := encodeDIB(source)
	if err != nil {
		t.Fatalf("encodeDIB: %v", err)
	}
	img, err := decodeDIB(payload)
	if err != nil {
		t.Fatalf("decodeDIB: %v", err)
	}
	for i, pixel := range pixels {
		expectPixel(t, img, i%3, i/3, pixel)
	}
}

func TestEncodeDIBCompositesOverWhite(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	source.SetNRGBA(0, 0, color.NRGBA{R: 0xFF, A: 0x80})

	payload, err := encodeDIB(source)
	if err != nil {
		t.Fatalf("encodeDIB: %v", err)
	}
	img, err := decodeDIB(payload)
	if err != nil {
		t.Fatalf("decodeDIB: %v", err)
	}
	pixel := img.NRGBAAt(0, 0)
	if pixel.R != 0xFF || pixel.A != 0xFF {
		t.Fatalf("pixel = %+v, want opaque red", pixel)
	}
	if pixel.G < 0x7C || pixel.G > 0x80 || pixel.B < 0x7C || pixel.B > 0x80 {
		t.Fatalf("pixel = %+v, want the red blended with white", pixel)
	}
}

func TestPNGToDIB(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	source.SetNRGBA(0, 0, red)
	source.SetNRGBA(1, 0, green)
	source.SetNRGBA(0, 1, blue)
	source.SetNRGBA(1, 1, white)
	encoded, err := encodePNG(source)
	if err != nil {
		t.Fatalf("encodePNG: %v", err)
	}

	payload, err := pngToDIB(encoded)
	if err != nil {
		t.Fatalf("pngToDIB: %v", err)
	}
	img, err := decodeDIB(payload)
	if err != nil {
		t.Fatalf("decodeDIB: %v", err)
	}
	expectPixel(t, img, 0, 0, red)
	expectPixel(t, img, 1, 0, green)
	expectPixel(t, img, 0, 1, blue)
	expectPixel(t, img, 1, 1, white)
}

func TestPNGToDIBRejectsBrokenPNG(t *testing.T) {
	if _, err := pngToDIB([]byte("not a png")); err == nil {
		t.Fatal("pngToDIB accepted a payload that is not PNG")
	}
}
