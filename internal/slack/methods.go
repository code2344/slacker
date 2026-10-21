package slack

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
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
	ID              string `json:"id"`
	Name            string `json:"name"`
	IsChannel       bool   `json:"is_channel"`
	IsGroup         bool   `json:"is_group"`
	IsIM            bool   `json:"is_im"`
	IsMPIM          bool   `json:"is_mpim"`
	IsPrivate       bool   `json:"is_private"`
	IsMember        bool   `json:"is_member"`
	User            string `json:"user"`
	NumMembers      int    `json:"num_members"`
	Created         int64  `json:"created"`
	Updated         int64  `json:"updated"`
	LatestMessageTS string `json:"-"`
	ActivityChecked bool   `json:"-"`
	Topic           struct {
		Value string `json:"value"`
	} `json:"topic"`
	Purpose struct {
		Value string `json:"value"`
	} `json:"purpose"`
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
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Title              string `json:"title"`
	Mimetype           string `json:"mimetype"`
	PrettyType         string `json:"pretty_type"`
	URLPrivate         string `json:"url_private"`
	URLPrivateDownload string `json:"url_private_download"`
	Permalink          string `json:"permalink"`
	User               string `json:"user"`
	Size               int64  `json:"size"`
}

type Reaction struct {
	Name  string   `json:"name"`
	Count int      `json:"count"`
	Users []string `json:"users"`
}

type MessageEdited struct {
	User      string `json:"user"`
	Timestamp string `json:"ts"`
}

type Message struct {
	Type       string         `json:"type"`
	Subtype    string         `json:"subtype"`
	User       string         `json:"user"`
	Username   string         `json:"username"`
	Text       string         `json:"text"`
	Timestamp  string         `json:"ts"`
	ThreadTS   string         `json:"thread_ts"`
	ReplyCount int            `json:"reply_count"`
	Files      []File         `json:"files"`
	Reactions  []Reaction     `json:"reactions"`
	Edited     *MessageEdited `json:"edited"`
}

func (c *Client) UpdateMessage(ctx context.Context, channel, timestamp, text string) (Message, error) {
	params := url.Values{"channel": {channel}, "ts": {timestamp}, "text": {text}}
	var result struct {
		Message Message `json:"message"`
	}
	err := c.Call(ctx, "chat.update", params, &result)
	return result.Message, err
}

func (c *Client) DeleteMessage(ctx context.Context, channel, timestamp string) error {
	return c.Call(ctx, "chat.delete", url.Values{"channel": {channel}, "ts": {timestamp}}, nil)
}

func (c *Client) AddReaction(ctx context.Context, channel, timestamp, name string) error {
	return c.Call(ctx, "reactions.add", url.Values{"channel": {channel}, "timestamp": {timestamp}, "name": {name}}, nil)
}

func (c *Client) RemoveReaction(ctx context.Context, channel, timestamp, name string) error {
	return c.Call(ctx, "reactions.remove", url.Values{"channel": {channel}, "timestamp": {timestamp}, "name": {name}}, nil)
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
	params := url.Values{"channel": {channel}, "text": {text}, "mrkdwn": {"true"}, "client_msg_id": {newClientMessageID()}}
	if threadTimestamp != "" {
		params.Set("thread_ts", threadTimestamp)
	}
	var result struct {
		Channel string  `json:"channel"`
		TS      string  `json:"ts"`
		Message Message `json:"message"`
	}
	err := c.Call(ctx, "chat.postMessage", params, &result)
	if err != nil {
		return Message{}, err
	}
	if result.Message.Timestamp == "" {
		result.Message.Timestamp = result.TS
	}
	if result.Message.Timestamp == "" {
		return Message{}, fmt.Errorf("Slack accepted chat.postMessage but returned no message timestamp")
	}
	if result.Channel != "" && result.Channel != channel {
		return Message{}, fmt.Errorf("Slack posted to unexpected conversation %s", result.Channel)
	}
	return result.Message, err
}

