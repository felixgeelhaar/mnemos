package embedding

import (
	"math"
	"testing"
)

func TestEncodeDecodeRoundtrip(t *testing.T) {
	original := []float32{0.1, 0.2, 0.3, -0.5, 1.0, 0.0}
	encoded := EncodeVector(original)

	if len(encoded) != len(original)*4 {
		t.Fatalf("encoded length = %d, want %d", len(encoded), len(original)*4)
	}

	decoded, err := DecodeVector(encoded)
	if err != nil {
		t.Fatalf("DecodeVector() error = %v", err)
	}

	if len(decoded) != len(original) {
		t.Fatalf("decoded length = %d, want %d", len(decoded), len(original))
	}

	for i := range original {
		if decoded[i] != original[i] {
			t.Errorf("decoded[%d] = %f, want %f", i, decoded[i], original[i])
		}
	}
}

func TestDecodeVectorInvalidLength(t *testing.T) {
	_, err := DecodeVector([]byte{0x01, 0x02, 0x03})
	if err == nil {
		t.Fatal("expected error for non-multiple-of-4 length")
	}
}

func TestDecodeVectorEmpty(t *testing.T) {
	decoded, err := DecodeVector([]byte{})
	if err != nil {
		t.Fatalf("DecodeVector() error = %v", err)
	}
	if len(decoded) != 0 {
		t.Fatalf("decoded length = %d, want 0", len(decoded))
	}
}

func TestCosineSimilarityIdentical(t *testing.T) {
	v := []float32{1, 2, 3}
	sim, err := CosineSimilarity(v, v)
	if err != nil {
		t.Fatalf("CosineSimilarity() error = %v", err)
	}
	if math.Abs(float64(sim-1.0)) > 0.0001 {
		t.Fatalf("CosineSimilarity identical vectors = %f, want 1.0", sim)
	}
}

func TestCosineSimilarityOrthogonal(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	sim, err := CosineSimilarity(a, b)
	if err != nil {
		t.Fatalf("CosineSimilarity() error = %v", err)
	}
	if math.Abs(float64(sim)) > 0.0001 {
		t.Fatalf("CosineSimilarity orthogonal = %f, want 0.0", sim)
	}
}

func TestCosineSimilarityOpposite(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{-1, 0}
	sim, err := CosineSimilarity(a, b)
	if err != nil {
		t.Fatalf("CosineSimilarity() error = %v", err)
	}
	if math.Abs(float64(sim+1.0)) > 0.0001 {
		t.Fatalf("CosineSimilarity opposite = %f, want -1.0", sim)
	}
}

func TestCosineSimilarityDifferentLengths(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{1, 0, 0}
	_, err := CosineSimilarity(a, b)
	if err == nil {
		t.Fatal("expected error for different length vectors")
	}
}

func TestCosineSimilarityZeroVector(t *testing.T) {
	a := []float32{0, 0, 0}
	b := []float32{1, 2, 3}
	sim, err := CosineSimilarity(a, b)
	if err != nil {
		t.Fatalf("CosineSimilarity() error = %v", err)
	}
	if sim != 0 {
		t.Fatalf("CosineSimilarity zero vector = %f, want 0", sim)
	}
}

func TestCosineSimilarityEmpty(t *testing.T) {
	sim, err := CosineSimilarity([]float32{}, []float32{})
	if err != nil {
		t.Fatalf("CosineSimilarity() error = %v", err)
	}
	if sim != 0 {
		t.Fatalf("CosineSimilarity empty = %f, want 0", sim)
	}
}
