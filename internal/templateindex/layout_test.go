package templateindex

import (
	"testing"
)

func TestClusterCount_Empty(t *testing.T) {
	if got := ClusterCount(nil, 100); got != 1 {
		t.Errorf("ClusterCount(nil) = %d, want 1", got)
	}
}

func TestClusterCount_SingleValue(t *testing.T) {
	if got := ClusterCount([]float64{500}, 100); got != 1 {
		t.Errorf("ClusterCount([500]) = %d, want 1", got)
	}
}

func TestClusterCount_AllClose(t *testing.T) {
	values := []float64{100, 150, 120, 180}
	if got := ClusterCount(values, 100); got != 1 {
		t.Errorf("ClusterCount = %d, want 1 (all within threshold)", got)
	}
}

func TestClusterCount_TwoClusters(t *testing.T) {
	values := []float64{100, 120, 500, 520}
	if got := ClusterCount(values, 200); got != 2 {
		t.Errorf("ClusterCount = %d, want 2", got)
	}
}

func TestClusterCount_ThreeClusters(t *testing.T) {
	values := []float64{100, 120, 500, 520, 1000, 1010}
	if got := ClusterCount(values, 200); got != 3 {
		t.Errorf("ClusterCount = %d, want 3", got)
	}
}

func TestClusterCount_UnsortedInput(t *testing.T) {
	values := []float64{1000, 100, 500, 1010, 120, 520}
	if got := ClusterCount(values, 200); got != 3 {
		t.Errorf("ClusterCount = %d, want 3 (should sort internally)", got)
	}
}
