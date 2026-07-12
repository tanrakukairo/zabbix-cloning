package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"github.com/t2-f/zabbix-cloning/internal/model"
)

type File struct{ path string }

var filePattern = regexp.MustCompile(`^([0-9a-fA-F-]+)_([0-9]+)_([0-9]+(?:\.[0-9]+)?)\.bz2$`)

func NewFile(path string) *File { return &File{path: path} }

func (f *File) Versions(_ context.Context, versionID string) ([]model.Version, error) {
	entries, err := os.ReadDir(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []model.Version{}, nil
		}
		return nil, err
	}
	versions := make([]model.Version, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		parts := filePattern.FindStringSubmatch(entry.Name())
		if len(parts) != 4 || versionID != "" && parts[1] != versionID {
			continue
		}
		unixTime, err1 := strconv.ParseInt(parts[2], 10, 64)
		masterVersion, err2 := strconv.ParseFloat(parts[3], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		versions = append(versions, model.Version{
			VersionID: parts[1], UnixTime: unixTime, MasterVersion: masterVersion,
			Description: "Import File " + entry.Name(),
		})
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].UnixTime > versions[j].UnixTime })
	return versions, nil
}

func (f *File) Load(_ context.Context, version model.Version) (model.Dataset, error) {
	data, err := os.ReadFile(filepath.Join(f.path, filename(version)))
	if err != nil {
		return nil, err
	}
	dataset := model.Dataset{}
	if err := decompressJSON(data, &dataset); err != nil {
		return nil, err
	}
	return dataset, nil
}

func (f *File) Save(_ context.Context, version model.Version, dataset model.Dataset) error {
	if err := os.MkdirAll(f.path, 0o755); err != nil {
		return err
	}
	data, err := compressJSON(dataset)
	if err != nil {
		return err
	}
	path := filepath.Join(f.path, filename(version))
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func (f *File) Clear(ctx context.Context, table string) error {
	if table != "ALL" && table != "VERSION" && table != "DATA" {
		return fmt.Errorf("invalid table %q", table)
	}
	versions, err := f.Versions(ctx, "")
	if err != nil {
		return err
	}
	for _, version := range versions {
		if err := os.Remove(filepath.Join(f.path, filename(version))); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (f *File) DeleteRecord(_ context.Context, _, _ string) error {
	return fmt.Errorf("file datastore cannot delete an individual record")
}
func (f *File) DeleteVersion(ctx context.Context, versionID string) error {
	versions, err := f.Versions(ctx, versionID)
	if err != nil {
		return err
	}
	for _, version := range versions {
		if err := os.Remove(filepath.Join(f.path, filename(version))); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
func (f *File) Close() error { return nil }

func filename(version model.Version) string {
	masterVersion := strconv.FormatFloat(version.MasterVersion, 'f', 1, 64)
	return fmt.Sprintf("%s_%d_%s.bz2", version.VersionID, version.UnixTime, masterVersion)
}
