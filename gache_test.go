package gache

import (
	"fmt"
	"testing"
)

var g *Gache

func init() {
	g = New()
}

func TestGache(t *testing.T) {
	data := map[string]interface{}{
		"string": "aaaa",
		"int":    123,
		"float":  99.99,
		"struct": struct{}{},
	}

	for k, v := range data {
		t.Run(fmt.Sprintf("key: %v\tval: %v", k, v), func(t *testing.T) {
			ok := Set(k, v)
			if !ok {
				t.Errorf("Gache Set failed key: %v\tval: %v\n", k, v)
			}
			val, ok := Get(k)
			if !ok {
				t.Errorf("Gache Get failed key: %v\tval: %v\n", k, v)
			}
			if val != v {
				t.Errorf("expect %v but got %v", v, val)
			}
			t.Log(val)
		})
	}
}
