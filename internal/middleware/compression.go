package middleware

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/andybalholm/brotli"
)

// CompressionConfig controls response compression behavior.
type CompressionConfig struct {
	Enabled       bool
	Level         int
	MinSize       int
	ExcludedTypes []string
}

// DefaultCompressionConfig returns production-ready compression configuration.
func DefaultCompressionConfig() *CompressionConfig {
	return &CompressionConfig{
		Enabled: true,
		Level:   gzip.DefaultCompression,
		MinSize: 1024,
		ExcludedTypes: []string{
			"image/",
			"video/",
			"audio/",
			"application/zip",
			"application/gzip",
			"application/x-brotli",
			"application/pdf",
		},
	}
}

var errUnsupportedEncoding = errors.New("unsupported encoding")

// NewCompression returns middleware that compresses HTTP responses
// based on Accept-Encoding negotiation.
func NewCompression(cfg *CompressionConfig) func(http.Handler) http.Handler {
	if cfg == nil {
		cfg = DefaultCompressionConfig()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			rec := &compressRecorder{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
				header:         make(http.Header),
			}
			next.ServeHTTP(rec, r)

			// Skip already compressed responses
			if rec.header.Get("Content-Encoding") != "" {
				copyHeaders(w, rec.header)
				w.WriteHeader(rec.statusCode)
				w.Write(rec.body)
				return
			}

			// Skip small responses
			if len(rec.body) < cfg.MinSize {
				copyHeaders(w, rec.header)
				w.WriteHeader(rec.statusCode)
				w.Write(rec.body)
				return
			}

			// Skip excluded content types
			ct := rec.header.Get("Content-Type")
			if isExcludedType(ct, cfg.ExcludedTypes) {
				copyHeaders(w, rec.header)
				w.WriteHeader(rec.statusCode)
				w.Write(rec.body)
				return
			}

			// Skip if no acceptable encoding
			encoding := negotiateEncoding(r)
			if encoding == "" {
				copyHeaders(w, rec.header)
				w.WriteHeader(rec.statusCode)
				w.Write(rec.body)
				return
			}

			// Compress
			compressed, err := compressData(rec.body, encoding, cfg.Level)
			if err != nil {
				copyHeaders(w, rec.header)
				w.WriteHeader(rec.statusCode)
				w.Write(rec.body)
				return
			}

			copyHeaders(w, rec.header)
			w.Header().Set("Content-Encoding", encoding)
			w.Header().Set("Vary", "Accept-Encoding")
			w.Header().Set("Content-Length", itoa(len(compressed)))
			w.WriteHeader(rec.statusCode)
			w.Write(compressed)
		})
	}
}

// negotiateEncoding picks the best encoding from Accept-Encoding.
func negotiateEncoding(r *http.Request) string {
	accept := r.Header.Get("Accept-Encoding")
	if accept == "" {
		return ""
	}

	encodings := parseAcceptEncoding(accept)

	// Priority: br > gzip > deflate
	if _, ok := encodings["br"]; ok {
		return "br"
	}
	if _, ok := encodings["gzip"]; ok {
		return "gzip"
	}
	if _, ok := encodings["deflate"]; ok {
		return "deflate"
	}
	return ""
}

// parseAcceptEncoding parses Accept-Encoding header into a map.
func parseAcceptEncoding(header string) map[string]float64 {
	encodings := make(map[string]float64)
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		parts := strings.SplitN(part, ";", 2)
		name := strings.TrimSpace(parts[0])
		if name == "*" || name == "" {
			continue
		}
		q := 1.0
		if len(parts) == 2 {
			qp := strings.TrimSpace(parts[1])
			if strings.HasPrefix(qp, "q=") {
				if v, err := parseFloat(qp[2:]); err == nil {
					q = v
				}
			}
		}
		if q > 0 {
			encodings[name] = q
		}
	}
	return encodings
}

func parseFloat(s string) (float64, error) {
	var f float64
	var div float64 = 1
	for _, c := range s {
		if c == '.' {
			div = 1
			continue
		}
		if c >= '0' && c <= '9' {
			f = f*10 + float64(c-'0')
			div *= 10
		}
	}
	if div == 1 && f > 0 {
		return f, nil
	}
	return f / div, nil
}

// compressData compresses data using the specified encoding.
func compressData(data []byte, encoding string, level int) ([]byte, error) {
	var buf bytes.Buffer
	switch encoding {
	case "gzip":
		w, err := gzip.NewWriterLevel(&buf, level)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
	case "br":
		w := brotli.NewWriterLevel(&buf, level)
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
	case "deflate":
		w, err := flate.NewWriter(&buf, level)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
	default:
		return nil, errUnsupportedEncoding
	}
	return buf.Bytes(), nil
}

