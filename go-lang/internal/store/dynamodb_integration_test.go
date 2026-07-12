package store

import (
	"context"
	"os"
	"testing"

	"github.com/t2-f/zabbix-cloning/internal/config"
)

func TestDynamoDBFileCompatibility(t *testing.T) {
	endpoint := os.Getenv("ZC_TEST_DYNAMODB_URL")
	filePath := os.Getenv("ZC_TEST_FILE_STORE")
	versionID := os.Getenv("ZC_TEST_VERSION_ID")
	if endpoint == "" || filePath == "" || versionID == "" {
		t.Skip("DynamoDB integration environment is not configured")
	}

	ctx := context.Background()
	fileStore := NewFile(filePath)
	versions, err := fileStore.Versions(ctx, versionID)
	if err != nil {
		t.Fatal(err)
	}
	version, err := Latest(versions, versionID)
	if err != nil {
		t.Fatal(err)
	}
	dataset, err := fileStore.Load(ctx, version)
	if err != nil {
		t.Fatal(err)
	}

	dynamoStore, err := NewDynamoDB(ctx, &config.Config{
		AWSRegion: "ap-northeast-1", AWSEndpointURL: endpoint,
		StoreAccess: "test", StoreCredential: "test",
		StoreLimit: 10000, StoreInterval: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := dynamoStore.Save(ctx, version, dataset); err != nil {
		t.Fatal(err)
	}
	loaded, err := dynamoStore.Load(ctx, version)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Records(versionID)) != len(dataset.Records(versionID)) {
		t.Fatalf("record count differs: wrote=%d read=%d", len(dataset.Records(versionID)), len(loaded.Records(versionID)))
	}
}
