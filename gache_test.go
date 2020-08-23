package gache

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sync/singleflight"
)

func TestNew(t *testing.T) {
	type args struct {
		opts []Option
	}
	tests := []struct {
		name string
		args args
		want Gache
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := New(tt.args.opts...); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("New() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_newGache(t *testing.T) {
	type args struct {
		opts []Option
	}
	tests := []struct {
		name string
		args args
		want *gache
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newGache(tt.args.opts...); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("newGache() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetGache(t *testing.T) {
	tests := []struct {
		name string
		want Gache
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetGache(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetGache() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_value_isValid(t *testing.T) {
	type fields struct {
		expire int64
		val    interface{}
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &value{
				expire: tt.fields.expire,
				val:    tt.fields.val,
			}
			if got := v.isValid(); got != tt.want {
				t.Errorf("value.isValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_gache_SetDefaultExpire(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	type args struct {
		ex time.Duration
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   Gache
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			if got := g.SetDefaultExpire(tt.args.ex); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("gache.SetDefaultExpire() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetDefaultExpire(t *testing.T) {
	type args struct {
		ex time.Duration
	}
	tests := []struct {
		name string
		args args
		want Gache
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SetDefaultExpire(tt.args.ex); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SetDefaultExpire() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_gache_EnableExpiredHook(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	tests := []struct {
		name   string
		fields fields
		want   Gache
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			if got := g.EnableExpiredHook(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("gache.EnableExpiredHook() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnableExpiredHook(t *testing.T) {
	tests := []struct {
		name string
		want Gache
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EnableExpiredHook(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("EnableExpiredHook() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_gache_DisableExpiredHook(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	tests := []struct {
		name   string
		fields fields
		want   Gache
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			if got := g.DisableExpiredHook(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("gache.DisableExpiredHook() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDisableExpiredHook(t *testing.T) {
	tests := []struct {
		name string
		want Gache
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DisableExpiredHook(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DisableExpiredHook() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_gache_SetExpiredHook(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	type args struct {
		f func(context.Context, string)
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   Gache
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			if got := g.SetExpiredHook(tt.args.f); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("gache.SetExpiredHook() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetExpiredHook(t *testing.T) {
	type args struct {
		f func(context.Context, string)
	}
	tests := []struct {
		name string
		args args
		want Gache
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SetExpiredHook(tt.args.f); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SetExpiredHook() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_gache_StartExpired(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	type args struct {
		ctx context.Context
		dur time.Duration
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   Gache
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			if got := g.StartExpired(tt.args.ctx, tt.args.dur); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("gache.StartExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_gache_ToMap(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *sync.Map
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			if got := g.ToMap(tt.args.ctx); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("gache.ToMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToMap(t *testing.T) {
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name string
		args args
		want *sync.Map
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ToMap(tt.args.ctx); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ToMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_gache_ToRawMap(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   map[string]interface{}
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			if got := g.ToRawMap(tt.args.ctx); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("gache.ToRawMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToRawMap(t *testing.T) {
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name string
		args args
		want map[string]interface{}
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ToRawMap(tt.args.ctx); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ToRawMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_gache_get(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	type args struct {
		key string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   interface{}
		want1  int64
		want2  bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			got, got1, got2 := g.get(tt.args.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("gache.get() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("gache.get() got1 = %v, want %v", got1, tt.want1)
			}
			if got2 != tt.want2 {
				t.Errorf("gache.get() got2 = %v, want %v", got2, tt.want2)
			}
		})
	}
}

func Test_gache_Get(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	type args struct {
		key string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   interface{}
		want1  bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			got, got1 := g.Get(tt.args.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("gache.Get() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("gache.Get() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestGet(t *testing.T) {
	type args struct {
		key string
	}
	tests := []struct {
		name  string
		args  args
		want  interface{}
		want1 bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := Get(tt.args.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Get() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("Get() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func Test_gache_GetWithExpire(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	type args struct {
		key string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   interface{}
		want1  int64
		want2  bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			got, got1, got2 := g.GetWithExpire(tt.args.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("gache.GetWithExpire() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("gache.GetWithExpire() got1 = %v, want %v", got1, tt.want1)
			}
			if got2 != tt.want2 {
				t.Errorf("gache.GetWithExpire() got2 = %v, want %v", got2, tt.want2)
			}
		})
	}
}

func TestGetWithExpire(t *testing.T) {
	type args struct {
		key string
	}
	tests := []struct {
		name  string
		args  args
		want  interface{}
		want1 int64
		want2 bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, got2 := GetWithExpire(tt.args.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetWithExpire() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("GetWithExpire() got1 = %v, want %v", got1, tt.want1)
			}
			if got2 != tt.want2 {
				t.Errorf("GetWithExpire() got2 = %v, want %v", got2, tt.want2)
			}
		})
	}
}

func Test_gache_set(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	type args struct {
		key    string
		val    interface{}
		expire int64
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			g.set(tt.args.key, tt.args.val, tt.args.expire)
		})
	}
}

func Test_gache_SetWithExpire(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	type args struct {
		key    string
		val    interface{}
		expire time.Duration
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			g.SetWithExpire(tt.args.key, tt.args.val, tt.args.expire)
		})
	}
}

func TestSetWithExpire(t *testing.T) {
	type args struct {
		key    string
		val    interface{}
		expire time.Duration
	}
	tests := []struct {
		name string
		args args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetWithExpire(tt.args.key, tt.args.val, tt.args.expire)
		})
	}
}

func Test_gache_Set(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	type args struct {
		key string
		val interface{}
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			g.Set(tt.args.key, tt.args.val)
		})
	}
}

func TestSet(t *testing.T) {
	type args struct {
		key string
		val interface{}
	}
	tests := []struct {
		name string
		args args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Set(tt.args.key, tt.args.val)
		})
	}
}

func Test_gache_Delete(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	type args struct {
		key string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			g.Delete(tt.args.key)
		})
	}
}

func TestDelete(t *testing.T) {
	type args struct {
		key string
	}
	tests := []struct {
		name string
		args args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Delete(tt.args.key)
		})
	}
}

func Test_gache_expiration(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	type args struct {
		key string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			g.expiration(tt.args.key)
		})
	}
}

func Test_gache_DeleteExpired(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   uint64
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			if got := g.DeleteExpired(tt.args.ctx); got != tt.want {
				t.Errorf("gache.DeleteExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeleteExpired(t *testing.T) {
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name string
		args args
		want uint64
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeleteExpired(tt.args.ctx); got != tt.want {
				t.Errorf("DeleteExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_gache_Foreach(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	type args struct {
		ctx context.Context
		f   func(string, interface{}, int64) bool
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   Gache
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			if got := g.Foreach(tt.args.ctx, tt.args.f); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("gache.Foreach() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestForeach(t *testing.T) {
	type args struct {
		ctx context.Context
		f   func(string, interface{}, int64) bool
	}
	tests := []struct {
		name string
		args args
		want Gache
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Foreach(tt.args.ctx, tt.args.f); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Foreach() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLen(t *testing.T) {
	tests := []struct {
		name string
		want int
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Len(); got != tt.want {
				t.Errorf("Len() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_gache_Len(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	tests := []struct {
		name   string
		fields fields
		want   int
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			if got := g.Len(); got != tt.want {
				t.Errorf("gache.Len() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_gache_Write(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantW   string
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			w := &bytes.Buffer{}
			if err := g.Write(tt.args.ctx, w); (err != nil) != tt.wantErr {
				t.Errorf("gache.Write() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotW := w.String(); gotW != tt.wantW {
				t.Errorf("gache.Write() = %v, want %v", gotW, tt.wantW)
			}
		})
	}
}

func TestWrite(t *testing.T) {
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name    string
		args    args
		wantW   string
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &bytes.Buffer{}
			if err := Write(tt.args.ctx, w); (err != nil) != tt.wantErr {
				t.Errorf("Write() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotW := w.String(); gotW != tt.wantW {
				t.Errorf("Write() = %v, want %v", gotW, tt.wantW)
			}
		})
	}
}

func Test_gache_Read(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	type args struct {
		r io.Reader
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			if err := g.Read(tt.args.r); (err != nil) != tt.wantErr {
				t.Errorf("gache.Read() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRead(t *testing.T) {
	type args struct {
		r io.Reader
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Read(tt.args.r); (err != nil) != tt.wantErr {
				t.Errorf("Read() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_gache_Stop(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	tests := []struct {
		name   string
		fields fields
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			g.Stop()
		})
	}
}

func TestStop(t *testing.T) {
	tests := []struct {
		name string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Stop()
		})
	}
}

func Test_gache_Clear(t *testing.T) {
	type fields struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         atomic.Value
		expire         int64
		l              uint64
		shards         [slen]*Map
	}
	tests := []struct {
		name   string
		fields fields
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				expChan:        tt.fields.expChan,
				expFunc:        tt.fields.expFunc,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expGroup:       tt.fields.expGroup,
				cancel:         tt.fields.cancel,
				expire:         tt.fields.expire,
				l:              tt.fields.l,
				shards:         tt.fields.shards,
			}
			g.Clear()
		})
	}
}

func TestClear(t *testing.T) {
	tests := []struct {
		name string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Clear()
		})
	}
}
