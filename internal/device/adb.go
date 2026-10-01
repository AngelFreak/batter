package device

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
)

// validSerial matches the characters ADB itself permits in a device serial:
// USB serials, "host:port" network serials, and emulator names. Anything else
// (notably a leading "-", which adb would parse as an option) is rejected.
var validSerial = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// checkSerial guards against argument injection: the serial is passed to
// `adb -s <serial>`, so a value like "--help" or "-e" would be interpreted as
// an adb option rather than a device. We reject leading dashes and any
// out-of-charset input before the value reaches exec.Command.
func checkSerial(serial string) error {
	if strings.HasPrefix(serial, "-") || !validSerial.MatchString(serial) {
		return fmt.Errorf("invalid device serial: %q", serial)
	}
	return nil
}

// ADBDevice represents a connected Android device.
type ADBDevice struct {
	Serial  string `json:"serial"`
	State   string `json:"state"`
	Model   string `json:"model"`
	Product string `json:"product"`
}

// ADB wraps adb command-line operations.
type ADB struct {
	adbPath string
	logger  *slog.Logger
}

// NewADB creates a new ADB wrapper, locating the adb binary in PATH.
func NewADB(logger *slog.Logger) (*ADB, error) {
	path, err := exec.LookPath("adb")
	if err != nil {
		return nil, fmt.Errorf("adb not found in PATH: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ADB{
		adbPath: path,
		logger:  logger.With("component", "adb"),
	}, nil
}

// ListDevices returns all connected ADB devices.
func (a *ADB) ListDevices(ctx context.Context) ([]ADBDevice, error) {
	out, err := a.run(ctx, "devices", "-l")
	if err != nil {
		return nil, fmt.Errorf("adb devices: %w", err)
	}

	var devices []ADBDevice
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "List of") || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		dev := ADBDevice{
			Serial: fields[0],
			State:  fields[1],
		}

		// Parse key:value properties like model:Pixel_6 product:oriole
		for _, f := range fields[2:] {
			parts := strings.SplitN(f, ":", 2)
			if len(parts) != 2 {
				continue
			}
			switch parts[0] {
			case "model":
				dev.Model = parts[1]
			case "product":
				dev.Product = parts[1]
			}
		}

		devices = append(devices, dev)
	}

	return devices, nil
}

// Push copies a local file to the device.
func (a *ADB) Push(ctx context.Context, serial, localPath, remotePath string) error {
	_, err := a.runWithSerial(ctx, serial, "push", localPath, remotePath)
	return err
}

// Forward sets up a TCP port forward to a device-side abstract socket.
func (a *ADB) Forward(ctx context.Context, serial string, localPort int, abstractName string) error {
	_, err := a.runWithSerial(ctx, serial, "forward",
		fmt.Sprintf("tcp:%d", localPort),
		fmt.Sprintf("localabstract:%s", abstractName),
	)
	return err
}

// RemoveForward removes a previously set up port forward.
func (a *ADB) RemoveForward(ctx context.Context, serial string, localPort int) error {
	_, err := a.runWithSerial(ctx, serial, "forward", "--remove", fmt.Sprintf("tcp:%d", localPort))
	return err
}

// Reverse sets up a reverse port forward: device-side abstract socket maps to host TCP port.
func (a *ADB) Reverse(ctx context.Context, serial string, abstractName string, localPort int) error {
	_, err := a.runWithSerial(ctx, serial, "reverse",
		fmt.Sprintf("localabstract:%s", abstractName),
		fmt.Sprintf("tcp:%d", localPort),
	)
	return err
}

// RemoveReverse removes a previously set up reverse forward.
func (a *ADB) RemoveReverse(ctx context.Context, serial string, abstractName string) error {
	_, err := a.runWithSerial(ctx, serial, "reverse", "--remove", fmt.Sprintf("localabstract:%s", abstractName))
	return err
}

// Shell executes a shell command on the device.
func (a *ADB) Shell(ctx context.Context, serial string, args ...string) ([]byte, error) {
	cmdArgs := append([]string{"shell"}, args...)
	return a.runWithSerial(ctx, serial, cmdArgs...)
}

// Screenshot captures the device screen as a PNG.
func (a *ADB) Screenshot(ctx context.Context, serial string) ([]byte, error) {
	return a.runWithSerial(ctx, serial, "exec-out", "screencap", "-p")
}

// Install installs an APK on the device. Returns the output for success checking.
func (a *ADB) Install(ctx context.Context, serial, apkPath string) ([]byte, error) {
	return a.runWithSerial(ctx, serial, "install", "-r", apkPath)
}

func (a *ADB) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, a.adbPath, args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("adb %s: %s", strings.Join(args, " "), string(exitErr.Stderr))
		}
		return nil, err
	}
	return out, nil
}

// GetState returns the ADB state for a device (e.g. "device", "offline", "unauthorized").
func (a *ADB) GetState(ctx context.Context, serial string) (string, error) {
	out, err := a.runWithSerial(ctx, serial, "get-state")
	if err != nil {
		return "", fmt.Errorf("adb get-state %s: %w", serial, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// GetProperties retrieves device properties via "adb shell getprop".
// Returns a map of property names to values, extracting common fields:
// ro.product.model, ro.product.name, ro.build.version.release
func (a *ADB) GetProperties(ctx context.Context, serial string) (map[string]string, error) {
	out, err := a.Shell(ctx, serial, "getprop")
	if err != nil {
		return nil, fmt.Errorf("adb getprop %s: %w", serial, err)
	}

	props := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		// Format: [key]: [value]
		if !strings.HasPrefix(line, "[") {
			continue
		}
		closeBracket := strings.Index(line, "]")
		if closeBracket < 0 {
			continue
		}
		key := line[1:closeBracket]

		rest := line[closeBracket+1:]
		valStart := strings.Index(rest, "[")
		valEnd := strings.LastIndex(rest, "]")
		if valStart < 0 || valEnd < 0 || valEnd <= valStart {
			continue
		}
		value := rest[valStart+1 : valEnd]
		props[key] = value
	}

	return props, nil
}

func (a *ADB) runWithSerial(ctx context.Context, serial string, args ...string) ([]byte, error) {
	if err := checkSerial(serial); err != nil {
		return nil, err
	}
	cmdArgs := append([]string{"-s", serial}, args...)
	return a.run(ctx, cmdArgs...)
}
