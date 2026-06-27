package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"sekailink/server/internal/config"
	"sekailink/server/internal/protocol"
	"sekailink/server/internal/proxy"
)

const (
	pollInterval   = 1 * time.Second
	heartbeatEvery = 30 * time.Second
)

type Client struct {
	cfg   *config.Config
	proxy *proxy.Proxy
	done  chan struct{}
	http  *http.Client
}

func New(cfg *config.Config) *Client {
	target := fmt.Sprintf("http://%s:%d", cfg.LocalHost, cfg.LocalPort)
	return &Client{
		cfg:   cfg,
		proxy: proxy.New(target, cfg.MaxBodySizeMB),
		done:  make(chan struct{}),
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) Run() error {
	poll := time.NewTicker(pollInterval)
	hb := time.NewTicker(heartbeatEvery)
	defer poll.Stop()
	defer hb.Stop()

	log.Printf("started for code: %s", c.cfg.ExtractCode())

	for {
		select {
		case <-c.done:
			log.Println("shutting down")
			return nil
		case <-hb.C:
			c.sendHeartbeat()
		case <-poll.C:
			c.pollAndProcess()
		}
	}
}

func (c *Client) pollAndProcess() {
	code := c.cfg.ExtractCode()
	relay := c.cfg.Relay

	url := fmt.Sprintf("%s/api/gateway/pending?code=%s", relay, code)

	resp, err := c.http.Get(url)
	if err != nil {
		log.Printf("poll error: %v", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("read poll response: %v", err)
		return
	}

	var result struct {
		Requests []protocol.PendingRequest `json:"requests"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("decode poll response: %v", err)
		return
	}

	for _, req := range result.Requests {
		c.handleRequest(req)
	}
}

func (c *Client) handleRequest(req protocol.PendingRequest) {
	var reqBody string
	if req.Body != nil {
		reqBody = *req.Body
	}

	proxyResult, err := c.proxy.Forward(req.Method, req.Path, req.Headers, reqBody)
	if err != nil {
		log.Printf("proxy error for %s: %v", req.ID, err)
		c.submitError(req.ID, err)
		return
	}

	c.submitResponse(req.ID, proxyResult)
}

func (c *Client) submitResponse(requestID string, result *proxy.Result) {
	relay := c.cfg.Relay

	payload := protocol.SubmitResponse{
		RequestID:  requestID,
		StatusCode: result.StatusCode,
		Headers:    result.Headers,
	}
	if result.Body != "" {
		payload.Body = &result.Body
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("marshal response: %v", err)
		return
	}

	resp, err := c.http.Post(
		fmt.Sprintf("%s/api/gateway/respond", relay),
		"application/json",
		bytes.NewReader(data),
	)
	if err != nil {
		log.Printf("submit response error: %v", err)
		return
	}
	resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("submit response status: %d", resp.StatusCode)
	}
}

func (c *Client) submitError(requestID string, err error) {
	relay := c.cfg.Relay
	errMsg := fmt.Sprintf("proxy error: %v", err)

	payload := protocol.SubmitResponse{
		RequestID:  requestID,
		StatusCode: 502,
		Headers:    map[string]string{"content-type": "text/plain"},
		Body:       &errMsg,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	resp, err := c.http.Post(
		fmt.Sprintf("%s/api/gateway/respond", relay),
		"application/json",
		bytes.NewReader(data),
	)
	if err != nil {
		log.Printf("submit error response: %v", err)
		return
	}
	resp.Body.Close()
}

func (c *Client) sendHeartbeat() {
	relay := c.cfg.Relay
	code := c.cfg.ExtractCode()

	payload := protocol.HeartbeatBody{Code: code}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	resp, err := c.http.Post(
		fmt.Sprintf("%s/api/gateway/heartbeat", relay),
		"application/json",
		bytes.NewReader(data),
	)
	if err != nil {
		log.Printf("heartbeat error: %v", err)
		return
	}
	resp.Body.Close()
}

func (c *Client) Stop() {
	close(c.done)
}
