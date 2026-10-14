package utils

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

type TronGRPCConfig struct {
	Endpoint        string
	TronGridAPIKeys []string
	DRPCAPIKeys     []string
}

// NormalizeTronAPIKeys prevents absent keys and trailing commas from sending
// empty credentials, which TronGrid rejects with HTTP 401.
func NormalizeTronAPIKeys(keys []string) []string {
	var clean []string
	for _, key := range keys {
		if key = strings.TrimSpace(key); key != "" {
			clean = append(clean, key)
		}
	}
	return clean
}

// Fingerprint invalidates cached connections after credentials change without
// exposing credentials in cache identifiers or logs.
func (c TronGRPCConfig) Fingerprint() [32]byte {
	c.Endpoint = strings.TrimSpace(c.Endpoint)
	c.TronGridAPIKeys = NormalizeTronAPIKeys(c.TronGridAPIKeys)
	c.DRPCAPIKeys = NormalizeTronAPIKeys(c.DRPCAPIKeys)
	b, _ := json.Marshal(c)
	return sha256.Sum256(b)
}

// NewTronGrpcClient 目前很多 TRON API 基本都有QPS限制，过于激进或者过于保守的重试策略都不合适
func NewTronGrpcClient(apiNode string, apiKey []string) (*grpc.ClientConn, error) {
	return NewTronGrpcClientWithConfig(TronGRPCConfig{Endpoint: apiNode, TronGridAPIKeys: apiKey})
}

func NewTronGrpcClientWithConfig(config TronGRPCConfig) (*grpc.ClientConn, error) {
	return newTronGrpcClient(config)
}

func newTronGrpcClient(config TronGRPCConfig, extraOptions ...grpc.DialOption) (*grpc.ClientConn, error) {
	apiNode := strings.TrimSpace(config.Endpoint)
	if apiNode == "" {
		return nil, errors.New("tron api node address is empty")
	}

	var useTLS bool
	if strings.HasPrefix(apiNode, "https://") || strings.HasPrefix(apiNode, "grpcs://") {
		u, err := url.Parse(apiNode)
		if err != nil || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("tron gRPC endpoint must contain only a host and port")
		}
		apiNode = u.Host
		if u.Port() == "" {
			apiNode = net.JoinHostPort(u.Hostname(), "443")
		}
		useTLS = true
	}
	address := strings.TrimPrefix(strings.TrimPrefix(apiNode, "dns:///"), "passthrough:///")
	host, port, _ := net.SplitHostPort(address)
	isDRPC := strings.EqualFold(host, "grpc.drpc.live")
	if isDRPC && port != "443" {
		return nil, errors.New("dRPC TRON gRPC requires port 443 and TLS")
	}
	useTLS = useTLS || isDRPC
	keys := NormalizeTronAPIKeys(config.TronGridAPIKeys)
	if isDRPC {
		keys = NormalizeTronAPIKeys(config.DRPCAPIKeys)
		if len(keys) == 0 {
			return nil, errors.New("dRPC TRON API key is required")
		}
	}
	for _, key := range keys {
		if strings.ContainsAny(key, "\r\n\x00") {
			return nil, errors.New("tron API key contains invalid characters")
		}
	}
	var transportCredentials credentials.TransportCredentials = insecure.NewCredentials()
	if useTLS {
		transportCredentials = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	}

	if !strings.Contains(apiNode, "://") {
		apiNode = "passthrough:///" + apiNode
	}

	opts := []grpc.DialOption{
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  1 * time.Second,
				MaxDelay:   30 * time.Second,
				Multiplier: 1.5,
			},
			MinConnectTimeout: 1 * time.Minute,
		}),
		grpc.WithTransportCredentials(transportCredentials),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	}

	if isDRPC {
		opts = append(opts, grpc.WithPerRPCCredentials(tronDRPCCredentials{keys: keys}))
	} else if len(keys) > 0 {
		opts = append(opts,
			grpc.WithUnaryInterceptor(tronGridApiKeyUnaryInterceptor(keys)),
			grpc.WithStreamInterceptor(tronGridApiKeyStreamInterceptor(keys)),
		)
	}

	return grpc.NewClient(apiNode, append(opts, extraOptions...)...)
}

type tronDRPCCredentials struct {
	keys []string
}

func (c tronDRPCCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	i := time.Now().UnixMicro() % int64(len(c.keys))
	return map[string]string{"x-drpc-key": c.keys[i], "x-drpc-chain": "tron"}, nil
}

func (tronDRPCCredentials) RequireTransportSecurity() bool { return true }

func tronGridApiKeyUnaryInterceptor(apiKeys []string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		i := time.Now().UnixMicro() % int64(len(apiKeys))
		ctx = addTronGridApiKeyToContext(ctx, apiKeys[i])
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func tronGridApiKeyStreamInterceptor(apiKeys []string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn,
		method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		i := time.Now().UnixMicro() % int64(len(apiKeys))
		ctx = addTronGridApiKeyToContext(ctx, apiKeys[i])
		return streamer(ctx, desc, cc, method, opts...)
	}
}

func addTronGridApiKeyToContext(ctx context.Context, apiKey string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "tron-pro-api-key", apiKey)
}
