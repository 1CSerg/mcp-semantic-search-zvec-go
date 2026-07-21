//go:build onnx

package onnx

import (
	"fmt"
	"math"
)

func meanPool(raw []float32, batchIdx, seqLen, hidden int, mask []int64) ([]float32, error) {
	out := make([]float32, hidden)
	var count float32
	offset := batchIdx * seqLen * hidden
	for t := 0; t < seqLen; t++ {
		if mask[t] == 0 {
			continue
		}
		count++
		row := offset + t*hidden
		for h := 0; h < hidden; h++ {
			out[h] += raw[row+h]
		}
	}
	if count == 0 {
		return nil, fmt.Errorf("mean pool: no unmasked tokens")
	}
	inv := 1 / count
	for i := range out {
		out[i] *= inv
	}
	return out, nil
}

func l2Normalize(vec []float32) ([]float32, error) {
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum == 0 {
		return nil, fmt.Errorf("zero vector after mean pool")
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range vec {
		vec[i] *= inv
	}
	return vec, nil
}
