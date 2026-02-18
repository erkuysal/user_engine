package presence

import "testing"

func TestDerivePrimaryDevice(t *testing.T) {
	tests := []struct {
		name    string
		devices map[string]int64
		want    string
	}{
		{"desktopWins", map[string]int64{"mobile": 2, "desktop": 1}, "desktop"},
		{"tabletOverMobile", map[string]int64{"tablet": 1, "mobile": 5}, "tablet"},
		{"mobileOnly", map[string]int64{"mobile": 1}, "mobile"},
		{"unknownWhenExplicit", map[string]int64{"unknown": 1}, "unknown"},
		{"empty", map[string]int64{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := derivePrimaryDevice(tt.devices); got != tt.want {
				t.Fatalf("derivePrimaryDevice() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseInt64(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"0", 0, false},
		{"42", 42, false},
		{"001", 1, false},
		{"-1", 0, true},
		{"1a", 0, true},
		{"", 0, false}, // empty yields 0 (loop never runs)
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseInt64(tt.in)
			if tt.wantErr && err == nil {
				t.Fatalf("parseInt64(%q) expected error, got nil (value=%d)", tt.in, got)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("parseInt64(%q) unexpected error: %v", tt.in, err)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("parseInt64(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseTransitionResult(t *testing.T) {
	tests := []struct {
		name    string
		in      interface{}
		want    *TransitionResult
		wantErr bool
	}{
		{
			name: "twoFields",
			in:   []interface{}{"online", int64(7)},
			want: &TransitionResult{Transition: "online", Version: 7, SessionCount: 0},
		},
		{
			name: "threeFields",
			in:   []interface{}{"offline", int64(9), int64(0)},
			want: &TransitionResult{Transition: "offline", Version: 9, SessionCount: 0},
		},
		{
			name:    "wrongType",
			in:      []interface{}{123, int64(1)},
			wantErr: true,
		},
		{
			name:    "wrongLen",
			in:      []interface{}{"online"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTransitionResult(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (value=%+v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Transition != tt.want.Transition || got.Version != tt.want.Version || got.SessionCount != tt.want.SessionCount {
				t.Fatalf("parseTransitionResult()=%+v, want %+v", got, tt.want)
			}
		})
	}
}

