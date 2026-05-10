package lifecycle

import "testing"

func TestDefaultRulesShape(t *testing.T) {
	rules := DefaultRules("mio/attachments/", 7)
	if len(rules) != 1 {
		t.Fatalf("want 1 rule, got %d", len(rules))
	}
	if rules[0].Prefix != "mio/attachments/" || rules[0].AgeDays != 7 {
		t.Fatalf("got %+v", rules[0])
	}
}

func TestDefaultRulesHonorsAge(t *testing.T) {
	rules := DefaultRules("p/", 14)
	if rules[0].AgeDays != 14 {
		t.Fatalf("expected AgeDays=14, got %d", rules[0].AgeDays)
	}
}
