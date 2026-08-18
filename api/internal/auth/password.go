package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemory      = 19 * 1024
	argonTime        = 2
	argonThreads     = 1
	argonKeyLen      = 32
	passwordLetters  = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	passwordDigits   = "0123456789"
	passwordSpecials = "!@#$%^&*_-+="
)

func HashPassword(password string) (string, error) {
	if !ValidPassword(password) {
		return "", errors.New("password must contain 8 to 16 ASCII characters including a letter, digit, and supported special character")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime,
		argonThreads, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

// ValidPassword applies the policy for newly created or changed passwords.
// VerifyPassword intentionally does not apply it so accounts using an older
// password policy can still sign in and migrate normally.
func ValidPassword(password string) bool {
	if len(password) < 8 || len(password) > 16 {
		return false
	}
	hasLetter, hasDigit, hasSpecial := false, false, false
	for _, character := range password {
		switch {
		case strings.ContainsRune(passwordLetters, character):
			hasLetter = true
		case strings.ContainsRune(passwordDigits, character):
			hasDigit = true
		case strings.ContainsRune(passwordSpecials, character):
			hasSpecial = true
		default:
			return false
		}
	}
	return hasLetter && hasDigit && hasSpecial
}

func RandomPassword(length int) (string, error) {
	if length < 8 || length > 16 {
		return "", errors.New("password length must be between 8 and 16")
	}
	value := make([]byte, length)
	groups := []string{passwordLetters, passwordDigits, passwordSpecials}
	for index, group := range groups {
		character, err := randomCharacter(group)
		if err != nil {
			return "", err
		}
		value[index] = character
	}
	all := passwordLetters + passwordDigits + passwordSpecials
	for index := len(groups); index < len(value); index++ {
		character, err := randomCharacter(all)
		if err != nil {
			return "", err
		}
		value[index] = character
	}
	for index := len(value) - 1; index > 0; index-- {
		selected, err := rand.Int(rand.Reader, big.NewInt(int64(index+1)))
		if err != nil {
			return "", err
		}
		other := int(selected.Int64())
		value[index], value[other] = value[other], value[index]
	}
	return string(value), nil
}

func randomCharacter(characters string) (byte, error) {
	index, err := rand.Int(rand.Reader, big.NewInt(int64(len(characters))))
	if err != nil {
		return 0, err
	}
	return characters[index.Int64()], nil
}

func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	var memory uint64
	var iterations uint64
	var threads uint64
	for _, value := range strings.Split(parts[3], ",") {
		pair := strings.SplitN(value, "=", 2)
		if len(pair) != 2 {
			return false
		}
		parsed, err := strconv.ParseUint(pair[1], 10, 32)
		if err != nil {
			return false
		}
		switch pair[0] {
		case "m":
			memory = parsed
		case "t":
			iterations = parsed
		case "p":
			threads = parsed
		}
	}
	if memory == 0 || iterations == 0 || threads == 0 || memory > 256*1024 || iterations > 10 || threads > 8 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) == 0 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, uint32(iterations), uint32(memory), uint8(threads), uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func RandomSecret(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
