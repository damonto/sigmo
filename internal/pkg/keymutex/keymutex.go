package keymutex

import (
	"context"
	"sync"
)

// KeyMutex provides mutex locking mechanism based on a key.
// This is useful when you need to ensure that operations with the same key
// cannot run concurrently, while operations with different keys can.
type KeyMutex struct {
	maps sync.Map // map[any]*keyLock
}

type keyLock struct {
	token chan struct{}
}

func newKeyLock() *keyLock {
	lock := &keyLock{token: make(chan struct{}, 1)}
	lock.token <- struct{}{}
	return lock
}

// New creates a new KeyMutex instance.
func New() *KeyMutex {
	return &KeyMutex{}
}

// Lock acquires the lock for the given key.
// If a lock for this key already exists, it will wait until it's released.
// The lock should be released by calling Unlock with the same key.
func (km *KeyMutex) Lock(key any) {
	_ = km.LockContext(context.Background(), key)
}

// LockContext acquires the lock or returns when ctx is canceled.
func (km *KeyMutex) LockContext(ctx context.Context, key any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	value, _ := km.maps.LoadOrStore(key, newKeyLock())
	lock := value.(*keyLock)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-lock.token:
		if err := ctx.Err(); err != nil {
			lock.token <- struct{}{}
			return err
		}
		return nil
	}
}

// Unlock releases the lock for the given key.
// The key must be the same as the one used for Lock.
func (km *KeyMutex) Unlock(key any) {
	value, ok := km.maps.Load(key)
	if !ok {
		panic("KeyMutex.Unlock: unlock of unlocked key")
	}
	lock := value.(*keyLock)
	select {
	case lock.token <- struct{}{}:
	default:
		panic("KeyMutex.Unlock: unlock of unlocked key")
	}
}
