package web

import (
	"context"
	"errors"
	"strings"
	"time"
)

var jobs []Job

const (
	StatusPending = "pending"
	StatusWorking = "working"
	StatusOK      = "ok"
	StatusFailed  = "failed"
)

type SelectParams struct {
	Status string
	Type   string
	Limit  int
}

type JobRepository interface {
	Get(context.Context, string) (Job, error)
	Create(context.Context, *Job) error
	Delete(context.Context, string) error
	Select(context.Context, SelectParams) ([]Job, error)
	Update(context.Context, *Job) error
}

type Job struct {
	ID     string    `json:"id"`
	Name   string    `json:"name"`
	Date   time.Time `json:"date"`
	Status string    `json:"status"`
	Type   string    `json:"type,omitempty"`
	Data   JobData   `json:"data"`
}

func (j *Job) Validate() error {
	if j.ID == "" {
		return errors.New("missing id")
	}

	if j.Name == "" {
		return errors.New("missing name")
	}

	if j.Status == "" {
		return errors.New("missing status")
	}

	if j.Date.IsZero() {
		return errors.New("missing date")
	}

	if err := j.Data.Validate(); err != nil {
		return err
	}

	return nil
}

type JobData struct {
	Type         string        `json:"type,omitempty"`
	Username     string        `json:"username,omitempty"`
	Query        string        `json:"query,omitempty"`
	Keywords     []string      `json:"keywords"`
	Lang         string        `json:"lang"`
	Zoom         int           `json:"zoom"`
	Lat          string        `json:"lat"`
	Lon          string        `json:"lon"`
	FastMode     bool          `json:"fast_mode"`
	Radius       int           `json:"radius"`
	Depth        int           `json:"depth"`
	MaxResults   int           `json:"max_results,omitempty"`
	Email        bool          `json:"email"`
	ExtraReviews bool          `json:"extra_reviews"`
	MaxTime      time.Duration `json:"max_time"`
	Proxies      []string      `json:"proxies"`
	Cookie       string        `json:"cookie,omitempty"`
}

func (d *JobData) Validate() error {
	switch strings.ToLower(strings.TrimSpace(d.Type)) {
	case "twitter", "facebook", "tiktok", "threads":
		if strings.TrimSpace(d.Username) == "" && strings.TrimSpace(d.Query) == "" {
			return errors.New("username or query is required for social media job")
		}
		if d.MaxResults <= 0 {
			if d.Depth <= 1 {
				d.MaxResults = 100
			} else {
				d.MaxResults = d.Depth * 20
			}
		}
	default:
		if len(d.Keywords) == 0 {
			return errors.New("missing keywords")
		}

		if d.Lang == "" {
			return errors.New("missing lang")
		}

		if len(d.Lang) != 2 {
			return errors.New("invalid lang")
		}

		if d.Depth == 0 {
			return errors.New("missing depth")
		}

		if d.MaxTime == 0 {
			return errors.New("missing max time")
		}

		if d.FastMode && (d.Lat == "" || d.Lon == "") {
			return errors.New("missing geo coordinates")
		}
	}

	return nil
}
