package device

import (
	"slices"
	"testing"
)

func TestServerArgsCarryBitRatePerTierAndQuality(t *testing.T) {
	cases := map[string]struct {
		opts SessionOptions
		want string
	}{
		"thumbnail": {TierOptions(TierThumbnail), "video_bit_rate=1000000"},
		"full":      {TierOptions(TierFull), "video_bit_rate=4000000"},
		"low":       {FullOptions(QualityLow), "video_bit_rate=1500000"},
		"medium":    {FullOptions(QualityMedium), "video_bit_rate=4000000"},
		"high":      {FullOptions(QualityHigh), "video_bit_rate=8000000"},
	}
	for name, c := range cases {
		if args := buildServerArgs(1, "3.3.4", c.opts); !slices.Contains(args, c.want) {
			t.Errorf("%s: args %v lack %s", name, args, c.want)
		}
	}
}

func TestParseQualityAcceptsOnlyTheLevels(t *testing.T) {
	for in, want := range map[string]Quality{"": QualityMedium, "low": QualityLow, "medium": QualityMedium, "high": QualityHigh} {
		if got, err := ParseQuality(in); err != nil || got != want {
			t.Errorf("ParseQuality(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"ultra", "8000000", "HIGH"} {
		if _, err := ParseQuality(bad); err == nil {
			t.Errorf("ParseQuality(%q) accepted", bad)
		}
	}
}
