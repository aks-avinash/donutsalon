package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"net/http"
	_ "net/http/pprof"
	"time"

	lightstep "github.com/lightstep/lightstep-tracer-go"
	opentracing "github.com/opentracing/opentracing-go"
	zipkin "github.com/openzipkin/zipkin-go-opentracing"
)

const (
	donutOriginKey   = "origin"
	maxQueueDuration = float64(8 * time.Second)
)

var (
	baseDir            = flag.String("basedir", "/etc/app/", "")
	accessToken        = flag.String("token", "DEVELOPMENT_TOKEN_bhs", "")
	port               = flag.Int("port", 8080, "")
	serviceHostport    = flag.String("service_hostport", "localhost:8080", "")
	collectorHost      = flag.String("collector_host", "collector-grpc.lightstep.com", "")
	collectorPort      = flag.Int("collector_port", 443, "")
	tracerType         = flag.String("tracer_type", "lightstep", "")
	orderProcesses     = flag.Int("order", 1, "")
	restockerProcesses = flag.Int("restock", 0, "")
	cleanerProcesses   = flag.Int("clean", 0, "")
	passthrough        = flag.Bool("passthrough", false, "")
)

func SleepGaussian(d time.Duration, queueLength float64) {
	cappedDuration := float64(d)
	if queueLength > 4 {
		cappedDuration = math.Min(float64(time.Millisecond*50), maxQueueDuration/(queueLength-4))
	}
	//	noise := (float64(cappedDuration) / 3) * rand.NormFloat64()
	time.Sleep(time.Duration(cappedDuration))
}

type TracerGenerator func(component string) opentracing.Tracer

func main() {
	flag.Parse()
	lightstep.SetGlobalEventHandler(lightstep.NewEventLogOneError())
	var tracerGen TracerGenerator
	if *tracerType == "lightstep" {
		tracerGen = func(component string) opentracing.Tracer {
			return lightstep.NewTracer(lightstep.Options{
				AccessToken: *accessToken,
				Collector: lightstep.Endpoint{
					Host: *collectorHost,
					Port: *collectorPort,
				},
				MaxBufferedSpans: 200000,
				UseGRPC:          true,
				Tags: opentracing.Tags{
					lightstep.ComponentNameKey: component,
				},
			})
		}
	} else if *tracerType == "zipkin" {
		tracerGen = func(component string) opentracing.Tracer {
			collector, _ := zipkin.NewHTTPCollector(
				fmt.Sprintf("http://donutsalon.com:9411/api/v1/spans"))
			tracer, _ := zipkin.NewTracer(
				zipkin.NewRecorder(collector, false, "127.0.0.1:0", component))
			return tracer
		}
		t := tracerGen("foo")
		sp := t.StartSpan("blah")
		sp.Finish()
	} else {
		panic(*tracerType)
	}
	ds := newDonutService(tracerGen)

	// Make fake queries in the background.
	//	backgroundProcess(*orderProcesses, ds, runFakeUser)
	//	backgroundProcess(*restockerProcesses, ds, runFakeRestocker)
	//	backgroundProcess(*cleanerProcesses, ds, runFakeCleaner)

	http.HandleFunc("/", ds.pageHandler("order"))
	http.HandleFunc("/clean", ds.pageHandler("clean"))
	http.HandleFunc("/restock", ds.pageHandler("restock"))

	http.HandleFunc("/api/clean", ds.webClean)
	http.HandleFunc("/api/order", ds.webOrder)
	http.HandleFunc("/api/restock", ds.webRestock)

	http.HandleFunc("/service/fry", ds.serviceFry)
	http.HandleFunc("/service/top", ds.serviceTop)

	http.HandleFunc("/status", ds.handleState)

	http.Handle("/public/", http.StripPrefix("/public/", http.FileServer(http.Dir(*baseDir+"public/"))))
	fmt.Println("Starting on :", *port)
	err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)
	fmt.Println("Exiting", err)
}

func backgroundProcess(max int, ds *DonutService, f func(flavor string, ds *DonutService)) {
	for i := 0; i < max; i++ {
		var flavor string
		switch i % 3 {
		case 0:
			flavor = "cinnamon"
		case 1:
			flavor = "chocolate"
		case 2:
			flavor = "sprinkles"
		}
		go f(flavor, ds)
	}
}

func runFakeUser(flavor string, ds *DonutService) {
	for {
		SleepGaussian(2500*time.Millisecond, 1)
		span := ds.tracer.StartSpan(fmt.Sprintf("background_order[%s]", flavor))
		ds.makeDonut(span.Context(), span.Context(), flavor)
		span.Finish()
	}
}

func runFakeRestocker(flavor string, ds *DonutService) {
	for {
		SleepGaussian(20000*time.Millisecond, 1)
		span := ds.tracer.StartSpan(fmt.Sprintf("background_restocker[%s]", flavor))
		ds.restock(span.Context(), flavor)
		span.Finish()
	}
}

func runFakeCleaner(flavor string, ds *DonutService) {
	for {
		SleepGaussian(time.Second, 1)
		span := ds.tracer.StartSpan("background_cleaner")
		ds.cleanFryer(span.Context())
		span.Finish()
	}
}

func startSpanFromContext(name string, tracer opentracing.Tracer, ctx context.Context) opentracing.Span {
	var parentSpanContext opentracing.SpanContext
	if parent := opentracing.SpanFromContext(ctx); parent != nil {
		parentSpanContext = parent.Context()
	}
	return tracer.StartSpan(name, opentracing.ChildOf(parentSpanContext))
}
