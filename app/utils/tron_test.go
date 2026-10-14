package utils

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

func testTronServer(t *testing.T, secure bool) (string, *x509.CertPool, <-chan metadata.MD) {
	t.Helper()
	received := make(chan metadata.MD, 4)
	opts := []grpc.ServerOption{grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if secure {
			p, ok := peer.FromContext(ctx)
			if !ok {
				t.Error("missing TLS peer")
			} else if _, ok := p.AuthInfo.(credentials.TLSInfo); !ok {
				t.Error("dRPC credentials arrived without TLS")
			}
		}
		md, _ := metadata.FromIncomingContext(ctx)
		received <- md.Copy()
		return handler(ctx, req)
	})}
	var roots *x509.CertPool
	if secure {
		certServer := httptest.NewTLSServer(nil)
		cert := certServer.TLS.Certificates[0]
		roots = x509.NewCertPool()
		roots.AddCert(certServer.Certificate())
		certServer.Close()
		opts = append(opts, grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})))
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer(opts...)
	grpc_health_v1.RegisterHealthServer(s, health.NewServer())
	go func() { _ = s.Serve(listener) }()
	t.Cleanup(s.Stop)
	return listener.Addr().String(), roots, received
}

func checkTronConnection(t *testing.T, conn *grpc.ClientConn) {
	t.Helper()
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{}); err != nil {
		t.Fatal(err)
	}
}

func TestTronGridEmptyKeysDoNotSendAuthentication(t *testing.T) {
	address, _, incoming := testTronServer(t, false)
	conn, err := NewTronGrpcClient(address, []string{"", " ", "\t"})
	if err != nil {
		t.Fatal(err)
	}
	checkTronConnection(t, conn)
	md := <-incoming
	if _, ok := md["tron-pro-api-key"]; ok {
		t.Fatal("empty authentication header must not be sent")
	}
}

func TestTronGridKeyTrimmingAndProviderIsolation(t *testing.T) {
	address, _, incoming := testTronServer(t, false)
	conn, err := NewTronGrpcClientWithConfig(TronGRPCConfig{Endpoint: address, TronGridAPIKeys: []string{"", "  grid-test-key  ", ""}, DRPCAPIKeys: []string{"drpc-test-key"}})
	if err != nil {
		t.Fatal(err)
	}
	checkTronConnection(t, conn)
	md := <-incoming
	if got := md.Get("tron-pro-api-key"); len(got) != 1 || got[0] != "grid-test-key" {
		t.Fatalf("expected exactly one trimmed TronGrid key, got %v", got)
	}
	if len(md.Get("x-drpc-key")) != 0 || len(md.Get("x-drpc-chain")) != 0 {
		t.Fatal("dRPC credentials must not reach a different provider")
	}
}

func TestTronDRPCUsesTLSAndMetadataOnEveryCall(t *testing.T) {
	address, roots, incoming := testTronServer(t, true)
	conn, err := newTronGrpcClient(TronGRPCConfig{Endpoint: "grpc.drpc.live:443", DRPCAPIKeys: []string{"", " drpc-test-key "}, TronGridAPIKeys: []string{"grid-test-key"}},
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", address)
		}),
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{RootCAs: roots, ServerName: "example.com", MinVersion: tls.VersionTLS12})))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		checkTronConnection(t, conn)
		md := <-incoming
		if got := md.Get("x-drpc-key"); len(got) != 1 || got[0] != "drpc-test-key" {
			t.Fatal("dRPC key missing from request")
		}
		if got := md.Get("x-drpc-chain"); len(got) != 1 || got[0] != "tron" {
			t.Fatal("TRON network metadata missing")
		}
		if len(md.Get("tron-pro-api-key")) != 0 {
			t.Fatal("TronGrid credentials must not reach dRPC")
		}
	}
}

func TestTronDRPCRejectsUntrustedCertificate(t *testing.T) {
	address, _, incoming := testTronServer(t, true)
	conn, err := newTronGrpcClient(TronGRPCConfig{Endpoint: "grpc.drpc.live:443", DRPCAPIKeys: []string{"drpc-test-key"}},
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", address)
		}))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{}); err == nil {
		t.Fatal("untrusted TLS certificate was accepted")
	}
	select {
	case <-incoming:
		t.Fatal("credentials reached an untrusted server")
	default:
	}
}

func TestTronDRPCConfigurationValidation(t *testing.T) {
	for _, tc := range []struct {
		endpoint string
		keys     []string
	}{
		{"grpc.drpc.live:443", nil},
		{"grpc.drpc.live:443", []string{"", " "}},
		{"grpc.drpc.live:50051", []string{"test-key"}},
		{"https://grpc.drpc.live:443/wallet", []string{"test-key"}},
		{"https://grpc.drpc.live:443?key=secret", []string{"test-key"}},
		{"https://secret@grpc.drpc.live:443", []string{"test-key"}},
		{"grpc.drpc.live:443", []string{"secret\nvalue"}},
	} {
		conn, err := NewTronGrpcClientWithConfig(TronGRPCConfig{Endpoint: tc.endpoint, DRPCAPIKeys: tc.keys})
		if conn != nil {
			conn.Close()
		}
		if err == nil {
			t.Errorf("invalid dRPC configuration accepted for endpoint %q", tc.endpoint)
		} else if strings.Contains(err.Error(), "secret") {
			t.Fatal("configuration error exposed a credential")
		}
	}
}
