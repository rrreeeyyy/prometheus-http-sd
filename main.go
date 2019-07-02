package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/documentation/examples/custom-sd/adapter"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	a               = kingpin.New("sd adapter usage", "Tool to generate file_sd target files for unimplemented SD mechanisms.")
	outputFile      = a.Flag("output.file", "Output file for file_sd compatible file.").Default("custom_sd.json").Strings()
	apiUrl          = a.Flag("api.url", "The url the HTTP API sd is listening on for requests.").Default("http://localhost:8080").Strings()
	refreshInterval = a.Flag("refresh.interval", "Refresh interval to re-read the instance list.").Default("60").Int()
	logger          log.Logger
)

type sdConfig struct {
	OutputFiles     []string
	ApiUrls         []string
	RefreshInterval int
}

type discovery struct {
	outputFile      string
	apiUrl          string
	refreshInterval int
	logger          log.Logger
}

func (d *discovery) Run(ctx context.Context, ch chan<- []*targetgroup.Group) {
	for c := time.Tick(time.Duration(d.refreshInterval) * time.Second); ; {
		resp, err := http.Get(fmt.Sprintf("%s", d.apiUrl))

		if err != nil {
			level.Error(d.logger).Log("msg", "Error getting targets", "err", err)
			time.Sleep(time.Duration(d.refreshInterval) * time.Second)
			continue
		}

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
			return
		}
	}
}

func newDiscovery(conf sdConfig) ([]*discovery, error) {
	var cds []*discovery
	for i := range conf.ApiUrls {
		cds = append(cds, &discovery{
			outputFile:      conf.OutputFiles[i],
			apiUrl:          conf.ApiUrls[i],
			refreshInterval: conf.RefreshInterval,
			logger:          logger,
		})
	}
	return cds, nil
}

func main() {
	a.HelpFlag.Short('h')

	_, err := a.Parse(os.Args[1:])
	if err != nil {
		fmt.Println("err: ", err)
		return
	}
	if len(*apiUrl) != len(*outputFile) {
		fmt.Println("err: The number of options differs between --api.url and --output.file")
		return
	}
	logger = log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))
	logger = log.With(logger, "ts", log.DefaultTimestampUTC, "caller", log.DefaultCaller)

	ctx := context.Background()

	cfg := sdConfig{
		OutputFiles:     *outputFile,
		ApiUrls:         *apiUrl,
		RefreshInterval: *refreshInterval,
	}

	disc, err := newDiscovery(cfg)
	if err != nil {
		fmt.Println("err: ", err)
	}

	for _, cd := range disc {
		sdAdapter := adapter.NewAdapter(ctx, cd.outputFile, "httpSD", cd, logger)
		sdAdapter.Run()
	}

	<-ctx.Done()
}
