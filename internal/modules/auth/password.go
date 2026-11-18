package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"strings"

	"golang.org/x/crypto/argon2"
)

const passwordPrefix = "$argon2id$v=19$m=65536,t=3,p=2$"

func hashPassword(password string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	encoding := base64.RawStdEncoding
	return passwordPrefix + encoding.EncodeToString(salt) + "$" + encoding.EncodeToString(passwordKey(password, salt))
}

func checkPassword(password, stored string) bool {
	body, ok := strings.CutPrefix(stored, passwordPrefix)
	saltEncoded, hashEncoded, found := strings.Cut(body, "$")
	salt, saltErr := base64.RawStdEncoding.DecodeString(saltEncoded)
	hash, hashErr := base64.RawStdEncoding.DecodeString(hashEncoded)
	return ok && found && saltErr == nil && hashErr == nil && len(salt) == 16 && len(hash) == 32 &&
		subtle.ConstantTimeCompare(passwordKey(password, salt), hash) == 1
}

func passwordKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
}
