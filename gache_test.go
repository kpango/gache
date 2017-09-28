package gache

import (
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"
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
			Set(k, v)
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

func TestNew(t *testing.T) {
	t.Run("New Instantiate", func(t *testing.T) {
		got := New()
		if got == nil {
			t.Error("New() is nil")
		}
		if got.mu == nil {
			t.Error("New().mu is nil")
		}
		if got.data == nil {
			t.Error("New().mu is nil")
		}
		if got.expire != time.Second*30 {
			t.Errorf("New().expire = %v, want %v", got.expire, time.Second*30)
		}
	})
}

func TestGetGache(t *testing.T) {
	t.Run("Get singleton instance", func(t *testing.T) {
		got := GetGache()
		if got == nil {
			t.Error("GetGache() is nil")
		}
		if got.mu == nil {
			t.Error("GetGache().mu is nil")
		}
		if got.data == nil {
			t.Error("GetGache().mu is nil")
		}
		if got.expire != time.Second*30 {
			t.Errorf("GetGache().expire = %v, want %v", got.expire, time.Second*30)
		}
	})

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
			v := value{
				expire: tt.fields.expire,
				val:    tt.fields.val,
			}
			if got := v.isValid(); got != tt.want {
				t.Errorf("value.isValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGache_SetDefaultexpire(t *testing.T) {
	type fields struct {
		mu     *sync.RWMutex
		data   *sync.Map
		expire time.Duration
	}
	type args struct {
		ex time.Duration
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *Gache
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &Gache{
				mu:     tt.fields.mu,
				data:   tt.fields.data,
				expire: tt.fields.expire,
			}
			if got := g.SetDefaultExpire(tt.args.ex); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Gache.SetDefaultexpire() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetDefaultexpire(t *testing.T) {
	type args struct {
		ex time.Duration
	}
	tests := []struct {
		name string
		args args
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetDefaultExpire(tt.args.ex)
		})
	}
}

func TestGet(t *testing.T) {
	type args struct {
		key interface{}
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

func TestGache_Get(t *testing.T) {
	type fields struct {
		mu     *sync.RWMutex
		data   *sync.Map
		expire time.Duration
	}
	type args struct {
		key interface{}
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
			g := &Gache{
				mu:     tt.fields.mu,
				data:   tt.fields.data,
				expire: tt.fields.expire,
			}
			got, got1 := g.Get(tt.args.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Gache.Get() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("Gache.Get() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestGache_get(t *testing.T) {
	type fields struct {
		mu     *sync.RWMutex
		data   *sync.Map
		expire time.Duration
	}
	type args struct {
		key interface{}
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
			g := &Gache{
				mu:     tt.fields.mu,
				data:   tt.fields.data,
				expire: tt.fields.expire,
			}
			got, got1 := g.get(tt.args.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Gache.get() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("Gache.get() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestSetWithExpire(t *testing.T) {
	type args struct {
		key    interface{}
		val    interface{}
		expire time.Duration
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetWithExpire(tt.args.key, tt.args.val, tt.args.expire)
		})
	}
}

func TestSet(t *testing.T) {
	type args struct {
		key interface{}
		val interface{}
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Set(tt.args.key, tt.args.val)
		})
	}
}

func TestGache_SetWithExpire(t *testing.T) {
	type fields struct {
		mu     *sync.RWMutex
		data   *sync.Map
		expire time.Duration
	}
	type args struct {
		key    interface{}
		val    interface{}
		expire time.Duration
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
			g := &Gache{
				mu:     tt.fields.mu,
				data:   tt.fields.data,
				expire: tt.fields.expire,
			}
			g.SetWithExpire(tt.args.key, tt.args.val, tt.args.expire)
		})
	}
}

func TestGache_Set(t *testing.T) {
	type fields struct {
		mu     *sync.RWMutex
		data   *sync.Map
		expire time.Duration
	}
	type args struct {
		key interface{}
		val interface{}
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
			g := &Gache{
				mu:     tt.fields.mu,
				data:   tt.fields.data,
				expire: tt.fields.expire,
			}
			g.Set(tt.args.key, tt.args.val)
		})
	}
}

func TestGache_set(t *testing.T) {
	type fields struct {
		mu     *sync.RWMutex
		data   *sync.Map
		expire time.Duration
	}
	type args struct {
		key    interface{}
		val    interface{}
		expire time.Duration
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
			g := &Gache{
				mu:     tt.fields.mu,
				data:   tt.fields.data,
				expire: tt.fields.expire,
			}
			g.set(tt.args.key, tt.args.val, tt.args.expire)
		})
	}
}

func TestGache_DeleteExpired(t *testing.T) {
	type fields struct {
		mu     *sync.RWMutex
		data   *sync.Map
		expire time.Duration
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
			g := &Gache{
				mu:     tt.fields.mu,
				data:   tt.fields.data,
				expire: tt.fields.expire,
			}
			if got := g.DeleteExpired(); got != tt.want {
				t.Errorf("Gache.DeleteExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGache_Delete(t *testing.T) {
	type fields struct {
		mu     *sync.RWMutex
		data   *sync.Map
		expire time.Duration
	}
	type args struct {
		key interface{}
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
			g := &Gache{
				mu:     tt.fields.mu,
				data:   tt.fields.data,
				expire: tt.fields.expire,
			}
			if got := g.Delete(tt.args.key); got != tt.want {
				t.Errorf("Gache.Delete() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGache_SGet(t *testing.T) {
	type fields struct {
		mu     *sync.RWMutex
		data   *sync.Map
		expire time.Duration
	}
	type args struct {
		key *http.Request
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *ServerCache
		want1  bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &Gache{
				mu:     tt.fields.mu,
				data:   tt.fields.data,
				expire: tt.fields.expire,
			}
			got, got1 := g.SGet(tt.args.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Gache.SGet() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("Gache.SGet() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestGache_SSetWithExpire(t *testing.T) {
	type fields struct {
		mu     *sync.RWMutex
		data   *sync.Map
		expire time.Duration
	}
	type args struct {
		key    *http.Request
		status int
		header http.Header
		body   []byte
		expire time.Duration
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
			g := &Gache{
				mu:     tt.fields.mu,
				data:   tt.fields.data,
				expire: tt.fields.expire,
			}
			if err := g.SSetWithExpire(tt.args.key, tt.args.status, tt.args.header, tt.args.body, tt.args.expire); (err != nil) != tt.wantErr {
				t.Errorf("Gache.SSetWithExpire() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGache_SSet(t *testing.T) {
	type fields struct {
		mu     *sync.RWMutex
		data   *sync.Map
		expire time.Duration
	}
	type args struct {
		key    *http.Request
		status int
		header http.Header
		body   []byte
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
			g := &Gache{
				mu:     tt.fields.mu,
				data:   tt.fields.data,
				expire: tt.fields.expire,
			}
			if err := g.SSet(tt.args.key, tt.args.status, tt.args.header, tt.args.body); (err != nil) != tt.wantErr {
				t.Errorf("Gache.SSet() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGache_CGet(t *testing.T) {
	type fields struct {
		mu     *sync.RWMutex
		data   *sync.Map
		expire time.Duration
	}
	type args struct {
		key *http.Request
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *ClientCache
		want1  bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &Gache{
				mu:     tt.fields.mu,
				data:   tt.fields.data,
				expire: tt.fields.expire,
			}
			got, got1 := g.CGet(tt.args.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Gache.CGet() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("Gache.CGet() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestGache_CSet(t *testing.T) {
	type fields struct {
		mu     *sync.RWMutex
		data   *sync.Map
		expire time.Duration
	}
	type args struct {
		key *http.Request
		val *http.Response
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
			g := &Gache{
				mu:     tt.fields.mu,
				data:   tt.fields.data,
				expire: tt.fields.expire,
			}
			if err := g.CSet(tt.args.key, tt.args.val); (err != nil) != tt.wantErr {
				t.Errorf("Gache.CSet() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSGet(t *testing.T) {
	type args struct {
		key *http.Request
	}
	tests := []struct {
		name  string
		args  args
		want  *ServerCache
		want1 bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := SGet(tt.args.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SGet() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("SGet() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestSSetWithExpire(t *testing.T) {
	type args struct {
		key    *http.Request
		status int
		header http.Header
		body   []byte
		expire time.Duration
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
			if err := SSetWithExpire(tt.args.key, tt.args.status, tt.args.header, tt.args.body, tt.args.expire); (err != nil) != tt.wantErr {
				t.Errorf("SSetWithExpire() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSSet(t *testing.T) {
	type args struct {
		key    *http.Request
		status int
		header http.Header
		body   []byte
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
			if err := SSet(tt.args.key, tt.args.status, tt.args.header, tt.args.body); (err != nil) != tt.wantErr {
				t.Errorf("SSet() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCGet(t *testing.T) {
	type args struct {
		key *http.Request
	}
	tests := []struct {
		name  string
		args  args
		want  *ClientCache
		want1 bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := CGet(tt.args.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CGet() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("CGet() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestCSet(t *testing.T) {
	type args struct {
		key *http.Request
		val *http.Response
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
			if err := CSet(tt.args.key, tt.args.val); (err != nil) != tt.wantErr {
				t.Errorf("CSet() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGache_getServerCache(t *testing.T) {
	type fields struct {
		mu     *sync.RWMutex
		data   *sync.Map
		expire time.Duration
	}
	type args struct {
		req *http.Request
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *ServerCache
		want1  bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &Gache{
				mu:     tt.fields.mu,
				data:   tt.fields.data,
				expire: tt.fields.expire,
			}
			got, got1 := g.getServerCache(tt.args.req)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Gache.getServerCache() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("Gache.getServerCache() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestGache_setServerCache(t *testing.T) {
	type fields struct {
		mu     *sync.RWMutex
		data   *sync.Map
		expire time.Duration
	}
	type args struct {
		req    *http.Request
		status int
		header http.Header
		body   []byte
		expire time.Duration
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
			g := &Gache{
				mu:     tt.fields.mu,
				data:   tt.fields.data,
				expire: tt.fields.expire,
			}
			if err := g.setServerCache(tt.args.req, tt.args.status, tt.args.header, tt.args.body, tt.args.expire); (err != nil) != tt.wantErr {
				t.Errorf("Gache.setServerCache() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGache_getClientCache(t *testing.T) {
	type fields struct {
		mu     *sync.RWMutex
		data   *sync.Map
		expire time.Duration
	}
	type args struct {
		req *http.Request
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *ClientCache
		want1  bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &Gache{
				mu:     tt.fields.mu,
				data:   tt.fields.data,
				expire: tt.fields.expire,
			}
			got, got1 := g.getClientCache(tt.args.req)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Gache.getClientCache() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("Gache.getClientCache() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestGache_setClientCache(t *testing.T) {
	type fields struct {
		mu     *sync.RWMutex
		data   *sync.Map
		expire time.Duration
	}
	type args struct {
		req *http.Request
		val *http.Response
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
			g := &Gache{
				mu:     tt.fields.mu,
				data:   tt.fields.data,
				expire: tt.fields.expire,
			}
			if err := g.setClientCache(tt.args.req, tt.args.val); (err != nil) != tt.wantErr {
				t.Errorf("Gache.setClientCache() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGache_Clear(t *testing.T) {
	type fields struct {
		mu     *sync.RWMutex
		data   *sync.Map
		expire time.Duration
	}
	tests := []struct {
		name   string
		fields fields
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &Gache{
				mu:     tt.fields.mu,
				data:   tt.fields.data,
				expire: tt.fields.expire,
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

func Test_generateHTTPKey(t *testing.T) {
	type args struct {
		r *http.Request
	}
	tests := []struct {
		name string
		args args
		want string
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := generateHTTPKey(tt.args.r); got != tt.want {
				t.Errorf("generateHTTPKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_createHTTPCache(t *testing.T) {
	type args struct {
		res *http.Response
	}
	tests := []struct {
		name    string
		args    args
		want    *ClientCache
		wantErr bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := createHTTPCache(tt.args.res)
			if (err != nil) != tt.wantErr {
				t.Errorf("createHTTPCache() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("createHTTPCache() = %v, want %v", got, tt.want)
			}
		})
	}
}
