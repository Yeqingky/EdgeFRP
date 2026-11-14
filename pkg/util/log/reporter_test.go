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
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHTTPReporterSendsLogs(t *testing.T) {
	bodyCh := make(chan string, 1)
	tokenCh := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		bodyCh <- string(body)
		tokenCh <- req.URL.Query().Get("token")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	reporter, err := NewHTTPReporter(HTTPReporterConfig{
		URL:   server.URL,
		Token: "secret",
	})
	require.NoError(t, err)
	require.NotNil(t, reporter)

	_, err = reporter.WriteLog([]byte("hello\n"), InfoLevel, time.Now())
	require.NoError(t, err)
	require.NoError(t, reporter.Close())

	require.Equal(t, "secret", <-tokenCh)
	require.Contains(t, <-bodyCh, `"message":"hello"`)
}

func TestHTTPReporterDisabled(t *testing.T) {
	reporter, err := NewHTTPReporter(HTTPReporterConfig{})
	require.NoError(t, err)
	require.Nil(t, reporter)
}
