package schedule

import (
	"testing"
	"time"
)

func TestParseScheduleRule(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "weekday range with timezone",
			input:   "Mon-Fri 08:00-20:00 Asia/Ho_Chi_Minh",
			wantErr: false,
		},
		{
			name:    "single day",
			input:   "Sun 00:00-24:00 Asia/Ho_Chi_Minh",
			wantErr: false,
		},
		{
			name:    "invalid timezone",
			input:   "Mon-Fri 08:00-20:00 Invalid/Zone",
			wantErr: true,
		},
		{
			name:    "invalid time format",
			input:   "Mon-Fri 8:00-20:00 Asia/Ho_Chi_Minh",
			wantErr: true,
		},
		{
			name:    "invalid day range",
			input:   "Abc-Fri 08:00-20:00 Asia/Ho_Chi_Minh",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseRule(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRule(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestParseSchedule(t *testing.T) {
	input := "Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh, Sun 00:00-24:00 Asia/Ho_Chi_Minh"
	rules, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(%q) unexpected error: %v", input, err)
	}
	if len(rules) != 2 {
		t.Fatalf("Parse(%q) got %d rules, want 2", input, len(rules))
	}
}

func TestIsActive(t *testing.T) {
	bkk, _ := time.LoadLocation("Asia/Ho_Chi_Minh")

	tests := []struct {
		name     string
		schedule string
		at       time.Time
		want     bool
	}{
		{
			name:     "monday 10am is within Mon-Sat 08:00-20:00",
			schedule: "Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh",
			at:       time.Date(2026, 4, 20, 10, 0, 0, 0, bkk), // Monday
			want:     true,
		},
		{
			name:     "monday 21:00 is outside Mon-Sat 08:00-20:00",
			schedule: "Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh",
			at:       time.Date(2026, 4, 20, 21, 0, 0, 0, bkk), // Monday
			want:     false,
		},
		{
			name:     "sunday 12:00 is outside Mon-Sat",
			schedule: "Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh",
			at:       time.Date(2026, 4, 19, 12, 0, 0, 0, bkk), // Sunday
			want:     false,
		},
		{
			name:     "sunday 12:00 matches Sun 00:00-24:00",
			schedule: "Sun 00:00-24:00 Asia/Ho_Chi_Minh",
			at:       time.Date(2026, 4, 19, 12, 0, 0, 0, bkk), // Sunday
			want:     true,
		},
		{
			name:     "exactly at start boundary is active",
			schedule: "Mon-Fri 08:00-20:00 Asia/Ho_Chi_Minh",
			at:       time.Date(2026, 4, 20, 8, 0, 0, 0, bkk), // Monday 08:00
			want:     true,
		},
		{
			name:     "exactly at end boundary is not active",
			schedule: "Mon-Fri 08:00-20:00 Asia/Ho_Chi_Minh",
			at:       time.Date(2026, 4, 20, 20, 0, 0, 0, bkk), // Monday 20:00
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules, err := Parse(tt.schedule)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.schedule, err)
			}
			got := IsActive(rules, tt.at)
			if got != tt.want {
				t.Errorf("IsActive(%q, %v) = %v, want %v", tt.schedule, tt.at, got, tt.want)
			}
		})
	}
}

func TestIsActiveMultiRule(t *testing.T) {
	bkk, _ := time.LoadLocation("Asia/Ho_Chi_Minh")

	schedule := "Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh, Sun 10:00-16:00 Asia/Ho_Chi_Minh"
	rules, err := Parse(schedule)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	// Sunday 12:00 — matches second rule
	got := IsActive(rules, time.Date(2026, 4, 19, 12, 0, 0, 0, bkk))
	if !got {
		t.Error("expected Sunday 12:00 to be active via second rule")
	}

	// Sunday 09:00 — no rule matches
	got = IsActive(rules, time.Date(2026, 4, 19, 9, 0, 0, 0, bkk))
	if got {
		t.Error("expected Sunday 09:00 to be inactive")
	}
}
