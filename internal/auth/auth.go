// Package auth provides local/intranet tool login (username + password + JWT).
// Demo-grade: scrypt-ish via SHA256-salt hash is intentional simplicity for local demo;
// production can swap HashPassword/Check without changing API.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const TokenTTL = 24 * time.Hour

// HashPassword returns salt$hash (hex).
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum := sha256.Sum256(append(salt, []byte(password)...))
	return hex.EncodeToString(salt) + "$" + hex.EncodeToString(sum[:]), nil
}

func CheckPassword(password, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 2 {
		return false
	}
	salt, err := hex.DecodeString(parts[0])
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	sum := sha256.Sum256(append(salt, []byte(password)...))
	return hmac.Equal(sum[:], want)
}

type claims struct {
	Sub  string `json:"sub"`
	Role string `json:"role"`
	Exp  int64  `json:"exp"`
	Iat  int64  `json:"iat"`
}

// IssueToken creates a compact HMAC-SHA256 JWT-like token: base64url(header).base64url(payload).base64url(sig)
func IssueToken(secret, username, role string, ttl time.Duration) (string, time.Time, error) {
	if secret == "" {
		return "", time.Time{}, fmt.Errorf("empty jwt secret")
	}
	if ttl <= 0 {
		ttl = TokenTTL
	}
	now := time.Now().UTC()
	exp := now.Add(ttl)
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(claims{Sub: username, Role: role, Exp: exp.Unix(), Iat: now.Unix()})
	body := header + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return body + "." + sig, exp, nil
}

// ParseToken verifies signature and expiry; returns username and role.
func ParseToken(secret, token string) (username, role string, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", "", fmt.Errorf("invalid token")
	}
	body := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(parts[2])) {
		return "", "", fmt.Errorf("bad signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", err
	}
	var c claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return "", "", err
	}
	if time.Now().UTC().Unix() > c.Exp {
		return "", "", fmt.Errorf("token expired")
	}
	return c.Sub, c.Role, nil
}

// LoadOrCreateSecret reads/writes jwt secret under dataDir/auth_secret.
func LoadOrCreateSecret(dataDir string) (string, error) {
	path := filepath.Join(dataDir, "auth_secret")
	b, err := os.ReadFile(path)
	if err == nil {
		s := strings.TrimSpace(string(b))
		if s != "" {
			return s, nil
		}
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	s := hex.EncodeToString(buf)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(s+"\n"), 0o600); err != nil {
		return "", err
	}
	return s, nil
}
