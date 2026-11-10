package tools

import "testing"

func TestNormalizeSheetRange(t *testing.T) {
	cases := map[string]string{
		"a1:c10":    "A1:C10",
		" B2 ":      "B2",
		"$A$1:$B$2": "A1:B2",
		"":          defaultUnitRange,
		"A1:B2:C3":  defaultUnitRange,
		"A1:":       defaultUnitRange,
		"sheet1":    defaultUnitRange,
	}
	for input, want := range cases {
		if got := normalizeSheetRange(input); got != want {
			t.Fatalf("normalizeSheetRange(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeSheetRangeStrictRejectsMalformed(t *testing.T) {
	for _, input := range []string{"", "A1:", ":B2", "A1:B2:C3", "A 1", "A1-B2"} {
		if _, err := normalizeSheetRangeStrict(input); err == nil {
			t.Fatalf("normalizeSheetRangeStrict(%q) accepted", input)
		}
	}
}