func isExcludedType(ct string, excluded []string) bool {
	ct = strings.ToLower(ct)
	for _, e := range excluded {
		if strings.HasPrefix(ct, e) {
			return true
		}
	}
	return false
}

// compressRecorder captures the upstream response for inspection.
type compressRecorder struct {
	http.ResponseWriter
	statusCode int
	header     http.Header
	body       []byte
}

func (r *compressRecorder) Header() http.Header {
	return r.header
}

func (r *compressRecorder) WriteHeader(code int) {
	r.statusCode = code
}

func (r *compressRecorder) Write(b []byte) (int, error) {
	r.body = append(r.body, b...)
	return len(b), nil
}

func (r *compressRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// copyHeaders copies headers from src to dst.
func copyHeaders(dst http.ResponseWriter, src http.Header) {
	for k, vs := range src {
		for _, v := range vs {
			dst.Header().Add(k, v)
		}
	}
}

// itoa converts an int to string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// NewStreamingCompression returns middleware that compresses responses using
// streaming for large/SSE responses. Uses buffer-then-flush approach.
func NewStreamingCompression(cfg *CompressionConfig) func(http.Handler) http.Handler {
	if cfg == nil {
		cfg = DefaultCompressionConfig()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			encoding := negotiateEncoding(r)
			if encoding == "" {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Vary", "Accept-Encoding")

			cw := newStreamingWriter(w, encoding, cfg)
			defer cw.Close()

			next.ServeHTTP(cw, r)
		})
	}
}

// streamingWriter wraps ResponseWriter with streaming compression.
type streamingWriter struct {
	w        http.ResponseWriter
	encoding string
	compress io.WriteCloser
	flusher  http.Flusher
	buf      bytes.Buffer
	wroteHdr bool
	hdr      http.Header
	level    int
	excluded []string
}

func newStreamingWriter(w http.ResponseWriter, encoding string, cfg *CompressionConfig) *streamingWriter {
	f, _ := w.(http.Flusher)
	return &streamingWriter{
		w:        w,
		encoding: encoding,
		flusher:  f,
		hdr:      make(http.Header),
		level:    cfg.Level,
		excluded: cfg.ExcludedTypes,
	}
}

func (sw *streamingWriter) Header() http.Header {
	return sw.hdr
}

func (sw *streamingWriter) WriteHeader(code int) {
	if sw.wroteHdr {
		return
	}
	sw.wroteHdr = true

	// Don't compress errors or excluded types
	ct := sw.hdr.Get("Content-Type")
	if code >= 300 || isExcludedType(ct, sw.excluded) {
		for k, vs := range sw.hdr {
			for _, v := range vs {
				sw.w.Header().Add(k, v)
			}
		}
		sw.w.WriteHeader(code)
		if sw.buf.Len() > 0 {
			sw.w.Write(sw.buf.Bytes())
			sw.buf.Reset()
		}
		return
	}

	// Start compressed stream
	sw.initCompressor()

	for k, vs := range sw.hdr {
		for _, v := range vs {
			sw.w.Header().Add(k, v)
		}
	}
	sw.w.Header().Set("Content-Encoding", sw.encoding)
	sw.w.Header().Del("Content-Length")
	sw.w.WriteHeader(code)

	if sw.buf.Len() > 0 {
		sw.compress.Write(sw.buf.Bytes())
		sw.buf.Reset()
	}
}

func (sw *streamingWriter) initCompressor() {
	switch sw.encoding {
	case "gzip":
		w, _ := gzip.NewWriterLevel(sw.w, sw.level)
		sw.compress = w
	case "br":
		sw.compress = brotli.NewWriterLevel(sw.w, sw.level)
	case "deflate":
		w, _ := flate.NewWriter(sw.w, sw.level)
		sw.compress = w
	}
}

func (sw *streamingWriter) Write(b []byte) (int, error) {
	if !sw.wroteHdr {
		sw.WriteHeader(http.StatusOK)
	}
	if sw.compress != nil {
		return sw.compress.Write(b)
	}
	return sw.w.Write(b)
}

func (sw *streamingWriter) Close() error {
	if sw.compress != nil {
		return sw.compress.Close()
	}
	return nil
}

func (sw *streamingWriter) Flush() {
	// Flush the compressor's internal buffer first, otherwise compressed
	// bytes stay stuck until Close() and the client never receives them.
	if sw.compress != nil {
		if f, ok := sw.compress.(interface{ Flush() error }); ok {
			_ = f.Flush()
		}
	}
	if sw.flusher != nil {
		sw.flusher.Flush()
	}
}

var _ http.Flusher = (*streamingWriter)(nil)
