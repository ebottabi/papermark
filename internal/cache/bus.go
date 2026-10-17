/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package cache

import (
	"context"
	"encoding/json"
	"log"
	"sync"

	"github.com/redis/go-redis/v9"
)

// Frame kinds carried on a task's channel.
const (
	FrameEvent    = "event"    // a persisted event; Seq and Event are set
	FrameDelta    = "delta"    // streaming text; not persisted
	FrameActivity = "activity" // Text is "thinking" or the tool being written; not persisted
	FrameStop     = "stop"     // the user asked the running task to stop
)

// Frame is one message on a task's live channel.
type Frame struct {
	Kind  string          `json:"kind"`
	Seq   int             `json:"seq"`
	Event json.RawMessage `json:"event,omitempty"`
	Text  string          `json:"text,omitempty"`
}

// Bus carries live task frames from the worker that runs a task to the HTTP
// servers streaming it, and stop requests the other way. It is best effort:
// persisted events are always replayable from Postgres.
type Bus interface {
	Publish(ctx context.Context, taskID string, f Frame)
	// Subscribe returns once the subscription is active, so a caller that
	// subscribes before reading Postgres misses nothing. Call the func to stop.
	Subscribe(ctx context.Context, taskID string) (<-chan Frame, func(), error)
}

// NewBus returns a Redis bus (channel papermark:task:<id>), or an in-process
// bus when rdb is nil (single-process mode).
func NewBus(rdb *redis.Client) Bus {
	if rdb == nil {
		return &memoryBus{subs: map[string]map[chan Frame]struct{}{}}
	}
	return &redisBus{rdb: rdb}
}

func channel(taskID string) string { return keyPrefix + ":task:" + taskID }

type redisBus struct{ rdb *redis.Client }

func (b *redisBus) Publish(ctx context.Context, taskID string, f Frame) {
	raw, _ := json.Marshal(f)
	if err := b.rdb.Publish(ctx, channel(taskID), raw).Err(); err != nil {
		log.Printf("bus: publish %s: %v", taskID, err)
	}
}

func (b *redisBus) Subscribe(ctx context.Context, taskID string) (<-chan Frame, func(), error) {
	ps := b.rdb.Subscribe(ctx, channel(taskID))
	if _, err := ps.Receive(ctx); err != nil {
		ps.Close()
		return nil, nil, err
	}
	out := make(chan Frame, 256)
	in := ps.Channel()
	go func() {
		defer close(out)
		for m := range in {
			var f Frame
			if json.Unmarshal([]byte(m.Payload), &f) != nil {
				continue
			}
			select {
			case out <- f:
			default: // same contract as the in-process bus
			}
		}
	}()
	return out, func() { ps.Close() }, nil
}

type memoryBus struct {
	mu   sync.Mutex
	subs map[string]map[chan Frame]struct{}
}

func (b *memoryBus) Publish(_ context.Context, taskID string, f Frame) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[taskID] {
		select {
		case ch <- f:
		default: // a slow reader loses frames; it refills events from Postgres
		}
	}
}

func (b *memoryBus) Subscribe(_ context.Context, taskID string) (<-chan Frame, func(), error) {
	ch := make(chan Frame, 256)
	b.mu.Lock()
	if b.subs[taskID] == nil {
		b.subs[taskID] = map[chan Frame]struct{}{}
	}
	b.subs[taskID][ch] = struct{}{}
	b.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs[taskID], ch)
			if len(b.subs[taskID]) == 0 {
				delete(b.subs, taskID)
			}
			b.mu.Unlock()
			close(ch)
		})
	}, nil
}
