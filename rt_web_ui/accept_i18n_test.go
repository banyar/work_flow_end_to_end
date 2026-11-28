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
		missing(c.ID+" gap", c.Gap)
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

// Every "how it runs" step has English text too.
func TestAcceptFlowEnglishText(t *testing.T) {
	for id, groups := range caseFlows {
		for _, g := range groups {
			if hasMyanmar(g.System) && g.SystemEn == "" {
				t.Errorf("%s: system %q has no English text", id, g.System)
			}
			for _, st := range g.Steps {
				if st.TextEn == "" || hasMyanmar(st.TextEn) {
					t.Errorf("%s: step %q has no English text", id, st.Text)
				}
				for _, it := range st.Items {
					if it.TextEn == "" || hasMyanmar(it.TextEn) {
						t.Errorf("%s: item %q has no English text", id, it.Text)
					}
				}
			}
		}
	}
}

// Every runnable case explains how it runs, and every flow belongs to a case.
func TestAcceptFlowForEveryCase(t *testing.T) {
	cases, err := loadAcceptCases()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, c := range cases {
		ids[c.ID] = true
		if c.Skip == "" && len(caseFlows[c.ID]) == 0 {
			t.Errorf("%s is runnable but has no flow in caseFlows", c.ID)
		}
	}
	for id := range caseFlows {
		if !ids[id] {
			t.Errorf("caseFlows has %s, which is not a case", id)
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
