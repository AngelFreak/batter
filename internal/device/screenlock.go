package device

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strings"
)

// ScreenLock is a device's lock-screen state.
type ScreenLock struct {
	// Credential is Android's CredentialType for the main user: "NONE",
	// "PIN", "PASSWORD", "PATTERN", ...
	Credential string `json:"credential"`
	// Disabled means the lock screen is skipped entirely (no swipe either).
	Disabled bool `json:"disabled"`
}

var (
	// ErrCredentialRequired: the device has a lock; its current PIN,
	// password or pattern is needed to remove it.
	ErrCredentialRequired = errors.New("current PIN, password or pattern required")
	// ErrWrongCredential: the device rejected the credential. This counts as
	// a failed unlock attempt on the device.
	ErrWrongCredential = errors.New("the PIN, password or pattern didn't match")
)

// ScreenLock reads the device's lock-screen state.
func (m *Manager) ScreenLock(ctx context.Context, serial string) (ScreenLock, error) {
	out, err := m.adb.Shell(ctx, serial, "dumpsys", "lock_settings")
	if err != nil {
		return ScreenLock{}, fmt.Errorf("read lock settings: %w", err)
	}
	cred := parseCredentialType(string(out))
	if cred == "" {
		return ScreenLock{}, errors.New("device doesn't report its lock settings")
	}
	dis, err := m.adb.Shell(ctx, serial, "locksettings", "get-disabled")
	if err != nil {
		return ScreenLock{}, fmt.Errorf("read lock screen state: %w", err)
	}
	return ScreenLock{Credential: cred, Disabled: strings.TrimSpace(string(dis)) == "true"}, nil
}

// RemoveScreenLock clears the device's PIN/password/pattern (given the
// current one) and disables the lock screen, so remote users aren't stopped
// by it. It makes exactly one attempt with the credential.
//
// The credential must never appear in errors or logs: adb errors include the
// full command line, so they're replaced by sentinel errors here.
func (m *Manager) RemoveScreenLock(ctx context.Context, serial, credential string) (ScreenLock, error) {
	state, err := m.ScreenLock(ctx, serial)
	if err != nil {
		return ScreenLock{}, err
	}

	if state.Credential != "NONE" {
		if credential == "" {
			return state, ErrCredentialRequired
		}
		// adb joins shell arguments into one command line for the device's
		// shell, so the credential must be quoted to arrive literally.
		out, err := m.adb.ShellSecret(ctx, serial, "locksettings", "clear", "--old", shellQuote(credential))
		switch {
		case err == nil && strings.Contains(string(out), "cleared"):
		case strings.Contains(string(out), "match"):
			return state, ErrWrongCredential
		default:
			return state, errors.New("device refused to clear the screen lock")
		}
	}

	if _, err := m.adb.Shell(ctx, serial, "locksettings", "set-disabled", "true"); err != nil {
		return ScreenLock{}, fmt.Errorf("disable lock screen: %w", err)
	}

	after, err := m.ScreenLock(ctx, serial)
	if err != nil {
		return ScreenLock{}, err
	}
	if after.Credential != "NONE" || !after.Disabled {
		return after, errors.New("screen lock still active after removal")
	}
	m.logger.Info("screen lock removed", "serial", serial, "was", state.Credential)
	return after, nil
}

// parseCredentialType returns user 0's CredentialType from
// `dumpsys lock_settings`, or "" if absent.
func parseCredentialType(dump string) string {
	inUserState, user0 := false, false
	sc := bufio.NewScanner(strings.NewReader(dump))
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "User State:":
			inUserState = true
		case inUserState && trimmed != "" && !strings.HasPrefix(line, " "):
			return "" // left the User State section without finding it
		case inUserState && strings.HasPrefix(trimmed, "User "):
			user0 = trimmed == "User 0"
		case inUserState && user0 && strings.HasPrefix(trimmed, "CredentialType:"):
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "CredentialType:"))
		}
	}
	return ""
}

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
