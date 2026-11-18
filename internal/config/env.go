package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// LoadEnv loads local settings without overriding the process environment.
func LoadEnv() error {
	loadErr := godotenv.Load(".env")
	if loadErr != nil && !errors.Is(loadErr, fs.ErrNotExist) {
		return errors.New("could not load .env in the current directory")
	}

	required := []string{
		"DATABASE_URL",
		"JWT_ISSUER",
		"JWT_AUDIENCE",
		"JWT_ACCESS_SECRET",
		"JWT_REFRESH_SECRET",
		"COOKIE_SECURE",
	}
	var missing []string
	for _, key := range required {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if loadErr != nil {
		return fmt.Errorf(".env not found in the current directory and required environment variables are missing: %s", strings.Join(missing, ", "))
	}
	return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
}
