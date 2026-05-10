package partition_test

import (
	"testing"
	"time"

	"github.com/crashchat-ai/mio/sink-gcs/internal/partition"
)

func TestPath(t *testing.T) {
	tests := []struct {
		name        string
		channelType string
		ts          time.Time
		want        string
	}{
		{
			name:        "zoho_cliq underscore slug",
			channelType: "zoho_cliq",
			ts:          time.Date(2026, 5, 8, 11, 0, 0, 0, time.UTC),
			want:        "channel_type=zoho_cliq/date=2026-05-08",
		},
		{
			name:        "UTC conversion from non-UTC timezone",
			channelType: "zoho_cliq",
			// 2026-05-08 23:00 ICT (UTC+7) = 2026-05-08 16:00 UTC
			ts:   time.Date(2026, 5, 8, 23, 0, 0, 0, time.FixedZone("ICT", 7*3600)),
			want: "channel_type=zoho_cliq/date=2026-05-08",
		},
		{
			name:        "midnight boundary stays in correct UTC date",
			channelType: "zoho_cliq",
			// 2026-05-09 01:00 ICT = 2026-05-08 18:00 UTC → still May 8
			ts:   time.Date(2026, 5, 9, 1, 0, 0, 0, time.FixedZone("ICT", 7*3600)),
			want: "channel_type=zoho_cliq/date=2026-05-08",
		},
		{
			name:        "midnight crossover to next day in UTC",
			channelType: "slack",
			// 2026-05-09 00:30 UTC → May 9
			ts:   time.Date(2026, 5, 9, 0, 30, 0, 0, time.UTC),
			want: "channel_type=slack/date=2026-05-09",
		},
		{
			name:        "key is channel_type not channel",
			channelType: "telegram",
			ts:          time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			want:        "channel_type=telegram/date=2026-01-01",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := partition.Path(tc.channelType, tc.ts)
			if got != tc.want {
				t.Errorf("Path(%q, %v) = %q; want %q", tc.channelType, tc.ts, got, tc.want)
			}
		})
	}
}
