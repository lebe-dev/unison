package clipboard

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

// testImage returns a small image with a few colours.
func testImage() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 3))
	for x := range 4 {
		for y := range 3 {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(40 * x), G: uint8(60 * y), B: uint8(20 * (x + y)), A: 255})
		}
	}
	return img
}

// encodeTestImage encodes the image in the given format.
func encodeTestImage(t *testing.T, format string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	var err error
	switch format {
	case MIMEImagePNG:
		err = png.Encode(&buffer, testImage())
	case MIMEImageBMP:
		err = bmp.Encode(&buffer, testImage())
	case MIMEImageTIFF:
		err = tiff.Encode(&buffer, testImage(), nil)
	case MIMEImageJPEG:
		err = jpeg.Encode(&buffer, testImage(), nil)
	default:
		t.Fatalf("unknown format %q", format)
	}
	if err != nil {
		t.Fatalf("encode %s: %v", format, err)
	}
	return buffer.Bytes()
}

func TestImageTargetPriority(t *testing.T) {
	cases := []struct {
		name    string
		targets targetList
		want    string
	}{
		{name: "png first", targets: targetList{MIMEImageJPEG, MIMEImageBMP, MIMEImagePNG}, want: MIMEImagePNG},
		{name: "bmp before tiff", targets: targetList{MIMEImageTIFF, MIMEImageBMP}, want: MIMEImageBMP},
		{name: "jpeg only", targets: targetList{MIMEImageJPEG}, want: MIMEImageJPEG},
		{name: "text only", targets: targetList{MIMETextPlain}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.targets.pickImage(); got != tc.want {
				t.Fatalf("pickImage() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReadImagePNGPassesThrough(t *testing.T) {
	source := encodeTestImage(t, MIMEImagePNG)
	got, err := readImagePNG(MIMEImagePNG, source)
	if err != nil {
		t.Fatalf("readImagePNG: %v", err)
	}
	if !bytes.Equal(got, source) {
		t.Fatal("the PNG payload changed, the own write hash would not match")
	}
}

func TestReadImageConvertsToPNG(t *testing.T) {
	for _, format := range []string{MIMEImageBMP, MIMEImageTIFF, MIMEImageJPEG} {
		t.Run(format, func(t *testing.T) {
			got, err := readImagePNG(format, encodeTestImage(t, format))
			if err != nil {
				t.Fatalf("readImagePNG: %v", err)
			}
			cfg, err := png.DecodeConfig(bytes.NewReader(got))
			if err != nil {
				t.Fatalf("decode converted png: %v", err)
			}
			if cfg.Width != 4 || cfg.Height != 3 {
				t.Fatalf("converted image is %dx%d, want 4x3", cfg.Width, cfg.Height)
			}
		})
	}
}

func TestReadImageRejectsBrokenPayload(t *testing.T) {
	if _, err := readImagePNG(MIMEImagePNG, []byte("not a png")); err == nil {
		t.Fatal("readImagePNG accepted a broken PNG")
	}
	if _, err := imageToPNG([]byte("not an image")); err == nil {
		t.Fatal("imageToPNG accepted a broken image")
	}
}

func TestCheckImageLimits(t *testing.T) {
	cases := []struct {
		name    string
		cfg     image.Config
		wantErr bool
	}{
		{name: "small", cfg: image.Config{Width: 1920, Height: 1080}},
		{name: "no pixels", cfg: image.Config{Width: 0, Height: 10}, wantErr: true},
		{name: "side limit", cfg: image.Config{Width: maxImageSide + 1, Height: 1}, wantErr: true},
		{name: "pixel limit", cfg: image.Config{Width: maxImageSide, Height: maxImageSide}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkImageLimits(tc.cfg)
			if (err != nil) != tc.wantErr {
				t.Fatalf("checkImageLimits(%+v) = %v, want error %v", tc.cfg, err, tc.wantErr)
			}
		})
	}
}

func TestCheckPNGRejectsWideImage(t *testing.T) {
	wide := image.NewNRGBA(image.Rect(0, 0, maxImageSide+1, 1))
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, wide); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := checkPNG(buffer.Bytes()); err == nil {
		t.Fatal("checkPNG accepted an image above the side limit")
	}
}
