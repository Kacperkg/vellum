package main

import (
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestHTTPServerEnforcesReadDeadlines(t *testing.T) {
	server := newHTTPServer("127.0.0.1:0", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	if server.ReadHeaderTimeout != 5*time.Second || server.ReadTimeout != 15*time.Second || server.WriteTimeout != 30*time.Second || server.IdleTimeout != time.Minute || server.MaxHeaderBytes != 16<<10 {
		t.Fatal("HTTP resource guards changed")
	}
	// Exercise actual sockets with shorter deadlines, rather than waiting for
	// production-duration read timeouts in every test run.
	server.ReadHeaderTimeout = 100 * time.Millisecond
	server.ReadTimeout = 200 * time.Millisecond
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() { server.Close(); <-done })
	for _, partial := range []string{
		"POST /login HTTP/1.1\r\nHost: localhost\r\n",
		"POST /login HTTP/1.1\r\nHost: localhost\r\nContent-Length: 100\r\n\r\n{",
	} {
		connection, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			connection.Close()
			t.Fatal(err)
		}
		if _, err := io.WriteString(connection, partial); err != nil {
			connection.Close()
			t.Fatal(err)
		}
		_, err = io.ReadAll(connection)
		connection.Close()
		if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
			t.Fatal("server failed to end slow request before client deadline")
		}
	}
}
