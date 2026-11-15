package clipboard

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/bits"
)

// Image limits. A header is checked against them before its pixels are
// allocated.
const (
	// maxImagePixels bounds the pixel count of one image.
	maxImagePixels = 24 << 20
	// maxImageSide bounds one side of an image in pixels.
	maxImageSide = 8192
	// maxImageBytes bounds the encoded size of one image.
	maxImageBytes = 64 << 20
	// maxDIBBytes bounds one decoded bitmap.
	maxDIBBytes = 128 << 20
)

// Bitmap header layout and compression identifiers.
const (
	// dibHeaderSize is the size of BITMAPINFOHEADER.
	dibHeaderSize = 40
	// dibMaxHeaderSize is the size of BITMAPV5HEADER, the largest header the
	// parser accepts.
	dibMaxHeaderSize = 124
	// dibMaskOffset is where BITMAPV4HEADER and BITMAPV5HEADER carry the
	// channel masks.
	dibMaskOffset = 40
	// pelsPerMeter is the resolution the encoded bitmap declares, 72 DPI.
	pelsPerMeter = 2835

	biRGB       = 0
	biBitfields = 3
	biPNG       = 5

	// rowAlign is the alignment of a bitmap row in bytes.
	rowAlign = 4
)

// dibHeader is the parsed header of a device independent bitmap.
type dibHeader struct {
	width       int
	rows        int
	topDown     bool
	bitCount    uint32
	compression uint32
	colors      int
	palette     int
	stride      int
	pixels      int
	masks       [4]uint32
}

// decodeDIB converts a device independent bitmap into an image.
func decodeDIB(payload []byte) (*image.NRGBA, error) {
	header, err := parseDIB(payload)
	if err != nil {
		return nil, err
	}
	switch {
	case header.compression == biPNG:
		return decodePNG(payload[header.pixels:])
	case header.bitCount <= 8:
		return decodePalette(payload, header), nil
	case header.bitCount == 24:
		return decodeBGR(payload, header), nil
	default:
		return decodeMasks(payload, header), nil
	}
}

// parseDIB validates a bitmap header and locates the palette and the pixels.
func parseDIB(payload []byte) (dibHeader, error) {
	if len(payload) < dibHeaderSize {
		return dibHeader{}, fmt.Errorf("clipboard: bitmap of %d bytes has no header", len(payload))
	}
	size := binary.LittleEndian.Uint32(payload)
	if size < dibHeaderSize || int(size) > len(payload) || size > dibMaxHeaderSize {
		return dibHeader{}, fmt.Errorf("clipboard: bitmap header size %d is invalid", size)
	}

	header := dibHeader{
		compression: binary.LittleEndian.Uint32(payload[16:]),
		bitCount:    uint32(binary.LittleEndian.Uint16(payload[14:])),
		colors:      int(binary.LittleEndian.Uint32(payload[32:])),
		palette:     int(size),
	}
	if header.compression == biPNG {
		header.pixels = int(size)
		return header, nil
	}

	width := int64(int32(binary.LittleEndian.Uint32(payload[4:])))
	height := int64(int32(binary.LittleEndian.Uint32(payload[8:])))
	header.topDown = height < 0
	if header.topDown {
		height = -height
	}
	if err := checkImageSize(width, height); err != nil {
		return dibHeader{}, err
	}
	header.width, header.rows = int(width), int(height)

	switch header.compression {
	case biRGB, biBitfields:
	default:
		return dibHeader{}, fmt.Errorf("clipboard: unsupported bitmap compression %d", header.compression)
	}
	switch header.bitCount {
	case 1, 4, 8, 16, 24, 32:
	default:
		return dibHeader{}, fmt.Errorf("clipboard: unsupported bitmap depth %d", header.bitCount)
	}

	if size >= dibMaskOffset+12 {
		for i := range 3 {
			header.masks[i] = binary.LittleEndian.Uint32(payload[dibMaskOffset+4*i:])
		}
	}
	if size >= dibMaskOffset+16 {
		header.masks[3] = binary.LittleEndian.Uint32(payload[52:])
	}
	if header.compression == biBitfields && size < dibMaskOffset+12 {
		masksEnd := dibHeaderSize + 12
		if len(payload) < masksEnd {
			return dibHeader{}, errors.New("clipboard: bitmap has no channel masks")
		}
		for i := range 3 {
			header.masks[i] = binary.LittleEndian.Uint32(payload[dibHeaderSize+4*i:])
		}
		header.palette = masksEnd
	}
	if header.masks[0] == 0 && header.masks[1] == 0 && header.masks[2] == 0 {
		switch header.bitCount {
		case 16:
			header.masks[0], header.masks[1], header.masks[2] = 0x7C00, 0x03E0, 0x001F
		case 32:
			header.masks[0], header.masks[1], header.masks[2] = 0x00FF0000, 0x0000FF00, 0x000000FF
		}
	}

	if header.bitCount <= 8 {
		if header.colors <= 0 || header.colors > 1<<header.bitCount {
			header.colors = 1 << header.bitCount
		}
		paletteEnd := header.palette + 4*header.colors
		if paletteEnd > len(payload) {
			return dibHeader{}, fmt.Errorf("clipboard: palette of %d colors exceeds the bitmap", header.colors)
		}
		header.pixels = paletteEnd
	} else {
		header.pixels = header.palette
	}
	header.stride = int((int64(header.width)*int64(header.bitCount) + 8*rowAlign - 1) / (8 * rowAlign) * rowAlign)
	end := int64(header.pixels) + int64(header.stride)*int64(header.rows)
	if end > int64(len(payload)) {
		return dibHeader{}, fmt.Errorf("clipboard: %d pixel bytes exceed the bitmap of %d bytes", end, len(payload))
	}
	return header, nil
}

