package client

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/rpc"
	"sync"
	"time"

	"github.com/hashicorp/go-msgpack/codec"
	"github.com/hashicorp/yamux"
)

const (
	// clientPreamble is the preamble to send before upgrading
	// the connection into a SCADA version 1 connection.
	clientPreamble = "SCADA 1\n"
)

var (
	// msgpackHandle is a shared handle for encoding/decoding of RPC messages
	msgpackHandle = &codec.MsgpackHandle{}
)

// Client is a SCADA compatible client. This is a bare bones client that
// only handles the framing and RPC protocol. Higher-level clients should
// be prefered.
type Client struct {
	conn   net.Conn
	client *yamux.Session

	closed     bool
	closedLock sync.Mutex
}

// Dial is used to establish a new connection over TCP
func Dial(addr string) (*Client, error) {
	// Dial a connection
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, err
	}
	return initClient(conn)
}

// DialTLS is used to establish a new connection using TLS/TCP
func DialTLS(addr string, tlsConf *tls.Config) (*Client, error) {
	// Dial a connection
	conn, err := tls.Dial("tcp", addr, tlsConf)
	if err != nil {
		return nil, err
	}
	return initClient(conn)
}

// initClient does the common initialization
func initClient(conn net.Conn) (*Client, error) {
	// Send the preamble
	_, err := conn.Write([]byte(clientPreamble))
	if err != nil {
		return nil, fmt.Errorf("preamble write failed: %v", err)
	}

	// Wrap the connection in yamux for multiplexing
	client, _ := yamux.Client(conn, yamux.DefaultConfig())

	// Create the client
	c := &Client{
		conn:   conn,
		client: client,
	}
	return c, nil
}

// Close is used to terminate the client connection
func (c *Client) Close() error {
	c.closedLock.Lock()
	defer c.closedLock.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true
	c.client.GoAway() // Notify the other side of the close
	return c.client.Close()
}

// RPC is used to perform an RPC
func (c *Client) RPC(method string, args interface{}, resp interface{}) error {
	// Get a stream
	stream, err := c.Open()
	if err != nil {
		return fmt.Errorf("failed to open stream: %v", err)
	}
	defer stream.Close()

	// Create the RPC client
	cc := codec.GoRpc.ClientCodec(stream, msgpackHandle)
	client := rpc.NewClientWithCodec(cc)
	return client.Call(method, args, resp)
}

// Accept is used to accept an incoming connection
func (c *Client) Accept() (net.Conn, error) {
	return c.client.Accept()
}

// Open is used to open an outgoing connection
func (c *Client) Open() (net.Conn, error) {
	return c.client.Open()
}
