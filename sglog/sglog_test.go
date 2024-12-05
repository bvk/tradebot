package sglog

import (
	"log"
	"testing"
	"time"

	"log/slog"
)

func TestFileName(t *testing.T) {
	backend := NewBackend(&Options{
		LogFileMaxSize: 5 * 1024 * 1024,
	})
	defer backend.Close()

	lfile := backend.newLevelFile(slog.LevelInfo)

	now := time.Now().Truncate(time.Second)
	fileName := lfile.fileName(now)
	ts, err := lfile.fileTime(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if !now.Equal(ts) {
		t.Logf("file name for time %v is %q", now, fileName)
		t.Fatalf("file time parsed back (%v)is not equal to the original (%v)", ts, now)
	}
}

func TestBasic(t *testing.T) {
	log.SetFlags(log.Lshortfile)

	backend := NewBackend(&Options{
		LogFileMaxSize: 4 * 1024,
	})
	defer backend.Flush()

	slog.SetDefault(slog.New(backend.Handler()))

	slog.Info("info message", "key", "value", "one", 1)
	slog.Warn("warning message", "key", "value", "one", 1)
	slog.Error("error message", "key", "value", "one", 1)

	slog.Debug("debug message before EnableDebugLog")
	backend.EnableDebugLog()
	slog.Debug("debug message after EnableDebugLog")
	slog.Info("info message after EnableDebugLog")
	backend.DisableDebugLog()
	slog.Debug("debug message after DisableDebugLog")
	slog.Info("info message after DisableDebugLog")

	slog.Info("info message with group", slog.Group("g", slog.Int("a", 1), slog.Int("b", 2)))
	slog.Info("info message with group", slog.Group("g1", slog.Group("g2", slog.Int("a", 1), slog.Int("b", 2))))

	log.Printf("hello world %d", 123)
}

func TestLogFileRotation(t *testing.T) {
	log.SetFlags(log.Lshortfile)

	backend := NewBackend(&Options{
		LogFileMaxSize: 1024 * 1024,
		LogDirs:        []string{"."},
	})
	defer backend.Flush()

	slog.SetDefault(slog.New(backend.Handler()))

	for i := 0; i < 1024; i++ {
		slog.Info("info message", "key", "value", "iteration", i)
		slog.Warn("warning message", "key", "value", "iteration", i)
		slog.Error("error message", "key", "value", "iteration", i)

		slog.Debug("debug message before EnableDebugLog", "iteration", i)
		backend.EnableDebugLog()
		slog.Debug("debug message after EnableDebugLog", "iteration", i)
		slog.Info("info message after EnableDebugLog", "iteration", i)
		backend.DisableDebugLog()
		slog.Debug("debug message after DisableDebugLog", "iteration", i)
		slog.Info("info message after DisableDebugLog", "iteration", i)

		slog.Info("info message with group", "iteration", i, slog.Group("g", slog.Int("a", 1), slog.Int("b", 2)))
		slog.Info("info message with group", "iteration", i, slog.Group("g1", slog.Group("g2", slog.Int("a", 1), slog.Int("b", 2))))

		log.Printf("hello world [iteration=%d]", i)
	}
}
