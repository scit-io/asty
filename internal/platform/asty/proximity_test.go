package asty

import (
	"testing"
)

func TestProximityMatrixLoadFromConfig(t *testing.T) {
	pm := NewProximityMatrix()

	config := "eu-west:us-east:100,eu-west:asia:250,us-east:asia:200"
	if err := pm.LoadFromConfig(config); err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	tests := []struct {
		dc1     string
		dc2     string
		want    int
		wantOK  bool
	}{
		{"eu-west", "us-east", 100, true},
		{"us-east", "eu-west", 100, true}, // Bidirectional
		{"eu-west", "asia", 250, true},
		{"us-east", "asia", 200, true},
		{"eu-west", "eu-west", 0, true}, // Same DC
		{"eu-west", "unknown", 0, false}, // Unknown pair
	}

	for _, tt := range tests {
		t.Run(tt.dc1+"->"+tt.dc2, func(t *testing.T) {
			got, gotOK := pm.GetLatency(tt.dc1, tt.dc2)
			if gotOK != tt.wantOK {
				t.Errorf("GetLatency(%s, %s) gotOK = %v, want %v", tt.dc1, tt.dc2, gotOK, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("GetLatency(%s, %s) = %d, want %d", tt.dc1, tt.dc2, got, tt.want)
			}
		})
	}
}

func TestProximityMatrixGetNearestDatacenter(t *testing.T) {
	pm := NewProximityMatrix()
	pm.SetLatency("eu-west", "us-east", 100)
	pm.SetLatency("eu-west", "asia", 250)
	pm.SetLatency("us-east", "asia", 200)

	tests := []struct {
		name       string
		source     string
		candidates []string
		want       string
	}{
		{
			name:       "same DC",
			source:     "eu-west",
			candidates: []string{"eu-west", "us-east", "asia"},
			want:       "eu-west",
		},
		{
			name:       "nearest to eu-west",
			source:     "eu-west",
			candidates: []string{"us-east", "asia"},
			want:       "us-east", // 100ms vs 250ms
		},
		{
			name:       "nearest to us-east",
			source:     "us-east",
			candidates: []string{"eu-west", "asia"},
			want:       "eu-west", // 100ms vs 200ms
		},
		{
			name:       "single candidate",
			source:     "eu-west",
			candidates: []string{"asia"},
			want:       "asia",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pm.GetNearestDatacenter(tt.source, tt.candidates)
			if got != tt.want {
				t.Errorf("GetNearestDatacenter(%s, %v) = %s, want %s", tt.source, tt.candidates, got, tt.want)
			}
		})
	}
}

func TestProximityMatrixSortByProximity(t *testing.T) {
	pm := NewProximityMatrix()
	pm.SetLatency("eu-west", "us-east", 100)
	pm.SetLatency("eu-west", "asia", 250)
	pm.SetLatency("us-east", "asia", 200)

	tests := []struct {
		name   string
		source string
		dcs    []string
		want   []string
	}{
		{
			name:   "from eu-west",
			source: "eu-west",
			dcs:    []string{"us-east", "asia"},
			want:   []string{"us-east", "asia"},
		},
		{
			name:   "from us-east",
			source: "us-east",
			dcs:    []string{"eu-west", "asia"},
			want:   []string{"eu-west", "asia"},
		},
		{
			name:   "includes same DC",
			source: "eu-west",
			dcs:    []string{"eu-west", "us-east", "asia"},
			want:   []string{"eu-west", "us-east", "asia"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pm.SortDatacentersByProximity(tt.source, tt.dcs)
			if len(got) != len(tt.want) {
				t.Errorf("SortDatacentersByProximity() len = %d, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("SortDatacentersByProximity()[%d] = %s, want %s", i, got[i], tt.want[i])
				}
			}
		})
	}
}