func newClientMessageID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Sprintf("slacker-%d", time.Now().UnixNano())
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}

type MessageSearchMatch struct {
	Message
	Channel   Conversation `json:"channel"`
	Permalink string       `json:"permalink"`
}

func (c *Client) SearchMessages(ctx context.Context, query string) ([]MessageSearchMatch, int, error) {
	params := url.Values{"query": {query}, "sort": {"timestamp"}, "sort_dir": {"desc"}, "highlight": {"false"}}
	var result struct {
		Messages struct {
			Matches []MessageSearchMatch `json:"matches"`
			Total   int                  `json:"total"`
		} `json:"messages"`
	}
	err := c.Call(ctx, "search.messages", params, &result)
	return result.Messages.Matches, result.Messages.Total, err
}

func (c *Client) SearchFiles(ctx context.Context, query string) ([]File, int, error) {
	params := url.Values{"query": {query}, "sort": {"timestamp"}, "sort_dir": {"desc"}, "highlight": {"false"}}
	var result struct {
		Files struct {
			Matches []File `json:"matches"`
			Total   int    `json:"total"`
		} `json:"files"`
	}
	err := c.Call(ctx, "search.files", params, &result)
	return result.Files.Matches, result.Files.Total, err
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

func (c *Client) OpenConversation(ctx context.Context, userIDs []string) (Conversation, error) {
	if len(userIDs) == 0 {
		return Conversation{}, fmt.Errorf("at least one user is required")
	}
	params := url.Values{
		"users":     {strings.Join(userIDs, ",")},
		"return_im": {"true"},
	}
	var result struct {
		Channel Conversation `json:"channel"`
	}
	if err := c.Call(ctx, "conversations.open", params, &result); err != nil {
		return Conversation{}, err
	}
	if result.Channel.ID == "" {
		return Conversation{}, fmt.Errorf("Slack returned an empty conversation")
	}
	if len(userIDs) == 1 {
		result.Channel.IsIM = true
		if result.Channel.User == "" {
			result.Channel.User = userIDs[0]
		}
	}
	return result.Channel, nil
}

type User struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	RealName          string `json:"real_name"`
	Deleted           bool   `json:"deleted"`
	TZ                string `json:"tz"`
	TZLabel           string `json:"tz_label"`
	TZOffset          int    `json:"tz_offset"`
	IsBot             bool   `json:"is_bot"`
	IsAdmin           bool   `json:"is_admin"`
	IsOwner           bool   `json:"is_owner"`
	IsPrimaryOwner    bool   `json:"is_primary_owner"`
	IsRestricted      bool   `json:"is_restricted"`
	IsUltraRestricted bool   `json:"is_ultra_restricted"`
	ProfileLoaded     bool   `json:"-"`
	Profile           struct {
		DisplayName      string                           `json:"display_name"`
		RealName         string                           `json:"real_name"`
		FirstName        string                           `json:"first_name"`
		LastName         string                           `json:"last_name"`
		Pronouns         string                           `json:"pronouns"`
		Title            string                           `json:"title"`
		Email            string                           `json:"email"`
		Phone            string                           `json:"phone"`
		Skype            string                           `json:"skype"`
		StatusText       string                           `json:"status_text"`
		StatusEmoji      string                           `json:"status_emoji"`
		StatusExpiration int64                            `json:"status_expiration"`
		StartDate        string                           `json:"start_date"`
		OOOMessage       string                           `json:"ooo_message"`
		Image48          string                           `json:"image_48"`
		Image192         string                           `json:"image_192"`
		Image512         string                           `json:"image_512"`
		ImageOriginal    string                           `json:"image_original"`
		Fields           map[string]UserProfileFieldValue `json:"fields"`
	} `json:"profile"`
}

type UserProfileFieldValue struct {
	Value string `json:"value"`
	Alt   string `json:"alt"`
}

