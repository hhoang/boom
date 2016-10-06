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

package boomer

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	barChar = "∎"
)

type report struct {
	host     string
	path     string
	reqType  string
	avgTotal float64
	fastest  float64
	slowest  float64
	average  float64
	rps      float64

	results    chan *Result
	aggregator *Aggregator //channel to forward results
	TotalTime  time.Duration

	errorDist      map[string]int
	statusCodeDist map[int]int
	lats           []float64
	sizeTotal      int64

	output string
}

type WriteOutput struct {
	Report *report
	Output string
	Csv    string
	Type   string
}

func NewExecutorReport(executor BoomExecutor, results chan *Result, aggr *Aggregator, output string, total time.Duration) *report {
	return newReport(executor.GetHost(), executor.GetEndpoint(), executor.GetRequestType(), results, aggr, output, total)
}

func NewAggregatorReport() *report {
	return &report{
		host:           "",
		path:           "",
		reqType:        "AGGREGATED REPORT",
		output:         "",
		results:        nil,
		TotalTime:      0, // set this once finished
		statusCodeDist: make(map[int]int),
		errorDist:      make(map[string]int),
	}
}

func newReport(host, path, reqType string, results chan *Result, aggr *Aggregator, output string, total time.Duration) *report {
	return &report{
		host:           host,
		path:           path,
		reqType:        reqType,
		output:         output,
		results:        results,
		aggregator:     aggr,
		TotalTime:      total,
		statusCodeDist: make(map[int]int),
		errorDist:      make(map[string]int),
	}
}

func (r *report) finalize() (string, string) {
	for {
		select {
		case res := <-r.results:
			r.recordResult(res)
			if r.aggregator.agg != nil {
				r.aggregator.agg <- res // forward results
			}
		default:
			r.averageResults()
			fmt.Println("Done")
			if r.aggregator.wg != nil {
				// notifies our aggr waitgroup that this boom has forwarded all results
				r.aggregator.wg.Done()
			}
			return r.print()
		}
	}
}

func (r *report) recordResult(res *Result) {
	if res.Err != nil {
		r.errorDist[res.Err.Error()]++
	} else {
		r.lats = append(r.lats, res.Duration.Seconds())
		r.avgTotal += res.Duration.Seconds()
		r.statusCodeDist[res.StatusCode]++
		if res.ContentLength > 0 {
			r.sizeTotal += res.ContentLength
		}
	}
}

func (r *report) averageResults() {
	r.rps = float64(len(r.lats)) / r.TotalTime.Seconds()
	r.average = r.avgTotal / float64(len(r.lats))

	if len(r.lats) > 0 {
		sort.Float64s(r.lats)
		r.fastest = r.lats[0]
		r.slowest = r.lats[len(r.lats)-1]
	}
}

func (r *report) print() (string, string) {
	var summary string
	if len(r.lats) > 0 {
		summary += fmt.Sprint("  ==============================\n")
		summary += fmt.Sprint("  Boom Results:\n")
		summary += fmt.Sprintf("  Request Type: %v\n", r.reqType)
		summary += fmt.Sprintf("  Host: %v\n", r.host)
		summary += fmt.Sprintf("  Endpoint: %v\n", r.path)
		summary += fmt.Sprint("  Times:\n")
		summary += fmt.Sprintf("  Total:\t%4.4f secs\n", r.TotalTime.Seconds())
		summary += fmt.Sprintf("  Slowest:\t%4.4f secs\n", r.slowest)
		summary += fmt.Sprintf("  Fastest:\t%4.4f secs\n", r.fastest)
		summary += fmt.Sprintf("  Average:\t%4.4f secs\n", r.average)
		summary += fmt.Sprintf("  Requests/sec:\t%4.4f\n", r.rps)
		if r.sizeTotal > 0 {
			summary += fmt.Sprintf("  Total data:\t%d bytes\n", r.sizeTotal)
			summary += fmt.Sprintf("  Size/request:\t%d bytes\n", r.sizeTotal/int64(len(r.lats)))
		}
		summary += r.printHistogram()
		summary += r.printLatencies()
		summary += r.printStatusCodes()
	}

	if len(r.errorDist) > 0 {
		summary += r.printErrors()
	}
	summary += fmt.Sprint("  ==============================\n")

	if r.output == "csv" {
		return summary, r.printCSV()
	}

	return summary, ""
}

func (r *report) printCSV() (out string) {
	for i, val := range r.lats {
		out += fmt.Sprintf("%v,%4.4f\n", i+1, val)
	}
	return
}

// Prints percentile latencies.
func (r *report) printLatencies() (out string) {
	pctls := []int{10, 25, 50, 75, 90, 95, 99}
	data := make([]float64, len(pctls))
	j := 0
	for i := 0; i < len(r.lats) && j < len(pctls); i++ {
		current := i * 100 / len(r.lats)
		if current >= pctls[j] {
			data[j] = r.lats[i]
			j++
		}
	}
	out = fmt.Sprintf("\nLatency distribution:\n")
	for i := 0; i < len(pctls); i++ {
		if data[i] > 0 {
			out += fmt.Sprintf("  %v%% in %4.4f secs\n", pctls[i], data[i])
		}
	}
	return
}

func (r *report) printHistogram() (out string) {
	bc := 10
	buckets := make([]float64, bc+1)
	counts := make([]int, bc+1)
	bs := (r.slowest - r.fastest) / float64(bc)
	for i := 0; i < bc; i++ {
		buckets[i] = r.fastest + bs*float64(i)
	}
	buckets[bc] = r.slowest
	var bi int
	var max int
	for i := 0; i < len(r.lats); {
		if r.lats[i] <= buckets[bi] {
			i++
			counts[bi]++
			if max < counts[bi] {
				max = counts[bi]
			}
		} else if bi < len(buckets)-1 {
			bi++
		}
	}
	out = fmt.Sprintf("\nResponse time histogram:\n")
	for i := 0; i < len(buckets); i++ {
		// Normalize bar lengths.
		var barLen int
		if max > 0 {
			barLen = counts[i] * 40 / max
		}
		out += fmt.Sprintf("  %4.3f [%v]\t|%v\n", buckets[i], counts[i], strings.Repeat(barChar, barLen))
	}
	return
}

// Prints status code distribution.
func (r *report) printStatusCodes() (out string) {
	out = fmt.Sprintf("\nStatus code distribution:\n")
	for code, num := range r.statusCodeDist {
		out += fmt.Sprintf("  [%d]\t%d responses\n", code, num)
	}
	return
}

func (r *report) printErrors() (out string) {
	out = fmt.Sprintf("\nError distribution:\n")
	for err, num := range r.errorDist {
		out += fmt.Sprintf("  [%d]\t%s\n", num, err)
	}
	return
}
