package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAcceptCasesCoverTheSpec(t *testing.T) {
	cases, err := loadAcceptCases()
	if err != nil {
		t.Fatal(err)
	}
	if spec := len(cases) - countSubCases(cases); spec != 81 {
		t.Fatalf("%d spec cases, the spec has 81", spec)
	}
	var subs []string
	for _, c := range cases {
		if c.Parent == "ADM-03" {
			subs = append(subs, c.ID)
		}
	}
	if strings.Join(subs, ",") != "ADM-03a,ADM-03b,ADM-03c,ADM-03d,ADM-03e" {
		t.Errorf("ADM-03 sub-cases %v", subs)
	}
	groups := map[string]bool{}
	for _, g := range acceptGroups {
		groups[g.Key] = true
	}
	runnable := 0
	for _, c := range cases {
		if !groups[c.Group] {
			t.Errorf("%s: group %q is not in acceptGroups", c.ID, c.Group)
		}
		if c.Given == "" || c.Expected == "" || c.Title == "" {
			t.Errorf("%s: missing given / expected / title", c.ID)
		}
		if c.Profile != "P" && c.Profile != "U" && c.Profile != "V" {
			t.Errorf("%s: profile %q", c.ID, c.Profile)
		}
		if c.run != nil {
			runnable++
		}
	}
	t.Logf("%d of %d cases runnable from rt_web_ui", runnable, len(cases))
}

func TestAcceptGroupJSON(t *testing.T) {
	b, _ := json.Marshal(acceptGroups[0])
	if string(b) != `{"key":"ADM","title":"1. Admission"}` {
		t.Fatalf("group JSON %s — the page reads key / title", b)
	}
}

func TestRTAuthHeader(t *testing.T) {
	for in, want := range map[string]string{
		"token 1-66-abc": "token 1-66-abc", // noc_automation's RT_TOKEN form
		"1-66-abc":       "token 1-66-abc",
		" Token 1-66-x ": "Token 1-66-x",
	} {
		if got := rtAuthHeader(in); got != want {
			t.Errorf("rtAuthHeader(%q) = %q, want %q", in, got, want)
		}
	}
}
