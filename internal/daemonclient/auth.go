package daemonclient

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// authTokenFileName is the local Bearer token shared by the user-level daemon
// and every proxy that talks to it.
const authTokenFileName = "auth.token"

// authTokenBytes is the raw entropy of a generated token (hex-encoded on disk).
const authTokenBytes = 32

const (
	// authTokenRaceTimeout bounds the wait for a competing creator to finish.
	authTokenRaceTimeout = 2 * time.Second
	// authTokenRacePoll is the re-read interval while waiting for that creator.
	authTokenRacePoll = 2 * time.Millisecond
)

// ErrCorruptAuthToken reports an existing token file that is not a valid token.
// It is never silently regenerated: a running daemon still holds the old value.
var ErrCorruptAuthToken = errors.New("auth.token is corrupt")

// ErrAuthTokenIncomplete reports a token file another process has created but
// not yet filled in. It is transient and must be retried.
var ErrAuthTokenIncomplete = errors.New("auth.token is being created")

// AuthTokenPath returns <userdir>/auth.token.
func AuthTokenPath() (string, error) {
	dir, err := userDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, authTokenFileName), nil
}

// ReadAuthToken loads the existing token. A missing file is reported as
// os.ErrNotExist so callers can distinguish "no daemon yet" from "corrupt".
//
// An existing but empty file is reported as ErrAuthTokenIncomplete: it is the
// transient state another process is in between O_EXCL create and its first
// write, and callers must retry rather than treat it as a corrupt token.
func ReadAuthToken() (string, error) {
	path, err := AuthTokenPath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		return "", fmt.Errorf("%w: %s", ErrAuthTokenIncomplete, path)
	}
	if !validAuthToken(raw) {
		return "", fmt.Errorf("%w: %s", ErrCorruptAuthToken, path)
	}
	return raw, nil
}

// EnsureAuthToken returns the local token, creating it atomically on first use.
// Concurrent creators converge on exactly one token: the file is created with
// O_EXCL and the loser waits for the winner's value.
func EnsureAuthToken() (string, error) {
	path, err := AuthTokenPath()
	if err != nil {
		return "", err
	}
	deadline := time.Now().Add(authTokenRaceTimeout)
	for {
		token, readErr := ReadAuthToken()
		switch {
		case readErr == nil:
			return token, nil
		case errors.Is(readErr, ErrCorruptAuthToken):
			return "", readErr
		case !errors.Is(readErr, os.ErrNotExist) && !errors.Is(readErr, ErrAuthTokenIncomplete):
			return "", readErr
		}

		token, created, createErr := createAuthToken(path)
		if createErr != nil {
			return "", createErr
		}
		if created {
			return token, nil
		}
		if time.Now().After(deadline) {
			if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
				return "", readErr
			}
			return "", fmt.Errorf("auth token did not appear at %s within %s", path, authTokenRaceTimeout)
		}
		time.Sleep(authTokenRacePoll)
	}
}

// createAuthToken creates the token file exclusively. It reports created=false
// when another process already owns the file.
func createAuthToken(path string) (token string, created bool, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", false, fmt.Errorf("create user dir: %w", err)
	}

	buf := make([]byte, authTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", false, fmt.Errorf("generate auth token: %w", err)
	}
	token = hex.EncodeToString(buf)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("create auth token: %w", err)
	}
	if _, err := f.WriteString(token + "\n"); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", false, fmt.Errorf("write auth token: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", false, fmt.Errorf("sync auth token: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", false, fmt.Errorf("close auth token: %w", err)
	}
	// Best effort on platforms without POSIX modes; the file already has 0600
	// from creation and lives in a 0700 user dir.
	_ = os.Chmod(path, 0o600)
	return token, true, nil
}

// validAuthToken reports whether token has the expected shape: hex, at least 32
// characters. Hand-provisioned tokens are accepted as long as they are not
// obviously truncated.
func validAuthToken(token string) bool {
	if len(token) < 32 {
		return false
	}
	if _, err := hex.DecodeString(token); err != nil {
		return false
	}
	return true
}

// tokensEqual compares two tokens in constant time.
func tokensEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
