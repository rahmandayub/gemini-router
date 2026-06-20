package key

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

const DefaultCooldownSeconds = 60

type Pool struct {
	keys          []string
	counter       atomic.Uint64
	mu            sync.RWMutex
	cooldowns     map[string]time.Time
	cooldownDur   time.Duration
	cleanupTicker *time.Ticker
	stopCleanup   chan struct{}
	stopOnce      sync.Once
}

func NewPool(keys []string, cooldownDur time.Duration) *Pool {
	p := &Pool{
		keys:        keys,
		cooldowns:   make(map[string]time.Time),
		cooldownDur: cooldownDur,
		stopCleanup: make(chan struct{}),
	}
	if cooldownDur > 0 {
		p.cleanupTicker = time.NewTicker(30 * time.Second)
		go p.cleanupLoop()
	}
	return p
}

func (p *Pool) Stop() {
	p.stopOnce.Do(func() {
		close(p.stopCleanup)
	})
}

func (p *Pool) cleanupLoop() {
	for {
		select {
		case <-p.cleanupTicker.C:
			p.mu.Lock()
			now := time.Now()
			for k, expiry := range p.cooldowns {
				if now.After(expiry) {
					delete(p.cooldowns, k)
				}
			}
			p.mu.Unlock()
		case <-p.stopCleanup:
			p.cleanupTicker.Stop()
			return
		}
	}
}

func (p *Pool) Next(ctx context.Context) string {
	if len(p.keys) == 0 {
		panic("key pool is empty")
	}

	p.mu.RLock()
	now := time.Now()
	var earliestExpiry time.Time
	allCooling := true

	for _, k := range p.keys {
		expiry, ok := p.cooldowns[k]
		if !ok || now.After(expiry) {
			allCooling = false
			break
		}
		if earliestExpiry.IsZero() || expiry.Before(earliestExpiry) {
			earliestExpiry = expiry
		}
	}

	if !allCooling {
		key := p.nextAvailableLocked()
		p.mu.RUnlock()
		return key
	}
	p.mu.RUnlock()

	waitDur := earliestExpiry.Sub(now)
	if waitDur > 5*time.Second {
		waitDur = 5 * time.Second
	}

	log.Printf("[key/pool] All %d API keys in cooldown, waiting %v", len(p.keys), waitDur)

	timer := time.NewTimer(waitDur)
	select {
	case <-timer.C:
	case <-ctx.Done():
		timer.Stop()
	}

	key := p.nextAvailable()
	p.mu.RLock()
	expiry, cooling := p.cooldowns[key]
	stillCooling := cooling && time.Now().Before(expiry)
	activeCooldowns := p.cooldownCountLocked()
	p.mu.RUnlock()

	if stillCooling {
		log.Printf("[key/pool] All keys still in cooldown after wait, proceeding with best-effort (%d cooldowns active)", activeCooldowns)
	}
	return key
}

func (p *Pool) nextAvailableLocked() string {
	now := time.Now()
	idx := int(p.counter.Add(1) - 1)
	for i := 0; i < len(p.keys); i++ {
		candidate := p.keys[(idx+i)%len(p.keys)]
		expiry, ok := p.cooldowns[candidate]
		if !ok || now.After(expiry) {
			return candidate
		}
	}
	return p.keys[idx%len(p.keys)]
}

func (p *Pool) nextAvailable() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.nextAvailableLocked()
}

func (p *Pool) MarkCooldown(apiKey string, dur time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if dur == 0 {
		dur = p.cooldownDur
	}
	p.cooldowns[apiKey] = time.Now().Add(dur)
}

func (p *Pool) DefaultCooldown() time.Duration {
	return p.cooldownDur
}

func (p *Pool) KeyStatus() (total, available, cooling int) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	now := time.Now()
	total = len(p.keys)
	for _, k := range p.keys {
		expiry, ok := p.cooldowns[k]
		if !ok || now.After(expiry) {
			available++
		} else {
			cooling++
		}
	}
	return total, available, cooling
}

func (p *Pool) cooldownCountLocked() int {
	now := time.Now()
	count := 0
	for _, k := range p.keys {
		expiry, ok := p.cooldowns[k]
		if ok && !now.After(expiry) {
			count++
		}
	}
	return count
}

func (p *Pool) Len() int { return len(p.keys) }
