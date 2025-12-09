package main

import (
	"container/heap"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// --- CONSTANTS ---
const (
	KeepAliveDuration = 5 * time.Minute
	BlockDuration     = 60 * time.Second
)

// --- TYPES & ENUMS ---

// EventType distinguishes between the 5-min death and the 60s ghost release
type EventType int

const (
	EventHardExpiry EventType = iota
	EventAutoUnblock
)

// TimerEvent is an item in the Priority Queue
type TimerEvent struct {
	TriggerTime time.Time
	KeyID       string
	Type        EventType
	Index       int // required by container/heap
}

// KeyMetadata holds the state of a single API key
type KeyMetadata struct {
	ID            string
	IsBlocked     bool
	LastKeepAlive time.Time
	BlockedAt     time.Time
}

// --- PRIORITY QUEUE (Min-Heap) implementation for O(log N) scheduling ---
type EventHeap []*TimerEvent

func (h EventHeap) Len() int           { return len(h) }
func (h EventHeap) Less(i, j int) bool { return h[i].TriggerTime.Before(h[j].TriggerTime) }
func (h EventHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i]; h[i].Index = i; h[j].Index = j }

func (h *EventHeap) Push(x interface{}) {
	n := len(*h)
	item := x.(*TimerEvent)
	item.Index = n
	*h = append(*h, item)
}

func (h *EventHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	item.Index = -1
	*h = old[0 : n-1]
	return item
}

// --- CORE SERVER LOGIC ---

type KeyServer struct {
	mu sync.Mutex

	// 1. Master Storage: O(1) access
	keys map[string]*KeyMetadata

	// 2. Random Selection Pool: O(1) random pick
	availableKeys []string            // Slice for random access
	availableIdx  map[string]int      // Map KeyID -> Slice Index (for O(1) removal)

	// 3. Time Event Scheduler: O(log N)
	events EventHeap
}

func NewKeyServer() *KeyServer {
	ks := &KeyServer{
		keys:          make(map[string]*KeyMetadata),
		availableKeys: make([]string, 0),
		availableIdx:  make(map[string]int),
		events:        make(EventHeap, 0),
	}
	heap.Init(&ks.events)
	
	// Start the background cleanup goroutine
	go ks.backgroundWorker()
	
	return ks
}

// Helper: Generates a random secure string
func generateRandomKey() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Helper: Adds a key to the Available pool
func (ks *KeyServer) addToAvailable(keyID string) {
	if _, exists := ks.availableIdx[keyID]; exists {
		return // Already available
	}
	ks.availableKeys = append(ks.availableKeys, keyID)
	ks.availableIdx[keyID] = len(ks.availableKeys) - 1
}

// Helper: Removes a key from the Available pool in O(1) using Swap-and-Pop
func (ks *KeyServer) removeFromAvailable(keyID string) {
	idx, exists := ks.availableIdx[keyID]
	if !exists {
		return
	}

	lastIdx := len(ks.availableKeys) - 1
	lastKey := ks.availableKeys[lastIdx]

	// Swap current with last
	ks.availableKeys[idx] = lastKey
	ks.availableIdx[lastKey] = idx

	// Remove last
	ks.availableKeys = ks.availableKeys[:lastIdx]
	delete(ks.availableIdx, keyID)
}

// E1: Generate Key
func (ks *KeyServer) GenerateKey() string {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	id := generateRandomKey()
	now := time.Now()

	// Store Metadata
	ks.keys[id] = &KeyMetadata{
		ID:            id,
		IsBlocked:     false,
		LastKeepAlive: now,
	}

	// Make Available
	ks.addToAvailable(id)

	// Schedule Expiry
	heap.Push(&ks.events, &TimerEvent{
		TriggerTime: now.Add(KeepAliveDuration),
		KeyID:       id,
		Type:        EventHardExpiry,
	})

	return id
}

// E2: Get Available Key
func (ks *KeyServer) GetAvailableKey() (string, bool) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if len(ks.availableKeys) == 0 {
		return "", false
	}

	// Pick random index using crypto/rand for better distribution
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(ks.availableKeys))))
	randIdx := int(n.Int64())
	
	keyID := ks.availableKeys[randIdx]
	meta := ks.keys[keyID]

	// Update State
	meta.IsBlocked = true
	meta.BlockedAt = time.Now()

	// Remove from available pool
	ks.removeFromAvailable(keyID)

	// Schedule Auto-Unblock (Ghost Rule)
	heap.Push(&ks.events, &TimerEvent{
		TriggerTime: time.Now().Add(BlockDuration),
		KeyID:       keyID,
		Type:        EventAutoUnblock,
	})

	return keyID, true
}

