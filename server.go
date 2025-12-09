package main

import (
	"container/heap"
	"crypto/rand"
	"math/big"
	"sync"
	"time"
)

type KeyServer struct {
	mu            sync.Mutex
	keys          map[string]*KeyMetadata
	availableKeys []string
	availableIdx  map[string]int
	events        EventHeap
}

func NewKeyServer() *KeyServer {
	ks := &KeyServer{
		keys:          make(map[string]*KeyMetadata),
		availableKeys: make([]string, 0),
		availableIdx:  make(map[string]int),
		events:        make(EventHeap, 0),
	}
	heap.Init(&ks.events)
	go ks.backgroundWorker()
	return ks
}

// Core API logic
func (ks *KeyServer) GenerateKey() string {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	id := generateRandomKey()
	now := time.Now()

	ks.keys[id] = &KeyMetadata{
		ID:            id,
		IsBlocked:     false,
		LastKeepAlive: now,
	}

	ks.addToAvailable(id)

	heap.Push(&ks.events, &TimerEvent{
		TriggerTime: now.Add(KeepAliveDuration),
		KeyID:       id,
		Type:        EventHardExpiry,
	})

	return id
}

func (ks *KeyServer) GetAvailableKey() (string, bool) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if len(ks.availableKeys) == 0 {
		return "", false
	}

	n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(ks.availableKeys))))
	keyID := ks.availableKeys[int(n.Int64())]

	meta := ks.keys[keyID]
	meta.IsBlocked = true
	meta.BlockedAt = time.Now()

	ks.removeFromAvailable(keyID)

	heap.Push(&ks.events, &TimerEvent{
		TriggerTime: time.Now().Add(BlockDuration),
		KeyID:       keyID,
		Type:        EventAutoUnblock,
	})

	return keyID, true
}

func (ks *KeyServer) UnblockKey(id string) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	meta, exists := ks.keys[id]
	if !exists || !meta.IsBlocked {
		return
	}

	meta.IsBlocked = false
	ks.addToAvailable(id)

	meta.LastKeepAlive = time.Now()
	heap.Push(&ks.events, &TimerEvent{
		TriggerTime: meta.LastKeepAlive.Add(KeepAliveDuration),
		KeyID:       id,
		Type:        EventHardExpiry,
	})
}

func (ks *KeyServer) DeleteKey(id string) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	delete(ks.keys, id)
	ks.removeFromAvailable(id)
}

func (ks *KeyServer) KeepAlive(id string) bool {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	meta, exists := ks.keys[id]
	if !exists {
		return false
	}

	meta.LastKeepAlive = time.Now()

	heap.Push(&ks.events, &TimerEvent{
		TriggerTime: meta.LastKeepAlive.Add(KeepAliveDuration),
		KeyID:       id,
		Type:        EventHardExpiry,
	})

	return true
}
