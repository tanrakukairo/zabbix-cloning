package store

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	redislib "github.com/redis/go-redis/v9"
	"github.com/tanrakukairo/zabbix-cloning/internal/config"
	"github.com/tanrakukairo/zabbix-cloning/internal/model"
)

const (
	redisVersionPrefix = "ZC_VERSION:"
	redisDataPrefix    = "ZC_DATA:"
)

type Redis struct{ client *redislib.Client }

func NewRedis(ctx context.Context, cfg *config.Config) (*Redis, error) {
	address := fmt.Sprintf("%s:%d", firstNonEmpty(cfg.StoreEndpoint, "localhost"), cfg.StorePort)
	options := &redislib.Options{Addr: address, Password: cfg.StoreCredential, DB: cfg.StoreDB}
	store := &Redis{client: redislib.NewClient(options)}
	if err := store.client.Ping(ctx).Err(); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func (r *Redis) Versions(ctx context.Context, versionID string) ([]model.Version, error) {
	var keys []string
	if versionID != "" {
		keys = []string{redisVersionKey(versionID)}
	} else {
		var cursor uint64
		for {
			batch, next, err := r.client.Scan(ctx, cursor, redisVersionPrefix+"*", 500).Result()
			if err != nil {
				return nil, err
			}
			keys = append(keys, batch...)
			cursor = next
			if cursor == 0 {
				break
			}
		}
	}
	versions := make([]model.Version, 0, len(keys))
	for _, key := range keys {
		values, err := r.client.HGetAll(ctx, key).Result()
		if err != nil {
			return nil, err
		}
		if len(values) == 0 {
			continue
		}
		unixTime, _ := strconv.ParseInt(values["UNIXTIME"], 10, 64)
		masterVersion, _ := strconv.ParseFloat(values["MASTER_VERSION"], 64)
		versions = append(versions, model.Version{VersionID: strings.TrimPrefix(key, redisVersionPrefix), UnixTime: unixTime, MasterVersion: masterVersion, Description: values["DESCRIPTION"]})
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].UnixTime > versions[j].UnixTime })
	return versions, nil
}

func (r *Redis) Load(ctx context.Context, version model.Version) (model.Dataset, error) {
	values, err := r.client.HGetAll(ctx, redisDataKey(version.VersionID)).Result()
	if err != nil {
		return nil, err
	}
	records := make([]model.Record, 0, len(values))
	for dataID, value := range values {
		var payload struct {
			Method string       `json:"METHOD"`
			Name   string       `json:"NAME"`
			Data   model.Object `json:"DATA"`
		}
		if err := decompressJSON([]byte(value), &payload); err != nil {
			return nil, fmt.Errorf("record %s: %w", dataID, err)
		}
		records = append(records, model.Record{DataID: dataID, Method: payload.Method, Name: payload.Name, Data: payload.Data})
	}
	return model.DatasetFromRecords(records), nil
}

func (r *Redis) Save(ctx context.Context, version model.Version, dataset model.Dataset) error {
	versionValues := map[string]any{"UNIXTIME": version.UnixTime, "MASTER_VERSION": strconv.FormatFloat(version.MasterVersion, 'f', 1, 64), "DESCRIPTION": version.Description}
	dataValues := map[string]any{}
	for _, record := range dataset.Records(version.VersionID) {
		payload, err := compressJSON(map[string]any{"METHOD": record.Method, "NAME": record.Name, "DATA": record.Data})
		if err != nil {
			return err
		}
		dataValues[record.DataID] = payload
	}
	pipe := r.client.TxPipeline()
	pipe.Del(ctx, redisDataKey(version.VersionID))
	if len(dataValues) > 0 {
		pipe.HSet(ctx, redisDataKey(version.VersionID), dataValues)
	}
	pipe.HSet(ctx, redisVersionKey(version.VersionID), versionValues)
	_, err := pipe.Exec(ctx)
	return err
}

func (r *Redis) Clear(ctx context.Context, table string) error {
	switch table {
	case "ALL":
		if err := r.deleteKeys(ctx, redisVersionPrefix+"*"); err != nil {
			return err
		}
		return r.deleteKeys(ctx, redisDataPrefix+"*")
	case "VERSION":
		return r.deleteKeys(ctx, redisVersionPrefix+"*")
	case "DATA":
		return r.deleteKeys(ctx, redisDataPrefix+"*")
	default:
		return fmt.Errorf("invalid table %q", table)
	}
}
func (r *Redis) DeleteRecord(ctx context.Context, versionID, dataID string) error {
	return r.client.HDel(ctx, redisDataKey(versionID), dataID).Err()
}
func (r *Redis) DeleteVersion(ctx context.Context, versionID string) error {
	pipe := r.client.TxPipeline()
	pipe.Del(ctx, redisVersionKey(versionID), redisDataKey(versionID))
	_, err := pipe.Exec(ctx)
	return err
}
func (r *Redis) Close() error {
	return r.client.Close()
}

func (r *Redis) deleteKeys(ctx context.Context, pattern string) error {
	var cursor uint64
	for {
		keys, next, err := r.client.Scan(ctx, cursor, pattern, 500).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := r.client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

func redisVersionKey(versionID string) string { return redisVersionPrefix + versionID }
func redisDataKey(versionID string) string    { return redisDataPrefix + versionID }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
