package setup

import (
	"slices"
	"testing"
)

func TestTabCompletesInsteadOfMovingOn(t *testing.T) {
	km := keyMap()
	if !slices.Contains(km.Input.AcceptSuggestion.Keys(), "tab") {
		t.Fatalf("tab must accept a suggestion, keys = %v", km.Input.AcceptSuggestion.Keys())
	}
	if slices.Contains(km.Input.Next.Keys(), "tab") {
		t.Fatalf("tab must not submit the input, keys = %v", km.Input.Next.Keys())
	}
}
