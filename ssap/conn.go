package ssap

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Conn struct {
	ws *websocket.Conn
}

func NewConn(ctx context.Context, addr string) (*Conn, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	if strings.HasPrefix(addr, "wss://") {
		dialer.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	ws, resp, err := dialer.DialContext(ctx, addr, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return &Conn{ws: ws}, nil
}

func (c *Conn) Register(ctx context.Context, key string) error {
	resp, err := c.SendMessage(ctx, &Message{
		Type: RegisterMessageType,
		ID:   uuid.NewString(),
		Payload: map[string]any{
			"client-key": key,
		},
	})
	if err != nil {
		return err
	}
	if resp.Type != RegisteredMessageType {
		return fmt.Errorf("expected Registered, received %T", resp.Type)
	}
	return nil
}

func (c *Conn) Request(ctx context.Context, command Command, payload map[string]any) (map[string]any, error) {
	resp, err := c.SendMessage(ctx, &Message{
		Type:    RequestMessageType,
		ID:      uuid.NewString(),
		URI:     command,
		Payload: payload,
	})
	if err != nil {
		return nil, err
	}
	if resp.Type != ResponseMessageType {
		return nil, fmt.Errorf("expected Response, received %T", resp.Type)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("received Response error: %v", resp.Error)
	}
	// check id
	return resp.Payload, nil
}

func (c *Conn) SendMessage(ctx context.Context, msg *Message) (*Message, error) {
	result := make(chan *Message)
	errch := make(chan error, 1)
	go func() {
		defer close(result)
		defer close(errch)

		_, data, err := c.ws.ReadMessage()
		if err != nil {
			m := websocket.FormatCloseMessage(websocket.CloseNormalClosure, fmt.Sprintf("%v", err))
			if e, ok := err.(*websocket.CloseError); ok {
				if e.Code != websocket.CloseNoStatusReceived {
					m = websocket.FormatCloseMessage(e.Code, e.Text)
				}
			}
			errch <- err
			_ = c.ws.WriteMessage(websocket.CloseMessage, m)
			return
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			errch <- err
			return
		}
		result <- &msg
	}()

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	if err := c.ws.WriteMessage(websocket.TextMessage, data); err != nil {
		return nil, err
	}

	select {
	case msg := <-result:
		return msg, nil
	case err := <-errch:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
