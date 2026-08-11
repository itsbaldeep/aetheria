package auth

import (
	"testing"
)

func TestValidateCharacterName(t *testing.T) {
	valid := []string{"Arin", "Blade_Master", "xXShadowXx", "Ab", "1234567890123456"}
	for _, n := range valid {
		if _, err := ValidateCharacterName(n); err != nil {
			t.Fatalf("ValidateCharacterName(%q) unexpected error: %v", n, err)
		}
	}
	invalid := []string{"", "A", "way too long name here", "has space", "punct!", "hyphen-name", "!@#$"}
	for _, n := range invalid {
		if _, err := ValidateCharacterName(n); err == nil {
			t.Fatalf("ValidateCharacterName(%q) expected error, got nil", n)
		}
	}
}

func TestValidCharacterClass(t *testing.T) {
	if !ValidCharacterClass(ClassBladeDancer) {
		t.Fatal("blade_dancer must be valid")
	}
	if !ValidCharacterClass(ClassSpellweaver) {
		t.Fatal("spellweaver must be valid")
	}
	for _, c := range []string{"", "knight", "mage", "HUMAN"} {
		if ValidCharacterClass(c) {
			t.Fatalf("class %q must be rejected", c)
		}
	}
}