// E3: Unblock Key
func (ks *KeyServer) UnblockKey(id string) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	meta, exists := ks.keys[id]
	if !exists || !meta.IsBlocked {
		return
	}

	meta.IsBlocked = false
	ks.addToAvailable(id)

	// Reset KeepAlive on unblock as per requirement
	now := time.Now()
	meta.LastKeepAlive = now
	heap.Push(&ks.events, &TimerEvent{
		TriggerTime: now.Add(KeepAliveDuration),
		KeyID:       id,
		Type:        EventHardExpiry,
	})
}

// E4: Delete Key
func (ks *KeyServer) DeleteKey(id string) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if _, exists := ks.keys[id]; exists {
		delete(ks.keys, id)
		ks.removeFromAvailable(id)
	}
}

// E5: Keep Alive
// CHANGE THIS FUNCTION
func (ks *KeyServer) KeepAlive(id string) bool { // <-- Change return type to bool
	ks.mu.Lock()
	defer ks.mu.Unlock()

	meta, exists := ks.keys[id]
	if !exists {
		return false // <-- Return false if key is missing
	}

	// Update timestamp
	meta.LastKeepAlive = time.Now()

	// Push new expiry event
	heap.Push(&ks.events, &TimerEvent{
		TriggerTime: meta.LastKeepAlive.Add(KeepAliveDuration),
		KeyID:       id,
		Type:        EventHardExpiry,
	})
	
	return true // <-- Return true if successful
}

// Background Worker: Processes Timeouts
func (ks *KeyServer) backgroundWorker() {
	for {
		ks.mu.Lock()
		if len(ks.events) == 0 {
			ks.mu.Unlock()
			time.Sleep(100 * time.Millisecond) // Nothing to do, brief sleep
			continue
		}

		nextEvent := ks.events[0]
		now := time.Now()

		if now.Before(nextEvent.TriggerTime) {
			// Not time yet. 
			// In production, we might use a condition variable here to sleep exactly needed amount,
			// but a short sleep loop is acceptable for this scope.
			ks.mu.Unlock()
			time.Sleep(50 * time.Millisecond)
			continue
		}

		// Pop the event
		heap.Pop(&ks.events)
		meta, exists := ks.keys[nextEvent.KeyID]

		// If key was manually deleted, ignore event
		if !exists {
			ks.mu.Unlock()
			continue
		}

		// Handle Event Logic with "Lazy" Checks
		if nextEvent.Type == EventHardExpiry {
			// Check if key was refreshed AFTER this event was created
			expiryLimit := meta.LastKeepAlive.Add(KeepAliveDuration)
			if expiryLimit.After(now.Add(100 * time.Millisecond)) {
				// Key is still alive, this event is stale. Ignore.
			} else {
				// Truly expired
				log.Printf("Purging expired key: %s\n", nextEvent.KeyID)
				delete(ks.keys, nextEvent.KeyID)
				ks.removeFromAvailable(nextEvent.KeyID)
			}
		} else if nextEvent.Type == EventAutoUnblock {
			// Check if key is still blocked and timeout matches
			blockLimit := meta.BlockedAt.Add(BlockDuration)
			// Using a small buffer for time comparison
			if meta.IsBlocked && blockLimit.Before(now.Add(100*time.Millisecond)) {
				log.Printf("Ghost Rule: Auto-unblocking %s\n", nextEvent.KeyID)
				meta.IsBlocked = false
				ks.addToAvailable(nextEvent.KeyID)
				
				// Reset lifecycle
				meta.LastKeepAlive = now
				heap.Push(&ks.events, &TimerEvent{
					TriggerTime: now.Add(KeepAliveDuration),
					KeyID:       nextEvent.KeyID,
					Type:        EventHardExpiry,
				})
			}
		}

		ks.mu.Unlock()
	}
}

// --- HTTP HANDLERS ---

var server = NewKeyServer()

func handleGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	key := server.GenerateKey()
	json.NewEncoder(w).Encode(map[string]string{"key": key})
}

func handleGetAvailable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	key, found := server.GetAvailableKey()
	if !found {
		http.Error(w, "No keys available", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"key": key})
}

func handleUnblock(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("id")
	if key == "" {
		http.Error(w, "Missing 'id' param", http.StatusBadRequest)
		return
	}
	server.UnblockKey(key)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Unblocked"))
}

func handleDelete(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("id")
	server.DeleteKey(key)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Deleted"))
}

// CHANGE THIS HANDLER
func handleKeepAlive(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("id")
	
	// Capture the result (true/false)
	success := server.KeepAlive(key) 

	if !success {
		// If false, send a 404 Error
		http.Error(w, "Key not found", http.StatusNotFound) 
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("KeepAlive Received"))
}

func main() {
	http.HandleFunc("/generate", handleGenerate)
	http.HandleFunc("/get", handleGetAvailable)
	http.HandleFunc("/unblock", handleUnblock)
	http.HandleFunc("/delete", handleDelete)
	http.HandleFunc("/keepalive", handleKeepAlive)

	fmt.Println("Server starting on :8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}