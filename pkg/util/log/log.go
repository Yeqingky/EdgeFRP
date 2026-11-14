// Copyright 2016 fatedier, fatedier@gmail.com
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
	"io"
	"os"
	"sync"
	"time"

	"github.com/fatedier/golib/log"
)

var (
	TraceLevel = log.TraceLevel
	DebugLevel = log.DebugLevel
	InfoLevel  = log.InfoLevel
	WarnLevel  = log.WarnLevel
	ErrorLevel = log.ErrorLevel
)

var Logger *log.Logger

var (
	fanoutMu      sync.RWMutex
	currentFanout *fanoutWriter
)

// Sink receives formatted log lines together with their level and timestamp.
type Sink interface {
	WriteLog(p []byte, level log.Level, when time.Time) (int, error)
	Close() error
}

type fanoutWriter struct {
	mu    sync.RWMutex
	base  io.Writer
	sinks []Sink
}

func newFanoutWriter(base io.Writer) *fanoutWriter {
	if base == nil {
		base = os.Stdout
	}
	return &fanoutWriter{base: base}
}

func (w *fanoutWriter) Write(p []byte) (int, error) {
	w.mu.RLock()
	base := w.base
	w.mu.RUnlock()
	return base.Write(p)
}

func (w *fanoutWriter) WriteLog(p []byte, level log.Level, when time.Time) (int, error) {
	w.mu.RLock()
	base := w.base
	sinks := append([]Sink(nil), w.sinks...)
	w.mu.RUnlock()

	if lw, ok := base.(log.Writer); ok {
		_, _ = lw.WriteLog(p, level, when)
	} else {
		_, _ = base.Write(p)
	}
	for _, sink := range sinks {
		_, _ = sink.WriteLog(p, level, when)
	}
	return len(p), nil
}

func (w *fanoutWriter) addSink(sink Sink) {
	w.mu.Lock()
	w.sinks = append(w.sinks, sink)
	w.mu.Unlock()
}

func (w *fanoutWriter) removeSink(sink Sink) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i, s := range w.sinks {
		if s == sink {
			w.sinks = append(w.sinks[:i], w.sinks[i+1:]...)
			return
		}
	}
}

// AddSink adds an extra log sink to the logger initialized by InitLogger.
func AddSink(sink Sink) {
	fanoutMu.RLock()
	fanout := currentFanout
	fanoutMu.RUnlock()
	if fanout != nil {
		fanout.addSink(sink)
	}
}

// RemoveSink removes a sink previously added by AddSink.
func RemoveSink(sink Sink) {
	fanoutMu.RLock()
	fanout := currentFanout
	fanoutMu.RUnlock()
	if fanout != nil {
		fanout.removeSink(sink)
	}
}

func init() {
	Logger = log.New(
		log.WithCaller(true),
		log.AddCallerSkip(1),
		log.WithLevel(log.InfoLevel),
	)
}

func InitLogger(logPath string, levelStr string, maxDays int, disableLogColor bool) {
	var base io.Writer
	if logPath == "console" {
		if !disableLogColor {
			base = log.NewConsoleWriter(log.ConsoleConfig{
				Colorful: true,
			}, os.Stdout)
		} else {
			base = os.Stdout
		}
	} else {
		writer := log.NewRotateFileWriter(log.RotateFileConfig{
			FileName: logPath,
			Mode:     log.RotateFileModeDaily,
			MaxDays:  maxDays,
		})
		writer.Init()
		base = writer
	}

	fanout := newFanoutWriter(base)
	fanoutMu.Lock()
	previous := currentFanout
	currentFanout = fanout
	fanoutMu.Unlock()
	if previous != nil {
		previous.mu.RLock()
		sinks := append([]Sink(nil), previous.sinks...)
		previous.mu.RUnlock()
		for _, sink := range sinks {
			_ = sink.Close()
		}
	}

	level, err := log.ParseLevel(levelStr)
	if err != nil {
		level = log.InfoLevel
	}
	Logger = Logger.WithOptions(log.WithOutput(fanout), log.WithLevel(level))
}

func Errorf(format string, v ...any) {
	Logger.Errorf(format, v...)
}

func Warnf(format string, v ...any) {
	Logger.Warnf(format, v...)
}

func Infof(format string, v ...any) {
	Logger.Infof(format, v...)
}

func Debugf(format string, v ...any) {
	Logger.Debugf(format, v...)
}

func Tracef(format string, v ...any) {
	Logger.Tracef(format, v...)
}

func Logf(level log.Level, offset int, format string, v ...any) {
	Logger.Logf(level, offset, format, v...)
}

type WriteLogger struct {
	level  log.Level
	offset int
}

func NewWriteLogger(level log.Level, offset int) *WriteLogger {
	return &WriteLogger{
		level:  level,
		offset: offset,
	}
}

func (w *WriteLogger) Write(p []byte) (n int, err error) {
	Logger.Log(w.level, w.offset, string(bytes.TrimRight(p, "\n")))
	return len(p), nil
}
