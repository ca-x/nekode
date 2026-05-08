package cache

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/redis/go-redis/v9"
)

const (
	DriverBadger = "badger"
	DriverRedis  = "redis"
	DriverNone   = "none"
)

var ErrNotFound = errors.New("cache: not found")

type Cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Close() error
}

type Options struct {
	Driver     string
	BadgerDir  string
	RedisAddr  string
	RedisUser  string
	RedisPass  string
	RedisDB    int
	DefaultTTL time.Duration
	KeyVersion string
}

func Open(ctx context.Context, opts Options) (Cache, error) {
	switch NormalizeDriver(opts.Driver) {
	case DriverBadger:
		if strings.TrimSpace(opts.BadgerDir) == "" {
			return nil, errors.New("badger cache dir is required")
		}
		if err := os.MkdirAll(opts.BadgerDir, 0o755); err != nil {
			return nil, err
		}
		db, err := badger.Open(badger.DefaultOptions(opts.BadgerDir).WithLogger(nil))
		if err != nil {
			return nil, err
		}
		return &badgerCache{db: db}, nil
	case DriverRedis:
		if strings.TrimSpace(opts.RedisAddr) == "" {
			return nil, errors.New("redis cache addr is required")
		}
		client := redis.NewClient(&redis.Options{
			Addr:     opts.RedisAddr,
			Username: opts.RedisUser,
			Password: opts.RedisPass,
			DB:       opts.RedisDB,
		})
		if err := client.Ping(ctx).Err(); err != nil {
			_ = client.Close()
			return nil, err
		}
		return &redisCache{client: client}, nil
	case DriverNone:
		return nopCache{}, nil
	default:
		return nil, fmt.Errorf("unsupported cache driver %q", opts.Driver)
	}
}

func NormalizeDriver(driver string) string {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "", DriverBadger:
		return DriverBadger
	case DriverRedis:
		return DriverRedis
	case DriverNone, "off", "disabled":
		return DriverNone
	default:
		return strings.ToLower(strings.TrimSpace(driver))
	}
}

func DefaultDir(dataDir string) string {
	return filepath.Join(dataDir, "cache")
}

func ProjectionKey(serverID string, protocolVersion int32, cacheVersion string, parts ...string) string {
	if cacheVersion == "" {
		cacheVersion = "v1"
	}
	keyParts := []string{
		"nekode",
		"projection",
		escapePart(serverID),
		"p" + strconv.FormatInt(int64(protocolVersion), 10),
		escapePart(cacheVersion),
	}
	for _, part := range parts {
		keyParts = append(keyParts, escapePart(part))
	}
	return strings.Join(keyParts, ":")
}

func escapePart(value string) string {
	if value == "" {
		return "-"
	}
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

type badgerCache struct {
	db *badger.DB
}

func (c *badgerCache) Get(ctx context.Context, key string) ([]byte, error) {
	var value []byte
	err := c.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		value, err = item.ValueCopy(nil)
		return err
	})
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return value, nil
	}
}

func (c *badgerCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return c.db.Update(func(txn *badger.Txn) error {
		entry := badger.NewEntry([]byte(key), value)
		if ttl > 0 {
			entry = entry.WithTTL(ttl)
		}
		return txn.SetEntry(entry)
	})
}

func (c *badgerCache) Delete(ctx context.Context, key string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return c.db.Update(func(txn *badger.Txn) error {
		err := txn.Delete([]byte(key))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		return err
	})
}

func (c *badgerCache) Close() error {
	return c.db.Close()
}

type redisCache struct {
	client *redis.Client
}

func (c *redisCache) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := c.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotFound
	}
	return value, err
}

func (c *redisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return c.client.Set(ctx, key, value, ttl).Err()
}

func (c *redisCache) Delete(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}

func (c *redisCache) Close() error {
	return c.client.Close()
}

type nopCache struct{}

func (nopCache) Get(context.Context, string) ([]byte, error) {
	return nil, ErrNotFound
}

func (nopCache) Set(context.Context, string, []byte, time.Duration) error {
	return nil
}

func (nopCache) Delete(context.Context, string) error {
	return nil
}

func (nopCache) Close() error {
	return nil
}
