package auth

import (
	"sync"
	"time"
)

const loginWindow = time.Minute
const loginPeerLimit = 5
const loginGlobalLimit = 30
const loginConcurrency = 2

type attempts struct {
	start time.Time
	count int
}

// The connection peer and whole Agent have separate admission budgets. Peer
// entries expire and are bounded by the global budget. Forwarded address
// headers are deliberately not used for this security decision.
type LoginLimiter struct {
	mu     sync.Mutex
	now    func() time.Time
	peers  map[string]attempts
	global attempts
	active int
}

func (l *LoginLimiter) Begin(peer string) (release func(), retrySeconds int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.now != nil {
		now = l.now()
	}
	if l.peers == nil {
		l.peers = make(map[string]attempts)
	}
	for key, entry := range l.peers {
		if now.Sub(entry.start) >= loginWindow {
			delete(l.peers, key)
		}
	}
	if l.global.start.IsZero() || now.Sub(l.global.start) >= loginWindow {
		l.global = attempts{start: now}
	}
	entry, exists := l.peers[peer]
	if !exists {
		entry = attempts{start: now}
	}
	remaining := func(start time.Time) int {
		seconds := int(loginWindow.Seconds()-now.Sub(start).Seconds()) + 1
		if seconds < 1 {
			return 1
		}
		return seconds
	}
	if entry.count >= loginPeerLimit {
		return nil, remaining(entry.start)
	}
	if l.global.count >= loginGlobalLimit {
		return nil, remaining(l.global.start)
	}
	if l.active >= loginConcurrency {
		return nil, 1
	}
	entry.count++
	l.peers[peer] = entry
	l.global.count++
	l.active++
	var once sync.Once
	return func() { once.Do(func() { l.mu.Lock(); l.active--; l.mu.Unlock() }) }, 0
}
