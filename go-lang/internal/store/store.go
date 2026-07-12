package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/t2-f/zabbix-cloning/internal/config"
	"github.com/t2-f/zabbix-cloning/internal/model"
)

type Store interface {
	Versions(ctx context.Context, versionID string) ([]model.Version, error)
	Load(ctx context.Context, version model.Version) (model.Dataset, error)
	Save(ctx context.Context, version model.Version, dataset model.Dataset) error
	Clear(ctx context.Context, table string) error
	DeleteRecord(ctx context.Context, versionID, dataID string) error
	DeleteVersion(ctx context.Context, versionID string) error
	Close() error
}

func Open(ctx context.Context, cfg *config.Config) (Store, error) {
	switch strings.ToLower(cfg.StoreType) {
	case "file":
		return NewFile(cfg.StorePath()), nil
	case "redis":
		return NewRedis(ctx, cfg)
	case "dydb":
		return NewDynamoDB(ctx, cfg)
	default:
		return nil, fmt.Errorf("unsupported datastore %q", cfg.StoreType)
	}
}

func Latest(versions []model.Version, target string) (model.Version, error) {
	if target != "" {
		for _, version := range versions {
			if version.VersionID == target {
				return version, nil
			}
		}
		return model.Version{}, fmt.Errorf("version %s does not exist", target)
	}
	if len(versions) == 0 {
		return model.Version{}, fmt.Errorf("no versions in datastore")
	}
	latest := versions[0]
	for _, version := range versions[1:] {
		if version.UnixTime > latest.UnixTime {
			latest = version
		}
	}
	return latest, nil
}
