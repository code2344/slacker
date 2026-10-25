package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
)

const activityFeedTypes = "at_user,at_user_group,at_channel,at_everyone,keyword,thread_v2,message_reaction,bot_dm_bundle,dm,unjoined_channel_mention,channel,saved_reminder,list_user_mentioned"

type ActivityItem struct {
	Key       string
	Type      string
	IsUnread  bool
	IsBot     bool
	FeedTS    string
	ChannelID string
	Timestamp string
	ThreadTS  string
	AuthorID  string
	Reaction  string
}

type ActivityFeed struct {
	Items      []ActivityItem
	NextCursor string
}

type ActivityMessage struct {
	Text   string
	UserID string
}

type ThreadSummary struct {
	ChannelID     string
	ThreadTS      string
	LatestReply   string
	UserID        string
	Text          string
	ReplyCount    int
	UnreadReplies int
}

type activityRawItem struct {
	IsUnread bool   `json:"is_unread"`
	IsBot    bool   `json:"is_bot"`
	FeedTS   string `json:"feed_ts"`
	Key      string `json:"key"`
	Item     struct {
		Type    string `json:"type"`
		Message *struct {
			Timestamp string `json:"ts"`
			Channel   string `json:"channel"`
			AuthorID  string `json:"author_user_id"`
			ThreadTS  string `json:"thread_ts"`
		} `json:"message"`
		Reaction *struct {
			User string `json:"user"`
			Name string `json:"name"`
		} `json:"reaction"`
		BundleInfo *struct {
			Payload struct {
				ThreadEntry *struct {
					ChannelID string `json:"channel_id"`
					ThreadTS  string `json:"thread_ts"`
					LatestTS  string `json:"latest_ts"`
				} `json:"thread_entry"`
				DMEntry *struct {
					LatestMessage *struct {
						Timestamp string `json:"ts"`
						Channel   string `json:"channel"`
					} `json:"latest_message"`
				} `json:"dm_entry"`
				ChannelEntry *struct {
					LatestMessage *struct {
						Timestamp string `json:"ts"`
						Channel   string `json:"channel"`
					} `json:"latest_message"`
				} `json:"channel_entry"`
				Message *struct {
					Timestamp string `json:"ts"`
					Channel   string `json:"channel"`
				} `json:"message"`
			} `json:"payload"`
		} `json:"bundle_info"`
	} `json:"item"`
}

