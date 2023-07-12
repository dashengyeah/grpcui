package grpcui

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/fullstorydev/grpcurl"
	"github.com/jhump/protoreflect/dynamic/grpcdynamic"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	insecurecreds "google.golang.org/grpc/credentials/insecure"
)

var (
	targetHost   string
	dialTime     time.Duration
	dialCreds    credentials.TransportCredentials
	dialFailFast bool
	dialOpts     []grpc.DialOption
)

var grpcConn *grpc.ClientConn

func SetDialOpts(timeout time.Duration, creds credentials.TransportCredentials, failFast bool, opts ...grpc.DialOption) {
	dialTime = timeout
	dialCreds = creds
	dialFailFast = failFast
}

func UpdateTarget(target string) error {
	targetHost = target
	dialCtx, cancel := context.WithTimeout(context.Background(), dialTime)
	defer func() {
		cancel()
	}()
	cc, err := dial(dialCtx, "tcp", targetHost, dialCreds, dialFailFast, dialOpts...)
	if err != nil {
		return err
	}
	grpcConn = cc
	return nil
}

func GetTarget() grpcdynamic.Channel {
	return grpcConn
}

func dial(ctx context.Context, network, addr string, creds credentials.TransportCredentials, failFast bool, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	if failFast {
		return grpcurl.BlockingDial(ctx, network, addr, creds, opts...)
	}
	// BlockingDial will return the first error returned. It is meant for interactive use.
	// If we don't want to fail fast, then we need to do a more customized dial.

	// TODO: perhaps this logic should be added to the grpcurl package, like in a new
	// BlockingDialNoFailFast function?

	dialer := &errTrackingDialer{
		dialer:  &net.Dialer{},
		network: network,
	}
	var errCreds errTrackingCreds
	if creds == nil {
		opts = append(opts, grpc.WithTransportCredentials(insecurecreds.NewCredentials()))
	} else {
		errCreds = errTrackingCreds{
			TransportCredentials: creds,
		}
		opts = append(opts, grpc.WithTransportCredentials(&errCreds))
	}

	cc, err := grpc.DialContext(ctx, addr, append(opts, grpc.WithBlock(), grpc.WithContextDialer(dialer.dial))...)
	if err == nil {
		return cc, nil
	}

	// prefer last observed TLS handshake error if there is one
	if err := errCreds.err(); err != nil {
		return nil, err
	}
	// otherwise, use the error the dialer last observed
	if err := dialer.err(); err != nil {
		return nil, err
	}
	// if we have no better source of error message, use what grpc.DialContext returned
	return nil, err
}

type errTrackingCreds struct {
	credentials.TransportCredentials

	mu      sync.Mutex
	lastErr error
}

func (c *errTrackingCreds) ClientHandshake(ctx context.Context, addr string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	conn, auth, err := c.TransportCredentials.ClientHandshake(ctx, addr, rawConn)
	if err != nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.lastErr = err
	}
	return conn, auth, err
}

func (c *errTrackingCreds) err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr
}

type errTrackingDialer struct {
	dialer  *net.Dialer
	network string

	mu      sync.Mutex
	lastErr error
}

func (c *errTrackingDialer) dial(ctx context.Context, addr string) (net.Conn, error) {
	conn, err := c.dialer.DialContext(ctx, c.network, addr)
	if err != nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.lastErr = err
	}
	return conn, err
}

func (c *errTrackingDialer) err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr
}
