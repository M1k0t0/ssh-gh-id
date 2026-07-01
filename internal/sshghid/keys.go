package sshghid

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/crypto/ssh"
)

func validateAndNormalizePublicKeyLines(source string, lines []string) ([]string, error) {
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		normalized, err := validateAndNormalizePublicKeyLine(line)
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %w", source, i+1, err)
		}
		out = append(out, normalized)
	}
	return out, nil
}

func validateAndNormalizePublicKeyLine(line string) (string, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", errors.New("empty public key line")
	}
	if containsControlChar(line) {
		return "", errors.New("public key line contains a control character")
	}
	fields := strings.Fields(line)
	if len(fields) != 2 {
		return "", errors.New("expected a bare public key without comments or authorized_keys options")
	}
	if !isAllowedPublicKeyType(fields[0]) {
		return "", fmt.Errorf("unsupported public key type %q", fields[0])
	}
	key, comment, options, rest, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return "", fmt.Errorf("parse public key: %w", err)
	}
	if len(options) != 0 {
		return "", errors.New("authorized_keys options are not allowed")
	}
	if comment != "" {
		return "", errors.New("public key comments are not allowed")
	}
	if strings.TrimSpace(string(rest)) != "" {
		return "", errors.New("unexpected trailing data after public key")
	}
	if !isAllowedPublicKeyType(key.Type()) {
		return "", fmt.Errorf("unsupported parsed public key type %q", key.Type())
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))), nil
}

func containsControlChar(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func isAllowedPublicKeyType(keyType string) bool {
	switch keyType {
	case "ssh-ed25519", "ssh-rsa",
		"ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521",
		"sk-ecdsa-sha2-nistp256@openssh.com", "sk-ssh-ed25519@openssh.com":
		return true
	default:
		return false
	}
}
