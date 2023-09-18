package gache

import (
	"sync"

	"github.com/dolthub/swiss"
)

type Node[V any] struct {
	prefix   string
	children *swiss.Map[rune, *Node[V]]
	value    *V
	mu       sync.RWMutex
}

type Trie[V any] interface {
	Insert(key string, value *V) bool
	Get(key string) (*V, bool)
	Delete(key string) (*V, bool)
}

type trie[V any] struct {
	root  *Node[V]
	nSize uint32
}

func NewTrie[V any](size uint32) Trie[V] {
	return &trie[V]{
		root:  newNode[V](),
		nSize: size,
	}
}

func newNode[V any]() *Node[V] {
	return &Node[V]{}
}

func (n *Node[V]) getChild(ch rune) (node *Node[V], ok bool) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if n.children != nil {
		node, ok = n.children.Get(ch)
	}
	return node, ok && node != nil
}

func (n *Node[V]) setChild(ch rune, child *Node[V], size uint32) (node *Node[V], ok bool) {
	if n.children == nil {
		n.children = swiss.NewMap[rune, *Node[V]](size)
	}
	node, ok = n.children.Get(ch)
	if !ok && node == nil {
		n.children.Put(ch, child)
		node = child
		ok = true
	}
	return
}

func (n *Node[V]) matchPrefix(key string) (i int) {
	m := min(len(n.prefix), len(key))
	for i = 0; i < m; i++ {
		if n.prefix[i] != key[i] {
			return i
		}
	}
	return i
}

func (t *trie[V]) Insert(key string, value *V) bool {
	return t.traverse(key, true, func(node *Node[V]) (ok bool) {
		if node != nil {
			node.mu.Lock()
			node.value = value
			node.mu.Unlock()
		}
		return true
	})
}

func (t *trie[V]) Get(key string) (v *V, ok bool) {
	return v, t.traverse(key, false, func(node *Node[V]) (ok bool) {
		if node != nil {
			node.mu.RLock()
			v = node.value
			node.mu.RUnlock()
			ok = v != nil
		}
		return ok
	})
}

func (t *trie[V]) Delete(key string) (v *V, ok bool) {
	return v, t.traverse(key, false, func(node *Node[V]) (ok bool) {
		if node != nil {
			node.mu.Lock()
			v = node.value
			node.value = nil
			node.mu.Unlock()
			ok = v != nil
		}
		return ok
	})
}

func (t *trie[V]) traverse(key string, edit bool, fn func(node *Node[V]) bool) (ok bool) {
	var (
		node = t.root
		ch   rune
		next *Node[V]
	)
	for len(key) > 0 && node != nil {
		node.mu.RLock()
		if node.prefix == key {
			node.mu.RUnlock()
			return fn(node)
		}
		pLen := node.matchPrefix(key)
		if edit && pLen < len(node.prefix) {
			next = &Node[V]{prefix: node.prefix[pLen:], children: node.children, value: node.value}
			ch = rune(node.prefix[pLen])
			node.mu.RUnlock()
			node.mu.Lock()
			node.value = nil
			node.prefix = node.prefix[:pLen]
			node.children = nil
			_, _ = node.setChild(ch, next, t.nSize)
			node.mu.Unlock()
		} else {
			node.mu.RUnlock()
		}

		if pLen < len(key) {
			ch = rune(key[pLen])
			next, ok = node.getChild(ch)
			if !ok {
				if !edit {
					return fn(node)
				}
				next = &Node[V]{prefix: key[pLen:]}
				node.mu.Lock()
				next, ok = node.setChild(ch, next, t.nSize)
				node.mu.Unlock()
				if !ok || next == nil {
					return fn(node)
				}
			}
			node = next
			key = key[pLen:]
		} else if !edit {
			return fn(node)
		}
	}
	return fn(node)
}
