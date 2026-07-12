package store

import (
	"context"
	"testing"

	"github.com/tanrakukairo/zabbix-cloning/internal/model"
)

func TestFileRoundTrip(t *testing.T) {
	ctx := context.Background()
	backend := NewFile(t.TempDir())
	version := model.Version{
		VersionID: "1511bbf1-8404-44ab-957c-495bc0d4385b",
		UnixTime:  1771490091, MasterVersion: 7.0, Description: "test",
	}
	dataset := model.Dataset{
		"host": {{DataID: "data-1", Name: "example", Data: model.Object{"host": "example"}}},
	}
	if err := backend.Save(ctx, version, dataset); err != nil {
		t.Fatal(err)
	}
	versions, err := backend.Versions(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].VersionID != version.VersionID {
		t.Fatalf("unexpected versions: %#v", versions)
	}
	loaded, err := backend.Load(ctx, versions[0])
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded["host"][0].Data["host"]; got != "example" {
		t.Fatalf("unexpected host: %v", got)
	}
}
