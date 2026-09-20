package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if hash == "correct horse battery staple" {
		t.Fatal("HashPassword() stored plaintext")
	}
	if !strings.HasPrefix(hash, "$2") {
		t.Fatalf("HashPassword() = %q, want bcrypt hash", hash)
	}

	valid, needsRehash := VerifyPassword(hash, "correct horse battery staple")
	if !valid || needsRehash {
		t.Fatalf("VerifyPassword() = (%t, %t), want (true, false)", valid, needsRehash)
	}
	if valid, _ := VerifyPassword(hash, "wrong password"); valid {
		t.Fatal("VerifyPassword() accepted wrong password")
	}
}

func TestVerifyWerkzeugScryptPassword(t *testing.T) {
	const werkzeugHash = "scrypt:32768:8:1$fixedsalt123456$37f7f2259be95268d2505bce8f233963bdb32597f392d9cd5ebb20637914bf5e700e8ae6c1a6fdf03a06df39a5231108e055ccdb90d13198103667a71269a419"

	valid, needsRehash := VerifyPassword(werkzeugHash, "correct horse battery staple")
	if !valid || !needsRehash {
		t.Fatalf("VerifyPassword() = (%t, %t), want (true, true)", valid, needsRehash)
	}
	if valid, _ := VerifyPassword(werkzeugHash, "wrong password"); valid {
		t.Fatal("VerifyPassword() accepted wrong Werkzeug password")
	}
}
