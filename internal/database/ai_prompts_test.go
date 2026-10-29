package database

import (
	"strings"
	"testing"
)

func TestAIPromptValidation(t *testing.T) {
	for _, input := range []string{"", "請簡短回答。\n必要時轉真人。", strings.Repeat("中", 4000)} {
		if !validAIPrompt(input) {
			t.Fatalf("valid prompt rejected: %d runes", len([]rune(input)))
		}
	}
	for _, input := range []string{strings.Repeat("中", 4001), "bad\x00prompt", string([]byte{0xff})} {
		if validAIPrompt(input) {
			t.Fatal("invalid prompt accepted")
		}
	}
}
