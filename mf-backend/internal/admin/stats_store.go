package admin

import (
	"context"
	"time"
)

func (s *Store) Stats(ctx context.Context, from, to time.Time) (StatsResponse, error) {
	return StatsResponse{}, nil
}
