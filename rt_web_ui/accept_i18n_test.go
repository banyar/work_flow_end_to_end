package main

import "testing"

// Every case title, skip reason and group title shown on the acceptance page
// has an English version for the language toggle.
func TestAcceptEnglishText(t *testing.T) {
	cases, err := loadAcceptCases()
	if err != nil {
		t.Fatal(err)
	}
	missing := func(what, s string) {
		if s != "" && en(s) == s && hasMyanmar(s) {
			t.Errorf("%s has no English text: %q", what, s)
		}
	}
	for _, c := range cases {
		missing(c.ID+" title", c.Title)
		missing(c.ID+" skip", c.Skip)
	}
	for _, g := range acceptGroups {
		missing("group "+g.Key, g.Title)
	}
	for _, c := range cases {
		cr := caseRequirements(c.ID, "3002")
		missing(c.ID+" note", cr.note)
		for _, d := range cr.reqs {
			missing(c.ID+" requirement "+d.name, d.why)
		}
	}
}

func hasMyanmar(s string) bool {
	for _, r := range s {
		if r >= 0x1000 && r <= 0x109F {
			return true
		}
	}
	return false
}
