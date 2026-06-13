package slack

import "testing"

func TestCompositeRoundTrip(t *testing.T) {
	cases := []struct {
		name    string
		channel string
		ts      string
	}{
		{"public channel", "C0123ABCD", "1700000000.000100"},
		{"private group", "G0456EFGH", "1699999999.999999"},
		{"dm", "D0789IJKL", "1700000001.000200"},
		{"mpim", "G09MPIM00", "1700000002.000300"},
		{"ts without micros", "C0123ABCD", "1700000000"},
		{"high-entropy ts", "C0123ABCD", "1700000003.123456"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := composite(tc.channel, tc.ts)
			if id == "" {
				t.Fatalf("composite(%q,%q) returned empty", tc.channel, tc.ts)
			}
			ch, ts, ok := splitComposite(id)
			if !ok {
				t.Fatalf("splitComposite(%q) ok=false", id)
			}
			if ch != tc.channel || ts != tc.ts {
				t.Fatalf("round-trip mismatch: got (%q,%q) want (%q,%q)", ch, ts, tc.channel, tc.ts)
			}
		})
	}
}

func TestCompositeEmptyParts(t *testing.T) {
	if got := composite("", "1700000000.0001"); got != "" {
		t.Errorf("composite with empty channel = %q, want empty", got)
	}
	if got := composite("C0123", ""); got != "" {
		t.Errorf("composite with empty ts = %q, want empty", got)
	}
}

func TestSplitCompositeInvalid(t *testing.T) {
	cases := []string{"", "no-separator", ":1700000000", "C0123:", ":"}
	for _, in := range cases {
		if _, _, ok := splitComposite(in); ok {
			t.Errorf("splitComposite(%q) ok=true, want false", in)
		}
	}
}

func TestSplitCompositeFirstSeparatorOnly(t *testing.T) {
	// Defensive: even if a future ts carried a ':', only the first split counts
	// and the channel half stays intact.
	ch, ts, ok := splitComposite("C0123:1700:extra")
	if !ok || ch != "C0123" || ts != "1700:extra" {
		t.Fatalf("got (%q,%q,%v)", ch, ts, ok)
	}
}
