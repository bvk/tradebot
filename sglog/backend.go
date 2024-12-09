package sglog

import (
	"log/slog"
	"sync"
	"time"
)

type Backend struct {
	mu sync.Mutex
	wg sync.WaitGroup

	opts *Options

	handler *slogHandler

	fileMap map[slog.Level]*levelFile

	flushChan chan slog.Level

	currentLevel slog.LevelVar
}

// NewBackend creates a slog backend.
func NewBackend(opts *Options) *Backend {
	opts.setDefaults()
	v := &Backend{
		opts:      opts,
		fileMap:   make(map[slog.Level]*levelFile),
		flushChan: make(chan slog.Level, 1),
	}
	v.handler = v.newHandler(opts)

	for _, l := range v.opts.Levels {
		v.fileMap[l] = v.newLevelFile(l)
	}

	v.wg.Add(1)
	go v.flushDaemon()
	return v
}

// Close flushes the logs and waits for the background goroutine to finish.
func (v *Backend) Close() {
	close(v.flushChan)
	v.wg.Wait()
}

// Handler returns slog.Handler for the log backend.
func (v *Backend) Handler() slog.Handler {
	return v.handler
}

// EnableDebugLog enables logging for slog.LevelDebug messages.
func (v *Backend) EnableDebugLog() {
	v.currentLevel.Set(slog.LevelDebug)
}

// DisableDebugLog disables logging for slog.LevelDebug messages.
func (v *Backend) DisableDebugLog() {
	v.currentLevel.Set(slog.LevelInfo)
}

func normalize(v slog.Level) slog.Level {
	if v >= slog.LevelError {
		return slog.LevelError
	}
	if v >= slog.LevelWarn {
		return slog.LevelWarn
	}
	if v >= slog.LevelInfo {
		return slog.LevelInfo
	}
	if v >= slog.LevelDebug {
		return slog.LevelDebug
	}
	return slog.LevelDebug
}

func (v *Backend) emit(level slog.Level, msg []byte) error {
	level = normalize(level)

	v.mu.Lock()
	var firstErr error
	for l, f := range v.fileMap {
		if l < v.currentLevel.Level() {
			continue
		}

		if l <= level {
			if _, err := f.Write(msg); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	v.mu.Unlock()

	// Log messages above the LogBufLevel are flushed immediately. FIXME: we
	// should not miss a log level just because channel is full.
	if level > v.opts.BufferedLogLevel {
		select {
		case v.flushChan <- level:
		default:
		}
	}

	return firstErr
}

// Flush force writes log messages to the log files.
func (v *Backend) Flush() error {
	return v.flush(slog.LevelDebug)
}

func (v *Backend) flush(level slog.Level) error {
	var firstErr error
	updateErr := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// Remember where we flushed, so we can call sync without holding
	// the lock.
	var files []*levelFile

	func() {
		// Flush from fatal down, in case there's trouble flushing.
		for l, lf := range v.fileMap {
			if l >= level {
				updateErr(lf.Flush())
				files = append(files, lf)
			}
		}
	}()

	for _, file := range files {
		updateErr(file.Sync())
	}
	return firstErr
}

func (v *Backend) flushDaemon() {
	defer v.wg.Done()

	tick := time.NewTicker(v.opts.FlushTimeout)
	defer tick.Stop()

	for {
		select {
		case <-tick.C:
			v.flush(slog.LevelDebug)

		case sev, ok := <-v.flushChan:
			if !ok {
				return
			}
			v.flush(sev)
		}
	}
}
