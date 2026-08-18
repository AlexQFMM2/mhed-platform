package auth

import "testing"

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("Hunter#2026")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "Hunter#2026") {
		t.Fatal("valid password rejected")
	}
	if VerifyPassword(hash, "Wrong#2026") {
		t.Fatal("invalid password accepted")
	}
}

func TestPasswordPolicy(t *testing.T) {
	valid := []string{"Abcdef#1", "hunter_2026!", "A1@bcdefghijklmn"}
	for _, value := range valid {
		if !ValidPassword(value) {
			t.Errorf("valid password rejected: %q", value)
		}
	}
	invalid := []string{"Ab#1234", "abcdefghijklmnopq1!", "abcdefgh!", "12345678!", "Abcdefg1", "Abcdef1?", "密码Abc#123"}
	for _, value := range invalid {
		if ValidPassword(value) {
			t.Errorf("invalid password accepted: %q", value)
		}
	}
}

func TestRandomPassword(t *testing.T) {
	for index := 0; index < 100; index++ {
		value, err := RandomPassword(16)
		if err != nil {
			t.Fatal(err)
		}
		if !ValidPassword(value) {
			t.Fatalf("generated password does not satisfy policy: %q", value)
		}
	}
}
