package http

import "testing"

func TestUnescapeKey(t *testing.T) {
	cases := map[string]string{
		"photos/cat.png":                 "photos/cat.png",
		"photos%2Fcat.png":               "photos/cat.png",
		"swiftleadsai_logo%20(1).png":    "swiftleadsai_logo (1).png",
		"swiftleadsai_logo%2520(1).png":  "swiftleadsai_logo (1).png",
		"/holiday.jpg":                  "holiday.jpg",
	}
	for in, want := range cases {
		if got := unescapeKey(in); got != want {
			t.Fatalf("unescapeKey(%q)=%q want %q", in, got, want)
		}
	}
}

func TestKeyGuessesIncludesEncodedSlash(t *testing.T) {
	got := keyGuesses("photos%2Funi%20of.png")
	want := map[string]bool{
		"photos%2Funi%20of.png": false,
		"photos/uni of.png":     false,
	}
	for _, k := range got {
		if _, ok := want[k]; ok {
			want[k] = true
		}
	}
	for k, found := range want {
		if !found {
			t.Fatalf("missing guess %q in %v", k, got)
		}
	}
}

func TestKeyGuessesKeepsPercentTwentyForm(t *testing.T) {
	got := keyGuesses("swiftleadsai_logo%2520(1).png")
	want := map[string]bool{
		"swiftleadsai_logo%2520(1).png": false,
		"swiftleadsai_logo%20(1).png":   false,
		"swiftleadsai_logo (1).png":     false,
	}
	for _, k := range got {
		if _, ok := want[k]; ok {
			want[k] = true
		}
	}
	for k, found := range want {
		if !found {
			t.Fatalf("missing guess %q in %v", k, got)
		}
	}
}
