package clipboard

import (
	"bytes"
	"fmt"
	"image"

	// JPEG decoder.
	_ "image/jpeg"
	"image/png"

	// BMP decoder.
	_ "golang.org/x/image/bmp"
	// TIFF decoder.
	_ "golang.org/x/image/tiff"
)

// MIME types of the images the clipboard reads and writes.
const (
	// MIMEImagePNG is the image type of a snapshot and the only image type the
	// clipboard writes.
	MIMEImagePNG = "image/png"
	// MIMEImageBMP and MIMEImageTIFF are lossless image targets some X11
	// applications offer instead of PNG.
	MIMEImageBMP  = "image/bmp"
	MIMEImageTIFF = "image/tiff"
	// MIMEImageJPEG is the lossy image target of some applications.
	MIMEImageJPEG = "image/jpeg"
)

// Image limits, the same the clipboard pipeline applies.
const (
	// maxImagePixels bounds the pixel count of one image.
	maxImagePixels = 24 << 20
	// maxImageSide bounds one side of an image in pixels.
	maxImageSide = 8192
	// maxImageBytes bounds the encoded size of one image.
	maxImageBytes = 64 << 20
)

// imageTargets lists the image targets in preference order: PNG travels
// unchanged, the lossless formats come before JPEG.
var imageTargets = []string{MIMEImagePNG, MIMEImageBMP, MIMEImageTIFF, MIMEImageJPEG}

// pickImage returns the preferred image target.
func (t targetList) pickImage() string {
	for _, candidate := range imageTargets {
		if t.has(candidate) {
			return candidate
		}
	}
	return ""
}

// readImagePNG returns the PNG form of an image target value. PNG bytes pass
// through unchanged, so their hash still matches the own write; other formats
// are converted.
func readImagePNG(target string, payload []byte) ([]byte, error) {
	if target == MIMEImagePNG {
		if err := checkPNG(payload); err != nil {
			return nil, err
		}
		return payload, nil
	}
	return imageToPNG(payload)
}

// checkPNG validates the header of PNG bytes against the image limits.
func checkPNG(payload []byte) error {
	if len(payload) > maxImageBytes {
		return fmt.Errorf("clipboard: png of %d bytes exceeds the limit %d", len(payload), maxImageBytes)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("clipboard: decode png header: %w", err)
	}
	return checkImageLimits(cfg)
}

// imageToPNG decodes an image and encodes it again as PNG. A panic of a decoder
// is reported as an error.
func imageToPNG(payload []byte) (converted []byte, err error) {
	if len(payload) > maxImageBytes {
		return nil, fmt.Errorf("clipboard: image of %d bytes exceeds the limit %d", len(payload), maxImageBytes)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			converted, err = nil, fmt.Errorf("clipboard: decode image: decoder failed: %v", recovered)
		}
	}()

	cfg, _, err := image.DecodeConfig(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("clipboard: decode image header: %w", err)
	}
	if err := checkImageLimits(cfg); err != nil {
		return nil, err
	}
	decoded, _, err := image.Decode(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("clipboard: decode image: %w", err)
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, decoded); err != nil {
		return nil, fmt.Errorf("clipboard: encode png: %w", err)
	}
	if buffer.Len() > maxImageBytes {
		return nil, fmt.Errorf("clipboard: png of %d bytes exceeds the limit %d", buffer.Len(), maxImageBytes)
	}
	return buffer.Bytes(), nil
}

// checkImageLimits rejects images the clipboard does not convert.
func checkImageLimits(cfg image.Config) error {
	switch {
	case cfg.Width <= 0 || cfg.Height <= 0:
		return fmt.Errorf("clipboard: image has no pixels")
	case cfg.Width > maxImageSide || cfg.Height > maxImageSide:
		return fmt.Errorf("clipboard: image of %dx%d exceeds the side limit %d", cfg.Width, cfg.Height, maxImageSide)
	case int64(cfg.Width)*int64(cfg.Height) > maxImagePixels:
		return fmt.Errorf("clipboard: image of %dx%d exceeds the pixel limit %d", cfg.Width, cfg.Height, maxImagePixels)
	default:
		return nil
	}
}
