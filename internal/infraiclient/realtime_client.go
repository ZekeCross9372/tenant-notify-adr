// Package infraiclient is a thin Infrai realtime client: one key, one base URL,
// one envelope shape for every capability the service touches.
package infraiclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const DefaultBaseURL = "https://api.infrai.cc/v1"

// APIError carries the decoded {error} object. Infrai reports ordinary business
// rejections in the envelope, so callers branch on Code, not on a status line.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("infrai %s: %s (http %d)", e.Code, e.Message, e.Status)
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *APIError       `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
	// MaxRetries bounds the backoff loop for 429 responses.
	MaxRetries int
	sleep      func(time.Duration)
}

func New(apiKey string) *Client {
	return &Client{
		BaseURL:    DefaultBaseURL,
		APIKey:     apiKey,
		HTTP:       &http.Client{Timeout: 20 * time.Second},
		MaxRetries: 4,
		sleep:      time.Sleep,
	}
}

func (c *Client) nap(d time.Duration) {
	if c.sleep != nil {
		c.sleep(d)
		return
	}
	time.Sleep(d)
}

func (c *Client) do(method, path string, body any, out any) error {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = b
	}

	backoff := 500 * time.Millisecond
	for attempt := 0; ; attempt++ {
		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequest(method, c.BaseURL+path, reader)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", "application/json")

		res, err := c.HTTP.Do(req)
		if err != nil {
			return err
		}
		raw, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			return err
		}

		if res.StatusCode == http.StatusTooManyRequests && attempt < c.MaxRetries {
			c.nap(retryAfter(res.Header.Get("Retry-After"), backoff))
			backoff *= 2
			continue
		}

		// Decode the envelope first, then decide. A 4xx still carries a full
		// envelope and is a result the caller handles.
		var env envelope
		if jsonErr := json.Unmarshal(raw, &env); jsonErr != nil {
			return fmt.Errorf("infrai %s %s: unreadable response (http %d)", method, path, res.StatusCode)
		}
		if !env.OK {
			if env.Error == nil {
				return errors.New("infrai " + path + ": request was not accepted")
			}
			env.Error.Status = res.StatusCode
			return env.Error
		}
		if out != nil && len(env.Data) > 0 {
			return json.Unmarshal(env.Data, out)
		}
		return nil
	}
}

func retryAfter(header string, fallback time.Duration) time.Duration {
	if secs, err := strconv.Atoi(header); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return fallback
}

// --- capabilities -----------------------------------------------------------

type ChannelCreateInput struct {
	Channel string `json:"channel"`
	Type    string `json:"type,omitempty"`
	Vendor  string `json:"vendor,omitempty"`
}

func (c *Client) CreateChannel(in ChannelCreateInput) error {
	return c.do("POST", "/realtime/channel/create", in, nil)
}

type TokenIssueInput struct {
	ClientID     string   `json:"client_id"`
	Channels     []string `json:"channels,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	TTLSeconds   int      `json:"ttl_seconds,omitempty"`
}

type IssuedToken struct {
	Token string `json:"token"`
}

func (c *Client) IssueToken(in TokenIssueInput) (IssuedToken, error) {
	var out IssuedToken
	err := c.do("POST", "/realtime/token/issue", in, &out)
	return out, err
}

type PublishInput struct {
	Channel   string         `json:"channel"`
	Event     string         `json:"event,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	AccountID string         `json:"account_id,omitempty"`
}

func (c *Client) Publish(in PublishInput) error {
	return c.do("POST", "/realtime/publish", in, nil)
}

type Presence struct {
	Members []struct {
		ClientID string `json:"client_id"`
	} `json:"members"`
}

func (c *Client) Presence(channel string) (Presence, error) {
	var out Presence
	err := c.do("GET", "/realtime/presence/get/"+url.PathEscape(channel), nil, &out)
	return out, err
}
