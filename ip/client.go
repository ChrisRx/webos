package ip

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.chrisrx.dev/x/run"
)

type Option func(*Client)

func WithLogger(l *slog.Logger) Option {
	return func(client *Client) {
		client.logger = l
	}
}

type Client struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu    sync.Mutex
	conn  net.Conn
	enc   *Encoder
	q     chan string
	state State

	addr   string
	logger *slog.Logger
}

func New(addr, key string, opts ...Option) (*Client, error) {
	enc, err := NewEncoder(key)
	if err != nil {
		return nil, err
	}
	c := &Client{
		enc:    enc,
		q:      make(chan string, 10),
		addr:   addr,
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(c)
	}
	c.ctx, c.cancel = context.WithCancel(context.TODO())

	if err := c.connect(); err != nil {
		return nil, err
	}

	go c.process()
	go run.Every(c.ctx, func() error {
		ctx, cancel := context.WithTimeout(c.ctx, 1*time.Second)
		defer cancel()
		_ = c.Send(ctx, "GET_MACADDRESS wired")
		_ = c.Send(ctx, "GET_MACADDRESS wifi")
		_ = c.Send(ctx, "MUTE_STATE")
		_ = c.Send(ctx, "CURRENT_VOL")
		_ = c.Send(ctx, "CURRENT_APP")
		_ = c.Send(ctx, "GET_IPCONTROL_STATE")
		return nil
	}, 5*time.Second)

	return c, nil
}

func (c *Client) connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	logger := c.logger.With(slog.String("addr", c.addr))
	logger.Info("attempting connection...")
	conn, err := net.Dial("tcp", c.addr)
	if err != nil {
		return err
	}
	logger.Info("connection successful")
	if err := conn.(*net.TCPConn).SetKeepAlive(true); err != nil {
		return err
	}
	c.conn = conn
	return nil
}

func (c *Client) Close() error {
	c.cancel()
	return c.conn.Close()
}

type State struct {
	MACAddressWired string
	MACAddressWifi  string
	MuteState       bool
	CurrentVolume   int64
	CurrentApp      string
}

func (c *Client) GetState() State {
	return c.state
}

const sendInterval = 10 * time.Millisecond

func (c *Client) process() {
	timer := time.NewTimer(sendInterval)
	defer timer.Stop()

	for {
		select {
		case cmd := <-c.q:
			<-timer.C
			timer.Reset(sendInterval)

			logger := c.logger.With(slog.String("command", cmd))
			resp, err := c.send(cmd)
			if err != nil {
				c.logger.Error("cannot send command", slog.Any("error", err))
				_ = run.Until(c.ctx, c.connect, 1*time.Second)
				continue
			}
			logger = logger.With(slog.String("response", resp))
			switch cmd {
			case "GET_MACADDRESS wired":
				c.state.MACAddressWired = resp
			case "GET_MACADDRESS wifi":
				c.state.MACAddressWifi = resp
			case "MUTE_STATE":
				c.state.MuteState = parseBool(strings.TrimPrefix(resp, "MUTE:"))
			case "CURRENT_VOL":
				i, err := strconv.ParseInt(strings.TrimPrefix(resp, "VOL:"), 10, 64)
				if err != nil {
					logger.Error("cannot parse command response", slog.Any("error", err))
					continue
				}
				c.state.CurrentVolume = i
			case "CURRENT_APP":
				c.state.CurrentApp = strings.TrimPrefix(resp, "APP:")
			case "GET_IPCONTROL_STATE":
				if !parseBool(resp) {
					logger.Error("ip control state is off")
				}
			default:
				if resp != "OK" {
					c.logger.Error("invalid command")
				}
			}
		case <-c.ctx.Done():
			return
		}
	}
}

func parseBool(resp string) bool {
	switch strings.ToLower(resp) {
	case "on":
		return true
	default:
		return false
	}
}

func (c *Client) send(command string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.conn.SetWriteDeadline(time.Now().Add(5 * time.Millisecond)); err != nil {
		return "", err
	}
	if _, err := c.conn.Write(c.enc.Encode([]byte(command))); err != nil {
		return "", err
	}
	if err := c.conn.SetReadDeadline(time.Now().Add(1000 * time.Millisecond)); err != nil {
		return "", err
	}
	b := make([]byte, 1024)
	n, err := c.conn.Read(b)
	if err != nil {
		if err == io.EOF {
			return "", nil
		}
		return "", err
	}
	plaintext, err := c.enc.Decode(b[:n])
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(plaintext)), nil
}

func (c *Client) Send(ctx context.Context, command string) error {
	select {
	case c.q <- command:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
