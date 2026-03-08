package gache

import (
	"runtime"
	"sync/atomic"
)

// simple MPMC lock-free ring buffer
type ringBuffer[T any] struct {
	head  atomic.Uint64
	tail  atomic.Uint64
	mask  uint64
	nodes []node[T]
}

type node[T any] struct {
	val T
	seq atomic.Uint64
}

func newRingBuffer[T any](size uint64) *ringBuffer[T] {
	// ensure size is power of 2
	var s uint64 = 1
	for s < size {
		s <<= 1
	}
	rb := &ringBuffer[T]{
		mask:  s - 1,
		nodes: make([]node[T], s),
	}
	for i := uint64(0); i < s; i++ {
		rb.nodes[i].seq.Store(i)
	}
	return rb
}

func (rb *ringBuffer[T]) Push(item T) bool {
	var head, seq uint64
	for {
		head = rb.head.Load()
		node := &rb.nodes[head&rb.mask]
		seq = node.seq.Load()
		dif := int64(seq) - int64(head)
		if dif == 0 {
			if rb.head.CompareAndSwap(head, head+1) {
				node.val = item
				node.seq.Store(head + 1)
				return true
			}
		} else if dif < 0 {
			return false // full
		} else {
			runtime.Gosched()
		}
	}
}

func (rb *ringBuffer[T]) Pop() (T, bool) {
	var tail, seq uint64
	var val T
	for {
		tail = rb.tail.Load()
		node := &rb.nodes[tail&rb.mask]
		seq = node.seq.Load()
		dif := int64(seq) - int64(tail+1)
		if dif == 0 {
			if rb.tail.CompareAndSwap(tail, tail+1) {
				val = node.val
				var empty T
				node.val = empty
				node.seq.Store(tail + rb.mask + 1)
				return val, true
			}
		} else if dif < 0 {
			return val, false // empty
		} else {
			runtime.Gosched()
		}
	}
}
