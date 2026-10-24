package server

import (
	"log/slog"
	"net/http"
	"time"
)

// LogRequests returns a handler that writes one structured access log line per
// request, in the style of a conventional HTTP server: client address, request
// line, response status, bytes written and how long the handler took. The Host
// header and any X-Forwarded-For chain are included, since ubimi is expected to
// sit behind a reverse proxy that terminates the client connection.
func LogRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		attrs := []any{
			"remote", r.RemoteAddr,
			"host", r.Host,
			"method", r.Method,
			"uri", r.URL.RequestURI(),
			"status", rec.status,
			"bytes", rec.bytes,
			"duration", time.Since(start),
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			attrs = append(attrs, "forwarded_for", xff)
		}
		slog.Info("request", attrs...)
	})
}

// statusRecorder records the status code and body size written by a handler so
// they can be logged once the request is done.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	n, err := r.ResponseWriter.Write(p)
	r.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
