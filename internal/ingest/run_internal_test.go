package ingest

import "testing"

func TestCheckMalformedRate(t *testing.T) {
	tests := []struct {
		name      string
		malformed int
		rowsRead  int
		maxPct    float64
		wantErr   bool
	}{
		{"under threshold", 1, 1000, 1.0, false},
		{"over threshold", 50, 1000, 1.0, true},
		{"exactly at threshold is allowed", 10, 1000, 1.0, false},
		{"empty input does not divide by zero", 0, 0, 1.0, false},
		{"zero threshold trips on any malformed", 1, 1000, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkMalformedRate(&ParseResult{Malformed: tc.malformed, RowsRead: tc.rowsRead}, tc.maxPct)
			if (err != nil) != tc.wantErr {
				t.Errorf("checkMalformedRate(malformed=%d, rowsRead=%d, maxPct=%v) error = %v, wantErr %v",
					tc.malformed, tc.rowsRead, tc.maxPct, err, tc.wantErr)
			}
		})
	}
}
