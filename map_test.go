package gache

import (
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"
)

func Test_newEntryMap(t *testing.T) {
	type args struct {
		i value
	}
	tests := []struct {
		name string
		args args
		want *entryMap
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newEntryMap(tt.args.i); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("newEntryMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMap_Load(t *testing.T) {
	type fields struct {
		mu     sync.Mutex
		read   atomic.Value
		dirty  map[string]*entryMap
		misses int
	}
	type args struct {
		key string
	}
	tests := []struct {
		name      string
		fields    fields
		args      args
		wantValue value
		wantOk    bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Map{
				mu:     tt.fields.mu,
				read:   tt.fields.read,
				dirty:  tt.fields.dirty,
				misses: tt.fields.misses,
			}
			gotValue, gotOk := m.Load(tt.args.key)
			if !reflect.DeepEqual(gotValue, tt.wantValue) {
				t.Errorf("Map.Load() gotValue = %v, want %v", gotValue, tt.wantValue)
			}
			if gotOk != tt.wantOk {
				t.Errorf("Map.Load() gotOk = %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}

func Test_entryMap_load(t *testing.T) {
	type fields struct {
		p unsafe.Pointer
	}
	tests := []struct {
		name    string
		fields  fields
		wantVal value
		wantOk  bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &entryMap{
				p: tt.fields.p,
			}
			gotVal, gotOk := e.load()
			if !reflect.DeepEqual(gotVal, tt.wantVal) {
				t.Errorf("entryMap.load() gotVal = %v, want %v", gotVal, tt.wantVal)
			}
			if gotOk != tt.wantOk {
				t.Errorf("entryMap.load() gotOk = %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}

func TestMap_Store(t *testing.T) {
	type fields struct {
		mu     sync.Mutex
		read   atomic.Value
		dirty  map[string]*entryMap
		misses int
	}
	type args struct {
		key string
		val value
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
			m := &Map{
				mu:     tt.fields.mu,
				read:   tt.fields.read,
				dirty:  tt.fields.dirty,
				misses: tt.fields.misses,
			}
			m.Store(tt.args.key, tt.args.val)
		})
	}
}

func Test_entryMap_tryStore(t *testing.T) {
	type fields struct {
		p unsafe.Pointer
	}
	type args struct {
		i *value
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &entryMap{
				p: tt.fields.p,
			}
			if got := e.tryStore(tt.args.i); got != tt.want {
				t.Errorf("entryMap.tryStore() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_entryMap_unexpungeLocked(t *testing.T) {
	type fields struct {
		p unsafe.Pointer
	}
	tests := []struct {
		name            string
		fields          fields
		wantWasExpunged bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &entryMap{
				p: tt.fields.p,
			}
			if gotWasExpunged := e.unexpungeLocked(); gotWasExpunged != tt.wantWasExpunged {
				t.Errorf("entryMap.unexpungeLocked() = %v, want %v", gotWasExpunged, tt.wantWasExpunged)
			}
		})
	}
}

func Test_entryMap_storeLocked(t *testing.T) {
	type fields struct {
		p unsafe.Pointer
	}
	type args struct {
		i *value
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
			e := &entryMap{
				p: tt.fields.p,
			}
			e.storeLocked(tt.args.i)
		})
	}
}

func TestMap_Delete(t *testing.T) {
	type fields struct {
		mu     sync.Mutex
		read   atomic.Value
		dirty  map[string]*entryMap
		misses int
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
			m := &Map{
				mu:     tt.fields.mu,
				read:   tt.fields.read,
				dirty:  tt.fields.dirty,
				misses: tt.fields.misses,
			}
			m.Delete(tt.args.key)
		})
	}
}

func Test_entryMap_delete(t *testing.T) {
	type fields struct {
		p unsafe.Pointer
	}
	tests := []struct {
		name         string
		fields       fields
		wantHadValue bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &entryMap{
				p: tt.fields.p,
			}
			if gotHadValue := e.delete(); gotHadValue != tt.wantHadValue {
				t.Errorf("entryMap.delete() = %v, want %v", gotHadValue, tt.wantHadValue)
			}
		})
	}
}

func TestMap_Range(t *testing.T) {
	type fields struct {
		mu     sync.Mutex
		read   atomic.Value
		dirty  map[string]*entryMap
		misses int
	}
	type args struct {
		f func(key string, value value) bool
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
			m := &Map{
				mu:     tt.fields.mu,
				read:   tt.fields.read,
				dirty:  tt.fields.dirty,
				misses: tt.fields.misses,
			}
			m.Range(tt.args.f)
		})
	}
}

func TestMap_missLocked(t *testing.T) {
	type fields struct {
		mu     sync.Mutex
		read   atomic.Value
		dirty  map[string]*entryMap
		misses int
	}
	tests := []struct {
		name   string
		fields fields
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Map{
				mu:     tt.fields.mu,
				read:   tt.fields.read,
				dirty:  tt.fields.dirty,
				misses: tt.fields.misses,
			}
			m.missLocked()
		})
	}
}

func TestMap_dirtyLocked(t *testing.T) {
	type fields struct {
		mu     sync.Mutex
		read   atomic.Value
		dirty  map[string]*entryMap
		misses int
	}
	tests := []struct {
		name   string
		fields fields
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Map{
				mu:     tt.fields.mu,
				read:   tt.fields.read,
				dirty:  tt.fields.dirty,
				misses: tt.fields.misses,
			}
			m.dirtyLocked()
		})
	}
}

func Test_entryMap_tryExpungeLocked(t *testing.T) {
	type fields struct {
		p unsafe.Pointer
	}
	tests := []struct {
		name           string
		fields         fields
		wantIsExpunged bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &entryMap{
				p: tt.fields.p,
			}
			if gotIsExpunged := e.tryExpungeLocked(); gotIsExpunged != tt.wantIsExpunged {
				t.Errorf("entryMap.tryExpungeLocked() = %v, want %v", gotIsExpunged, tt.wantIsExpunged)
			}
		})
	}
}
