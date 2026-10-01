package device

import (
	"encoding/binary"
)

// Scrcpy control message types.
const (
	ControlTypeKeycode            = 0
	ControlTypeText               = 1
	ControlTypeTouch              = 2
	ControlTypeScroll             = 3
	ControlTypeBackOrScreenOn     = 4
	ControlTypeExpandNotification = 5
	ControlTypeExpandSettings     = 6
	ControlTypeCollapsePanels     = 7
	ControlTypeGetClipboard       = 8
	ControlTypeSetClipboard       = 9
	ControlTypeSetScreenPowerMode = 10
	ControlTypeRotateDevice       = 11
)

// Clipboard copy-key constants for EncodeGetClipboard.
const (
	CopyKeyNone = 0
	CopyKeyCopy = 1
)

// Screen power modes (Android SurfaceControl.POWER_MODE_*).
const (
	ScreenPowerOff    = 0
	ScreenPowerNormal = 2
)

// Android key/touch actions.
const (
	ActionDown = 0
	ActionUp   = 1
	ActionMove = 2
)

// Android keycodes used for screen control.
const (
	KeycodePower  = 26
	KeycodeWakeUp = 224
	KeycodeSleep  = 223
)

// EncodeTouchEvent encodes a scrcpy touch control message.
// x, y are normalized 0.0-1.0 coordinates from the browser.
// width, height are the device screen dimensions used by scrcpy.
// The protocol expects absolute pixel coordinates as uint32.
// Format: type(1) + action(1) + pointerId(8) + x(4) + y(4) + w(2) + h(2) + pressure(2) + actionButton(4) + buttons(4) = 32 bytes
func EncodeTouchEvent(action uint8, pointerID uint64, x, y float32, width, height uint16, pressure uint16) []byte {
	buf := make([]byte, 32)
	buf[0] = ControlTypeTouch
	buf[1] = action
	binary.BigEndian.PutUint64(buf[2:10], pointerID)
	// Convert normalized coords to absolute pixel coordinates
	pixelX := uint32(x * float32(width))
	pixelY := uint32(y * float32(height))
	binary.BigEndian.PutUint32(buf[10:14], pixelX)
	binary.BigEndian.PutUint32(buf[14:18], pixelY)
	binary.BigEndian.PutUint16(buf[18:20], width)
	binary.BigEndian.PutUint16(buf[20:22], height)
	binary.BigEndian.PutUint16(buf[22:24], pressure)
	binary.BigEndian.PutUint32(buf[24:28], 0) // actionButton
	binary.BigEndian.PutUint32(buf[28:32], 0) // buttons
	return buf
}

// EncodeKeyEvent encodes a scrcpy key event control message.
// Format: type(1) + action(1) + keycode(4) + repeat(4) + metastate(4) = 14 bytes
func EncodeKeyEvent(action uint8, keycode, repeat, metastate uint32) []byte {
	buf := make([]byte, 14)
	buf[0] = ControlTypeKeycode
	buf[1] = action
	binary.BigEndian.PutUint32(buf[2:6], keycode)
	binary.BigEndian.PutUint32(buf[6:10], repeat)
	binary.BigEndian.PutUint32(buf[10:14], metastate)
	return buf
}

// EncodeTextEvent encodes a scrcpy text injection control message.
// Format: type(1) + length(4) + text(N)
func EncodeTextEvent(text string) []byte {
	textBytes := []byte(text)
	buf := make([]byte, 5+len(textBytes))
	buf[0] = ControlTypeText
	binary.BigEndian.PutUint32(buf[1:5], uint32(len(textBytes)))
	copy(buf[5:], textBytes)
	return buf
}

// floatToI16FixedPoint converts a float in [-1.0, 1.0] to a signed 16-bit fixed-point value.
// Matches scrcpy's sc_float_to_i16fp: multiplies by 0x7FFF and clamps.
func floatToI16FixedPoint(f float32) int16 {
	if f >= 1.0 {
		return 0x7FFF
	}
	if f <= -1.0 {
		return -0x7FFF
	}
	return int16(f * 0x7FFF)
}

// EncodeScrollEvent encodes a scrcpy scroll control message.
// x, y are normalized 0.0-1.0 coordinates from the browser.
// scrollH, scrollV are discrete scroll amounts (-1, 0, 1) from the browser.
// The protocol uses: position(12) + hscroll(2) + vscroll(2) + buttons(4) = 21 bytes total.
func EncodeScrollEvent(x, y float32, width, height uint16, scrollH, scrollV int32) []byte {
	buf := make([]byte, 21)
	buf[0] = ControlTypeScroll
	// Position: x(4) + y(4) + w(2) + h(2) = 12 bytes
	pixelX := uint32(x * float32(width))
	pixelY := uint32(y * float32(height))
	binary.BigEndian.PutUint32(buf[1:5], pixelX)
	binary.BigEndian.PutUint32(buf[5:9], pixelY)
	binary.BigEndian.PutUint16(buf[9:11], width)
	binary.BigEndian.PutUint16(buf[11:13], height)
	// Scroll values: normalized to [-1, 1] then encoded as signed 16-bit fixed-point
	hNorm := float32(scrollH) / 16.0
	if hNorm > 1.0 {
		hNorm = 1.0
	} else if hNorm < -1.0 {
		hNorm = -1.0
	}
	vNorm := float32(scrollV) / 16.0
	if vNorm > 1.0 {
		vNorm = 1.0
	} else if vNorm < -1.0 {
		vNorm = -1.0
	}
	binary.BigEndian.PutUint16(buf[13:15], uint16(floatToI16FixedPoint(hNorm)))
	binary.BigEndian.PutUint16(buf[15:17], uint16(floatToI16FixedPoint(vNorm)))
	binary.BigEndian.PutUint32(buf[17:21], 0) // buttons
	return buf
}

// EncodeBackOrScreenOn encodes a scrcpy back-or-screen-on control message.
// If screen is off, this wakes it. If screen is on, this sends BACK.
// Format: type(1) + action(1) = 2 bytes
func EncodeBackOrScreenOn(action uint8) []byte {
	return []byte{ControlTypeBackOrScreenOn, action}
}

// EncodeSetScreenPowerMode turns the physical screen on or off.
// mode: ScreenPowerOff (0) or ScreenPowerNormal (2)
// Format: type(1) + mode(1) = 2 bytes
func EncodeSetScreenPowerMode(mode uint8) []byte {
	return []byte{ControlTypeSetScreenPowerMode, mode}
}

// EncodeSetClipboard encodes a scrcpy SET_CLIPBOARD control message.
// Format: type(1) + sequence(8) + paste(1) + textLen(4) + text(N)
func EncodeSetClipboard(sequence uint64, text string, paste bool) []byte {
	textBytes := []byte(text)
	buf := make([]byte, 14+len(textBytes))
	buf[0] = ControlTypeSetClipboard
	binary.BigEndian.PutUint64(buf[1:9], sequence)
	if paste {
		buf[9] = 1
	}
	binary.BigEndian.PutUint32(buf[10:14], uint32(len(textBytes)))
	copy(buf[14:], textBytes)
	return buf
}

// EncodeGetClipboard encodes a scrcpy GET_CLIPBOARD control message.
// Format: type(1) + copyKey(1)
func EncodeGetClipboard(copyKey uint8) []byte {
	return []byte{ControlTypeGetClipboard, copyKey}
}
