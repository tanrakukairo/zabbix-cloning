package store

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	redislib "github.com/redis/go-redis/v9"
	"github.com/tanrakukairo/zabbix-cloning/internal/config"
	"github.com/tanrakukairo/zabbix-cloning/internal/model"
)

type Redis struct{ versions, data *redislib.Client }

func NewRedis(ctx context.Context, cfg *config.Config) (*Redis, error) {
	address := fmt.Sprintf("%s:%d", firstNonEmpty(cfg.StoreEndpoint, "localhost"), cfg.StorePort)
	options := &redislib.Options{Addr: address, Password: cfg.StoreCredential}
	versionOptions := *options
	versionOptions.DB = 0
	dataOptions := *options
	dataOptions.DB = 1
	store := &Redis{versions: redislib.NewClient(&versionOptions), data: redislib.NewClient(&dataOptions)}
	if err := store.versions.Ping(ctx).Err(); err != nil {
		_ = store.Close()
		return nil, err
	}
	if err := store.data.Ping(ctx).Err(); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func (r *Redis) Versions(ctx context.Context, versionID string) ([]model.Version, error) {
	var keys []string
	if versionID != "" {
		keys = []string{versionID}
	} else {
		var cursor uint64
		for {
			batch, next, err := r.versions.Scan(ctx, cursor, "*", 500).Result()
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
		values, err := r.versions.HGetAll(ctx, key).Result()
		if err != nil {
			return nil, err
		}
		if len(values) == 0 {
			continue
		}
		unixTime, _ := strconv.ParseInt(values["UNIXTIME"], 10, 64)
		masterVersion, _ := strconv.ParseFloat(values["MASTER_VERSION"], 64)
		versions = append(versions, model.Version{VersionID: key, UnixTime: unixTime, MasterVersion: masterVersion, Description: values["DESCRIPTION"]})
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].UnixTime > versions[j].UnixTime })
	return versions, nil
}

func (r *Redis) Load(ctx context.Context, version model.Version) (model.Dataset, error) {
	values, err := r.data.HGetAll(ctx, version.VersionID).Result()
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
	pipe := r.data.TxPipeline()
	pipe.Del(ctx, version.VersionID)
	if len(dataValues) > 0 {
		pipe.HSet(ctx, version.VersionID, dataValues)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	return r.versions.HSet(ctx, version.VersionID, versionValues).Err()
}

func (r *Redis) Clear(ctx context.Context, table string) error {
	switch table {
	case "ALL":
		if err := r.versions.FlushDB(ctx).Err(); err != nil {
			return err
		}
		return r.data.FlushDB(ctx).Err()
	case "VERSION":
		return r.versions.FlushDB(ctx).Err()
	case "DATA":
		return r.data.FlushDB(ctx).Err()
	default:
		return fmt.Errorf("invalid table %q", table)
	}
}
func (r *Redis) DeleteRecord(ctx context.Context, versionID, dataID string) error {
	return r.data.HDel(ctx, versionID, dataID).Err()
}
func (r *Redis) DeleteVersion(ctx context.Context, versionID string) error {
	pipe := r.versions.TxPipeline()
	pipe.Del(ctx, versionID)
	r.data.Del(ctx, versionID)
	_, err := pipe.Exec(ctx)
	return err
}
func (r *Redis) Close() error {
	err := r.versions.Close()
	if dataErr := r.data.Close(); err == nil {
		err = dataErr
	}
	return err
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
