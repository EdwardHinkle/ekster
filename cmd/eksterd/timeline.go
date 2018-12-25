package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/gomodule/redigo/redis"
	"p83.nl/go/ekster/pkg/microsub"
)

type TimelineBackend interface {
	Items(before, after string) (microsub.Timeline, error)
	AddItem(item microsub.Item) error
	Count() (int, error)

	MarkRead(uid string) error
	MarkUnread(uid string) error
}

type redisSortedSetTimeline struct {
	channel string
}

type redisStreamTimeline struct {
	channel string
}

func (b *memoryBackend) getTimeline(channel string) TimelineBackend {
	// TODO: fetch timeline type from channel
	timelineType := "sorted-set"
	if timelineType == "sorted-set" {
		return &redisSortedSetTimeline{channel}
	}
	if timelineType == "stream" {
		return &redisStreamTimeline{channel}
	}
	return nil
}

/*
 * REDIS SORTED SETS TIMELINE
 */
func (timeline *redisSortedSetTimeline) Items(before, after string) (microsub.Timeline, error) {
	conn := pool.Get()
	defer conn.Close()

	items := []microsub.Item{}

	channel := timeline.channel

	zchannelKey := fmt.Sprintf("zchannel:%s:posts", channel)

	afterScore := "-inf"
	if len(after) != 0 {
		afterScore = "(" + after
	}
	beforeScore := "+inf"
	if len(before) != 0 {
		beforeScore = "(" + before
	}

	var itemJSONs [][]byte

	itemScores, err := redis.Strings(
		conn.Do(
			"ZRANGEBYSCORE",
			zchannelKey,
			afterScore,
			beforeScore,
			"LIMIT",
			0,
			20,
			"WITHSCORES",
		),
	)

	if err != nil {
		return microsub.Timeline{
			Paging: microsub.Pagination{},
			Items:  items,
		}, err
	}

	if len(itemScores) >= 2 {
		before = itemScores[1]
		after = itemScores[len(itemScores)-1]
	} else {
		before = ""
		after = ""
	}

	for i := 0; i < len(itemScores); i += 2 {
		itemID := itemScores[i]
		itemJSON, err := redis.Bytes(conn.Do("HGET", itemID, "Data"))
		if err != nil {
			log.Println(err)
			continue
		}
		itemJSONs = append(itemJSONs, itemJSON)
	}

	for _, obj := range itemJSONs {
		item := microsub.Item{}
		err := json.Unmarshal(obj, &item)
		if err != nil {
			// FIXME: what should we do if one of the items doen't unmarshal?
			log.Println(err)
			continue
		}
		item.Read = false
		items = append(items, item)
	}
	paging := microsub.Pagination{
		After:  after,
		Before: before,
	}

	return microsub.Timeline{
		Paging: paging,
		Items:  items,
	}, nil
}

func (timeline *redisSortedSetTimeline) AddItem(item microsub.Item) error {
	conn := pool.Get()
	defer conn.Close()

	channel := timeline.channel
	zchannelKey := fmt.Sprintf("zchannel:%s:posts", channel)

	if item.Published == "" {
		item.Published = time.Now().Format(time.RFC3339)
	}

	data, err := json.Marshal(item)
	if err != nil {
		log.Printf("error while creating item for redis: %v\n", err)
		return err
	}

	forRedis := redisItem{
		ID:        item.ID,
		Published: item.Published,
		Read:      item.Read,
		Data:      data,
	}

	itemKey := fmt.Sprintf("item:%s", item.ID)
	_, err = redis.String(conn.Do("HMSET", redis.Args{}.Add(itemKey).AddFlat(&forRedis)...))
	if err != nil {
		return fmt.Errorf("error while writing item for redis: %v", err)
	}

	readChannelKey := fmt.Sprintf("channel:%s:read", channel)
	isRead, err := redis.Bool(conn.Do("SISMEMBER", readChannelKey, itemKey))
	if err != nil {
		return err
	}

	if isRead {
		return nil
	}

	score, err := time.Parse(time.RFC3339, item.Published)
	if err != nil {
		return fmt.Errorf("error can't parse %s as time", item.Published)
	}

	_, err = redis.Int64(conn.Do("ZADD", zchannelKey, score.Unix()*1.0, itemKey))
	if err != nil {
		return fmt.Errorf("error while zadding item %s to channel %s for redis: %v", itemKey, zchannelKey, err)
	}

	// FIXME: send message to events...
	// b.sendMessage(microsub.Message("item added " + item.ID))

	return nil
}

func (timeline *redisSortedSetTimeline) Count() (int, error) {
	conn := pool.Get()
	defer conn.Close()

	channel := timeline.channel
	zchannelKey := fmt.Sprintf("zchannel:%s:posts", channel)
	unread, err := redis.Int(conn.Do("ZCARD", zchannelKey))
	if err != nil {
		return -1, fmt.Errorf("while updating channel unread count for %s: %s", channel, err)
	}
	return unread, nil
}

func (timeline *redisSortedSetTimeline) MarkRead(uid string) error {
	panic("implement me")
}

func (timeline *redisSortedSetTimeline) MarkUnread(uid string) error {
	panic("implement me")
}

/*
 * REDIS STREAMS TIMELINE
 */
func (*redisStreamTimeline) Items(before, after string) (microsub.Timeline, error) {
	panic("implement me")
}

func (*redisStreamTimeline) AddItem(item microsub.Item) error {
	panic("implement me")
}

func (*redisStreamTimeline) Count() (int, error) {
	return 0, nil
}

func (*redisStreamTimeline) MarkRead(uid string) error {
	panic("implement me")
}

func (*redisStreamTimeline) MarkUnread(uid string) error {
	panic("implement me")
}
