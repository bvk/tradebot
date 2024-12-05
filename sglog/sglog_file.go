// Go support for leveled logs, analogous to https://github.com/google/glog.
//
// Copyright 2023 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// File I/O for logs.

package sglog

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

var (
	pid      = os.Getpid()
	program  = filepath.Base(os.Args[0])
	host     = "unknownhost"
	userName = "unknownuser"
)

func init() {
	h, err := os.Hostname()
	if err == nil {
		host = shortHostname(h)
	}

	if strings.ContainsRune(program, '.') {
		program, _, _ = strings.Cut(program, ".")
	}

	if u := lookupUser(); u != "" {
		userName = u
	}
	// Sanitize userName since it is used to construct file paths.
	userName = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		default:
			return '_'
		}
		return r
	}, userName)
}

// shortHostname returns its argument, truncating at the first period.
// For instance, given "www.google.com" it returns "www".
func shortHostname(hostname string) string {
	if i := strings.Index(hostname, "."); i >= 0 {
		return hostname[:i]
	}
	return hostname
}

type levelFile struct {
	backend *Backend

	level slog.Level

	filePrefix string

	file   *os.File
	bio    *bufio.Writer
	nbytes uint64

	fpaths []string
}

func (v *Backend) newLevelFile(level slog.Level) *levelFile {
	return &levelFile{
		backend:    v,
		level:      level,
		filePrefix: fmt.Sprintf("%s.%s.%s.log.%s", program, host, userName, level.String()),
	}
}

func (f *levelFile) Write(p []byte) (n int, err error) {
	if f.file == nil || f.nbytes >= f.backend.opts.LogFileMaxSize {
		if err := f.rotateFile(time.Now()); err != nil {
			return 0, err
		}
	}
	n, err = f.bio.Write(p)
	f.nbytes += uint64(n)
	return n, err
}

func (f *levelFile) Sync() error {
	return f.file.Sync()
}

func (f *levelFile) Flush() error {
	if f.bio == nil {
		return nil
	}
	return f.bio.Flush()
}

func (f *levelFile) fileName(t time.Time) string {
	return f.filePrefix + t.Format(".20060102-150405.") + "0"
}

func (f *levelFile) fileTime(name string) (time.Time, error) {
	return time.ParseInLocation(f.filePrefix+".20060102-150405.0", name, time.Local)
}

func (f *levelFile) linkName(t time.Time) string {
	return program + "." + f.level.String()
}

func (f *levelFile) lastFileName(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	var times []time.Time
	for _, entry := range entries {
		t, err := f.fileTime(entry.Name())
		if err != nil {
			continue
		}
		times = append(times, t)
	}
	if len(times) == 0 {
		return "", nil
	}

	last := slices.MaxFunc(times, time.Time.Compare)
	return f.fileName(last), nil
}

func (f *levelFile) filePath(dir string, t time.Time) (string, int64, error) {
	if len(f.fpaths) == 0 {
		lastName, err := f.lastFileName(dir)
		if err != nil {
			return "", 0, err
		}

		if lastName != "" {
			lastFileTime, err := f.fileTime(lastName)
			if err != nil {
				return "", 0, err
			}

			if lastFileTime.After(t.Truncate(f.backend.opts.ReuseFileDuration)) {
				lastPath := filepath.Join(dir, lastName)
				fstat, err := os.Stat(lastPath)
				if err != nil {
					return "", 0, err
				}

				if size := fstat.Size(); uint64(size) < f.backend.opts.LogFileMaxSize {
					return lastPath, size, nil
				}
			}
		}
	}

	fpath := filepath.Join(dir, f.fileName(t))
	return fpath, 0, nil
}

func (f *levelFile) createFile(t time.Time) (fp *os.File, filename string, err error) {
	link := f.linkName(t)

	var lastErr error
	for _, dir := range f.backend.opts.LogDirs {
		fpath, offset, err := f.filePath(dir, t)
		if err != nil {
			lastErr = err
			continue
		}

		flags := os.O_WRONLY | os.O_CREATE
		fp, err := os.OpenFile(fpath, flags, f.backend.opts.LogFileMode)
		if err != nil {
			lastErr = err
			continue
		}
		if _, err := fp.Seek(offset, io.SeekStart); err != nil {
			lastErr = err
			fp.Close()
			continue
		}
		f.nbytes = uint64(offset)

		{
			fname := filepath.Base(fpath)
			symlink := filepath.Join(dir, link)
			os.Remove(symlink)         // ignore err
			os.Symlink(fname, symlink) // ignore err
			if f.backend.opts.LogLinkDir != "" {
				lsymlink := filepath.Join(f.backend.opts.LogLinkDir, link)
				os.Remove(lsymlink)         // ignore err
				os.Symlink(fname, lsymlink) // ignore err
			}
		}
		return fp, fpath, nil
	}
	return nil, "", fmt.Errorf("log: cannot create log: %w", lastErr)
}

func (f *levelFile) rotateFile(now time.Time) error {
	if f.bio != nil {
		f.bio.Flush()
	}

	var err error
	pn := "<none>"
	file, fpath, err := f.createFile(now)
	if err != nil {
		return err
	}

	if f.file != nil {
		// The current log file becomes the previous log at the end of
		// this block, so save its name for use in the header of the next
		// file.
		pn = f.file.Name()
		f.bio.Flush()
		f.file.Close()
	}

	f.file = file
	f.fpaths = append(f.fpaths, fpath)
	f.bio = bufio.NewWriterSize(f.file, f.backend.opts.BufferSize)

	if f.backend.opts.LogFileHeader {
		if f.nbytes == 0 {
			// Write header.
			var buf bytes.Buffer
			fmt.Fprintf(&buf, "Log file created at: %s\n", now.Format("2006/01/02 15:04:05"))
			fmt.Fprintf(&buf, "Running on machine: %s\n", host)
			fmt.Fprintf(&buf, "Binary: Built with %s %s for %s/%s\n", runtime.Compiler, runtime.Version(), runtime.GOOS, runtime.GOARCH)
			fmt.Fprintf(&buf, "Previous log: %s\n", pn)
			fmt.Fprintf(&buf, "Log line format: [IWEF]mmdd hh:mm:ss.uuuuuu threadid file:line] msg\n")
			n, err := f.file.Write(buf.Bytes())
			f.nbytes += uint64(n)
			if err != nil {
				return err
			}
		} else {
			// Write header.
			var buf bytes.Buffer
			fmt.Fprintf(&buf, "Log file is reopened at: %s\n", now.Format("2006/01/02 15:04:05"))
			fmt.Fprintf(&buf, "Running on machine: %s\n", host)
			fmt.Fprintf(&buf, "Binary: Built with %s %s for %s/%s\n", runtime.Compiler, runtime.Version(), runtime.GOOS, runtime.GOARCH)
			n, err := f.file.Write(buf.Bytes())
			f.nbytes += uint64(n)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
