// Package sglog provides a log/slog logging handler that writes to multiple
// log files based on the log severity -- similar to the Google's glog package.
//
// Most of the code is copied from the Google's glog package for Go. However,
// there are some differences and a new log file reuse feature.
//
// # DIFFERENCES
//
//   - The standard log/slog package doesn't define Fatal level and log.Fatalf
//     is treated as LevelInfo, so FATAL log files are not supported by this
//     package.
//
//   - Globally defined flags are replaced with an Options structure.
//
//   - Thread-ID field in the log file names is always set to zero to enable
//     log file reuse across restarts. Thread-ID is still included in the
//     individual log messages, even though it is not very useful in Go.
//
//   - Google's glog package adds a footer message when a log file is rotated,
//     which is not supported in this package.
//
//   - When the log file reuse feature is enabled, log file names do not
//     exactly match the log file creation time. However, timestamps in the log
//     file names still follow the sorted order.
//
// # LOG FILE REUSE
//
// Google's glog package creates a new log file every time the process is
// restarted. This can exhaust filesystem inodes if the process is crashing
// repeatedly.
//
// This package changes the above behavior and enables log file reuse with a
// certain timeout (ex: maximum one log file per hour.) As part of this,
// thread-id field in the log file name is replaced with a zero.
//
// Note that log file is still rotated when the file size reaches up to the
// maximum limit.
package sglog
