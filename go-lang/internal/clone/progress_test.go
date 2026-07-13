package clone

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tanrakukairo/zabbix-cloning/internal/logx"
)

func TestQuietProgressLogsEveryFiftyAndFinalRemainder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.log")
	logger, err := logx.New("test", "CRITICAL", path, true)
	if err != nil {
		t.Fatal(err)
	}
	progress := newApplyProgress(logger, true, "Host Import", 125, "create")
	for i := 0; i < 124; i++ {
		progress.record("create")
	}
	progress.fail("failed-host", errors.New("request failed"))
	progress.finish()
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	log := string(data)
	for _, count := range []string{"50/125", "100/125", "125/125"} {
		if !strings.Contains(log, count) {
			t.Fatalf("progress log does not contain %s: %s", count, log)
		}
	}
	if strings.Count(log, "Host Import:") != 3 {
		t.Fatalf("unexpected progress/failure log count: %s", log)
	}
	if !strings.Contains(log, "Host Import [failed-host]: request failed") {
		t.Fatalf("failure was not logged: %s", log)
	}
}
