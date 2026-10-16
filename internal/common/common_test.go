package common

import "testing"

func TestSimplifyPathElem(t *testing.T) {
	cases := map[string]string{
		"000ABC.txt": "abctxt",
		"folder-01": "folder01",
		"..::file": "file",
		"": "",
		"  00000  ": "",
	}

	for input, want := range cases {
		if got := SimplifyPathElem(input); got != want {
			t.Fatalf("SimplifyPathElem(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestTimeBins(t *testing.T) {
	if got, want := len(TimeBins), 6; got != want {
		t.Fatalf("len(TimeBins) = %d, want %d", got, want)
	}
	if got, want := TimeBins[0].Txt, "< 4 months"; got != want {
		t.Fatalf("TimeBins[0].Txt = %q, want %q", got, want)
	}
}
