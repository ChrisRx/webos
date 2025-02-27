package ssap

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.chrisrx.dev/x/log"
	"go.chrisrx.dev/x/run"
)

type Client struct {
	conn *Conn

	mu    sync.Mutex
	input *Conn
}

func New(ctx context.Context, addr, key string) (*Client, error) {
	conn, err := NewConn(ctx, addr)
	if err != nil {
		return nil, err
	}
	if err := conn.Register(ctx, key); err != nil {
		return nil, err
	}
	c := &Client{
		conn: conn,
	}
	if err := c.connect(ctx); err != nil {
		return nil, err
	}
	c.input.ws.SetCloseHandler(func(code int, text string) error {
		log.FromContext(ctx).Info("input socket closed, attempting reconnect ...",
			slog.Int("code", code),
			slog.String("text", text),
		)
		return c.connect(ctx)
	})
	return c, nil
}

func (c *Client) connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	resp, err := c.conn.Request(ctx, GetPointerInputSocket, nil)
	if err != nil {
		return err
	}
	sockPath, ok := resp["socketPath"]
	if !ok {
		return fmt.Errorf("failed to get pointer input socket")
	}
	c.input, err = NewConn(ctx, sockPath.(string))
	if err != nil {
		return err
	}
	c.input.ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	c.input.ws.SetPongHandler(func(string) error {
		c.input.ws.SetReadDeadline(time.Now().Add(5 * time.Second))
		return nil
	})

	go run.Every(ctx, func() error {
		return c.input.ws.WriteMessage(websocket.PingMessage, nil)
	}, 1*time.Second)

	go func() {
		for {
			_, data, err := c.input.ws.ReadMessage()
			if err != nil {
				fmt.Printf("ReadMessage error: %v\n", err)
				return
			}
			fmt.Printf("ReadMessage data: %v\n", data)
		}
	}()
	return nil
}

func (c *Client) Request(ctx context.Context, command Command, payload map[string]any) (map[string]any, error) {
	return c.conn.Request(ctx, command, payload)
}

func (c *Client) Button(name string) error {
	body := fmt.Sprintf("type:button\nname:%s\n\n", name)
	if err := c.input.ws.WriteMessage(websocket.TextMessage, []byte(body)); err != nil {
		return err
	}
	return nil
}
