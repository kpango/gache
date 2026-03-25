package gache

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

func BenchmarkRead(b *testing.B) {
	gc := New[int]()
	for i := 0; i < 10000; i++ {
		gc.Set(fmt.Sprintf("key-%d", i), i)
	}

	var buf bytes.Buffer
	err := gc.Write(context.Background(), &buf)
	if err != nil {
		b.Fatal(err)
	}
	data := buf.Bytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gc2 := New[int]()
		r := bytes.NewReader(data)
		err := gc2.Read(r)
		if err != nil {
			b.Fatal(err)
		}
	}
}
