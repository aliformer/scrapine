package web

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

var ErrTweetsNotFound = errors.New("tweets not found")

type TweetDTO struct {
	ID             string    `json:"id"`
	URL            string    `json:"url"`
	AuthorUsername string    `json:"author_username"`
	AuthorName     string    `json:"author_name"`
	Text           string    `json:"text"`
	CreatedAt      time.Time `json:"created_at"`
	Replies        int       `json:"replies"`
	Retweets       int       `json:"retweets"`
	Likes          int       `json:"likes"`
	Views          int       `json:"views"`
	IsReply        bool      `json:"is_reply"`
	IsRetweet      bool      `json:"is_retweet"`
}

func (s *Service) GetTweets(_ context.Context, id string) ([]TweetDTO, error) {
	path, err := s.csvPath(id)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("csv file not found for job %s: %w", id, ErrTweetsNotFound)
		}

		return nil, err
	}

	defer func() {
		_ = f.Close()
	}()

	return parseTweets(f)
}

func parseTweets(r io.Reader) ([]TweetDTO, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return []TweetDTO{}, nil
		}

		return nil, err
	}

	pos := make(map[string]int, len(header))
	for i, h := range header {
		pos[strings.TrimSpace(strings.ToLower(h))] = i
	}

	get := func(row []string, col string) string {
		idx, ok := pos[col]
		if !ok || idx >= len(row) {
			return ""
		}

		return strings.TrimSpace(row[idx])
	}

	getInt := func(row []string, col string) int {
		s := get(row, col)
		v, _ := strconv.Atoi(s)
		return v
	}

	getBool := func(row []string, col string) bool {
		s := get(row, col)
		v, _ := strconv.ParseBool(s)
		return v
	}

	getTime := func(row []string, col string) time.Time {
		s := get(row, col)
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t
		}
		return time.Time{}
	}

	var tweets []TweetDTO

	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			continue
		}

		id := get(row, "id")
		if id == "" {
			continue
		}

		tweets = append(tweets, TweetDTO{
			ID:             id,
			URL:            get(row, "url"),
			AuthorUsername: get(row, "author_username"),
			AuthorName:     get(row, "author_name"),
			Text:           get(row, "text"),
			CreatedAt:      getTime(row, "created_at"),
			Replies:        getInt(row, "replies"),
			Retweets:       getInt(row, "retweets"),
			Likes:          getInt(row, "likes"),
			Views:          getInt(row, "views"),
			IsReply:        getBool(row, "is_reply"),
			IsRetweet:      getBool(row, "is_retweet"),
		})
	}

	return tweets, nil
}
