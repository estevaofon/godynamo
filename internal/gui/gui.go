package gui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"
)

// Run starts the loopback HTTP bridge and launches the Electron desktop app.
// It blocks until the Electron window is closed or the process is interrupted.
func Run(args []string) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to bind loopback port: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	token, err := newToken()
	if err != nil {
		return err
	}

	srv := &http.Server{Handler: newServer(token).handler()}
	go func() {
		if serveErr := srv.Serve(ln); serveErr != nil && serveErr != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "bridge server error: %v\n", serveErr)
		}
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	fmt.Printf("GoDynamo GUI bridge listening on http://127.0.0.1:%d\n", port)

	electron, err := startElectron(port, token)
	if err != nil {
		return err
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	done := make(chan struct{})
	go func() {
		_ = electron.Wait()
		close(done)
	}()

	select {
	case <-sigCh:
		_ = electron.Process.Kill()
	case <-done:
	}
	return nil
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
