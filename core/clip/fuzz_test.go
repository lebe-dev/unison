package clip

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"lukechampine.com/blake3"

	"github.com/lebe-dev/unison/core/model/protoconv"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// FuzzClipAnnounce checks that an announced item is either refused or passes
// the checks the pipeline runs before it reaches the history and the clipboard.
func FuzzClipAnnounce(f *testing.F) {
	text := []byte("copied text")
	sum := blake3.Sum256(text)
	seed := &wirev1.ClipAnnounce{Item: &wirev1.ClipItem{
		Id:     make([]byte, 16),
		Origin: make([]byte, 32),
		Seq:    1,
		Kind:   wirev1.Kind_KIND_TEXT,
		Source: wirev1.Source_SOURCE_CLIPBOARD,
		Representations: []*wirev1.Representation{{
			MimeType:    MIMETextPlain,
			Size:        uint64(len(text)),
			Blake3:      sum[:],
			InlineBytes: text,
		}},
	}}
	if data, err := proto.Marshal(seed); err == nil {
		f.Add(data)
	}

	file := []byte("file content")
	fileSum := blake3.Sum256(file)
	fileSeed := &wirev1.ClipAnnounce{Item: &wirev1.ClipItem{
		Id:     make([]byte, 16),
		Origin: make([]byte, 32),
		Seq:    2,
		Kind:   wirev1.Kind_KIND_FILES,
		Source: wirev1.Source_SOURCE_CLIPBOARD,
		Representations: []*wirev1.Representation{{
			MimeType:    MIMEUnisonFile,
			Size:        uint64(len(file)),
			Blake3:      fileSum[:],
			InlineBytes: file,
			FileName:    "report.pdf",
		}},
	}}
	if data, err := proto.Marshal(fileSeed); err == nil {
		f.Add(data)
	}
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		announce := &wirev1.ClipAnnounce{}
		if err := proto.Unmarshal(data, announce); err != nil {
			return
		}
		item, err := protoconv.ClipItemFromProto(announce.GetItem())
		if err != nil {
			return
		}
		if err := validateItem(item); err != nil {
			return
		}

		var inline int
		for i, rep := range item.Representations {
			inline += len(rep.Inline)
			name := downloadName(rep, i)
			if name == "" || name == "." || name == ".." {
				t.Fatalf("representation %d gets the file name %q", i, name)
			}
			if strings.ContainsAny(name, `/\`) {
				t.Fatalf("representation %d gets the path %q", i, name)
			}
		}
		if inline > MaxInlineText {
			t.Fatalf("accepted %d inline bytes", inline)
		}
	})
}
