package view

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/tanrakukairo/zabbix-cloning/internal/config"
	"github.com/tanrakukairo/zabbix-cloning/internal/model"
	"github.com/tanrakukairo/zabbix-cloning/internal/store"
)

func Run(ctx context.Context, cfg *config.Config, out io.Writer) error {
	dataStore, err := store.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer dataStore.Close()
	fmt.Fprintf(out, "STORE TYPE:[ %s ] / COMMAND: %s\n", cfg.StoreType, cfg.Command)
	versions, err := dataStore.Versions(ctx, cfg.TargetVersion)
	if err != nil {
		return err
	}
	if cfg.Command == "showversions" {
		return showVersions(out, versions, cfg.IDOnly)
	}
	version, err := store.Latest(versions, cfg.TargetVersion)
	if err != nil {
		return err
	}
	dataset, err := dataStore.Load(ctx, version)
	if err != nil {
		return err
	}
	return showData(out, dataset, cfg.Method, cfg.Name, cfg.IDOnly)
}

func showVersions(out io.Writer, versions []model.Version, idOnly bool) error {
	fmt.Fprintln(out, "In Store Versions:")
	for _, version := range versions {
		if idOnly {
			fmt.Fprintf(out, "  %s: %d\n", version.VersionID, version.UnixTime)
			continue
		}
		data, _ := json.MarshalIndent(version, "  ", "  ")
		fmt.Fprintf(out, "  %s\n", data)
	}
	return nil
}
func showData(out io.Writer, dataset model.Dataset, methods, names []string, idOnly bool) error {
	methodFilter := set(methods)
	nameFilter := set(names)
	keys := make([]string, 0, len(dataset))
	for key := range dataset {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, method := range keys {
		if len(methodFilter) > 0 && !methodFilter[method] {
			continue
		}
		fmt.Fprintf(out, "%s:\n", method)
		items := sortedItems(dataset[method])
		for _, item := range items {
			if len(nameFilter) > 0 && !nameFilter[item.Name] {
				continue
			}
			if idOnly {
				fmt.Fprintf(out, "  %s: %s\n", item.DataID, item.Name)
				continue
			}
			data, _ := json.MarshalIndent(item, "  ", "  ")
			fmt.Fprintf(out, "  %s\n", data)
		}
	}
	return nil
}
func set(values []string) map[string]bool {
	result := map[string]bool{}
	for _, value := range values {
		result[value] = true
	}
	return result
}
func sortedItems(items []model.StoreItem) []model.StoreItem {
	result := append([]model.StoreItem(nil), items...)
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
