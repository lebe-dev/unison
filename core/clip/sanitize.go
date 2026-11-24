package clip

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"

	"golang.org/x/image/draw"
)

// MaxThumbnailSide bounds the longer side of a thumbnail in pixels.
const MaxThumbnailSide = 1024

// ErrImageFormat reports an image format that is not accepted from a peer.
var ErrImageFormat = errors.New("clip: image format is not accepted")

// Signatures of the image formats accepted from a peer.
var (
	pngSignature   = []byte("\x89PNG\r\n\x1a\n")
	jpegSignature  = []byte{0xff, 0xd8, 0xff}
	gif87Signature = []byte("GIF87a")
	gif89Signature = []byte("GIF89a")
)

// Pixels is a decoded image as rows of non-premultiplied RGBA bytes.
type Pixels struct {
	Width  int
	Height int
	RGBA   []byte
}

// imageCodec decodes one image format.
type imageCodec struct {
	mime   string
	config func(io.Reader) (image.Config, error)
	decode func(io.Reader) (image.Image, error)
}

// receivedCodec picks the decoder of an image that came from a peer by its
// signature. Only PNG, JPEG and GIF are accepted.
func receivedCodec(data []byte) (imageCodec, error) {
	switch {
	case bytes.HasPrefix(data, pngSignature):
		return imageCodec{mime: MIMEImagePNG, config: png.DecodeConfig, decode: png.Decode}, nil
	case bytes.HasPrefix(data, jpegSignature):
		return imageCodec{mime: "image/jpeg", config: jpeg.DecodeConfig, decode: jpeg.Decode}, nil
	case bytes.HasPrefix(data, gif87Signature), bytes.HasPrefix(data, gif89Signature):
		return imageCodec{mime: "image/gif", config: gif.DecodeConfig, decode: gif.Decode}, nil
	default:
		return imageCodec{}, ErrImageFormat
	}
}

// SniffImage reports the MIME type of r when it holds an image accepted by
// Thumbnail and sanitizeImage within the image size and dimension limits.
func SniffImage(r io.Reader, size int64) (string, bool) {
	if size <= 0 || size > MaxImageBytes {
		return "", false
	}
	var header [64]byte
	n, err := io.ReadFull(r, header[:])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", false
	}
	codec, err := receivedCodec(header[:n])
	if err != nil {
		return "", false
	}
	cfg, err := codec.config(io.MultiReader(bytes.NewReader(header[:n]), r))
	if err != nil {
		return "", false
	}
	if checkImageBounds(cfg) != nil {
		return "", false
	}
	return codec.mime, true
}

// decodeReceived decodes an image of a peer within the image limits. A panic
// of a decoder is reported as an error.
func decodeReceived(data []byte) (decoded image.Image, err error) {
	if len(data) > MaxImageBytes {
		return nil, fmt.Errorf("image of %d bytes, limit is %d", len(data), MaxImageBytes)
	}
	codec, err := receivedCodec(data)
	if err != nil {
		return nil, err
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			decoded, err = nil, fmt.Errorf("decode image: decoder failed: %v", recovered)
		}
	}()

	cfg, err := codec.config(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image header: %w", err)
	}
	if err := checkImageBounds(cfg); err != nil {
		return nil, err
	}
	decoded, err = codec.decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	if size := decoded.Bounds().Size(); size.X != cfg.Width || size.Y != cfg.Height {
		return nil, fmt.Errorf("decoded image is %dx%d, header says %dx%d", size.X, size.Y, cfg.Width, cfg.Height)
	}
	return decoded, nil
}

// sanitizeImage decodes an image of a peer and encodes it again as PNG. The
// decoders of the platform then only see PNG that this encoder produced, never
// the bytes of the peer.
func sanitizeImage(data []byte) ([]byte, error) {
	decoded, err := decodeReceived(data)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, decoded); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return buffer.Bytes(), nil
}

// Thumbnail decodes an image with the decoders accepted from a peer and scales
// it down to fit maxSide. A smaller image keeps its size.
func Thumbnail(data []byte, maxSide int) (Pixels, error) {
	if maxSide <= 0 || maxSide > MaxThumbnailSide {
		return Pixels{}, fmt.Errorf("thumbnail side %d is outside 1..%d", maxSide, MaxThumbnailSide)
	}
	decoded, err := decodeReceived(data)
	if err != nil {
		return Pixels{}, err
	}

	source := decoded.Bounds()
	width, height := fitSide(source.Dx(), source.Dy(), maxSide)
	target := image.NewNRGBA(image.Rect(0, 0, width, height))
	if width == source.Dx() && height == source.Dy() {
		draw.Draw(target, target.Bounds(), decoded, source.Min, draw.Src)
	} else {
		draw.BiLinear.Scale(target, target.Bounds(), decoded, source, draw.Src, nil)
	}
	return Pixels{Width: width, Height: height, RGBA: target.Pix}, nil
}

// fitSide returns the size of an image scaled down so that its longer side is
// at most maxSide. Neither side drops below one pixel.
func fitSide(width, height, maxSide int) (int, int) {
	if width <= maxSide && height <= maxSide {
		return width, height
	}
	if width >= height {
		return maxSide, max(1, height*maxSide/width)
	}
	return max(1, width*maxSide/height), maxSide
}
