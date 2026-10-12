package redisaccel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
	"github.com/redis/go-redis/v9"
)

const listTTL = 10 * time.Second

var loginScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then redis.call('EXPIRE', KEYS[1], ARGV[1]) end
return count
`)

// Layer is optional acceleration. PostgreSQL remains the metadata source of truth.
type Layer struct {
	client *redis.Client
}

func Open(ctx context.Context, rawURL string) (*Layer, error) {
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse Redis URL: %w", err)
	}
	options.DialTimeout = time.Second
	options.ReadTimeout = time.Second
	options.WriteTimeout = time.Second
	client := redis.NewClient(options)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connect Redis: %w", err)
	}
	return &Layer{client: client}, nil
}

func (l *Layer) Close() error { return l.client.Close() }

// AllowLogin atomically limits each remote IP to 10 attempts per rolling minute.
func (l *Layer) AllowLogin(ctx context.Context, remote string) (bool, error) {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	digest := sha256.Sum256([]byte(host))
	key := "homeos:login:" + hex.EncodeToString(digest[:])
	count, err := loginScript.Run(ctx, l.client, []string{key}, 60).Int64()
	return count <= 10, err
}

func (l *Layer) Generation(ctx context.Context, ownerID int64) (string, error) {
	value, err := l.client.Get(ctx, fmt.Sprintf("homeos:drive:gen:%d", ownerID)).Result()
	if err == redis.Nil {
		return "0", nil
	}
	return value, err
}

func listKey(ownerID int64, folder, generation string) string {
	digest := sha256.Sum256([]byte(folder))
	return fmt.Sprintf("homeos:drive:list:%d:%s:%s", ownerID, generation, hex.EncodeToString(digest[:]))
}

func (l *Layer) ListFiles(ctx context.Context, ownerID int64, folder string) ([]drive.Entry, bool, string, error) {
	generation, err := l.Generation(ctx, ownerID)
	if err != nil {
		return nil, false, "", err
	}
	data, err := l.client.Get(ctx, listKey(ownerID, folder, generation)).Bytes()
	if err == redis.Nil {
		return nil, false, generation, nil
	}
	if err != nil {
		return nil, false, "", err
	}
	var entries []drive.Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, false, "", err
	}
	return entries, true, generation, nil
}

func (l *Layer) StoreFiles(ctx context.Context, ownerID int64, folder, generation string, entries []drive.Entry) error {
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	return l.client.Set(ctx, listKey(ownerID, folder, generation), data, listTTL).Err()
}

// InvalidateOwner changes the namespace for every cached folder of one owner.
func (l *Layer) InvalidateOwner(ctx context.Context, ownerID int64) error {
	return l.client.Incr(ctx, fmt.Sprintf("homeos:drive:gen:%d", ownerID)).Err()
}