type TeamProfileField struct {
	ID        string `json:"id"`
	Ordering  int    `json:"ordering"`
	FieldName string `json:"field_name"`
	Label     string `json:"label"`
	Hint      string `json:"hint"`
	Type      string `json:"type"`
}

func (c *Client) TeamProfile(ctx context.Context) ([]TeamProfileField, error) {
	var result struct {
		Profile struct {
			Fields []TeamProfileField `json:"fields"`
		} `json:"profile"`
	}
	err := c.Call(ctx, "team.profile.get", nil, &result)
	return result.Profile.Fields, err
}

type UserBoot struct {
	Channels         []Conversation `json:"channels"`
	IMs              []Conversation `json:"ims"`
	MPIMs            []Conversation `json:"mpims"`
	Members          []User         `json:"members"`
	ChannelsPriority map[string]int `json:"channels_priority"`
}

func (c *Client) UserInfo(ctx context.Context, userID string) (User, error) {
	var result struct {
		User User `json:"user"`
	}
	err := c.Call(ctx, "users.info", url.Values{"user": {userID}}, &result)
	if err == nil {
		result.User.ProfileLoaded = true
	}
	return result.User, err
}

type UserPresence struct {
	Presence     string `json:"presence"`
	Online       bool   `json:"online"`
	AutoAway     bool   `json:"auto_away"`
	ManualAway   bool   `json:"manual_away"`
	LastActivity int64  `json:"last_activity"`
}

func (c *Client) UserPresence(ctx context.Context, userID string) (UserPresence, error) {
	var result UserPresence
	err := c.Call(ctx, "users.getPresence", url.Values{"user": {userID}}, &result)
	return result, err
}

type DNDInfo struct {
	DNDEnabled    bool  `json:"dnd_enabled"`
	SnoozeEnabled bool  `json:"snooze_enabled"`
	SnoozeEndtime int64 `json:"snooze_endtime"`
	NextDNDStart  int64 `json:"next_dnd_start_ts"`
	NextDNDEnd    int64 `json:"next_dnd_end_ts"`
}

func (c *Client) DNDInfo(ctx context.Context, userID string) (DNDInfo, error) {
	var result DNDInfo
	err := c.Call(ctx, "dnd.info", url.Values{"user": {userID}}, &result)
	return result, err
}

func (c *Client) UserBoot(ctx context.Context) (UserBoot, error) {
	var result UserBoot
	err := c.Call(ctx, "client.userBoot", url.Values{"return_all_relevant_mpdms": {"true"}}, &result)
	for index := range result.Members {
		result.Members[index].ProfileLoaded = true
	}
	return result, err
}

type ConversationCount struct {
	ID           string `json:"id"`
	LastRead     string `json:"last_read"`
	Latest       string `json:"latest"`
	MentionCount int    `json:"mention_count"`
	UnreadCount  int    `json:"unread_count"`
}

func (c *Client) ConversationCounts(ctx context.Context) (map[string]ConversationCount, error) {
	var result struct {
		Channels json.RawMessage `json:"channels"`
		IMs      json.RawMessage `json:"ims"`
		MPIMs    json.RawMessage `json:"mpims"`
	}
	if err := c.Call(ctx, "client.counts", url.Values{"include_all_unreads": {"true"}, "thread_counts_by_channel": {"true"}}, &result); err != nil {
		return nil, err
	}
	out := map[string]ConversationCount{}
	for _, raw := range []json.RawMessage{result.Channels, result.IMs, result.MPIMs} {
		var items []ConversationCount
		if len(raw) == 0 || json.Unmarshal(raw, &items) != nil {
			continue
		}
		for _, item := range items {
			out[item.ID] = item
		}
	}
	return out, nil
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
	for index := range result.Members {
		result.Members[index].ProfileLoaded = true
	}
	return result.Members, result.Metadata.NextCursor, err
}
