package service

import (
	"context"
	"errors"
	"github.com/shopspring/decimal"
	"sync"
	"time"
)

type cachedTronPriceClient struct {
	source    TronPriceClient
	now       func() time.Time
	mu        sync.Mutex
	inflight  chan struct{}
	rate      decimal.Decimal
	updatedAt time.Time
	fetchedAt time.Time
	retryAt   time.Time
	backoff   time.Duration
	lastErr   error
}

func newCachedTronPriceClient(source TronPriceClient, now func() time.Time) TronPriceClient {
	if source == nil {
		return nil
	}
	return &cachedTronPriceClient{source: source, now: now}
}

func (c *cachedTronPriceClient) USDTToCNY(ctx context.Context) (decimal.Decimal, time.Time, error) {
	for {
		if err := ctx.Err(); err != nil {
			return decimal.Zero, time.Time{}, err
		}
		c.mu.Lock()
		now := c.now()
		fresh := c.rate.IsPositive() && now.Sub(c.updatedAt) <= tronPriceMaxAge && !c.updatedAt.After(now.Add(30*time.Second))
		if fresh && now.Sub(c.fetchedAt) < time.Minute && !now.Before(c.fetchedAt) {
			rate, updated := c.rate, c.updatedAt
			c.mu.Unlock()
			return rate, updated, nil
		}
		if now.Before(c.retryAt) {
			err := c.lastErr
			c.mu.Unlock()
			return decimal.Zero, time.Time{}, err
		}
		if pending := c.inflight; pending != nil {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return decimal.Zero, time.Time{}, ctx.Err()
			case <-pending:
				continue
			}
		}
		pending := make(chan struct{})
		c.inflight = pending
		c.mu.Unlock()
		rate, updated, err := c.source.USDTToCNY(ctx)
		c.mu.Lock()
		now = c.now()
		if err == nil && (!rate.IsPositive() || now.Sub(updated) > tronPriceMaxAge || updated.After(now.Add(30*time.Second))) {
			err = errors.New("USDT/CNY price is stale or invalid")
		}
		if err == nil {
			c.rate = rate
			c.updatedAt = updated
			c.fetchedAt = now
			c.backoff = 0
			c.retryAt = time.Time{}
			c.lastErr = nil
		} else if !errors.Is(err, context.Canceled) {
			if c.backoff == 0 {
				c.backoff = 10 * time.Second
			} else {
				c.backoff *= 2
			}
			if c.backoff > time.Minute {
				c.backoff = time.Minute
			}
			c.retryAt = now.Add(c.backoff)
			c.lastErr = err
		}
		c.inflight = nil
		close(pending)
		c.mu.Unlock()
		return rate, updated, err
	}
}
