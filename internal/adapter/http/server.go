// Package httpsrv wraps net/http with timeouts and graceful shutdown.
package httpsrv

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

type Server struct {
	srv      *http.Server
	listener net.Listener
	log      *slog.Logger
}

func New(addr string, handler http.Handler, log *slog.Logger) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", addr, err)
	}
	return &Server{
		srv: &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
		},
		listener: ln,
		log:      log,
	}, nil
}

func (s *Server) Addr() string { return s.listener.Addr().String() }

func (s *Server) Serve() error {
	s.log.Info("http server listening", "addr", s.Addr())
	if err := s.srv.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error { return s.srv.Shutdown(ctx) }
