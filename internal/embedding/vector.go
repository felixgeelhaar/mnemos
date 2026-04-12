package embedding

import (
	"encoding/binary"
	"fmt"
	"math"
)

// EncodeVector serializes a float32 vector to a compact binary BLOB
// using little-endian encoding (4 bytes per dimension).
func EncodeVector(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// DecodeVector deserializes a binary BLOB back to a float32 vector.
func DecodeVector(data []byte) ([]float32, error) {
	if len(data)%4 != 0 {
		return nil, fmt.Errorf("invalid vector blob: length %d is not a multiple of 4", len(data))
	}
	v := make([]float32, len(data)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return v, nil
}

// CosineSimilarity computes the cosine similarity between two vectors.
// Returns 0.0 if either vector has zero magnitude. Returns an error if
// the vectors have different dimensions.
func CosineSimilarity(a, b []float32) (float32, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("embedding dimension mismatch: got %d and %d", len(a), len(b))
	}
	if len(a) == 0 {
		return 0, nil
	}
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0, nil
	}
	return dot / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB)))), nil
}
