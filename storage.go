package main

import (
	"crypto/rand"
	"encoding/hex"
)

func generateRandomKey() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (ks *KeyServer) addToAvailable(id string) {
	if _, ok := ks.availableIdx[id]; ok {
		return
	}
	ks.availableKeys = append(ks.availableKeys, id)
	ks.availableIdx[id] = len(ks.availableKeys) - 1
}

func (ks *KeyServer) removeFromAvailable(id string) {
	idx, exists := ks.availableIdx[id]
	if !exists {
		return
	}

	lastIdx := len(ks.availableKeys) - 1
	lastKey := ks.availableKeys[lastIdx]

	ks.availableKeys[idx] = lastKey
	ks.availableIdx[lastKey] = idx

	ks.availableKeys = ks.availableKeys[:lastIdx]
	delete(ks.availableIdx, id)
}
