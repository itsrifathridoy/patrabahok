// Package secretkey manages small symmetric keys used to encrypt sensitive values this
// application stores in the database (e.g. a connected Cloudflare API token). The key
// itself lives in /etc/patrabahok/secrets.env, the same root-only (0600) file the
// installer already uses for database credentials, so it never needs its own secret
// management story or an installer/migration change to introduce a new one.
package secretkey

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

const SecretsFile = "/etc/patrabahok/secrets.env"

// Ensure returns the 32-byte value stored under name in the secrets file, generating
// and persisting a new random one on first use.
func Ensure(name string) ([]byte, error) {
	if v, ok, err := read(name); err != nil {
		return nil, err
	} else if ok {
		return v, nil
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(raw)

	if err := os.MkdirAll("/etc/patrabahok", 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(SecretsFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	_, writeErr := fmt.Fprintf(f, "%s=%s\n", name, encoded)
	closeErr := f.Close()
	if writeErr != nil {
		return nil, writeErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if err := os.Chmod(SecretsFile, 0o600); err != nil {
		return nil, err
	}

	// Re-read rather than trusting our own write, in case a concurrent caller won the
	// race and appended its own value first — the first line for this name that was
	// actually persisted wins for everyone reading afterward.
	v, ok, err := read(name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("wrote %s to %s but could not read it back", name, SecretsFile)
	}
	return v, nil
}

func read(name string) ([]byte, bool, error) {
	f, err := os.Open(SecretsFile)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	prefix := name + "="
	scanner := bufio.NewScanner(f)
	var last string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, prefix) {
			last = strings.TrimPrefix(line, prefix)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, false, err
	}
	if last == "" {
		return nil, false, nil
	}
	raw, err := base64.StdEncoding.DecodeString(last)
	if err != nil {
		return nil, false, fmt.Errorf("decode %s: %w", name, err)
	}
	return raw, true, nil
}