// decodePalette converts a bitmap of up to 8 bits per pixel through its palette.
func decodePalette(payload []byte, header dibHeader) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, header.width, header.rows))
	perByte := int(8 / header.bitCount)
	mask := byte(1<<header.bitCount - 1)
	for y := range header.rows {
		row := payload[header.pixels+rowOffset(header, y)*header.stride:]
		for x := range header.width {
			shift := 8 - int(header.bitCount)*(x%perByte+1)
			index := int((row[x/perByte] >> shift) & mask)
			if index >= header.colors {
				index = 0
			}
			entry := header.palette + 4*index
			pixel := img.PixOffset(x, y)
			img.Pix[pixel], img.Pix[pixel+1], img.Pix[pixel+2] = payload[entry+2], payload[entry+1], payload[entry]
			img.Pix[pixel+3] = 0xFF
		}
	}
	return img
}

// decodeBGR converts a bitmap of 24 bits per pixel.
func decodeBGR(payload []byte, header dibHeader) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, header.width, header.rows))
	for y := range header.rows {
		row := payload[header.pixels+rowOffset(header, y)*header.stride:]
		for x := range header.width {
			pixel := img.PixOffset(x, y)
			img.Pix[pixel], img.Pix[pixel+1], img.Pix[pixel+2] = row[3*x+2], row[3*x+1], row[3*x]
			img.Pix[pixel+3] = 0xFF
		}
	}
	return img
}

// decodeMasks converts a bitmap of 16 or 32 bits per pixel through its channel
// masks. An alpha mask whose channel is zero everywhere is read as opaque,
// because applications leave it unset when they copy screen content.
func decodeMasks(payload []byte, header dibHeader) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, header.width, header.rows))
	transparent := false
	for y := range header.rows {
		row := payload[header.pixels+rowOffset(header, y)*header.stride:]
		for x := range header.width {
			var word uint32
			if header.bitCount == 16 {
				word = uint32(binary.LittleEndian.Uint16(row[2*x:]))
			} else {
				word = binary.LittleEndian.Uint32(row[4*x:])
			}
			pixel := img.PixOffset(x, y)
			img.Pix[pixel] = channelValue(word, header.masks[0])
			img.Pix[pixel+1] = channelValue(word, header.masks[1])
			img.Pix[pixel+2] = channelValue(word, header.masks[2])
			alpha := uint8(0xFF)
			if header.masks[3] != 0 {
				alpha = channelValue(word, header.masks[3])
				transparent = transparent || alpha != 0
			}
			img.Pix[pixel+3] = alpha
		}
	}
	if header.masks[3] != 0 && !transparent {
		makeOpaque(img)
	}
	return img
}

// rowOffset returns the storage row of the visible row y. A positive bitmap
// height stores the rows bottom-up.
func rowOffset(header dibHeader, y int) int {
	if header.topDown {
		return y
	}
	return header.rows - 1 - y
}

// channelValue extracts the channel addressed by mask from a pixel word.
func channelValue(word, mask uint32) uint8 {
	if mask == 0 {
		return 0
	}
	shift := bits.TrailingZeros32(mask)
	depth := bits.OnesCount32(mask)
	value := (word & mask) >> shift
	if depth >= 8 {
		return uint8(value >> (depth - 8))
	}
	return uint8(value * 255 / (1<<depth - 1))
}

