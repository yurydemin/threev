// Package profiling provides an opt-in HTTP pprof debug server used purely
// as local, developer-facing tooling.
//
// It wraps the standard library's net/http/pprof handlers (no
// authentication, no TLS, nothing production-grade) so that a human can
// attach `go tool pprof` (or a browser at /debug/pprof/) to a running
// instance of threev during manual RAM/CPU profiling, as called for by
// Stage 5 AC-005/AC-006 of the project plan. It is never started unless a
// caller explicitly opts in by supplying a non-empty address - this package
// itself never inspects environment variables or any other implicit
// configuration source; the caller (app.go, driven by the
// THREEV_PPROF_ADDR environment variable) decides whether and where to
// listen. It must never be enabled by default in a shipped build.
package profiling

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"time"
)

// shutdownTimeout bounds how long stop (returned by EnableDebugServer)
// waits for the debug server's in-flight requests to finish during
// graceful shutdown, so app exit is never blocked indefinitely by a stuck
// profiling request (e.g. a long-running CPU profile capture).
const shutdownTimeout = 2 * time.Second

// EnableDebugServer starts an HTTP server bound to addr, serving the
// standard net/http/pprof endpoints (/debug/pprof/, cmdline, profile,
// symbol, trace) on a private http.ServeMux - never on
// http.DefaultServeMux, which would otherwise leak these unauthenticated
// debug endpoints onto any other server the process might run using the
// default mux.
//
// The listener is bound synchronously via net.Listen before returning, so
// a bad or already-in-use addr is reported back to the caller as a non-nil
// error instead of only being logged from inside a background goroutine.
// Once bound successfully, the server itself runs in a goroutine; any
// error it later returns (other than the expected http.ErrServerClosed on
// a clean stop) is only logged, since by that point EnableDebugServer has
// already returned successfully and there is no channel back to the
// original caller.
//
// The returned stop function shuts the server down gracefully (bounded by
// shutdownTimeout) and is safe to call exactly once. Any error from
// shutdown is logged, never panicked, since tearing down this purely
// diagnostic server must never itself crash the application during exit.
func EnableDebugServer(addr string) (stop func(), err error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %q: %w", addr, err)
	}

	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			log.Printf("threev: pprof debug server on %s: %v", addr, serveErr)
		}
	}()

	stop = func() {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if shutdownErr := server.Shutdown(ctx); shutdownErr != nil {
			log.Printf("threev: shut down pprof debug server on %s: %v", addr, shutdownErr)
		}
	}

	return stop, nil
}
