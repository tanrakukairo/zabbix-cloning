package store

import "testing"

func TestRedisKeysUseSeparateNamespaces(t *testing.T) {
	versionID := "9b9382da-38f2-4a70-9b5f-282b7799a56c"
	versionKey := redisVersionKey(versionID)
	dataKey := redisDataKey(versionID)
	if versionKey != "ZC_VERSION:"+versionID {
		t.Fatalf("unexpected version key: %s", versionKey)
	}
	if dataKey != "ZC_DATA:"+versionID {
		t.Fatalf("unexpected data key: %s", dataKey)
	}
	if versionKey == dataKey {
		t.Fatal("version and data keys must not collide in a single Redis database")
	}
}
