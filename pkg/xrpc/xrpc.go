// Package xrpc provides a simple wrapper around the xrpc client
// with some helper functions for authentication
package xrpc

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/pkg/errors"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// RefreshAuth refreshes the auth token for the client
func RefreshAuth(ctx context.Context, client *xrpc.Client) error {
	// Set the AccessJWT to the RefreshJWT so we have permission to refresh
	client.Auth.AccessJwt = client.Auth.RefreshJwt

	refreshedSession, err := comatproto.ServerRefreshSession(ctx, client)
	if err != nil {
		e := errors.Wrap(err, "failed to refresh session")
		return e
	}

	client.Auth = &xrpc.AuthInfo{
		Handle:     refreshedSession.Handle,
		Did:        refreshedSession.Did,
		RefreshJwt: refreshedSession.RefreshJwt,
		AccessJwt:  refreshedSession.AccessJwt,
	}

	return nil
}

// GetXRPCClient returns an XRPC client for the ATProto server
// with Authentication from the ATP_AUTH environment variable
func GetXRPCClient(ctx context.Context) (*xrpc.Client, error) {
	// Create an instrumented transport for OTEL Tracing of HTTP Requests
	instrumentedTransport := otelhttp.NewTransport(&http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	})

	// Create the XRPC Client
	client := xrpc.Client{
		Client: &http.Client{
			Transport: instrumentedTransport,
		},
		Host: "https://bsky.social",
	}

	ATPAuthString := os.Getenv("ATP_AUTH")
	if ATPAuthString == "" {
		return nil, fmt.Errorf("ATP_AUTH not set")
	}

	authParts := strings.Split(ATPAuthString, ":")
	if len(authParts) != 2 {
		return nil, fmt.Errorf("ATP_AUTH not set correctly: {email}:{app_password}")
	}

	ses, err := comatproto.ServerCreateSession(ctx, &client, &comatproto.ServerCreateSession_Input{
		Identifier: authParts[0],
		Password:   authParts[1],
	})
	if err != nil {
		e := errors.Wrap(err, "failed to create session")
		return nil, e
	}

	client.Auth = &xrpc.AuthInfo{
		Handle:     ses.Handle,
		Did:        ses.Did,
		RefreshJwt: ses.RefreshJwt,
		AccessJwt:  ses.AccessJwt,
	}

	return &client, nil
}
