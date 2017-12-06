package main

import (
	"fmt"
	"time"

	"golang.org/x/net/context"

	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
)

type Topper struct {
	tracer    opentracing.Tracer
	lock      *SmartLock
	donutType string
	duration  time.Duration
	quantity  int
}

func newTopper(tracerGen TracerGenerator, donutType string, duration time.Duration) *Topper {
	return &Topper{
		tracer:    tracerGen("donut-topper"),
		donutType: donutType,
		duration:  duration,
		lock:      NewSmartLock(true),
		quantity:  20,
	}
}

func (t *Topper) SprinkleTopping(ctx context.Context) error {
	var span opentracing.Span
	if !*passthrough {
		span = startSpanFromContext(fmt.Sprintf("sprinkle_topping[%s]", t.donutType), t.tracer, ctx)
		defer span.Finish()
	}

	t.lock.Lock(span)
	defer t.lock.Unlock()
	if t.quantity < 1 {
		err := fmt.Errorf("out of %s", t.donutType)
		if span != nil {
			span.LogFields(log.Error(err))
		}
		return err
	}
	if span != nil {
		span.LogEvent(fmt.Sprint("starting donut topping: ", span.BaggageItem(donutOriginKey)))
	}
	SleepGaussian(t.duration, t.lock.QueueLength())
	t.quantity--

	return nil
}

func (t *Topper) Restock(ctx context.Context) {
	var span opentracing.Span
	if !*passthrough {
		span = startSpanFromContext(fmt.Sprint("restock_topping: ", t.donutType), t.tracer, ctx)
		defer span.Finish()
	}

	t.lock.Lock(span)
	defer t.lock.Unlock()

	if span != nil {
		span.LogEvent(fmt.Sprint("restocking donut topping: ", span.BaggageItem(donutOriginKey)))
	}
	SleepGaussian(t.duration*3, t.lock.QueueLength())
	t.quantity += 10

}

func (t *Topper) Quantity(parentSpan opentracing.Span) int {
	if parentSpan != nil {
		span := t.tracer.StartSpan(fmt.Sprint("checking_quantity: ", t.donutType), opentracing.ChildOf(parentSpan.Context()))
		defer span.Finish()
	}

	return t.quantity
}
