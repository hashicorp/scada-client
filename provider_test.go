package client

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/rpc"
	"reflect"
	"testing"
	"time"

	"github.com/hashicorp/go-msgpack/codec"
	"github.com/hashicorp/yamux"
)

func testProviderConfig() *ProviderConfig {
	return &ProviderConfig{
		Endpoint: "127.0.0.1:65500", // Blackhole
		Service: &ProviderService{
			Service:        "test",
			ServiceVersion: "v1.0",
			ResourceType:   "test",
		},
		ResourceGroup: "hashicorp/test",
		Token:         "abcdefg",
	}
}

func TestValidate(t *testing.T) {
	type tcase struct {
		valid bool
		inp   *ProviderConfig
	}
	tcases := []*tcase{
		&tcase{
			false,
			&ProviderConfig{},
		},
		&tcase{
			false,
			&ProviderConfig{
				Service: &ProviderService{},
			},
		},
		&tcase{
			false,
			&ProviderConfig{
				Service: &ProviderService{
					Service: "foo",
				},
			},
		},
		&tcase{
			false,
			&ProviderConfig{
				Service: &ProviderService{
					Service:        "foo",
					ServiceVersion: "v1.0",
				},
			},
		},
		&tcase{
			false,
			&ProviderConfig{
				Service: &ProviderService{
					Service:        "foo",
					ServiceVersion: "v1.0",
					ResourceType:   "foo",
				},
			},
		},
		&tcase{
			false,
			&ProviderConfig{
				Service: &ProviderService{
					Service:        "foo",
					ServiceVersion: "v1.0",
					ResourceType:   "foo",
				},
				ResourceGroup: "hashicorp/test",
			},
		},
		&tcase{
			true,
			&ProviderConfig{
				Service: &ProviderService{
					Service:        "foo",
					ServiceVersion: "v1.0",
					ResourceType:   "foo",
				},
				ResourceGroup: "hashicorp/test",
				Token:         "abcdefg",
			},
		},
		&tcase{
			false,
			&ProviderConfig{
				Service: &ProviderService{
					Service:        "foo",
					ServiceVersion: "v1.0",
					ResourceType:   "foo",
					Capabilities: map[string]int{
						"http": 1,
					},
				},
				ResourceGroup: "hashicorp/test",
				Token:         "abcdefg",
			},
		},
		&tcase{
			true,
			&ProviderConfig{
				Service: &ProviderService{
					Service:        "foo",
					ServiceVersion: "v1.0",
					ResourceType:   "foo",
					Capabilities: map[string]int{
						"http": 1,
					},
				},
				ResourceGroup: "hashicorp/test",
				Token:         "abcdefg",
				Handlers: map[string]CapabilityProvider{
					"http": nil,
				},
			},
		},
	}
	for idx, tc := range tcases {
		err := validateConfig(tc.inp)
		if (err == nil) != tc.valid {
			t.Fatalf("%d err: %v", idx, err)
		}
	}
}

func TestProvider_StartStop(t *testing.T) {
	p, err := NewProvider(testProviderConfig())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if p.IsShutdown() {
		t.Fatalf("bad")
	}

	p.Shutdown()

	if !p.IsShutdown() {
		t.Fatalf("bad")
	}
}

func TestProvider_backoff(t *testing.T) {
	p, err := NewProvider(testProviderConfig())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	defer p.Shutdown()

	if b := p.backoffDuration(); b != DefaultBackoff {
		t.Fatalf("bad: %v", b)
	}

	// Set a new minimum
	p.backoffLock.Lock()
	p.backoff = 60 * time.Second
	p.backoffLock.Unlock()

	if b := p.backoffDuration(); b != 60*time.Second {
		t.Fatalf("bad: %v", b)
	}

	// Set no retry
	p.backoffLock.Lock()
	p.noRetry = true
	p.backoffLock.Unlock()

	if b := p.backoffDuration(); b != 0 {
		t.Fatalf("bad: %v", b)
	}
}

func testTLSListener(t *testing.T) (string, net.Listener) {
	list, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", list.Addr().(*net.TCPAddr).Port)

	// Load the certificates
	cert, err := tls.LoadX509KeyPair("./test/cert.pem", "./test/key.pem")
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	// Create the tls config
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	// TLS listener
	tlsList := tls.NewListener(list, tlsConfig)
	return addr, tlsList
}

type TestHandshake struct {
	t      *testing.T
	expect *HandshakeRequest
}

func (t *TestHandshake) Handshake(arg *HandshakeRequest, resp *HandshakeResponse) error {
	if !reflect.DeepEqual(arg, t.expect) {
		t.t.Fatalf("bad: %#v %#v", *arg, *t.expect)
	}
	resp.Authenticated = true
	resp.SessionID = "foobarbaz"
	return nil
}

func testHandshake(t *testing.T, list net.Listener, expect *HandshakeRequest) {
	client, err := list.Accept()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	defer client.Close()

	preamble := make([]byte, len(clientPreamble))
	n, err := client.Read(preamble)
	if err != nil || n != len(preamble) {
		t.Fatalf("err: %v", err)
	}

	server, _ := yamux.Server(client, yamux.DefaultConfig())
	conn, err := server.Accept()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	defer conn.Close()
	rpcCodec := codec.GoRpc.ServerCodec(conn, &codec.MsgpackHandle{})

	rpcSrv := rpc.NewServer()
	rpcSrv.RegisterName("Session", &TestHandshake{t, expect})

	err = rpcSrv.ServeRequest(rpcCodec)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestProvider_Setup(t *testing.T) {
	addr, list := testTLSListener(t)
	defer list.Close()

	config := testProviderConfig()
	config.Endpoint = addr
	config.TLSConfig = &tls.Config{
		InsecureSkipVerify: true,
	}

	p, err := NewProvider(config)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	defer p.Shutdown()

	exp := &HandshakeRequest{
		Service:        "test",
		ServiceVersion: "v1.0",
		Capabilities:   nil,
		Meta:           nil,
		ResourceType:   "test",
		ResourceGroup:  "hashicorp/test",
		Token:          "abcdefg",
	}
	testHandshake(t, list, exp)

	start := time.Now()
	for time.Now().Sub(start) < time.Second {
		if p.SessionID() != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if p.SessionID() != "foobarbaz" {
		t.Fatalf("bad: %v", p.SessionID())
	}
	if !p.SessionAuthenticated() {
		t.Fatalf("bad: %v", p.SessionAuthenticated())
	}
}
