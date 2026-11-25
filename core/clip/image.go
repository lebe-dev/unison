package clip

import (
	"bytes"
	"errors"
	"fmt"
	"image"

	// GIF decoder.
	_ "image/gif"
	// JPEG decoder.
	_ "image/jpeg"
	"image/png"
	"io"
	"os"

	"github.com/lebe-dev/unison/core/ports"
	// BMP decoder.
	_ "golang.org/x/image/bmp"
	// TIFF decoder.
	_ "golang.org/x/image/tiff"
	// WebP decoder.
	_ "golang.org/x/image/webp"
)

// Image limits. An image above them is left out instead of being decoded.
const (
	// MaxImagePixels bounds the pixel count of one image.
	MaxImagePixels = 56 << 20
	// MaxImageSide bounds one side of an image in pixels.
	MaxImageSide = 8192
	// MaxImageBytes bounds the encoded size of one image.
	MaxImageBytes = 64 << 20
)

// imagePayloads normalizes the images of the clipboard to PNG. The original
// format travels as a second representation when it differs from PNG and is
// smaller than twice the PNG size.
func (p *Pipeline) imagePayloads(reps []ports.Representation) []payload {
	payloads := make([]payload, 0, len(reps))
	for _, rep := range reps {
		normalized, err := normalizeImage(rep)
		if err != nil {
			p.log.Debug("skipped clipboard image", "mime", rep.MIME, "error", err)
			continue
		}
		payloads = append(payloads, normalized...)
	}
	return payloads
}

// normalizeImage converts one image representation to PNG. A panic of a
// decoder is reported as an error.
func normalizeImage(rep ports.Representation) (payloads []payload, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			payloads, err = nil, fmt.Errorf("decode image: decoder failed: %v", recovered)
		}
	}()

	source, closeSource, err := openImage(rep)
	if err != nil {
		return nil, err
	}
	defer closeSource()

	size, err := imageSize(rep, source)
	if err != nil {
		return nil, err
	}
	if size > MaxImageBytes {
		return nil, fmt.Errorf("image of %d bytes, limit is %d", size, MaxImageBytes)
	}

	cfg, format, err := image.DecodeConfig(source)
	if err != nil {
		return nil, fmt.Errorf("decode image header: %w", err)
	}
	if err := checkImageBounds(cfg); err != nil {
		return nil, err
	}

	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind image: %w", err)
	}
	pngPayload, err := encodePNG(rep, source, format, size)
	if err != nil {
		return nil, err
	}
	payloads = []payload{pngPayload}

	original := imageMIME(format, rep.MIME)
	if original != MIMEImagePNG && size < int64(2*len(pngPayload.data)) {
		payloads = append(payloads, payload{mime: original, size: size, data: originalData(rep), path: rep.FilePath})
	}
	return payloads, nil
}

// encodePNG returns the PNG payload of the image. A PNG source is kept as is.
func encodePNG(rep ports.Representation, source io.ReadSeeker, format string, size int64) (payload, error) {
	if format == "png" {
		return payload{mime: MIMEImagePNG, size: size, data: originalData(rep), path: rep.FilePath}, nil
	}

	decoded, _, err := image.Decode(source)
	if err != nil {
		return payload{}, fmt.Errorf("decode image: %w", err)
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, decoded); err != nil {
		return payload{}, fmt.Errorf("encode png: %w", err)
	}
	return payload{mime: MIMEImagePNG, size: int64(buffer.Len()), data: buffer.Bytes()}, nil
}

// openImage returns a reader over the image payload.
func openImage(rep ports.Representation) (io.ReadSeeker, func(), error) {
	if rep.FilePath != "" {
		file, err := os.Open(rep.FilePath)
		if err != nil {
			return nil, nil, fmt.Errorf("open image: %w", err)
		}
		return file, func() { _ = file.Close() }, nil
	}
	if len(rep.Data) == 0 {
		return nil, nil, errors.New("image has no payload")
	}
	return bytes.NewReader(rep.Data), func() {}, nil
}

// imageSize returns the encoded size of the image.
func imageSize(rep ports.Representation, source io.ReadSeeker) (int64, error) {
	if rep.FilePath == "" {
		return int64(len(rep.Data)), nil
	}
	file, ok := source.(*os.File)
	if !ok {
		return 0, errors.New("image source is not a file")
	}
	info, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat image: %w", err)
	}
	return info.Size(), nil
}

// originalData returns the payload bytes when the image came from memory.
func originalData(rep ports.Representation) []byte {
	if rep.FilePath != "" {
		return nil
	}
	return rep.Data
}

// checkImageBounds rejects images the pipeline does not decode.
func checkImageBounds(cfg image.Config) error {
	switch {
	case cfg.Width <= 0 || cfg.Height <= 0:
		return fmt.Errorf("image has no pixels")
	case cfg.Width > MaxImageSide || cfg.Height > MaxImageSide:
		return fmt.Errorf("image of %dx%d exceeds the side limit %d", cfg.Width, cfg.Height, MaxImageSide)
	case int64(cfg.Width)*int64(cfg.Height) > MaxImagePixels:
		return fmt.Errorf("image of %dx%d exceeds the pixel limit %d", cfg.Width, cfg.Height, MaxImagePixels)
	default:
		return nil
	}
}

// imageMIME returns the MIME type of an image format.
func imageMIME(format, fallback string) string {
	if format == "" {
		return fallback
	}
	return "image/" + format
}
