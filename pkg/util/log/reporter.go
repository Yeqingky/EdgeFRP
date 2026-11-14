// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package log

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fatedier/golib/log"
)

const (
	defaultReportQueue        = 1024
	defaultReportMaxBytes     = 8192
	defaultReportMaxPerSecond = 50
	reportBatchSize           = 100
	reportFlushInterval       = time.Second
	reportRequestTimeout      = 5 * time.Second
)

// HTTPReporterConfig configures the optional frpc log reporter.
type HTTPReporterConfig struct {
	URL          string
	Token        string
	MaxQueue     int
	MaxBytes     int
	MaxPerSecond int
}

type reportEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// HTTPReporter sends log lines to a token-authenticated HTTP endpoint.
type HTTPReporter struct {
	endpoint     string
	client       *http.Client
	ch           chan reportEntry
	done         chan struct{}
	wg           sync.WaitGroup
	closeOnce    sync.Once
	closed       atomic.Bool
	maxBytes     int
	maxPerSecond int
	window       int64
	windowCount  int
	mu           sync.Mutex
}

// NewHTTPReporter creates a reporter. An empty URL disables it.
func NewHTTPReporter(cfg HTTPReporterConfig) (*HTTPReporter, error) {
	if cfg.URL == "" {
		return nil, nil
	}
	endpoint, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, err
	}
	if cfg.Token != "" {
		query := endpoint.Query()
		query.Set("token", cfg.Token)
		endpoint.RawQuery = query.Encode()
	}

	maxQueue := cfg.MaxQueue
	if maxQueue <= 0 {
		maxQueue = defaultReportQueue
	}
	maxBytes := cfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultReportMaxBytes
	}
	maxPerSecond := cfg.MaxPerSecond
	if maxPerSecond <= 0 {
		maxPerSecond = defaultReportMaxPerSecond
	}

	reporter := &HTTPReporter{
		endpoint:     endpoint.String(),
		client:       &http.Client{Timeout: reportRequestTimeout},
		ch:           make(chan reportEntry, maxQueue),
		done:         make(chan struct{}),
		maxBytes:     maxBytes,
		maxPerSecond: maxPerSecond,
	}
	reporter.wg.Add(1)
	go reporter.run()
	return reporter, nil
}

func (r *HTTPReporter) WriteLog(p []byte, level log.Level, when time.Time) (int, error) {
	if r == nil || r.closed.Load() {
		return len(p), nil
	}

	message := strings.TrimRight(string(p), "\r\n")
	if len(message) > r.maxBytes {
		message = message[:r.maxBytes]
	}

	if !r.allow() {
		return len(p), nil
	}

	select {
	case r.ch <- reportEntry{
		Time:    when.Format(time.RFC3339Nano),
		Level:   level.String(),
		Message: message,
	}:
	default:
	}
	return len(p), nil
}

func (r *HTTPReporter) allow() bool {
	now := time.Now().Unix()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.window != now {
		r.window = now
		r.windowCount = 0
	}
	if r.windowCount >= r.maxPerSecond {
		return false
	}
	r.windowCount++
	return true
}

func (r *HTTPReporter) run() {
	defer r.wg.Done()

	ticker := time.NewTicker(reportFlushInterval)
	defer ticker.Stop()

	batch := make([]reportEntry, 0, reportBatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		r.send(batch)
		batch = batch[:0]
	}

	for {
		select {
		case entry := <-r.ch:
			batch = append(batch, entry)
			if len(batch) >= reportBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-r.done:
			for {
				select {
				case entry := <-r.ch:
					batch = append(batch, entry)
				default:
					flush()
					return
				}
			}
		}
	}
}

func (r *HTTPReporter) send(entries []reportEntry) {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	for _, entry := range entries {
		if err := encoder.Encode(entry); err != nil {
			continue
		}
	}
	if body.Len() == 0 {
		return
	}

	req, err := http.NewRequest(http.MethodPost, r.endpoint, &body)
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-ndjson")

	resp, err := r.client.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

// Close flushes pending entries and stops the reporter.
func (r *HTTPReporter) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		r.closed.Store(true)
		close(r.done)
	})
	r.wg.Wait()
	return nil
}
