package proxy

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Proxy struct {
	Target string
	client *http.Client
}

func New(target string) *Proxy {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
	}
	return &Proxy{
		Target: target,
		client: &http.Client{
			Timeout:   120 * time.Second,
			Transport: transport,
		},
	}
}

type Result struct {
	StatusCode int
	Headers    map[string]string
	Body       string
}

func (p *Proxy) Forward(method, path string, headers map[string]string, body string) (*Result, error) {
	url := fmt.Sprintf("%s%s", p.Target, path)

	var reqBody io.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	for k, v := range headers {
		key := strings.ToLower(k)
		if key == "host" || key == "connection" || key == "upgrade" {
			continue
		}
		req.Header.Set(k, v)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("forwarding request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	outHeaders := make(map[string]string)
	for k, v := range resp.Header {
		outHeaders[k] = v[0]
	}

	return &Result{
		StatusCode: resp.StatusCode,
		Headers:    outHeaders,
		Body:       string(respBody),
	}, nil
}
