sed -i 's/\[slen\]\*Map\[string, \*value\[V\]\]/[slen]mapInterface[V]/g' gache.go

sed -i '/type (/a \
	mapInterface[V any] interface {\
		Load(key string) (*value[V], bool)\
		Store(key string, value *value[V])\
		LoadOrStore(key string, value *value[V]) (actual *value[V], loaded bool)\
		Swap(key string, value *value[V]) (previous *value[V], loaded bool)\
		LoadAndDelete(key string) (value *value[V], loaded bool)\
		Delete(key string)\
		CompareAndSwap(key string, old, new *value[V]) (swapped bool)\
		CompareAndDelete(key string, old *value[V]) (deleted bool)\
		Range(f func(key string, value *value[V]) bool)\
		Clear()\
		Len() int\
		Size() uintptr\
	}\
' gache.go

sed -i '/gache\[V any\] struct {/a \
		useLockMap     bool\
' gache.go

sed -i 's/func newMap\[V any\]() (m \*Map\[string, \*value\[V\]\]) {/func newMap\[V any\]() mapInterface\[V\] {/g' gache.go
sed -i 's/return new(Map\[string, \*value\[V\]\])/return new(Map\[string, \*value\[V\]\])/g' gache.go

# Modify gache.Clear to instantiate the right map type
sed -i 's/g.shards\[i\] = newMap\[V\]()/if g.useLockMap { g.shards[i] = new(MapLock[string, *value[V]]) } else { g.shards[i] = newMap[V]() }/g' gache.go
