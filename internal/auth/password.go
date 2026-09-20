package auth

import (
	"crypto/subtle"
	"encoding/hex"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/scrypt"
)

const bcryptCost = bcrypt.DefaultCost

// HashPassword creates a bcrypt password hash.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword checks bcrypt and current Werkzeug scrypt hashes.
// The second return value reports whether a successful hash should be replaced.
func VerifyPassword(storedHash string, password string) (bool, bool) {
	if strings.HasPrefix(storedHash, "$2") {
		if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password)); err != nil {
			return false, false
		}
		cost, err := bcrypt.Cost([]byte(storedHash))
		return true, err == nil && cost < bcryptCost
	}

	if strings.HasPrefix(storedHash, "scrypt:") {
		valid := verifyWerkzeugScrypt(storedHash, password)
		return valid, valid
	}

	return false, false
}

func verifyWerkzeugScrypt(storedHash string, password string) bool {
	parts := strings.Split(storedHash, "$")
	if len(parts) != 3 {
		return false
	}

	method := strings.Split(parts[0], ":")
	if len(method) != 4 || method[0] != "scrypt" {
		return false
	}

	n, err := strconv.Atoi(method[1])
	if err != nil || n < 2 || n > 1<<20 || n&(n-1) != 0 {
		return false
	}
	r, err := strconv.Atoi(method[2])
	if err != nil || r < 1 || r > 32 {
		return false
	}
	p, err := strconv.Atoi(method[3])
	if err != nil || p < 1 || p > 16 {
		return false
	}

	want, err := hex.DecodeString(parts[2])
	if err != nil || len(want) == 0 || len(want) > 128 {
		return false
	}

	got, err := scrypt.Key([]byte(password), []byte(parts[1]), n, r, p, len(want))
	if err != nil {
		return false
	}

	return subtle.ConstantTimeCompare(got, want) == 1
}
