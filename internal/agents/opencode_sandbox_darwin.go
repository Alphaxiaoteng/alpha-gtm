//go:build darwin

package agents

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const openCodeSandboxExec = "/usr/bin/sandbox-exec"

func openCodeSandboxStatus() error {
	info, err := os.Lstat(openCodeSandboxExec)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return errors.New("sandbox-exec is not a trusted executable")
	}
	resolved, err := filepath.EvalSymlinks(openCodeSandboxExec)
	if err != nil || resolved != openCodeSandboxExec {
		return errors.New("sandbox-exec realpath mismatch")
	}
	return nil
}

func openCodeSandboxCommand(bin string, args []string, session *openCodeSession) (string, []string, error) {
	if err := openCodeSandboxStatus(); err != nil {
		return "", nil, err
	}
	if session == nil {
		return "", nil, errors.New("missing broker session")
	}
	profile, err := openCodeSandboxProfile(bin, session)
	if err != nil {
		return "", nil, err
	}
	wrapped := make([]string, 0, len(args)+3)
	wrapped = append(wrapped, "-p", profile, bin)
	wrapped = append(wrapped, args...)
	return openCodeSandboxExec, wrapped, nil
}

func openCodeSandboxProfile(bin string, session *openCodeSession) (string, error) {
	paths := append([]string{session.root, bin}, session.forbidden...)
	for _, path := range paths {
		if err := validateSandboxProfilePath(path); err != nil {
			return "", err
		}
	}

	var profile strings.Builder
	profile.WriteString("(version 1)\n")
	profile.WriteString("(deny default)\n")
	profile.WriteString("(import \"system.sb\")\n")
	profile.WriteString("(allow process*)\n")
	profile.WriteString("(allow file-read* file-test-existence file-map-executable (subpath \"/bin\"))\n")
	profile.WriteString("(allow file-read* file-test-existence file-map-executable (subpath \"/usr/bin\"))\n")
	profile.WriteString("(allow file-read* file-test-existence file-map-executable (subpath \"/usr/sbin\"))\n")
	profile.WriteString("(allow file-read* file-test-existence file-map-executable (subpath \"/sbin\"))\n")
	profile.WriteString("(allow file-read* file-test-existence (subpath \"/private/var/select\"))\n")
	profile.WriteString("(allow file-read* file-test-existence file-map-executable (literal ")
	profile.WriteString(sandboxProfileQuote(bin))
	profile.WriteString("))\n")
	profile.WriteString("(allow file-read* file-test-existence (subpath ")
	profile.WriteString(sandboxProfileQuote(session.root))
	profile.WriteString("))\n")
	profile.WriteString("(allow file-write* (subpath ")
	profile.WriteString(sandboxProfileQuote(session.root))
	profile.WriteString("))\n")
	for _, forbidden := range session.forbidden {
		profile.WriteString("(deny file-read* file-write* file-map-executable (subpath ")
		profile.WriteString(sandboxProfileQuote(forbidden))
		profile.WriteString("))\n")
	}
	return profile.String(), nil
}

func validateSandboxProfilePath(path string) error {
	if path == "" || !filepath.IsAbs(path) {
		return errors.New("sandbox path must be absolute")
	}
	for _, r := range path {
		if r == 0 || r == '\n' || r == '\r' {
			return fmt.Errorf("sandbox path contains control character")
		}
	}
	return nil
}

func sandboxProfileQuote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}
