package sglog

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NOTE: Most of the following code is copied from the example
// slog-handler-guide.

// groupOrAttrs holds either a group name or a list of slog.Attrs.
type groupOrAttrs struct {
	group string      // group name if non-empty
	attrs []slog.Attr // attrs if non-empty
}

type slogHandler struct {
	// mu is a pointer cause it is shared by all copies of the Handler created
	// for different groups and attributes.
	mu *sync.Mutex

	backend *Backend

	goas []groupOrAttrs
}

func (v *Backend) newHandler(opts *Options) *slogHandler {
	return &slogHandler{
		backend: v,
		mu:      new(sync.Mutex),
	}
}

func (h *slogHandler) withGroupOrAttrs(goa groupOrAttrs) *slogHandler {
	h2 := *h
	h2.goas = make([]groupOrAttrs, len(h.goas)+1)
	copy(h2.goas, h.goas)
	h2.goas[len(h2.goas)-1] = goa
	return &h2
}

// WithGroup implements the WithGroup method for slog.Handler interface.
func (h *slogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return h.withGroupOrAttrs(groupOrAttrs{group: name})
}

// WithAttrs implements the WithAttrs method for slog.Handler interface.
func (h *slogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return h.withGroupOrAttrs(groupOrAttrs{attrs: attrs})
}

// Enabled implements the Enabled method for slog.Handler interface.
func (h *slogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.backend.currentLevel.Level()
}

// Handle implements the Handle method for slog.Handler interface.
func (h *slogHandler) Handle(ctx context.Context, r slog.Record) error {
	bufi := bufs.Get()
	var buf *bytes.Buffer
	if bufi == nil {
		buf = bytes.NewBuffer(nil)
		bufi = buf
	} else {
		buf = bufi.(*bytes.Buffer)
		buf.Reset()
	}
	defer bufs.Put(bufi)

	h.format(ctx, buf, r)
	return h.backend.emit(r.Level, buf.Bytes())
}

// bufs is a pool of *bytes.Buffer used in formatting log entries.
var bufs sync.Pool // Pool of *bytes.Buffer.

func (h *slogHandler) format(ctx context.Context, buf *bytes.Buffer, r slog.Record) {

	// Lmmdd hh:mm:ss.uuuuuu PID/GID file:line]
	//
	// The "PID" entry arguably ought to be TID for consistency with other
	// environments, but TID is not meaningful in a Go program due to the
	// multiplexing of goroutines across threads.
	//
	// Avoid Fprintf, for speed. The format is so simple that we can do it quickly by hand.
	// It's worth about 3X. Fprintf is hard.

	switch {
	case r.Level >= slog.LevelError:
		buf.WriteByte(byte('E'))
	case r.Level >= slog.LevelWarn:
		buf.WriteByte(byte('W'))
	default:
		buf.WriteByte(byte('I'))
	}

	_, month, day := r.Time.Date()
	hour, minute, second := r.Time.Clock()
	twoDigits(buf, int(month))
	twoDigits(buf, day)
	buf.WriteByte(' ')
	twoDigits(buf, hour)
	buf.WriteByte(':')
	twoDigits(buf, minute)
	buf.WriteByte(':')
	twoDigits(buf, second)
	buf.WriteByte('.')
	nDigits(buf, 6, uint64(r.Time.Nanosecond()/1000), '0')
	buf.WriteByte(' ')

	nDigits(buf, 7, uint64(pid), ' ')
	buf.WriteByte(' ')

	file, line := "unknownfile.go", 0
	if r.PC != 0 {
		fs := runtime.CallersFrames([]uintptr{r.PC})
		f, _ := fs.Next()
		file, line = f.File, f.Line
	}

	{
		if i := strings.LastIndex(file, "/"); i >= 0 {
			file = file[i+1:]
		}
		buf.WriteString(file)
	}

	buf.WriteByte(':')
	{
		var tmp [19]byte
		buf.Write(strconv.AppendInt(tmp[:0], int64(line), 10))
	}
	buf.WriteString("] ")

	fmt.Fprintf(buf, r.Message)

	// Handle state from WithGroup and WithAttrs.
	goas := h.goas
	if r.NumAttrs() == 0 {
		// If the record has no Attrs, remove groups at the end of the list; they are empty.
		for len(goas) > 0 && goas[len(goas)-1].group != "" {
			goas = goas[:len(goas)-1]
		}
	}

	prefix := ""
	for _, goa := range goas {
		if goa.group != "" {
			prefix = fmt.Sprintf("%s.", goa.group)
		} else {
			for _, a := range goa.attrs {
				h.appendAttr(buf, a, prefix)
			}
		}
	}
	r.Attrs(func(a slog.Attr) bool {
		h.appendAttr(buf, a, prefix)
		return true
	})

	if buf.Len() > h.backend.opts.LogMessageMaxLen-1 {
		buf.Truncate(h.backend.opts.LogMessageMaxLen - 1)
	}
	if b := buf.Bytes(); b[len(b)-1] != '\n' {
		buf.WriteByte('\n')
	}
	fmt.Fprintf(os.Stderr, "%s", buf.Bytes())
}

const digits = "0123456789"

// twoDigits formats a zero-prefixed two-digit integer to buf.
func twoDigits(buf *bytes.Buffer, d int) {
	buf.WriteByte(digits[(d/10)%10])
	buf.WriteByte(digits[d%10])
}

// nDigits formats an n-digit integer to buf, padding with pad on the left. It
// assumes d != 0.
func nDigits(buf *bytes.Buffer, n int, d uint64, pad byte) {
	var tmp [20]byte

	cutoff := len(tmp) - n
	j := len(tmp) - 1
	for ; d > 0; j-- {
		tmp[j] = digits[d%10]
		d /= 10
	}
	for ; j >= cutoff; j-- {
		tmp[j] = pad
	}
	j++
	buf.Write(tmp[j:])
}

func (h *slogHandler) appendAttr(buf *bytes.Buffer, a slog.Attr, prefix string) {
	// Resolve the Attr's value before doing anything else.
	a.Value = a.Value.Resolve()
	// Ignore empty Attrs.
	if a.Equal(slog.Attr{}) {
		return
	}

	switch a.Value.Kind() {
	case slog.KindString:
		// Quote string values, to make them easy to parse.
		fmt.Fprintf(buf, " %s%s=%q", prefix, a.Key, a.Value.String())

	case slog.KindTime:
		// Write times in a standard way, without the monotonic time.
		fmt.Fprintf(buf, " %s%s=%s", prefix, a.Key, a.Value.Time().Format(time.RFC3339Nano))

	case slog.KindGroup:
		attrs := a.Value.Group()
		// Ignore empty groups.
		if len(attrs) == 0 {
			return
		}
		if a.Key != "" {
			prefix = fmt.Sprintf("%s%s.", prefix, a.Key)
		}
		for _, ga := range attrs {
			h.appendAttr(buf, ga, prefix)
		}

	default:
		if len(prefix) == 0 {
			fmt.Fprintf(buf, " %s=%s", a.Key, a.Value)
		} else {
			fmt.Fprintf(buf, " %s%s=%s", prefix, a.Key, a.Value)
		}
	}
}
