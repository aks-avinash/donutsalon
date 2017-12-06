package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"time"

	"golang.org/x/net/context"

	"io"

	opentracing "github.com/opentracing/opentracing-go"
)

const (
	fryDuration = time.Millisecond * 50
	payDuration = time.Millisecond * 250
	topDuration = time.Millisecond * 350
)

type State struct {
	OilLevel  int
	Inventory map[string]int
}

type DonutService struct {
	tracer    opentracing.Tracer
	payer     *Payer
	fryer     *Fryer
	tracerGen TracerGenerator

	toppersLock *SmartLock
	toppers     map[string]*Topper
}

func newDonutService(tracerGen TracerGenerator) *DonutService {
	return &DonutService{
		tracer:      tracerGen("donut-webserver"),
		payer:       NewPayer(tracerGen, payDuration),
		fryer:       newFryer(tracerGen, fryDuration),
		toppers:     make(map[string]*Topper),
		toppersLock: NewSmartLock(true),
		tracerGen:   tracerGen,
	}
}

func (ds *DonutService) pageHandler(pageBasename string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		t, err := template.New("").ParseFiles(
			*baseDir+pageBasename+".go.html",
			*baseDir+"header.go.html",
			*baseDir+"status.go.html")
		panicErr(err)

		err = t.ExecuteTemplate(w, pageBasename+".go.html", ds.state())
		panicErr(err)
	}
}

