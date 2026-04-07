package gache

import (
	"fmt"
	"testing"
)

type dummyV struct{}

func BenchmarkReadLoop_Modulo3(b *testing.B) {
	numWorkers := 8
	m := make(map[string]dummyV, 10000)
	for i := 0; i < 10000; i++ {
		m[fmt.Sprintf("key%d", i)] = dummyV{}
	}

	// Pre-allocate to isolate the loop performance
	chunks := make([][]kv[dummyV], numWorkers)
	for i := range chunks {
		chunks[i] = make([]kv[dummyV], 0, len(m)/numWorkers+1)
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		for i := range chunks {
			chunks[i] = chunks[i][:0] // reset slices
		}

		i := 0
		for k, v := range m {
			chunks[i%numWorkers] = append(chunks[i%numWorkers], kv[dummyV]{key: k, value: v})
			i++
		}
	}
}

func BenchmarkReadLoop_Branch3(b *testing.B) {
	numWorkers := 8
	m := make(map[string]dummyV, 10000)
	for i := 0; i < 10000; i++ {
		m[fmt.Sprintf("key%d", i)] = dummyV{}
	}

	chunks := make([][]kv[dummyV], numWorkers)
	for i := range chunks {
		chunks[i] = make([]kv[dummyV], 0, len(m)/numWorkers+1)
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		for i := range chunks {
			chunks[i] = chunks[i][:0] // reset slices
		}

		i := 0
		for k, v := range m {
			chunks[i] = append(chunks[i], kv[dummyV]{key: k, value: v})
			i++
			if i == numWorkers {
				i = 0
			}
		}
	}
}

func BenchmarkReadLoop_Bitwise3(b *testing.B) {
	// Must be power of 2 for bitwise operation
	numWorkers := 8
	mask := numWorkers - 1
	m := make(map[string]dummyV, 10000)
	for i := 0; i < 10000; i++ {
		m[fmt.Sprintf("key%d", i)] = dummyV{}
	}

	chunks := make([][]kv[dummyV], numWorkers)
	for i := range chunks {
		chunks[i] = make([]kv[dummyV], 0, len(m)/numWorkers+1)
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		for i := range chunks {
			chunks[i] = chunks[i][:0] // reset slices
		}

		i := 0
		for k, v := range m {
			chunks[i&mask] = append(chunks[i&mask], kv[dummyV]{key: k, value: v})
			i++
		}
	}
}
