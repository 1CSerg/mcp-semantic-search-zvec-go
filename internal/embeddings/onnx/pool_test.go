package onnx

import (
	"math"
	"testing"
)

func TestMeanPool(t *testing.T) {
	raw := []float32{
		1, 0, 0, 0,
		3, 0, 0, 0,
	}
	mask := []int64{1, 1}
	vec := meanPool(raw, 0, 2, 4, mask)
	if math.Abs(float64(vec[0]-2)) > 1e-5 {
		t.Fatalf("vec[0]=%v want 2", vec[0])
	}
}

func TestL2Normalize(t *testing.T) {
	vec := l2Normalize([]float32{3, 4})
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if math.Abs(sum-1) > 1e-5 {
		t.Fatalf("norm=%v", sum)
	}
}
