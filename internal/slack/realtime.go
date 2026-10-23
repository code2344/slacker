package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/websocket"
)

type RealtimeEvent struct {
	Type      string  `json:"type"`
	Subtype   string  `json:"subtype"`
	Channel   string  `json:"channel"`
	User      string  `json:"user"`
	Text      string  `json:"text"`
	Timestamp string  `json:"ts"`
	ThreadTS  string  `json:"thread_ts"`
	Message   Message `json:"message"`
	DeletedTS string  `json:"deleted_ts"`
	Item      struct {
		Channel   string `json:"channel"`
		Timestamp string `json:"ts"`
	} `json:"item"`
}

func (e RealtimeEvent) ConversationID() string {
	if e.Channel != "" {
		return e.Channel
	}
	return e.Item.Channel
}

type Realtime struct{ conn *websocket.Conn }

func (c *Client) OpenRealtime(ctx context.Context) (*Realtime, error) {
	params := url.Values{"presence_sub": {"true"}, "batch_presence_aware": {"true"}}
	var result struct {
		URL string `json:"url"`
	}
	if err := c.Call(ctx, "rtm.connect", params, &result); err != nil {
		return nil, err
	}
	if result.URL == "" {
		return nil, fmt.Errorf("Slack rtm.connect returned no websocket URL")
	}
	config, err := websocket.NewConfig(result.URL, "https://app.slack.com")
	if err != nil {
		return nil, fmt.Errorf("configure Slack websocket: %w", err)
	}
	config.Header = http.Header{}
	if c.session.UserAgent != "" {
		config.Header.Set("User-Agent", c.session.UserAgent)
	}
	cookies := []string{"d=" + c.session.Cookie}
	if c.session.CookieS != "" {
		cookies = append(cookies, "d-s="+c.session.CookieS)
	}
	config.Header.Set("Cookie", strings.Join(cookies, "; "))
	conn, err := websocket.DialConfig(config)
	if err != nil {
		return nil, fmt.Errorf("connect Slack websocket: %w", err)
	}
	return &Realtime{conn: conn}, nil
}

func (r *Realtime) Next() (RealtimeEvent, error) {
	var payload []byte
	if err := websocket.Message.Receive(r.conn, &payload); err != nil {
		return RealtimeEvent{}, err
	}
	var event RealtimeEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return RealtimeEvent{}, fmt.Errorf("decode Slack realtime event: %w", err)
	}
	return event, nil
}

func (r *Realtime) Close() error {
	if r == nil || r.conn == nil {
		return nil
	}
	return r.conn.Close()
}
