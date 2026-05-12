package xhttp

import (
	"context"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"
)

type cache struct {
	kv  jetstream.KeyValue
	log zerolog.Logger
}

func newCache(kv jetstream.KeyValue, log zerolog.Logger) *cache {
	return &cache{kv: kv, log: log}
}

func (c *cache) Get(ctx context.Context, key string) []byte {
	entry, err := c.kv.Get(ctx, key)
	if err != nil {
		return nil
	}
	return entry.Value()
}

func (c *cache) Put(ctx context.Context, key string, val []byte) {
	if _, err := c.kv.Put(ctx, key, val); err != nil {
		c.log.Error().Err(err).Str("key", key).Msg("cache.Put")
	}
}

func (c *cache) Invalidate(ctx context.Context, keys ...string) {
	for _, key := range keys {
		if err := c.kv.Delete(ctx, key); err != nil {
			c.log.Error().Err(err).Str("key", key).Msg("cache.Invalidate")
		}
	}
}
