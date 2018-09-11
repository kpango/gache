package gache

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sync/singleflight"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name string
		want Gache
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := New(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("New() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_newGache(t *testing.T) {
	tests := []struct {
		name string
		want *gache
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newGache(); !reflect.DeepEqual(got, tt.want) {
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
		val    *interface{}
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
		l              uint64
		shards         [255]*shard
		expire         *atomic.Value
		expFuncEnabled bool
		expFunc        func(context.Context, string)
		expChan        chan string
		expGroup       singleflight.Group
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
				l:              tt.fields.l,
				shards:         tt.fields.shards,
				expire:         tt.fields.expire,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expFunc:        tt.fields.expFunc,
				expChan:        tt.fields.expChan,
				expGroup:       tt.fields.expGroup,
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
		l              uint64
		shards         [255]*shard
		expire         *atomic.Value
		expFuncEnabled bool
		expFunc        func(context.Context, string)
		expChan        chan string
		expGroup       singleflight.Group
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
				l:              tt.fields.l,
				shards:         tt.fields.shards,
				expire:         tt.fields.expire,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expFunc:        tt.fields.expFunc,
				expChan:        tt.fields.expChan,
				expGroup:       tt.fields.expGroup,
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
		l              uint64
		shards         [255]*shard
		expire         *atomic.Value
		expFuncEnabled bool
		expFunc        func(context.Context, string)
		expChan        chan string
		expGroup       singleflight.Group
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
				l:              tt.fields.l,
				shards:         tt.fields.shards,
				expire:         tt.fields.expire,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expFunc:        tt.fields.expFunc,
				expChan:        tt.fields.expChan,
				expGroup:       tt.fields.expGroup,
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
		l              uint64
		shards         [255]*shard
		expire         *atomic.Value
		expFuncEnabled bool
		expFunc        func(context.Context, string)
		expChan        chan string
		expGroup       singleflight.Group
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
				l:              tt.fields.l,
				shards:         tt.fields.shards,
				expire:         tt.fields.expire,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expFunc:        tt.fields.expFunc,
				expChan:        tt.fields.expChan,
				expGroup:       tt.fields.expGroup,
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
		l              uint64
		shards         [255]*shard
		expire         *atomic.Value
		expFuncEnabled bool
		expFunc        func(context.Context, string)
		expChan        chan string
		expGroup       singleflight.Group
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
				l:              tt.fields.l,
				shards:         tt.fields.shards,
				expire:         tt.fields.expire,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expFunc:        tt.fields.expFunc,
				expChan:        tt.fields.expChan,
				expGroup:       tt.fields.expGroup,
			}
			if got := g.StartExpired(tt.args.ctx, tt.args.dur); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("gache.StartExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_gache_ToMap(t *testing.T) {
	type fields struct {
		l              uint64
		shards         [255]*shard
		expire         *atomic.Value
		expFuncEnabled bool
		expFunc        func(context.Context, string)
		expChan        chan string
		expGroup       singleflight.Group
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
				l:              tt.fields.l,
				shards:         tt.fields.shards,
				expire:         tt.fields.expire,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expFunc:        tt.fields.expFunc,
				expChan:        tt.fields.expChan,
				expGroup:       tt.fields.expGroup,
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

func Test_gache_get(t *testing.T) {
	type fields struct {
		l              uint64
		shards         [255]*shard
		expire         *atomic.Value
		expFuncEnabled bool
		expFunc        func(context.Context, string)
		expChan        chan string
		expGroup       singleflight.Group
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
				l:              tt.fields.l,
				shards:         tt.fields.shards,
				expire:         tt.fields.expire,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expFunc:        tt.fields.expFunc,
				expChan:        tt.fields.expChan,
				expGroup:       tt.fields.expGroup,
			}
			got, got1 := g.get(tt.args.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("gache.get() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("gache.get() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func Test_gache_Get(t *testing.T) {
	type fields struct {
		l              uint64
		shards         [255]*shard
		expire         *atomic.Value
		expFuncEnabled bool
		expFunc        func(context.Context, string)
		expChan        chan string
		expGroup       singleflight.Group
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
				l:              tt.fields.l,
				shards:         tt.fields.shards,
				expire:         tt.fields.expire,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expFunc:        tt.fields.expFunc,
				expChan:        tt.fields.expChan,
				expGroup:       tt.fields.expGroup,
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

func Test_gache_set(t *testing.T) {
	type fields struct {
		l              uint64
		shards         [255]*shard
		expire         *atomic.Value
		expFuncEnabled bool
		expFunc        func(context.Context, string)
		expChan        chan string
		expGroup       singleflight.Group
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
				l:              tt.fields.l,
				shards:         tt.fields.shards,
				expire:         tt.fields.expire,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expFunc:        tt.fields.expFunc,
				expChan:        tt.fields.expChan,
				expGroup:       tt.fields.expGroup,
			}
			g.set(tt.args.key, tt.args.val, tt.args.expire)
		})
	}
}

func Test_gache_SetWithExpire(t *testing.T) {
	type fields struct {
		l              uint64
		shards         [255]*shard
		expire         *atomic.Value
		expFuncEnabled bool
		expFunc        func(context.Context, string)
		expChan        chan string
		expGroup       singleflight.Group
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
				l:              tt.fields.l,
				shards:         tt.fields.shards,
				expire:         tt.fields.expire,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expFunc:        tt.fields.expFunc,
				expChan:        tt.fields.expChan,
				expGroup:       tt.fields.expGroup,
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
		l              uint64
		shards         [255]*shard
		expire         *atomic.Value
		expFuncEnabled bool
		expFunc        func(context.Context, string)
		expChan        chan string
		expGroup       singleflight.Group
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
				l:              tt.fields.l,
				shards:         tt.fields.shards,
				expire:         tt.fields.expire,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expFunc:        tt.fields.expFunc,
				expChan:        tt.fields.expChan,
				expGroup:       tt.fields.expGroup,
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
		l              uint64
		shards         [255]*shard
		expire         *atomic.Value
		expFuncEnabled bool
		expFunc        func(context.Context, string)
		expChan        chan string
		expGroup       singleflight.Group
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
				l:              tt.fields.l,
				shards:         tt.fields.shards,
				expire:         tt.fields.expire,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expFunc:        tt.fields.expFunc,
				expChan:        tt.fields.expChan,
				expGroup:       tt.fields.expGroup,
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
		l              uint64
		shards         [255]*shard
		expire         *atomic.Value
		expFuncEnabled bool
		expFunc        func(context.Context, string)
		expChan        chan string
		expGroup       singleflight.Group
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
				l:              tt.fields.l,
				shards:         tt.fields.shards,
				expire:         tt.fields.expire,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expFunc:        tt.fields.expFunc,
				expChan:        tt.fields.expChan,
				expGroup:       tt.fields.expGroup,
			}
			g.expiration(tt.args.key)
		})
	}
}

func Test_gache_DeleteExpired(t *testing.T) {
	type fields struct {
		l              uint64
		shards         [255]*shard
		expire         *atomic.Value
		expFuncEnabled bool
		expFunc        func(context.Context, string)
		expChan        chan string
		expGroup       singleflight.Group
	}
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   <-chan uint64
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gache{
				l:              tt.fields.l,
				shards:         tt.fields.shards,
				expire:         tt.fields.expire,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expFunc:        tt.fields.expFunc,
				expChan:        tt.fields.expChan,
				expGroup:       tt.fields.expGroup,
			}
			if got := g.DeleteExpired(tt.args.ctx); !reflect.DeepEqual(got, tt.want) {
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
		want <-chan uint64
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeleteExpired(tt.args.ctx); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DeleteExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_gache_Foreach(t *testing.T) {
	type fields struct {
		l              uint64
		shards         [255]*shard
		expire         *atomic.Value
		expFuncEnabled bool
		expFunc        func(context.Context, string)
		expChan        chan string
		expGroup       singleflight.Group
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
				l:              tt.fields.l,
				shards:         tt.fields.shards,
				expire:         tt.fields.expire,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expFunc:        tt.fields.expFunc,
				expChan:        tt.fields.expChan,
				expGroup:       tt.fields.expGroup,
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

func Test_gache_selectShard(t *testing.T) {
	type fields struct {
		l              uint64
		shards         [255]*shard
		expire         *atomic.Value
		expFuncEnabled bool
		expFunc        func(context.Context, string)
		expChan        chan string
		expGroup       singleflight.Group
	}
	type args struct {
		key string
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
				l:              tt.fields.l,
				shards:         tt.fields.shards,
				expire:         tt.fields.expire,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expFunc:        tt.fields.expFunc,
				expChan:        tt.fields.expChan,
				expGroup:       tt.fields.expGroup,
			}
			if got := g.selectShard(tt.args.key); got != tt.want {
				t.Errorf("gache.selectShard() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_gache_getShard(t *testing.T) {
	type fields struct {
		l              uint64
		shards         [255]*shard
		expire         *atomic.Value
		expFuncEnabled bool
		expFunc        func(context.Context, string)
		expChan        chan string
		expGroup       singleflight.Group
	}
	type args struct {
		key string
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
				l:              tt.fields.l,
				shards:         tt.fields.shards,
				expire:         tt.fields.expire,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expFunc:        tt.fields.expFunc,
				expChan:        tt.fields.expChan,
				expGroup:       tt.fields.expGroup,
			}
			if got := g.getShard(tt.args.key); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("gache.getShard() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_gache_Clear(t *testing.T) {
	type fields struct {
		l              uint64
		shards         [255]*shard
		expire         *atomic.Value
		expFuncEnabled bool
		expFunc        func(context.Context, string)
		expChan        chan string
		expGroup       singleflight.Group
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
				l:              tt.fields.l,
				shards:         tt.fields.shards,
				expire:         tt.fields.expire,
				expFuncEnabled: tt.fields.expFuncEnabled,
				expFunc:        tt.fields.expFunc,
				expChan:        tt.fields.expChan,
				expGroup:       tt.fields.expGroup,
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
