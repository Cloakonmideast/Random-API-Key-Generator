package main

import (
	"container/heap"
	"log"
	"time"
)

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
	old[n-1] = nil
	item.Index = -1
	*h = old[:n-1]
	return item
}

// Background worker stays here
func (ks *KeyServer) backgroundWorker() {
	for {
		ks.mu.Lock()

		if len(ks.events) == 0 {
			ks.mu.Unlock()
			time.Sleep(100 * time.Millisecond)
			continue
		}

		nextEvent := ks.events[0]
		now := time.Now()

		if now.Before(nextEvent.TriggerTime) {
			ks.mu.Unlock()
			time.Sleep(50 * time.Millisecond)
			continue
		}

		heap.Pop(&ks.events)
		meta, exists := ks.keys[nextEvent.KeyID]

		if !exists {
			ks.mu.Unlock()
			continue
		}

		switch nextEvent.Type {
		case EventHardExpiry:
			expiryLimit := meta.LastKeepAlive.Add(KeepAliveDuration)
			if expiryLimit.Before(now) {
				log.Printf("Purging expired key: %s\n", nextEvent.KeyID)
				delete(ks.keys, nextEvent.KeyID)
				ks.removeFromAvailable(nextEvent.KeyID)
			}

		case EventAutoUnblock:
			blockLimit := meta.BlockedAt.Add(BlockDuration)
			if meta.IsBlocked && blockLimit.Before(now) {
				log.Printf("Ghost Rule: Auto-unblocking %s\n", nextEvent.KeyID)
				meta.IsBlocked = false
				ks.addToAvailable(nextEvent.KeyID)
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
