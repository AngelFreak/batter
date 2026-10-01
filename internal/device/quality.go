package device

import "fmt"

// Quality is the full-quality view's video bitrate level. A device has one
// shared session, so the latest choice applies to everyone watching it.
type Quality string

const (
	QualityLow    Quality = "low"
	QualityMedium Quality = "medium"
	QualityHigh   Quality = "high"
)

// Bitrates passed to scrcpy-server as video_bit_rate (its own default is
// 8 Mbps, heavy for remote viewers on relayed links).
const (
	thumbnailBitRate = 1_000_000
	lowBitRate       = 1_500_000
	mediumBitRate    = 4_000_000
	highBitRate      = 8_000_000
)

// ParseQuality accepts the three levels; "" means medium.
func ParseQuality(s string) (Quality, error) {
	switch q := Quality(s); q {
	case "":
		return QualityMedium, nil
	case QualityLow, QualityMedium, QualityHigh:
		return q, nil
	}
	return "", fmt.Errorf("unknown quality %q (want low, medium or high)", s)
}

// BitRate is the level's video bitrate in bits per second.
func (q Quality) BitRate() int {
	switch q {
	case QualityLow:
		return lowBitRate
	case QualityHigh:
		return highBitRate
	}
	return mediumBitRate
}

// FullOptions are the full tier's session options at quality q.
func FullOptions(q Quality) SessionOptions {
	opts := TierOptions(TierFull)
	opts.VideoBitRate = q.BitRate()
	return opts
}

// qualityFor names the level a session's bitrate corresponds to (sessions
// started without a level, e.g. via StartSession, run at scrcpy's default,
// which is High's bitrate).
func qualityFor(bitRate int) Quality {
	switch bitRate {
	case lowBitRate:
		return QualityLow
	case mediumBitRate:
		return QualityMedium
	}
	return QualityHigh
}
