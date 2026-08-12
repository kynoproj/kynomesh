/*
Copyright 2026 The Kynoproj Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package broker

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/kynoproj/kynomesh/pkg/broker/ratelimit"
)

// rawCodec is a grpc.Codec that passes frames through as opaque []byte,
// so the broker can proxy gRPC without knowing the proto descriptors.
// Name returns "proto" — the codec lookup is keyed off that string.
type rawCodec struct{}

func (rawCodec) Name() string { return "proto" }

// String satisfies the older grpc.Codec interface still used by some grpc-go versions.
func (rawCodec) String() string { return "proto" }

func (rawCodec) Marshal(v any) ([]byte, error) {
	b, ok := v.(*[]byte)
	if !ok {
		return nil, fmt.Errorf("broker.rawCodec: expected *[]byte, got %T", v)
	}
	return *b, nil
}

func (rawCodec) Unmarshal(data []byte, v any) error {
	dst, ok := v.(*[]byte)
	if !ok {
		return fmt.Errorf("broker.rawCodec: expected *[]byte, got %T", v)
	}
	*dst = data
	return nil
}

// GRPCPassthroughOptions turns a fresh *grpc.Server into a transparent
// proxy to backendConn. Both options must be applied; no other services
// should be registered on the server.
func GRPCPassthroughOptions(backendConn *grpc.ClientConn, metrics *Metrics, limiter ratelimit.Limiter) []grpc.ServerOption {
	set := metrics.GRPCSet()
	return []grpc.ServerOption{
		grpc.ForceServerCodec(rawCodec{}),
		grpc.UnknownServiceHandler(func(_ any, ss grpc.ServerStream) error {
			if limiter != nil {
				release, ok := limiter.Acquire()
				if !ok {
					set.rejected.Inc()
					return status.Error(codes.ResourceExhausted, "broker at max in-flight capacity")
				}
				defer release()
			}
			set.inflight.Inc()
			start := time.Now()
			defer func() {
				set.inflight.Dec()
				set.requests.Inc()
				set.duration.Observe(time.Since(start).Seconds())
			}()
			return forwardGRPCStream(backendConn, ss, set.streamMessages)
		}),
	}
}

// forwardGRPCStream proxies one bidi gRPC stream end-to-end: copies
// metadata, pumps frames in both directions, propagates backend
// header/trailer/status to the caller. serverMessages is incremented
// per backend→client frame so the broker reports server-produced
// message volume; nil disables the counting (used by tests that
// don't care about metrics).
func forwardGRPCStream(backendConn *grpc.ClientConn, ss grpc.ServerStream, serverMessages prometheus.Counter) error {
	ctx := ss.Context()

	method, ok := grpc.Method(ctx)
	if !ok {
		return status.Error(13 /*Internal*/, "broker: failed to extract method name from context")
	}

	outCtx := ctx
	if inMD, ok := metadata.FromIncomingContext(ctx); ok {
		outCtx = metadata.NewOutgoingContext(ctx, inMD.Copy())
	}

	desc := &grpc.StreamDesc{
		StreamName:    method,
		ClientStreams: true,
		ServerStreams: true,
	}
	clientStream, err := backendConn.NewStream(outCtx, desc, method, grpc.ForceCodec(rawCodec{}))
	if err != nil {
		return err
	}

	clientToBackendErr := make(chan error, 1)
	go func() {
		// Client→server frames: not counted by metrics — "work" is
		// what the agent produced, not what the caller sent.
		clientToBackendErr <- copyMessages(ss, clientStream, nil)
		// Half-close: tell the backend the client is done sending.
		_ = clientStream.CloseSend()
	}()

	backendToClientErr := make(chan error, 1)
	go func() {
		// Header() blocks until the backend sends its first response;
		// forward it before any messages so order is preserved.
		header, herr := clientStream.Header()
		if herr == nil {
			_ = ss.SetHeader(header)
		}
		// Server→client frames are the metric signal.
		backendToClientErr <- copyMessages(clientStream, ss, serverMessages)
	}()

	backendErr := <-backendToClientErr
	clientErr := <-clientToBackendErr

	// Trailers must be set before this handler returns — gRPC flushes
	// them on exit.
	ss.SetTrailer(clientStream.Trailer())

	// Backend errors dominate so callers see the same status they
	// would without the broker in the path.
	if backendErr != nil && !errors.Is(backendErr, io.EOF) {
		return backendErr
	}
	if clientErr != nil && !errors.Is(clientErr, io.EOF) {
		return clientErr
	}
	return nil
}

// msgRecv/msgSend are the minimal interfaces that grpc.ServerStream and
// grpc.ClientStream both satisfy, letting copyMessages flow frames in
// either direction.
type msgRecv interface {
	RecvMsg(m any) error
}
type msgSend interface {
	SendMsg(m any) error
}

// copyMessages pumps opaque frames from src to dst until src returns
// io.EOF or any error. counter, if non-nil, is incremented once per
// successfully-forwarded frame.
func copyMessages(src msgRecv, dst msgSend, counter prometheus.Counter) error {
	for {
		var frame []byte
		if err := src.RecvMsg(&frame); err != nil {
			return err
		}
		if err := dst.SendMsg(&frame); err != nil {
			return err
		}
		if counter != nil {
			counter.Inc()
		}
	}
}
