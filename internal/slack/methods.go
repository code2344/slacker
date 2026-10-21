package slack

import (
	"context"
	"fmt"
	"net/url"
)

type Auth struct {
	OK     bool   `json:"ok"`
	URL    string `json:"url"`
	Team   string `json:"team"`
	TeamID string `json:"team_id"`
	User   string `json:"user"`
	UserID string `json:"user_id"`
}

func (c *Client) AuthTest(ctx context.Context) (Auth, error) {
	var result Auth
	err := c.Call(ctx, "auth.test", nil, &result)
	if err != nil {
		return Auth{}, err
	}
	return result, nil
}

type ClientBoot struct {
	OK   bool `json:"ok"`
	Self struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		TeamID string `json:"team_id"`
	} `json:"self"`
	Team struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Domain string `json:"domain"`
		URL    string `json:"url"`
	} `json:"team"`
}

func (c *Client) ClientBoot(ctx context.Context) (ClientBoot, error) {
	var result ClientBoot
	err := c.Call(ctx, "client.boot", nil, &result)
	if err != nil {
		return ClientBoot{}, err
	}
	if result.Team.ID == "" {
		result.Team.ID = result.Self.TeamID
	}
	return result, nil
}

type Conversation struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsChannel bool   `json:"is_channel"`
	IsGroup   bool   `json:"is_group"`
	IsIM      bool   `json:"is_im"`
	IsMPIM    bool   `json:"is_mpim"`
	IsPrivate bool   `json:"is_private"`
	IsMember  bool   `json:"is_member"`
	User      string `json:"user"`
}

type ChannelSection struct {
	ID                   string `json:"channel_section_id"`
	Name                 string `json:"name"`
	Type                 string `json:"type"`
	NextChannelSectionID string `json:"next_channel_section_id"`
	ChannelIDsPage       struct {
		ChannelIDs []string `json:"channel_ids"`
		Count      int      `json:"count"`
		Cursor     string   `json:"cursor"`
		IsRedacted bool     `json:"is_redacted"`
	} `json:"channel_ids_page"`
}

func (c *Client) ListChannelSections(ctx context.Context) ([]ChannelSection, error) {
	var result struct {
		ChannelSections []ChannelSection `json:"channel_sections"`
	}
	err := c.Call(ctx, "users.channelSections.list", nil, &result)
	return result.ChannelSections, err
}

type File struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Title      string `json:"title"`
	Mimetype   string `json:"mimetype"`
	URLPrivate string `json:"url_private"`
}

type Reaction struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type Message struct {
	Type       string     `json:"type"`
	Subtype    string     `json:"subtype"`
	User       string     `json:"user"`
	Username   string     `json:"username"`
	Text       string     `json:"text"`
	Timestamp  string     `json:"ts"`
	ThreadTS   string     `json:"thread_ts"`
	ReplyCount int        `json:"reply_count"`
	Files      []File     `json:"files"`
	Reactions  []Reaction `json:"reactions"`
}

func (c *Client) ConversationHistory(ctx context.Context, channel string, limit int) ([]Message, bool, error) {
	params := url.Values{"channel": {channel}}
	if limit > 0 {
		params.Set("limit", fmt.Sprint(limit))
	}
	var result struct {
		Messages []Message `json:"messages"`
		HasMore  bool      `json:"has_more"`
	}
	err := c.Call(ctx, "conversations.history", params, &result)
	return result.Messages, result.HasMore, err
}

func (c *Client) ConversationReplies(ctx context.Context, channel, timestamp string, limit int) ([]Message, string, error) {
	params := url.Values{"channel": {channel}, "ts": {timestamp}}
	if limit > 0 {
		params.Set("limit", fmt.Sprint(limit))
	}
	var result struct {
		Messages []Message `json:"messages"`
		Metadata struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	err := c.Call(ctx, "conversations.replies", params, &result)
	return result.Messages, result.Metadata.NextCursor, err
}

func (c *Client) PostMessage(ctx context.Context, channel, text, threadTimestamp string) (Message, error) {
	params := url.Values{"channel": {channel}, "text": {text}}
	if threadTimestamp != "" {
		params.Set("thread_ts", threadTimestamp)
	}
	var result struct {
		Message Message `json:"message"`
	}
	err := c.Call(ctx, "chat.postMessage", params, &result)
	return result.Message, err
}

func (c *Client) ListConversations(ctx context.Context, cursor string, limit int) ([]Conversation, string, error) {
	params := url.Values{"types": {"public_channel,private_channel,mpim,im"}, "exclude_archived": {"true"}}
	if cursor != "" {
		params.Set("cursor", cursor)
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprint(limit))
	}
	var result struct {
		OK       bool           `json:"ok"`
		Channels []Conversation `json:"channels"`
		Metadata struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	err := c.Call(ctx, "users.conversations", params, &result)
	return result.Channels, result.Metadata.NextCursor, err
}

type User struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
	Profile struct {
		DisplayName string `json:"display_name"`
		RealName    string `json:"real_name"`
		Pronouns    string `json:"pronouns"`
		Image48     string `json:"image_48"`
	} `json:"profile"`
}

func (c *Client) ListUsers(ctx context.Context, cursor string, limit int) ([]User, string, error) {
	params := url.Values{}
	if cursor != "" {
		params.Set("cursor", cursor)
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprint(limit))
	}
	var result struct {
		OK       bool   `json:"ok"`
		Members  []User `json:"members"`
		Metadata struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	err := c.Call(ctx, "users.list", params, &result)
	return result.Members, result.Metadata.NextCursor, err
}
