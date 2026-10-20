package common

import "testing"

func TestSimplifyPathElem(t *testing.T) {
	cases := map[string]string{
		"000ABC.txt": "abctxt",
		"folder-01":  "folder01",
		"..::file":   "file",
		"":           "",
		"  00000  ":  "",
	}

	for input, want := range cases {
		if got := SimplifyPathElem(input); got != want {
			t.Fatalf("SimplifyPathElem(%q) = %q, want %q", input, got, want)
		}
	}
}
