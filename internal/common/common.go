package common

import (
	"strings"

	"github.com/524D/filelist2db/dataprovider"
)

// TimeBins defines the common age buckets used by directory summaries.
var TimeBins = []dataprovider.TimeBin{
	{MaxAgeS: 3600 * 24 * 31 * 4, Txt: "< 4 months"},
	{MaxAgeS: 3600 * 24 * 365, Txt: "4 to 12 months"},
	{MaxAgeS: 3600 * 24 * 365 * 3, Txt: "1 to 3 years"},
	{MaxAgeS: 3600 * 24 * 365 * 6, Txt: "3 to 6 years"},
	{MaxAgeS: 3600 * 24 * 365 * 12, Txt: "6 to 12 years"},
	{MaxAgeS: 3600 * 24 * 365 * 999, Txt: "> 12 years"},
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
