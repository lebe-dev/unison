package share

import (
	"errors"
	"strings"
	"testing"

	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

func TestFilesOfRejectsBadOffers(t *testing.T) {
	hash := make([]byte, 32)
	hash[0] = 1

	tests := []struct {
		name  string
		files []*wirev1.FileMeta
		want  error
	}{
		{name: "none", files: nil, want: ErrNoFiles},
		{name: "empty name", files: []*wirev1.FileMeta{{Name: " ", Blake3: hash}}, want: ErrInvalidFile},
		{name: "short hash", files: []*wirev1.FileMeta{{Name: "a.txt", Blake3: hash[:16]}}, want: ErrInvalidFile},
		{name: "zero hash", files: []*wirev1.FileMeta{{Name: "a.txt", Blake3: make([]byte, 32)}}, want: ErrInvalidFile},
		{name: "nil file", files: []*wirev1.FileMeta{nil}, want: ErrInvalidFile},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := filesOf(test.files); !errors.Is(err, test.want) {
				t.Fatalf("error is %v, want %v", err, test.want)
			}
		})
	}
}

func TestFilesOfSanitizesNames(t *testing.T) {
	hash := make([]byte, 32)
	hash[0] = 1

	files, err := filesOf([]*wirev1.FileMeta{{Name: "../etc/passwd", Blake3: hash}})
	if err != nil {
		t.Fatalf("read the offer: %v", err)
	}
	if files[0].Name != "passwd" {
		t.Fatalf("the name is %q", files[0].Name)
	}
}

func TestFilesOfLimit(t *testing.T) {
	hash := make([]byte, 32)
	hash[0] = 1

	metas := make([]*wirev1.FileMeta, MaxFiles+1)
	for i := range metas {
		metas[i] = &wirev1.FileMeta{Name: "file.txt", Blake3: hash}
	}
	if _, err := filesOf(metas); !errors.Is(err, ErrTooManyFiles) {
		t.Fatalf("error is %v, want %v", err, ErrTooManyFiles)
	}
}

func TestCheckTransferID(t *testing.T) {
	tests := []struct {
		id    string
		valid bool
	}{
		{id: "0123456789abcdef", valid: true},
		{id: "", valid: false},
		{id: "with space", valid: false},
		{id: "with\x00control", valid: false},
		{id: strings.Repeat("a", 129), valid: false},
	}
	for _, test := range tests {
		if err := checkTransferID(test.id); (err == nil) != test.valid {
			t.Fatalf("checkTransferID(%q) = %v", test.id, err)
		}
	}
}