// makeOpaque clears the transparency of an image.
func makeOpaque(img *image.NRGBA) {
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 0xFF
	}
}

// encodeDIB converts an image into the bitmap applications without PNG support
// read: bottom-up 24 bit rows on a white background.
func encodeDIB(img image.Image) ([]byte, error) {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if err := checkImageSize(int64(width), int64(height)); err != nil {
		return nil, err
	}
	stride := (width*3 + rowAlign - 1) &^ (rowAlign - 1)
	size := int64(dibHeaderSize) + int64(stride)*int64(height)
	if size > maxDIBBytes {
		return nil, fmt.Errorf("clipboard: bitmap of %d bytes exceeds the limit %d", size, maxDIBBytes)
	}

	canvas := opaqueOrComposited(img, bounds)
	payload := make([]byte, size)
	binary.LittleEndian.PutUint32(payload[0:], dibHeaderSize)
	binary.LittleEndian.PutUint32(payload[4:], uint32(width))
	binary.LittleEndian.PutUint32(payload[8:], uint32(height))
	binary.LittleEndian.PutUint16(payload[12:], 1)
	binary.LittleEndian.PutUint16(payload[14:], 24)
	binary.LittleEndian.PutUint32(payload[20:], uint32(stride*height))
	binary.LittleEndian.PutUint32(payload[24:], pelsPerMeter)
	binary.LittleEndian.PutUint32(payload[28:], pelsPerMeter)
	for y := range height {
		source := canvas.Pix[y*canvas.Stride:]
		target := payload[dibHeaderSize+(height-1-y)*stride:]
		for x := range width {
			target[3*x], target[3*x+1], target[3*x+2] = source[4*x+2], source[4*x+1], source[4*x]
		}
	}
	return payload, nil
}

// opaqueOrComposited returns the image as NRGBA without transparency. A bitmap
// has no alpha channel, so translucent pixels are blended onto white.
func opaqueOrComposited(img image.Image, bounds image.Rectangle) *image.NRGBA {
	if source, ok := img.(*image.NRGBA); ok && source.Opaque() {
		return source
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(canvas, canvas.Bounds(), img, bounds.Min, draw.Over)
	return canvas
}

// dibToPNG converts a CF_DIB or CF_DIBV5 payload into PNG.
func dibToPNG(payload []byte) ([]byte, error) {
	img, err := decodeDIB(payload)
	if err != nil {
		return nil, err
	}
	return encodePNG(img)
}

// pngToDIB converts PNG bytes into the CF_DIB payload of the same image.
func pngToDIB(payload []byte) ([]byte, error) {
	img, err := decodePNG(payload)
	if err != nil {
		return nil, err
	}
	return encodeDIB(img)
}

// encodePNG encodes an image as PNG.
func encodePNG(img image.Image) ([]byte, error) {
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		return nil, fmt.Errorf("clipboard: encode png: %w", err)
	}
	if buffer.Len() > maxImageBytes {
		return nil, fmt.Errorf("clipboard: png of %d bytes exceeds the limit %d", buffer.Len(), maxImageBytes)
	}
	return buffer.Bytes(), nil
}

// decodePNG decodes PNG bytes after checking them against the image limits.
func decodePNG(payload []byte) (*image.NRGBA, error) {
	if err := checkPNG(payload); err != nil {
		return nil, err
	}
	img, err := png.Decode(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("clipboard: decode png: %w", err)
	}
	bounds := img.Bounds()
	result := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(result, result.Bounds(), img, bounds.Min, draw.Src)
	return result, nil
}

// checkPNG validates the header of PNG bytes against the image limits.
func checkPNG(payload []byte) error {
	if len(payload) == 0 {
		return errors.New("clipboard: empty png")
	}
	if len(payload) > maxImageBytes {
		return fmt.Errorf("clipboard: png of %d bytes exceeds the limit %d", len(payload), maxImageBytes)
	}
	config, err := png.DecodeConfig(bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("clipboard: decode png header: %w", err)
	}
	return checkImageSize(int64(config.Width), int64(config.Height))
}

// checkImageSize applies the image limits before pixels are allocated.
func checkImageSize(width, height int64) error {
	switch {
	case width <= 0 || height <= 0:
		return fmt.Errorf("clipboard: image of %dx%d has no area", width, height)
	case width > maxImageSide || height > maxImageSide:
		return fmt.Errorf("clipboard: image of %dx%d exceeds the side limit %d", width, height, maxImageSide)
	case width*height > maxImagePixels:
		return fmt.Errorf("clipboard: image of %dx%d exceeds the pixel limit %d", width, height, maxImagePixels)
	}
	return nil
}
