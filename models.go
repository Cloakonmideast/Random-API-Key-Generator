package main

import "time"

const (
	KeepAliveDuration = 5 * time.Minute
	BlockDuration     = 60 * time.Second
)

type EventType int

const (
	EventHardExpiry EventType = iota
	EventAutoUnblock
)

type TimerEvent struct {
	TriggerTime time.Time
	KeyID       string
	Type        EventType
	Index       int
}

type KeyMetadata struct {
	ID            string
	IsBlocked     bool
	LastKeepAlive time.Time
	BlockedAt     time.Time
}
