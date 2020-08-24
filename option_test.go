package gache

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestWithDefaultExpirationString(t *testing.T) {
	type args struct {
		t string
	}
	tests := []struct {
		name string
		args args
		want Option
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WithDefaultExpirationString(tt.args.t); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WithDefaultExpirationString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWithDefaultExpiration(t *testing.T) {
	type args struct {
		dur time.Duration
	}
	tests := []struct {
		name string
		args args
		want Option
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WithDefaultExpiration(tt.args.dur); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WithDefaultExpiration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWithExpiredHookFunc(t *testing.T) {
	type args struct {
		f func(ctx context.Context, key string)
	}
	tests := []struct {
		name string
		args args
		want Option
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WithExpiredHookFunc(tt.args.f); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WithExpiredHookFunc() = %v, want %v", got, tt.want)
			}
		})
	}
}
