package alerts

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A connected SMTP endpoint can stall before its greeting or during either TLS
// handshake. These tests use real sockets so context cancellation cannot be
// accidentally verified only at the dial call.
func TestSMTPDeadlineBoundsUnresponsiveTransport(t *testing.T) {
	for _, mode := range []string{"plain greeting", "implicit TLS", "STARTTLS"} {
		t.Run(mode, func(t *testing.T) {
			cfg := smtpTestEndpoint(t, func(conn net.Conn) {
				if mode == "STARTTLS" {
					r := bufio.NewReader(conn)
					_, _ = fmt.Fprint(conn, "220 local SMTP\r\n")
					if _, err := r.ReadString('\n'); err != nil {
						return
					}
					_, _ = fmt.Fprint(conn, "250-local\r\n250 STARTTLS\r\n")
					if _, err := r.ReadString('\n'); err != nil {
						return
					}
					_, _ = fmt.Fprint(conn, "220 Begin TLS\r\n")
				}
				_, _ = io.Copy(io.Discard, conn) // Deliberately never respond.
			})
			cfg["use_tls"] = mode == "implicit TLS"
			cfg["starttls"] = mode == "STARTTLS"
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := (&Dispatcher{}).sendSMTP(ctx, cfg, []string{"ops@example.com"}, "report", "body")
				done <- err
			}()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("unresponsive SMTP unexpectedly succeeded")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("SMTP ignored its context deadline")
			}
		})
	}
}

func TestSMTPCancellationInterruptsPendingDataAcknowledgment(t *testing.T) {
	messageReceived := make(chan struct{}, 1)
	cfg := smtpTestEndpoint(t, func(conn net.Conn) {
		r := bufio.NewReader(conn)
		_, _ = fmt.Fprint(conn, "220 local SMTP\r\n")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"), strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
				_, _ = fmt.Fprint(conn, "250 OK\r\n")
			case strings.HasPrefix(line, "DATA"):
				_, _ = fmt.Fprint(conn, "354 Send message\r\n")
				for {
					line, err = r.ReadString('\n')
					if err != nil {
						return
					}
					if line == ".\r\n" {
						break
					}
				}
				messageReceived <- struct{}{}
				_, _ = io.Copy(io.Discard, r) // Never acknowledge the message.
				return
			default:
				return
			}
		}
	})
	cfg["starttls"] = false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := (&Dispatcher{}).sendSMTP(ctx, cfg, []string{"ops@example.com"}, "report", "weekly body")
		done <- err
	}()
	select {
	case <-messageReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("SMTP never reached DATA acknowledgment")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled SMTP unexpectedly succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not interrupt SMTP after connection establishment")
	}
}

func TestSMTPBoundedTransportDeliversMessage(t *testing.T) {
	delivered := make(chan string, 1)
	cfg := smtpTestEndpoint(t, func(conn net.Conn) {
		r := bufio.NewReader(conn)
		_, _ = fmt.Fprint(conn, "220 local SMTP\r\n")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"), strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
				_, _ = fmt.Fprint(conn, "250 OK\r\n")
			case strings.HasPrefix(line, "DATA"):
				_, _ = fmt.Fprint(conn, "354 Send message\r\n")
				var body strings.Builder
				for {
					line, err = r.ReadString('\n')
					if err != nil {
						return
					}
					if line == ".\r\n" {
						break
					}
					body.WriteString(line)
				}
				delivered <- body.String()
				_, _ = fmt.Fprint(conn, "250 Message accepted\r\n")
			case strings.HasPrefix(line, "QUIT"):
				_, _ = fmt.Fprint(conn, "221 Bye\r\n")
				return
			default:
				return
			}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	id, err := (&Dispatcher{}).sendSMTP(ctx, cfg, []string{"ops@example.com"}, "Weekly operations report", "Saved report body")
	if err != nil || id != "smtp" {
		t.Fatalf("delivery: %s %v", id, err)
	}
	select {
	case body := <-delivered:
		for _, want := range []string{"To: ops@example.com", "Subject: Weekly operations report", "Saved report body"} {
			if !strings.Contains(body, want) {
				t.Fatalf("missing %q from message", want)
			}
		}
	default:
		t.Fatal("sender succeeded without sending its message")
	}
}

func smtpTestEndpoint(t *testing.T, handle func(net.Conn)) map[string]interface{} {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan net.Conn, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		accepted <- conn
		defer conn.Close()
		handle(conn)
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case conn := <-accepted:
			_ = conn.Close()
		default:
		}
		select {
		case <-finished:
		case <-time.After(2 * time.Second):
			t.Error("SMTP test peer did not stop")
		}
	})
	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	portNumber, _ := strconv.Atoi(port)
	return map[string]interface{}{"host": host, "port": portNumber, "from_email": "reports@example.com"}
}
