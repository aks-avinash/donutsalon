package main

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	opentracing "github.com/opentracing/opentracing-go"
)

var (
	seededRNG = rand.New(rand.NewSource(time.Now().UnixNano()))
)

type SmartLock struct {
	realLock    sync.Mutex
	queueLength int64
	lockID      string
	cont        bool
	acquired    time.Time
	activeSpan  opentracing.Span
}

func NewSmartLock(cont bool) *SmartLock {
	return &SmartLock{
		realLock: sync.Mutex{},
		lockID:   fmt.Sprintf("smart_lock-%v", seededRNG.Int63()),
		cont:     cont,
	}
}

func (sl *SmartLock) Lock(activeSpan opentracing.Span) {
	if sl.cont && activeSpan != nil {
		activeSpan.SetTag("c:", sl.lockID)
	}
	waiters := atomic.AddInt64(&sl.queueLength, 1)
	before := time.Now()
	sl.realLock.Lock()
	lockDuration := time.Now().Sub(before)

	if lockDuration.Seconds() > 0.01 && activeSpan != nil {
		acquireSpan := activeSpan.Tracer().StartSpan(
			"mutex_acquire",
			opentracing.ChildOf(activeSpan.Context()),
			opentracing.StartTime(before))
		acquireSpan.SetTag("num_waiters", waiters)
		acquireSpan.Finish()
	}

	sl.activeSpan = activeSpan
	atomic.AddInt64(&sl.queueLength, -1)
	sl.acquired = time.Now()
}

func (sl *SmartLock) Unlock() {
	released := time.Now()

	heldTime := released.Sub(sl.acquired)
	if sl.activeSpan != nil {
		sl.activeSpan.SetTag("weight", int(heldTime.Seconds()*1000.0+1))
		if heldTime.Seconds() > 0.01 {
			heldSpan := sl.activeSpan.Tracer().StartSpan(
				"mutex_hold",
				opentracing.ChildOf(sl.activeSpan.Context()),
				opentracing.StartTime(sl.acquired))
			heldSpan.Finish()
		}

	}
	sl.realLock.Unlock()
}

func (sl *SmartLock) QueueLength() float64 {
	return float64(sl.queueLength)
}
