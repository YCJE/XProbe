package security

import (
	"strings"
	"testing"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("CorrectHorseBattery9")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("unexpected hash format: %s", hash)
	}
	if !VerifyPassword("CorrectHorseBattery9", hash) {
		t.Fatal("correct password was rejected")
	}
	if VerifyPassword("WrongHorseBattery9", hash) {
		t.Fatal("wrong password was accepted")
	}
	if VerifyPassword("CorrectHorseBattery9", "$argon2id$broken") {
		t.Fatal("malformed hash was accepted")
	}
}

func TestPasswordHashesAreSalted(t *testing.T) {
	first, err := HashPassword("CorrectHorseBattery9")
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword("CorrectHorseBattery9")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("two hashes of the same password are equal")
	}
	if !VerifyPassword("CorrectHorseBattery9", first) || !VerifyPassword("CorrectHorseBattery9", second) {
		t.Fatal("a salted hash did not verify")
	}
}

func TestValidatePasswordPolicy(t *testing.T) {
	valid := []string{"CorrectHorse9", "Abcdefghijk1"}
	for _, p := range valid {
		if err := ValidatePassword(p); err != nil {
			t.Fatalf("%q should be valid: %v", p, err)
		}
	}
	invalid := []string{"short1A", "alllowercase1", "ALLUPPERCASE1", "NoDigitsHere!"}
	for _, p := range invalid {
		if err := ValidatePassword(p); err == nil {
			t.Fatalf("%q should be invalid", p)
		}
	}
}
