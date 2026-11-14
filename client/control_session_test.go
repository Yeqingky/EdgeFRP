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

package client

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fatedier/frp/pkg/auth"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/msg"
	"github.com/fatedier/frp/pkg/proto/wire"
)

type testConnector struct {
	conn   net.Conn
	closed atomic.Bool
}

func (c *testConnector) Open() error {
	return nil
}

func (c *testConnector) Connect() (net.Conn, error) {
	return c.conn, nil
}

func (c *testConnector) Close() error {
	c.closed.Store(true)
	return nil
}

type trackingConn struct {
	net.Conn
	closed atomic.Bool
}

func (c *trackingConn) Close() error {
	c.closed.Store(true)
	return c.Conn.Close()
}

func newTestControlSessionDialer(t *testing.T, connector Connector, clientSpec *msg.ClientSpec) *controlSessionDialer {
	t.Helper()

	authRuntime, err := auth.BuildClientAuth(&v1.AuthClientConfig{
		Method: v1.AuthMethodToken,
		Token:  "token",
	})
	require.NoError(t, err)

	return &controlSessionDialer{
		ctx: context.Background(),
		common: &v1.ClientCommonConfig{
			User: "test-user",
			Transport: v1.ClientTransportConfig{
				Protocol: "tcp",
			},
		},
		auth:       authRuntime,
		clientSpec: clientSpec,
		connectorCreator: func(context.Context, *v1.ClientCommonConfig) Connector {
			return connector
		},
	}
}

// serverV2Login performs the server side of the v2 handshake and returns the
// negotiated message read writer.
func serverV2Login(t *testing.T, conn net.Conn) *msg.V2ReadWriter {
	t.Helper()

	magic := make([]byte, len(wire.MagicV2))
	_, err := io.ReadFull(conn, magic)
	require.NoError(t, err)
	require.Equal(t, wire.MagicV2, string(magic))

	wireConn := wire.NewConn(conn)
	clientHelloFrame, err := wireConn.ReadFrame()
	require.NoError(t, err)
	require.Equal(t, wire.FrameTypeClientHello, clientHelloFrame.Type)

	var hello wire.ClientHello
	require.NoError(t, wireConn.UnmarshalFrame(clientHelloFrame, &hello))
	require.NoError(t, wire.ValidateClientHello(hello))

	rw := msg.NewV2ReadWriterWithConn(wireConn)
	var loginMsg msg.Login
	require.NoError(t, rw.ReadMsgInto(&loginMsg))

	serverHello, err := wire.NewServerHello(hello)
	require.NoError(t, err)
	serverHelloFrame, err := wire.NewJSONFrame(wire.FrameTypeServerHello, serverHello)
	require.NoError(t, err)
	require.NoError(t, wireConn.WriteFrame(serverHelloFrame))

	return rw
}

func TestControlSessionDialerDial(t *testing.T) {
	clientRaw, serverRaw := net.Pipe()
	defer serverRaw.Close()

	connector := &testConnector{conn: &trackingConn{Conn: clientRaw}}
	serverErrCh := make(chan error, 1)
	go func() {
		rw := serverV2Login(t, serverRaw)
		serverErrCh <- rw.WriteMsg(&msg.LoginResp{RunID: "run-v2"})
	}()

	dialer := newTestControlSessionDialer(t, connector, nil)
	sessionCtx, err := dialer.Dial("")
	require.NoError(t, err)
	defer sessionCtx.Conn.Close()
	defer sessionCtx.Connector.Close()

	require.Equal(t, "run-v2", sessionCtx.RunID)
	require.NotNil(t, sessionCtx.Conn)
	require.NotNil(t, sessionCtx.Connector)
	require.False(t, connector.closed.Load())
	require.NoError(t, <-serverErrCh)
}

func TestControlSessionDialerDialLoginErrorClosesResources(t *testing.T) {
	clientRaw, serverRaw := net.Pipe()
	defer serverRaw.Close()

	clientConn := &trackingConn{Conn: clientRaw}
	connector := &testConnector{conn: clientConn}
	serverErrCh := make(chan error, 1)
	go func() {
		rw := serverV2Login(t, serverRaw)
		serverErrCh <- rw.WriteMsg(&msg.LoginResp{Error: "login denied"})
	}()

	dialer := newTestControlSessionDialer(t, connector, nil)
	sessionCtx, err := dialer.Dial("")
	require.Nil(t, sessionCtx)
	require.ErrorContains(t, err, "login denied")
	require.True(t, clientConn.closed.Load())
	require.True(t, connector.closed.Load())
	require.NoError(t, <-serverErrCh)
}

func TestControlSessionDialerDialSSHTunnelSkipsControlEncryption(t *testing.T) {
	clientRaw, serverRaw := net.Pipe()
	defer serverRaw.Close()

	connector := &testConnector{conn: &trackingConn{Conn: clientRaw}}
	serverErrCh := make(chan error, 1)
	go func() {
		rw := serverV2Login(t, serverRaw)
		require.NoError(t, rw.WriteMsg(&msg.LoginResp{RunID: "run-ssh-tunnel"}))

		_ = serverRaw.SetReadDeadline(time.Now().Add(time.Second))
		var ping msg.Ping
		if err := rw.ReadMsgInto(&ping); err != nil {
			serverErrCh <- err
			return
		}
		serverErrCh <- nil
	}()

	dialer := newTestControlSessionDialer(t, connector, &msg.ClientSpec{Type: "ssh-tunnel"})
	sessionCtx, err := dialer.Dial("")
	require.NoError(t, err)
	defer sessionCtx.Conn.Close()
	defer sessionCtx.Connector.Close()

	require.Equal(t, "run-ssh-tunnel", sessionCtx.RunID)
	require.NoError(t, sessionCtx.Conn.WriteMsg(&msg.Ping{}))
	require.NoError(t, <-serverErrCh)
}

func TestControlSessionDialerDialHandshakeFailure(t *testing.T) {
	clientRaw, serverRaw := net.Pipe()
	defer serverRaw.Close()

	connector := &testConnector{conn: &trackingConn{Conn: clientRaw}}
	serverErrCh := make(chan error, 1)
	go func() {
		magic := make([]byte, len(wire.MagicV2))
		_, err := io.ReadFull(serverRaw, magic)
		if err != nil {
			serverErrCh <- err
			return
		}
		_ = serverRaw.Close()
		serverErrCh <- nil
	}()

	dialer := newTestControlSessionDialer(t, connector, nil)
	sessionCtx, err := dialer.Dial("")
	require.Nil(t, sessionCtx)
	require.Error(t, err)
	require.NoError(t, <-serverErrCh)
}
