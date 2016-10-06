// Copyright 2014 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package boomer provides commands to run load tests and display results.
package boomer

import (
	"net/url"
	"os"
	"os/signal"
	"sync"
	"time"
)

type Result struct {
	Err           error
	StatusCode    int
	Duration      time.Duration
	ContentLength int64
}

type Boomer struct {
	// Request is the request to be made.
	Request BoomExecutor

	Body []byte

	// N is the total number of requests to make.
	NumRequest int

	// C is the concurrency level, the number of concurrent workers to run.
	ConcurrentCalls int

	// Timeout in seconds.
	Timeout int

	// Qps is the rate limit.
	Qps int

	// DisableCompression is an option to disable compression in response
	DisableCompression bool

	// DisableKeepAlives is an option to prevents re-use of TCP connections between different HTTP requests
	DisableKeepAlives bool

	// Output represents the output type. If "csv" is provided, the
	// output will be dumped as a csv stream.
	Output string

	// ProxyAddr is the address of HTTP proxy server in the format on "host:port".
	// Optional.
	ProxyAddr *url.URL

	Results chan *Result

	// Results channel receiver will forward results to this channel
	Aggregator *Aggregator
}

type Boomable interface {
	GetBooms() []*Boomer
}

type Aggregator struct {
	wg        *sync.WaitGroup
	agg       chan *Result
	done      chan WriteOutput
	boomCount int
}

func Run(boomie Boomable, writeOutChan chan<- WriteOutput) {
	booms := boomie.GetBooms()

	var w sync.WaitGroup
	aggregator := &Aggregator{
		wg:        &w,
		agg:       make(chan *Result, len(booms)),
		done:      make(chan WriteOutput),
		boomCount: len(booms),
	}

	go startAggregatingResults(aggregator)

	for _, b := range booms {
		b.Aggregator = aggregator
		output := b.Run()
		writeOutChan <- output
	}

	reportAvg := <-aggregator.done

	writeOutChan <- reportAvg
	close(writeOutChan)
}

/**
*  Should be called in a goroutine.
*  Receives forwarded results from Boomers and generates an aggregate report.
*  Blocks until all Boomers have run.
 */
func startAggregatingResults(aggr *Aggregator) {
	aggr.wg.Add(aggr.boomCount)
	go func() {
		aggr.wg.Wait()
		close(aggr.agg)
	}()

	start := time.Now()

	finalReport := NewAggregatorReport()

	// Listen for results until all boom reports have been finalized
	for res := range aggr.agg {
		finalReport.recordResult(res)
	}

	// the original library would wait for all requests to finish before creating a report. For aggregate scores,
	// I'm generating an empty report and averaging results as they come in, then setting the time afterwards
	finalReport.TotalTime = time.Now().Sub(start)
	finalReport.averageResults()

	report, csv := finalReport.print()

	aggr.done <- WriteOutput{finalReport, report, csv, "COMBINED"}
}

func (b *Boomer) Run() WriteOutput {
	b.Results = make(chan *Result, b.NumRequest)

	start := time.Now()
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		// TODO(jbd): Progress bar should not be finalized.
		NewExecutorReport(b.Request, b.Results, b.Aggregator, b.Output, time.Now().Sub(start)).finalize()
		os.Exit(1)
	}()

	b.runWorkers()
	report := NewExecutorReport(b.Request, b.Results, b.Aggregator, b.Output, time.Now().Sub(start))
	output, csv := report.finalize()

	close(b.Results)
	return WriteOutput{report, output, csv, b.Request.GetRequestType()}
}

func (b *Boomer) runWorker(n int) {
	var throttle <-chan time.Time
	if b.Qps > 0 {
		throttle = time.Tick(time.Duration(1e6/(b.Qps)) * time.Microsecond)
	}

	for i := 0; i < n; i++ {
		if b.Qps > 0 {
			<-throttle
		}
		b.Request.Execute(b.Results)
	}
}

func (b *Boomer) runWorkers() {
	var wg sync.WaitGroup
	wg.Add(b.ConcurrentCalls)

	// Ignore the case where b.N % b.C != 0.
	for i := 0; i < b.ConcurrentCalls; i++ {
		go func() {
			b.runWorker(b.NumRequest / b.ConcurrentCalls)
			wg.Done()
		}()
	}
	wg.Wait()
}
