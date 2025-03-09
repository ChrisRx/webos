package ip

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.chrisrx.dev/group"
	"go.chrisrx.dev/x/run"
)

type Option func(*Client)

func WithLogger(l *slog.Logger) Option {
	return func(client *Client) {
		client.logger = l
	}
}

func WithMACAddress(addr string) Option {
	return func(client *Client) {
		client.macAddr = addr
	}
}

type Client struct {
	mu        sync.Mutex
	conn      net.Conn
	connected atomic.Bool
	enc       *Encoder
	q         chan string
	state     State

	ctx    context.Context
	cancel context.CancelFunc

	addr    string
	macAddr string
	logger  *slog.Logger
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

	// The underlying tcp connection is established asynchronously to allow
	// clients to be constructed even if the device is currently unavailable.
	go run.Until(c.ctx, func() bool {
		if err := c.connect(); err != nil {
			c.logger.Error("connection attempt failed", slog.Any("error", err))
			return false
		}
		return true
	}, 1*time.Second)

	// Query-based commands are scheduled periodically to update device state.
	go run.Every(c.ctx, func() {
		if !c.connected.Load() {
			return
		}
		c.Send("GET_MACADDRESS wired")
		c.Send("GET_MACADDRESS wifi")
		c.Send("MUTE_STATE")
		c.Send("CURRENT_VOL")
		c.Send("CURRENT_APP")
		c.Send("GET_IPCONTROL_STATE")
	}, 5*time.Second)

	go c.process()

	return c, nil
}

func (c *Client) connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	logger := c.logger.With(slog.String("addr", c.addr))
	logger.Info("attempting connection...")
	conn, err := (&net.Dialer{Timeout: 1 * time.Second}).DialContext(c.ctx, "tcp", c.addr)
	if err != nil {
		return err
	}
	logger.Info("connection successful")
	if err := conn.(*net.TCPConn).SetKeepAlive(true); err != nil {
		return err
	}
	c.conn = conn
	c.connected.Store(true)
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
				c.connected.Store(false)
				c.logger.Error("cannot send command", slog.Any("error", err))

				for attempt, err := range run.Retry(c.ctx, c.connect, run.RetryOptions{
					InitialInterval: 100 * time.Millisecond,
					MaxInterval:     5 * time.Minute,
				}) {
					c.logger.Debug("connection attempt failed",
						slog.Any("error", err),
						slog.Int("attempt", attempt),
					)
				}
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
				volume := strings.TrimPrefix(resp, "VOL:")
				if volume == "" {
					continue
				}
				i, err := strconv.ParseInt(volume, 10, 64)
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
					logger.Error("invalid command")
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
		// If the read deadline is exceeded, it can be assumed that the write
		// to the connection was successful. This indicates that most likely
		// the connection is still open but the device didn't return a response
		// due to, for example, receiving an invalid command. It is important
		// to return no error in this situation to prevent signaling that the
		// connection should attempt reconnecting.
		if err == io.EOF || errors.Is(err, os.ErrDeadlineExceeded) {
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

func (c *Client) MustSend(ctx context.Context, command string) error {
	select {
	case c.q <- command:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

const defaultCommandTimeout = 100 * time.Millisecond

func (c *Client) Send(command string) {
	ctx, cancel := context.WithTimeout(c.ctx, defaultCommandTimeout)
	defer cancel()

	select {
	case c.q <- command:
	case <-ctx.Done():
		c.logger.Debug("sending command timed out",
			slog.String("command", command),
			slog.Duration("timeout", defaultCommandTimeout),
		)
	}
}

var (
	inputs = []string{
		"hdmi1",
		"hdmi2",
		"hdmi3",
		"hdmi4",
		"atv",
		"dtv",
		"av1",
		"component1",
	}

	inputNameMap = map[string]string{
		"hdmi1":      "hdmi1", // com.webos.app.hdmi1
		"hdmi2":      "hdmi2",
		"hdmi3":      "hdmi3",
		"hdmi4":      "hdmi4",
		"atv":        "atv",
		"dtv":        "dtv",
		"av1":        "av1",
		"component1": "component1",
		"youtube":    "youtube.leanback.v4",
	}
)

func (c *Client) ChangeInput(input string) error {
	name, ok := inputNameMap[input]
	if !ok {
		return fmt.Errorf("invalid input name: %q", input)
	}
	var command string
	if slices.Contains(inputs, name) {
		command = fmt.Sprintf("INPUT_SELECT %s", name)
	} else {
		command = fmt.Sprintf("APP_LAUNCH %s", name)
	}
	c.logger.Debug("input command",
		slog.String("input", input),
		slog.String("command", command),
	)
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()

	if err := c.MustSend(ctx, command); err != nil {
		return err
	}

	ready := make(chan struct{})
	go run.Until(ctx, func() bool {
		if strings.Contains(c.state.CurrentApp, input) {
			close(ready)
			return true
		}
		c.Send(command)
		return false
	}, 100*time.Millisecond)

	select {
	case <-ready:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("timed out waiting for input change: %q", input)
	}
}

func (c *Client) PowerOff() error {
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()

	return c.MustSend(ctx, "POWER off")
}

func (c *Client) PowerOn() error {
	if c.state.MACAddressWifi == "" && c.state.MACAddressWired == "" && c.macAddr == "" {
		return fmt.Errorf("mac address must be provided to send WOL packet")
	}
	g := group.New(c.ctx)
	for _, addr := range []string{c.state.MACAddressWifi, c.state.MACAddressWired, c.macAddr} {
		if addr == "" {
			continue
		}
		g.Go(func(ctx context.Context) error {
			return SendWOLPacket(strings.ToLower(addr), 5*time.Second)
		})
	}
	return g.Wait()
}
