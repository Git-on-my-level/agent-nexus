package app

import "testing"

func TestWatchCLIGrammar(t *testing.T) {
	_, body, err := parseLoopWork("work goal", []string{"card:loop", "--metric", "drain", "--direction", "down", "--label", "Battery drain", "--unit", "%/h"})
	if err != nil {
		t.Fatal(err)
	}
	goal := body["goal"].(map[string]any)
	if goal["label"] != "Battery drain" || goal["unit"] != "%/h" {
		t.Fatal(goal)
	}
	parsed, err := parseWorkCommand([]string{"report", "render", "document:watch", "--panels", "loops,activity"})
	if err != nil || parsed.query.Get("panels") != "loops,activity" {
		t.Fatal(parsed, err)
	}
	for _, args := range [][]string{{"list"}, {"set", "document:one", "document:two"}, {"set", "none"}} {
		if err := validateDashboardPinsArgs(args); err != nil {
			t.Fatal(args, err)
		}
	}
	for _, args := range [][]string{{"set"}, {"list", "extra"}, {"set", "document:a", "document:a"}, {"set", "--as", "someone"}} {
		if err := validateDashboardPinsArgs(args); err == nil {
			t.Fatal(args)
		}
	}
	if commandSideEffectClass("workspace dashboard pins set") != "remote_coordination_write" {
		t.Fatal("pins write misclassified")
	}
}
