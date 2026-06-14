package key

import (
	"sync/atomic"
)

type Pool struct {
	keys    []string
	counter atomic.Uint64
}

func NewPool(keys []string) *Pool {
	return &Pool{
		keys: keys,
	}
}

func (p *Pool) Next() string {
	idx := p.counter.Add(1) - 1
	return p.keys[idx%uint64(len(p.keys))]
}

func (p *Pool) Len() int {
	return len(p.keys)
}
