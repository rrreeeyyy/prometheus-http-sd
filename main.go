package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/documentation/examples/custom-sd/adapter"
	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var Version string

const (
	metricsNamespace = "prometheus_sd_http"
)

var (
	metricsRequestsCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "http_requests_total",
			Help:      "Number of http requests.",
		},
		[]string{"code", "api_url"},
	)
)

var (
	a               = kingpin.New("sd adapter usage", "Tool to generate file_sd target files for unimplemented SD mechanisms.")
	apiURL          = a.Flag("api.url", "The url the HTTP API sd is listening on for requests.").Default("http://localhost:8080").Strings()
	outputFile      = a.Flag("output.file", "Output file for file_sd compatible file.").Default("custom_sd.json").Strings()
	refreshInterval = a.Flag("refresh.interval", "Refresh interval to re-read the instance list.").Default("60").Int()
	metricsAddr     = a.Flag("metrics.addr", "Address to bind metrics server to").Default(":8080").String()
	metricsPath     = a.Flag("metrics.path", "Path to serve metrics server to").Default("/metrics").String()
	logger          log.Logger
)

var discoverCancel []context.CancelFunc

type sdConfig struct {
	APIURL          string
	OutputFile      string
	RefreshInterval int
}

type discovery struct {
	apiURL          string
	outputFile      string
	refreshInterval int
	logger          log.Logger
}

func init() {
	prometheus.MustRegister(metricsRequestsCounter)
}

func (d *discovery) Run(ctx context.Context, ch chan<- []*targetgroup.Group) {
	for c := time.Tick(time.Duration(d.refreshInterval) * time.Second); ; {
		url := fmt.Sprintf("%s", d.apiURL)
		resp, err := http.Get(url)

		if err != nil {
			level.Error(d.logger).Log("msg", "Error getting targets", "err", err)
			time.Sleep(time.Duration(d.refreshInterval) * time.Second)
			continue
		}

		metricsRequestsCounter.With(
			prometheus.Labels{
				"code":    strconv.Itoa(resp.StatusCode),
				"api_url": url,
			},
		).Inc()

		rawtgs := []struct {
			Targets []string          `json:"targets"`
			Labels  map[string]string `json:"labels"`
		}{}

		dec := json.NewDecoder(resp.Body)
		err = dec.Decode(&rawtgs)
		resp.Body.Close()
		if err != nil {
			level.Error(d.logger).Log("msg", "Error reading targets", "err", err)
			time.Sleep(time.Duration(d.refreshInterval) * time.Second)
			continue
		}

		var tgs []*targetgroup.Group

		for index, rawtg := range rawtgs {
			tg := targetgroup.Group{
				Source:  strconv.Itoa(index),
				Targets: make([]model.LabelSet, 0, len(rawtg.Targets)),
				Labels:  make(model.LabelSet),
			}

			for _, addr := range rawtg.Targets {
				target := model.LabelSet{model.AddressLabel: model.LabelValue(addr)}
				tg.Targets = append(tg.Targets, target)
			}
			for name, value := range rawtg.Labels {
				label := model.LabelSet{model.LabelName(name): model.LabelValue(value)}
				tg.Labels = tg.Labels.Merge(label)
			}

			tgs = append(tgs, &tg)
		}

		ch <- tgs

		select {
		case <-c:
			continue
		case <-ctx.Done():
			level.Error(d.logger).Log("msg", "Error occurred during HTTP SD. Terminating all discoverers.", "api_url", d.apiURL)
			cancelDiscoverers()
			return
		}
	}
}

func newDiscovery(conf sdConfig) (*discovery, error) {
	cd := &discovery{
		apiURL:          conf.APIURL,
		outputFile:      conf.OutputFile,
		refreshInterval: conf.RefreshInterval,
		logger:          logger,
	}
	return cd, nil
}

func cancelDiscoverers() {
	for _, c := range discoverCancel {
		c()
	}
	discoverCancel = nil
	return
}

func main() {
	a.Version(Version)
	a.HelpFlag.Short('h')

	_, err := a.Parse(os.Args[1:])
	if err != nil {
		fmt.Println("err: ", err)
		return
	}

	if len(*apiURL) != len(*outputFile) {
		fmt.Println("err: The number of options differs between --api.url and --output.file")
		return
	}
	logger = log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))
	logger = log.With(logger, "ts", log.DefaultTimestampUTC, "caller", log.DefaultCaller)

	ctx, cancel := context.WithCancel(context.Background())
	discoverCancel = append(discoverCancel, cancel)
	defer cancel()

	go func() {
		http.Handle(*metricsPath, promhttp.Handler())
		err := http.ListenAndServe(*metricsAddr, nil)
		if err != nil {
			level.Error(logger).Log("msg", "Error occurred during serve metrics server", "err", err)
		}
	}()

	var cfgs []sdConfig
	for i := range *apiURL {
		cfgs = append(cfgs, sdConfig{
			APIURL:          (*apiURL)[i],
			OutputFile:      (*outputFile)[i],
			RefreshInterval: *refreshInterval,
		})
	}

	var discs []*discovery
	for _, cfg := range cfgs {
		disc, err := newDiscovery(cfg)
		if err != nil {
			fmt.Println("err: ", err)
		}
		discs = append(discs, disc)
	}

	var wg sync.WaitGroup

	for _, disc := range discs {
		wg.Add(1)
		func(d *discovery) {
			defer wg.Done()

			ctxSd, cancelSd := context.WithCancel(ctx)
			discoverCancel = append(discoverCancel, cancelSd)

			sdAdapter := adapter.NewAdapter(ctxSd, d.outputFile, "httpSD", d, logger)
			sdAdapter.Run()
		}(disc)
	}
	wg.Wait()

	<-ctx.Done()
}
