package client

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"sekailink/server/internal/config"
	"sekailink/server/internal/protocol"
	"sekailink/server/internal/proxy"

	"github.com/gorilla/websocket"
)

const (
	reconnectDelay  = 5 * time.Second
	writeTimeout    = 30 * time.Second
	maxConcurrent   = 10
)

type Client struct {
	cfg   *config.Config
	p     *proxy.Proxy
	done  chan struct{}
	conn  *websocket.Conn
	connMu sync.Mutex
	sem   chan struct{}
}

func New(cfg *config.Config) *Client {
	target := fmt.Sprintf("http://%s:%d", cfg.LocalHost, cfg.LocalPort)
	return &Client{
		cfg:  cfg,
		p:    proxy.New(target, cfg.MaxBodySizeMB),
		done: make(chan struct{}),
		sem:  make(chan struct{}, maxConcurrent),
	}
}

func (c *Client) send(msg protocol.Message) error {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	return c.conn.WriteJSON(msg)
}

func (c *Client) wsURL() string {
	scheme := c.cfg.RelayScheme()
	host := c.cfg.RelayHost()
	u := url.URL{Scheme: scheme, Host: host, Path: "/ws"}
	return u.String()
}

func (c *Client) Run() error {
	log.Printf("connecting to relay: %s", c.wsURL())
	log.Printf("local target: http://%s:%d", c.cfg.LocalHost, c.cfg.LocalPort)

	for {
		select {
		case <-c.done:
			return nil
		default:
		}

		err := c.connectAndServe()
		if err != nil {
			log.Printf("connection error: %v — reconnecting in %v", err, reconnectDelay)
		}

		select {
		case <-c.done:
			return nil
		case <-time.After(reconnectDelay):
		}
	}
}

func (c *Client) connectAndServe() error {
	conn, _, err := websocket.DefaultDialer.Dial(c.wsURL(), nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()

	defer func() {
		c.connMu.Lock()
		c.conn = nil
		c.connMu.Unlock()
		conn.Close()
	}()

	if err := c.authenticate(conn); err != nil {
		return err
	}

	log.Println("authenticated, waiting for requests")

	for {
		select {
		case <-c.done:
			return nil
		default:
		}

		var msg protocol.Message
		if err := conn.ReadJSON(&msg); err != nil {
			return fmt.Errorf("read: %w", err)
		}

		switch msg.Type {
		case protocol.MsgRequest:
			var req protocol.RequestPayload
			if err := json.Unmarshal([]byte(msg.Payload), &req); err != nil {
				log.Printf("invalid request payload: %v", err)
				continue
			}

			select {
			case c.sem <- struct{}{}:
			case <-c.done:
				return nil
			}

			if req.Stream {
				go func() {
					defer func() { <-c.sem }()
					c.handleStreamRequest(req)
				}()
			} else {
				go func() {
					defer func() { <-c.sem }()
					c.handleRequest(req)
				}()
			}

		case protocol.MsgHeartbeat:
			// gorilla/websocket handles ping/pong natively

		default:
			log.Printf("unexpected message type: %s", msg.Type)
		}
	}
}

func (c *Client) authenticate(conn *websocket.Conn) error {
	payload, err := json.Marshal(protocol.AuthPayload{
		Code: c.cfg.ExtractCode(),
		Key:  c.cfg.APIKey,
	})
	if err != nil {
		return fmt.Errorf("marshal auth: %w", err)
	}

	if err := conn.WriteJSON(protocol.Message{
		Type:    protocol.MsgAuth,
		Payload: string(payload),
	}); err != nil {
		return fmt.Errorf("write auth: %w", err)
	}

	var resp protocol.Message
	if err := conn.ReadJSON(&resp); err != nil {
		return fmt.Errorf("read auth response: %w", err)
	}

	switch resp.Type {
	case protocol.MsgAuthOK:
		return nil
	case protocol.MsgError:
		return fmt.Errorf("auth rejected: %s", resp.Payload)
	default:
		return fmt.Errorf("unexpected auth response type: %s", resp.Type)
	}
}

func (c *Client) handleRequest(req protocol.RequestPayload) {
	var reqBody string
	if req.Body != nil {
		reqBody = *req.Body
	}

	result, err := c.p.Forward(req.Method, req.Path, req.Headers, reqBody)
	if err != nil {
		log.Printf("proxy error for %s: %v", req.ID, err)
		c.sendError(req.ID, err)
		return
	}

	payload, err := json.Marshal(protocol.ResponsePayload{
		ID:         req.ID,
		StatusCode: result.StatusCode,
		Headers:    result.Headers,
		Body:       &result.Body,
	})
	if err != nil {
		log.Printf("marshal response for %s: %v", req.ID, err)
		return
	}

	if err := c.send(protocol.Message{
		Type:    protocol.MsgResponse,
		Payload: string(payload),
	}); err != nil {
		log.Printf("send response for %s: %v", req.ID, err)
	}
}

func (c *Client) handleStreamRequest(req protocol.RequestPayload) {
	var reqBody string
	if req.Body != nil {
		reqBody = *req.Body
	}

	ch, err := c.p.ForwardStream(req.Method, req.Path, req.Headers, reqBody)
	if err != nil {
		log.Printf("proxy stream error for %s: %v", req.ID, err)
		c.sendError(req.ID, err)
		return
	}

	for chunk := range ch {
		if chunk.Err != nil {
			log.Printf("stream error for %s: %v", req.ID, chunk.Err)
			break
		}

		payload, err := json.Marshal(protocol.StreamChunkPayload{
			ID:   req.ID,
			Data: chunk.Data,
		})
		if err != nil {
			continue
		}

		if err := c.send(protocol.Message{
			Type:    protocol.MsgStreamChunk,
			Payload: string(payload),
		}); err != nil {
			log.Printf("send stream chunk for %s: %v", req.ID, err)
			return
		}
	}

	endPayload, _ := json.Marshal(protocol.StreamEndPayload{ID: req.ID})
	c.send(protocol.Message{
		Type:    protocol.MsgStreamEnd,
		Payload: string(endPayload),
	})
}

func (c *Client) sendError(requestID string, err error) {
	errMsg := fmt.Sprintf("proxy error: %v", err)
	payload, _ := json.Marshal(protocol.ResponsePayload{
		ID:         requestID,
		StatusCode: 502,
		Headers:    map[string]string{"content-type": "text/plain"},
		Body:       &errMsg,
	})
	c.send(protocol.Message{
		Type:    protocol.MsgResponse,
		Payload: string(payload),
	})
}

func (c *Client) Stop() {
	close(c.done)
	c.connMu.Lock()
	if c.conn != nil {
		c.conn.Close()
	}
	c.connMu.Unlock()
}
