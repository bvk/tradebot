package sglog

import (
	"log/slog"
	"os"
	"time"
)

type Options struct {
	// LogDirs if non-empty write log files in this directory.
	LogDirs []string

	// LogLinkDir if non-empty, adds symbolic links in this directory to the log
	// files.
	LogLinkDir string

	// LogFileMaxSize is the maximum size of a log file in bytes. Header and
	// footer messages are not accounted in this so final log file size can be
	// larger than this limit by a few hundred bytes.
	LogFileMaxSize uint64

	// BufferSize sizes the buffer associated with each log file. It's large so
	// that log records can accumulate without the logging thread blocking on
	// disk I/O. The flushDaemon will block instead.
	BufferSize int

	// FlushTimeout is the maximum buffering time interval before writing
	// messsages to the file.
	FlushTimeout time.Duration

	// BufferedLogLevel is the log level (and below) which to buffer log
	// messages. Log messages above this are flushed immediately.
	BufferedLogLevel slog.Level

	// Log levels enabled for logging.
	Levels []slog.Level

	// LogFileMode is the log file mode/permissions.
	LogFileMode os.FileMode

	// LogFileHeader when true writes the file header at the start of each log
	// file.
	LogFileHeader bool

	// ReuseFileDuration maximum duration to reuse the last log file as long as
	// it doesn't cross the maximum log file size.
	ReuseFileDuration time.Duration

	// LogMessageMaxLen is the limit on length of a formatted log message,
	// including the standard line prefix and trailing newline.
	LogMessageMaxLen int
}

func (v *Options) setDefaults() {
	if len(v.LogDirs) == 0 {
		v.LogDirs = []string{os.TempDir()}
	} else {
		v.LogDirs = append(v.LogDirs, os.TempDir())
	}
	if v.LogFileMaxSize == 0 {
		v.LogFileMaxSize = 1024 * 1024 * 1800
	}
	if v.BufferSize == 0 {
		v.BufferSize = 256 * 1024
	}
	if len(v.Levels) == 0 {
		v.Levels = []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
	}
	if v.FlushTimeout == 0 {
		v.FlushTimeout = 30 * time.Second
	}
	if v.LogFileMode == 0 {
		v.LogFileMode = 0644
	}
	if v.ReuseFileDuration == 0 {
		v.ReuseFileDuration = time.Hour
	}
	if v.LogMessageMaxLen == 0 {
		v.LogMessageMaxLen = 15000
	}
}
