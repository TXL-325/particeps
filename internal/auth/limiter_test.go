package auth

import (
	"fmt"
	"testing"
	"time"
)

func TestLoginLimiterBoundsConcurrencyAndRecovers(t *testing.T) {
	now := time.Unix(100, 0)
	limiter := LoginLimiter{now: func() time.Time { return now }}
	first, _ := limiter.Begin("peer")
	second, _ := limiter.Begin("peer")
	if first == nil || second == nil {
		t.Fatal("initial attempts rejected")
	}
	if release, retry := limiter.Begin("other"); release != nil || retry < 1 {
		t.Fatal("bcrypt concurrency is unbounded")
	}
	first()
	first()
	second()
	for i := 2; i < loginPeerLimit; i++ {
		release, _ := limiter.Begin("peer")
		if release == nil {
			t.Fatal("attempt rejected too early")
		}
		release()
	}
	if release, retry := limiter.Begin("peer"); release != nil || retry < 1 {
		t.Fatal("peer limit not enforced")
	}
	now = now.Add(loginWindow)
	release, _ := limiter.Begin("peer")
	if release == nil {
		t.Fatal("peer did not recover after the window")
	}
	release()
}
func TestLoginLimiterBoundsManyPeersAndDropsExpiredEntries(t *testing.T) {
	now := time.Unix(100, 0)
	limiter := LoginLimiter{now: func() time.Time { return now }}
	for i := 0; i < loginGlobalLimit; i++ {
		release, _ := limiter.Begin(fmt.Sprint(i))
		if release == nil {
			t.Fatal("global budget rejected too early")
		}
		release()
	}
	for i := 0; i < 1000; i++ {
		if release, _ := limiter.Begin(fmt.Sprint(i + 1000)); release != nil {
			t.Fatal("global limit bypassed")
		}
	}
	if len(limiter.peers) != loginGlobalLimit {
		t.Fatal("rejected peers grew the limiter")
	}
	now = now.Add(loginWindow)
	release, _ := limiter.Begin("new")
	if release == nil {
		t.Fatal("global limiter did not recover")
	}
	release()
	if len(limiter.peers) != 1 {
		t.Fatal("old peers retained")
	}
}
