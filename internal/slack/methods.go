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
