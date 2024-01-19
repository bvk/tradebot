// Copyright (c) 2024 BVK Chaitanya

/*
Package logdir implements a log backend that limits log file(s) size to a fixed
size in a given directory.
*/
package logdir

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var (
	// FileNameReuseInterval contains the time interval during which a new log
	// backend instance will attempt to reuse (i.e., append-to) existing log file
	// name if present. This will avoid filling up the logs directory with too
	// many log files (and exhaust filesystem inodes) if the program happens to
	// be in a crash-loop.
	FileNameReuseInterval = time.Hour

	// FileNameTimeLocation contains the timezone for the timestamp in the log
	// file names.
	FileNameTimeLocation = time.UTC

	// FileSizeLimitMB contains the maximum size limit for the log files.
	FileSizeLimitMB int64 = 100

	// FileMode contains the file mode and permissions value for the log files.
	FileMode = os.FileMode(0600)
)

type Backend struct {
	fp *os.File

	size int64

	dirname, logname string
}

func New(dirname, logname string) (*Backend, error) {
	fp, size, err := openFile(dirname, logname, FileNameReuseInterval)
	if err != nil {
		return nil, fmt.Errorf("could not open log file: %w", err)
	}
	b := &Backend{
		fp:      fp,
		size:    size,
		dirname: dirname,
		logname: logname,
	}
	return b, nil
}

func (b *Backend) Close() {
	b.fp.Close()
	b.fp = nil
}

func fileName(logname string, at time.Time, truncate time.Duration) string {
	at = at.In(FileNameTimeLocation)
	if truncate != 0 {
		at = at.Truncate(truncate)
	}
	uniq := fmt.Sprintf("%d%02d%02d-%02d%02d%02d.%09d", at.Year(), at.Month(), at.Day(), at.Hour(), at.Minute(), at.Second(), at.Nanosecond())
	return fmt.Sprintf("%s-%s.log", logname, uniq)
}

func openFile(dirname, logname string, truncate time.Duration) (*os.File, int64, error) {
	filename := fileName(logname, time.Now(), truncate)
	fp, err := os.OpenFile(filepath.Join(dirname, filename), os.O_CREATE|os.O_WRONLY|os.O_APPEND, FileMode)
	if err != nil {
		return nil, -1, fmt.Errorf("could not open/create log file: %w", err)
	}
	finfo, err := fp.Stat()
	if err != nil {
		fp.Close()
		return nil, -1, fmt.Errorf("could not get file size: %w", err)
	}
	size := finfo.Size()
	if size >= FileSizeLimitMB*1024*1024 {
		fp.Close()
		return openFile(dirname, logname, 0)
	}
	return fp, size, nil
}

func (b *Backend) Write(data []byte) (int, error) {
	if b.size+int64(len(data)) > FileSizeLimitMB*1024*1024 {
		fp, size, err := openFile(b.dirname, b.logname, FileNameReuseInterval)
		if err != nil {
			return 0, fmt.Errorf("could not open new log file: %w", err)
		}
		b.fp.Close()
		b.fp, b.size = fp, size
	}
	n, err := b.fp.Write(data)
	b.size += int64(n)
	return n, err
}
