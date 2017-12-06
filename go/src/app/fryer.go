package main

import (
	"fmt"
	"math"
	"time"

	"golang.org/x/net/context"

	opentracing "github.com/opentracing/opentracing-go"
)

type Fryer struct {
	tracer   opentracing.Tracer
	lock     *SmartLock
	duration time.Duration
	oilLevel int
}

func newFryer(tracerGen TracerGenerator, duration time.Duration) *Fryer {
	return &Fryer{
		tracer:   tracerGen("donut-fryer"),
		duration: duration,
		lock:     NewSmartLock(true),
	}
}

func (f *Fryer) FryDonut(ctx context.Context) {
	var span opentracing.Span
	if !*passthrough {
		span = startSpanFromContext("fry_donut", f.tracer, ctx)
		defer span.Finish()
	}

	f.lock.Lock(span)
	defer f.lock.Unlock()

	if span != nil {
		span.LogEvent(fmt.Sprint("starting to fry: ", span.BaggageItem(donutOriginKey)))
	}
	SleepGaussian(f.duration+time.Duration(math.Min(500, float64(f.oilLevel)))*time.Millisecond, f.lock.QueueLength())
	f.oilLevel++
}

func (f *Fryer) ChangeOil(ctx context.Context) {
	var span opentracing.Span
	if !*passthrough {
		span = startSpanFromContext("change_oil", f.tracer, ctx)
		defer span.Finish()
	}

	f.lock.Lock(span)
	defer f.lock.Unlock()

	if f.oilLevel < 10 {
		SleepGaussian(f.duration*5, f.lock.QueueLength())
	} else {
		SleepGaussian(time.Second*6, 0)
	}
	f.oilLevel = f.oilLevel / 2
}

func (f *Fryer) OilLevel() int {
	span := f.tracer.StartSpan("oil_level")
	defer span.Finish()

	return f.oilLevel
}