func (ds *DonutService) webOrder(w http.ResponseWriter, r *http.Request) {
	carrier := opentracing.HTTPHeadersCarrier(r.Header)
	clientContext, _ := ds.tracer.Extract(opentracing.HTTPHeaders, carrier)

	p := struct {
		Flavor string `json:"flavor"`
	}{}
	unmarshalJSON(r.Body, &p)
	if p.Flavor == "" {
		panic("flavor not set")
	}

	var spanContext opentracing.SpanContext
	if !*passthrough {
		span := ds.tracer.StartSpan(fmt.Sprintf("order_donut[%s]", p.Flavor), opentracing.ChildOf(clientContext))
		defer span.Finish()
		spanContext = span.Context()
	}

	err := ds.makeDonut(clientContext, spanContext, p.Flavor)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (ds *DonutService) handleState(w http.ResponseWriter, r *http.Request) {
	state := ds.state()
	data, err := json.Marshal(state)
	panicErr(err)
	w.Write(data)
}

func (ds *DonutService) webClean(w http.ResponseWriter, r *http.Request) {
	carrier := opentracing.HTTPHeadersCarrier(r.Header)
	clientContext, _ := ds.tracer.Extract(opentracing.HTTPHeaders, carrier)
	ds.cleanFryer(clientContext)
}

func (ds *DonutService) webRestock(w http.ResponseWriter, r *http.Request) {
	carrier := opentracing.HTTPHeadersCarrier(r.Header)
	clientContext, _ := ds.tracer.Extract(opentracing.HTTPHeaders, carrier)

	p := struct {
		Flavor string `json:"flavor"`
	}{}
	unmarshalJSON(r.Body, &p)
	if p.Flavor == "" {
		panic("flavor not set")
	}

	span := ds.tracer.StartSpan(
		fmt.Sprintf("restock[%s]", p.Flavor),
		opentracing.ChildOf(clientContext))

	ds.restock(span.Context(), p.Flavor)
}

func (ds *DonutService) serviceFry(w http.ResponseWriter, r *http.Request) {
	carrier := opentracing.HTTPHeadersCarrier(r.Header)
	clientContext, _ := ds.tracer.Extract(opentracing.HTTPHeaders, carrier)

	var span opentracing.Span
	if !*passthrough {
		span = ds.tracer.StartSpan(
			"fry",
			opentracing.ChildOf(clientContext))
		defer span.Finish()
	}

	goCtx := opentracing.ContextWithSpan(context.Background(), span)
	ds.fryer.FryDonut(goCtx)
}

func (ds *DonutService) serviceTop(w http.ResponseWriter, r *http.Request) {
	carrier := opentracing.HTTPHeadersCarrier(r.Header)
	clientContext, _ := ds.tracer.Extract(opentracing.HTTPHeaders, carrier)

	p := struct {
		Flavor string `json:"flavor"`
	}{}
	unmarshalJSON(r.Body, &p)
	if p.Flavor == "" {
		panic("flavor not set")
	}

	var span opentracing.Span
	if !*passthrough {
		span = ds.tracer.StartSpan(
			"top",
			opentracing.ChildOf(clientContext))
		defer span.Finish()
	}

	err := ds.addTopping(span, p.Flavor)
	if err != nil {
		http.Error(w, err.Error(), http.StatusGone)
	}
}

func (ds *DonutService) state() *State {
	return &State{
		OilLevel:  ds.fryer.OilLevel(),
		Inventory: ds.inventory(),
	}
}

func (ds *DonutService) call(passthroughCtx opentracing.SpanContext, clientSpanContext opentracing.SpanContext, path string, postBody []byte) error {
	url := fmt.Sprintf("http://%s%s", *serviceHostport, path)

	if *passthrough {
		clientSpanContext = passthroughCtx
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(postBody))
	req.Header.Set("Content-Type", "application/json")
	carrier := opentracing.HTTPHeadersCarrier(req.Header)
	err = ds.tracer.Inject(clientSpanContext, opentracing.HTTPHeaders, carrier)
	if err != nil {
		panic(err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	ioutil.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("call failure")
	}
	return nil
}

func (ds *DonutService) makeDonut(passthroughCtx opentracing.SpanContext, parentSpanContext opentracing.SpanContext, flavor string) error {
	var localSpanContext opentracing.SpanContext
	var donutSpan opentracing.Span
	if !*passthrough {
		donutSpan = ds.tracer.StartSpan("make_donut", opentracing.ChildOf(parentSpanContext))
		defer donutSpan.Finish()
		localSpanContext = donutSpan.Context()
	}
	ctx := opentracing.ContextWithSpan(context.Background(), donutSpan)

	ds.payer.BuyDonut(ctx)
	err := ds.call(passthroughCtx, localSpanContext, "/service/fry", []byte{})
	if err != nil {
		return err
	}
	return ds.call(
		passthroughCtx,
		localSpanContext,
		"/service/top",
		[]byte(fmt.Sprintf(`{"flavor":"%s"}`, flavor)))
}

func (ds *DonutService) addTopping(span opentracing.Span, flavor string) error {
	ds.toppersLock.Lock(span)
	topper := ds.toppers[flavor]
	if topper == nil {
		topper = newTopper(ds.tracerGen, flavor, topDuration)
		ds.toppers[flavor] = topper
	}
	ds.toppersLock.Unlock()

	return topper.SprinkleTopping(opentracing.ContextWithSpan(context.Background(), span))
}

func (ds *DonutService) cleanFryer(passthroughCtx opentracing.SpanContext) {
	var donutSpan opentracing.Span
	if !*passthrough {
		donutSpan = ds.tracer.StartSpan("clean_fryer", opentracing.ChildOf(passthroughCtx))
		defer donutSpan.Finish()
	}
	ctx := opentracing.ContextWithSpan(context.Background(), donutSpan)

	ds.fryer.ChangeOil(ctx)
}

func (ds *DonutService) inventory() map[string]int {
	inventory := make(map[string]int)
	var donutSpan opentracing.Span
	if !*passthrough {
		donutSpan = ds.tracer.StartSpan("inventory")
		defer donutSpan.Finish()
	}

	ds.toppersLock.Lock(donutSpan)
	for flavor, topper := range ds.toppers {
		inventory[flavor] = topper.Quantity(donutSpan)
	}
	ds.toppersLock.Unlock()

	return inventory
}

func (ds *DonutService) restock(parentSpanContext opentracing.SpanContext, flavor string) {
	var donutSpan opentracing.Span
	if parentSpanContext != nil {
		donutSpan = ds.tracer.StartSpan("restock_ingredients", opentracing.ChildOf(parentSpanContext))
		defer donutSpan.Finish()
	}
	ctx := opentracing.ContextWithSpan(context.Background(), donutSpan)

	ds.toppersLock.Lock(donutSpan)
	topper := ds.toppers[flavor]
	if topper == nil {
		topper = newTopper(ds.tracerGen, flavor, topDuration)
		ds.toppers[flavor] = topper
	}
	ds.toppersLock.Unlock()

	topper.Restock(ctx)
}

func panicErr(err error) {
	if err != nil {
		panic(err)
	}
}

func unmarshalJSON(body io.ReadCloser, data interface{}) {
	defer body.Close()
	decoder := json.NewDecoder(body)
	err := decoder.Decode(&data)
	panicErr(err)
}
