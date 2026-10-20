package common

import (
	"strings"
)

// FileExt returns the lowercased extension of a file name without the dot,
// or "" if there is none (a leading dot, as in ".bashrc", is not an extension).
func FileExt(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 {
		return ""
	}
	return strings.ToLower(name[i+1:])
}

// SimplifyPathElem removes leading zeros, strips non-alphanumeric characters,
// and normalizes casing for simplified path-element matching.
func SimplifyPathElem(elem string) string {
	elem = strings.TrimSpace(elem)
	elem = strings.TrimLeft(elem, "0")
	elem = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, elem)
	return strings.ToLower(elem)
}