func (c *Client) ActivityFeed(ctx context.Context, limit int, cursor string, unreadOnly bool) (ActivityFeed, error) {
	params := url.Values{
		"limit":               {strconv.Itoa(limit)},
		"types":               {activityFeedTypes},
		"mode":                {"chrono_v1"},
		"archive_only":        {"false"},
		"unread_only":         {strconv.FormatBool(unreadOnly)},
		"priority_only":       {"false"},
		"exclude_automations": {"false"},
	}
	if cursor != "" {
		params.Set("cursor", cursor)
	}
	var result struct {
		Items    []json.RawMessage `json:"items"`
		Metadata struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if err := c.Call(ctx, "activity.feed", params, &result); err != nil {
		return ActivityFeed{}, err
	}
	feed := ActivityFeed{NextCursor: result.Metadata.NextCursor}
	for _, encoded := range result.Items {
		var raw activityRawItem
		if json.Unmarshal(encoded, &raw) != nil {
			continue
		}
		feed.Items = append(feed.Items, flattenActivity(raw))
	}
	return feed, nil
}

func flattenActivity(raw activityRawItem) ActivityItem {
	item := ActivityItem{Key: raw.Key, Type: raw.Item.Type, IsUnread: raw.IsUnread, IsBot: raw.IsBot, FeedTS: raw.FeedTS}
	if raw.Item.Message != nil {
		item.ChannelID = raw.Item.Message.Channel
		item.Timestamp = raw.Item.Message.Timestamp
		item.ThreadTS = raw.Item.Message.ThreadTS
		item.AuthorID = raw.Item.Message.AuthorID
	}
	if raw.Item.Reaction != nil {
		item.Reaction = raw.Item.Reaction.Name
		item.AuthorID = raw.Item.Reaction.User
	}
	if raw.Item.BundleInfo == nil {
		return item
	}
	payload := raw.Item.BundleInfo.Payload
	switch raw.Item.Type {
	case "thread_v2":
		if payload.ThreadEntry != nil {
			item.ChannelID = payload.ThreadEntry.ChannelID
			item.Timestamp = payload.ThreadEntry.LatestTS
			item.ThreadTS = payload.ThreadEntry.ThreadTS
		}
	case "dm":
		if payload.DMEntry != nil && payload.DMEntry.LatestMessage != nil {
			item.ChannelID = payload.DMEntry.LatestMessage.Channel
			item.Timestamp = payload.DMEntry.LatestMessage.Timestamp
		}
	case "channel":
		if payload.ChannelEntry != nil && payload.ChannelEntry.LatestMessage != nil {
			item.ChannelID = payload.ChannelEntry.LatestMessage.Channel
			item.Timestamp = payload.ChannelEntry.LatestMessage.Timestamp
		}
	case "bot_dm_bundle":
		if payload.Message != nil {
			item.ChannelID = payload.Message.Channel
			item.Timestamp = payload.Message.Timestamp
		}
	}
	return item
}

func ActivityMessageKey(channel, timestamp string) string { return channel + "\x00" + timestamp }

func (c *Client) ActivityMessages(ctx context.Context, refs map[string][]string) (map[string]ActivityMessage, error) {
	channels := make([]string, 0, len(refs))
	for channel, timestamps := range refs {
		if channel != "" && len(timestamps) > 0 {
			channels = append(channels, channel)
		}
	}
	sort.Strings(channels)
	result := make(map[string]ActivityMessage)
	for start := 0; start < len(channels); start += 8 {
		end := min(start+8, len(channels))
		groups := make([]struct {
			Channel    string   `json:"channel"`
			Timestamps []string `json:"timestamps"`
		}, 0, end-start)
		for _, channel := range channels[start:end] {
			groups = append(groups, struct {
				Channel    string   `json:"channel"`
				Timestamps []string `json:"timestamps"`
			}{Channel: channel, Timestamps: refs[channel]})
		}
		encoded, err := json.Marshal(groups)
		if err != nil {
			return nil, err
		}
		var response struct {
			MessagesData map[string]struct {
				Messages []struct {
					Timestamp string `json:"ts"`
					User      string `json:"user"`
					Text      string `json:"text"`
				} `json:"messages"`
			} `json:"messages_data"`
		}
		params := url.Values{"message_ids": {string(encoded)}, "org_wide_aware": {"true"}, "cached_latest_updates": {"{}"}}
		if err := c.Call(ctx, "messages.list", params, &response); err != nil {
			return nil, fmt.Errorf("hydrate activity: %w", err)
		}
		for channel, data := range response.MessagesData {
			for _, message := range data.Messages {
				result[ActivityMessageKey(channel, message.Timestamp)] = ActivityMessage{Text: message.Text, UserID: message.User}
			}
		}
	}
	return result, nil
}

func (c *Client) UnreadThreads(ctx context.Context, currentTS string) ([]ThreadSummary, bool, error) {
	params := url.Values{"limit": {"10"}}
	if currentTS != "" {
		params.Set("current_ts", currentTS)
	}
	var response struct {
		HasMore bool `json:"has_more"`
		Threads []struct {
			Root struct {
				User        string `json:"user"`
				Text        string `json:"text"`
				Timestamp   string `json:"ts"`
				ThreadTS    string `json:"thread_ts"`
				Channel     string `json:"channel"`
				ReplyCount  int    `json:"reply_count"`
				LatestReply string `json:"latest_reply"`
			} `json:"root_msg"`
			UnreadReplies int `json:"unread_replies"`
		} `json:"threads"`
	}
	if err := c.Call(ctx, "subscriptions.thread.getView", params, &response); err != nil {
		return nil, false, err
	}
	threads := make([]ThreadSummary, 0, len(response.Threads))
	for _, item := range response.Threads {
		threadTS := item.Root.ThreadTS
		if threadTS == "" {
			threadTS = item.Root.Timestamp
		}
		threads = append(threads, ThreadSummary{ChannelID: item.Root.Channel, ThreadTS: threadTS, LatestReply: item.Root.LatestReply, UserID: item.Root.User, Text: item.Root.Text, ReplyCount: item.Root.ReplyCount, UnreadReplies: item.UnreadReplies})
	}
	return threads, response.HasMore, nil
}
