package clipboard

import (
	"testing"
)

func TestParseAtomList(t *testing.T) {
	data := []byte{
		0x01, 0x00, 0x00, 0x00,
		0x02, 0x01, 0x00, 0x00,
		0xFF, 0xFF, 0x00, 0x00,
		0xAA, 0xBB,
	}
	got := parseAtomList(data)
	want := []atom{1, 0x102, 0xFFFF}
	if len(got) != len(want) {
		t.Fatalf("parseAtomList returned %d atoms, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("atom %d = %#x, want %#x", i, got[i], want[i])
		}
	}
}

func TestParseAtomListEmpty(t *testing.T) {
	if got := parseAtomList(nil); len(got) != 0 {
		t.Fatalf("parseAtomList(nil) = %v, want empty", got)
	}
}

func TestTargetsPickPlain(t *testing.T) {
	cases := []struct {
		name    string
		targets targetList
		want    string
		latin1  bool
	}{
		{
			name:    "utf8 preferred",
			targets: targetList{atomSTRING, atomTextPlain, atomUTF8String},
			want:    atomUTF8String,
		},
		{
			name:    "charset utf8 before bare plain",
			targets: targetList{atomSTRING, atomTextPlain, atomTextPlainUTF8},
			want:    atomTextPlainUTF8,
		},
		{
			name:    "bare plain",
			targets: targetList{atomSTRING, atomTextPlain},
			want:    atomTextPlain,
		},
		{
			name:    "string is latin-1",
			targets: targetList{atomSTRING},
			want:    atomSTRING,
			latin1:  true,
		},
		{
			name:    "text is latin-1",
			targets: targetList{atomTEXT},
			want:    atomTEXT,
			latin1:  true,
		},
		{
			name:    "nothing",
			targets: targetList{atomTextHTML},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name, latin1 := tc.targets.pickPlain()
			if name != tc.want || latin1 != tc.latin1 {
				t.Fatalf("pickPlain() = (%q, %v), want (%q, %v)", name, latin1, tc.want, tc.latin1)
			}
		})
	}
}

func TestTargetsPickHTML(t *testing.T) {
	cases := []struct {
		name    string
		targets targetList
		want    string
	}{
		{name: "exact", targets: targetList{atomTextHTML}, want: atomTextHTML},
		{name: "charset", targets: targetList{"text/html;charset=utf-8"}, want: "text/html;charset=utf-8"},
		{name: "plain only", targets: targetList{atomTextPlain}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.targets.pickHTML(); got != tc.want {
				t.Fatalf("pickHTML() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTargetsHas(t *testing.T) {
	targets := targetList{"a", "b"}
	if !targets.has("b") {
		t.Fatal("has(b) = false, want true")
	}
	if targets.has("c") {
		t.Fatal("has(c) = true, want false")
	}
}
