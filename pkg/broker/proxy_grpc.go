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

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// rawCodec implements grpc.Codec over an opaque []byte so the broker can
// forward gRPC frames without knowing their proto descriptors. The
// "framer" codec name is what the rest of the gRPC stack expects to see
// when a caller wants to bypass message marshalling.
type rawCodec struct{}

// Name implements grpc.Codec for the v2 interface (the field-tag based
// codec lookup is keyed off this string).
func (rawCodec) Name() string { return "proto" }

// String preserves backward compatibility with the v1 Codec interface
// that some gRPC versions still reach for.
func (rawCodec) String() string { return "proto" }

// Marshal accepts a *[]byte and returns its contents unchanged.
func (rawCodec) Marshal(v any) ([]byte, error) {
	b, ok := v.(*[]byte)
	if !ok {
		return nil, fmt.Errorf("broker.rawCodec: expected *[]byte, got %T", v)
	}
	return *b, nil
}

// Unmarshal accepts a *[]byte and assigns the incoming frame payload.
func (rawCodec) Unmarshal(data []byte, v any) error {
	dst, ok := v.(*[]byte)
	if !ok {
		return fmt.Errorf("broker.rawCodec: expected *[]byte, got %T", v)
	}
	*dst = data
	return nil
}

// GRPCPassthroughOptions returns the grpc.ServerOptions required to
// turn a fresh *grpc.Server into a transparent pass-through to
// backendConn. Two options are required and must be applied together:
//
//   - ForceServerCodec(rawCodec{}) so the server side unmarshals
//     incoming messages as opaque []byte payloads (otherwise gRPC tries
//     to decode the proto bytes against the registered proto codec and
//     fails because the proxy doesn't know the proto descriptor).
//   - UnknownServiceHandler — every incoming method falls through to
//     forwardGRPCStream, which opens a stream to the backend, propagates
//     headers/trailers/status, and increments the gRPC counter for the
//     stream's lifetime.
//
// The broker's gRPC server should register no other services.
func GRPCPassthroughOptions(backendConn *grpc.ClientConn, counters *Counters) []grpc.ServerOption {
	gauge := counters.GRPC()
	return []grpc.ServerOption{
		grpc.ForceServerCodec(rawCodec{}),
		grpc.UnknownServiceHandler(func(_ any, ss grpc.ServerStream) error {
			gauge.Inc()
			defer gauge.Dec()
			return forwardGRPCStream(backendConn, ss)
		}),
	}
}

// forwardGRPCStream is the core pass-through. It opens a fresh stream to
// the backend with bidi semantics (ClientStreams=true, ServerStreams=true),
// copies request metadata, pumps messages both directions concurrently,
// and finally propagates the backend's trailer + status back to the
// inbound caller.
func forwardGRPCStream(backendConn *grpc.ClientConn, ss grpc.ServerStream) error {
	ctx := ss.Context()

	method, ok := grpc.Method(ctx)
	if !ok {
		return status.Error(13 /*Internal*/, "broker: failed to extract method name from context")
	}

	// Propagate inbound metadata to the backend. Strip hop-by-hop
	// headers gRPC reserves for internal use ("user-agent" gets rewritten
	// downstream, ":authority" is set by the dialer, etc.) — leaving
	// them in caused mysterious upstream failures in some grpc-go
	// versions, so we whitelist what we know is safe.
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

	// Header propagation: wait until the backend sends its header (or
	// closes) before forwarding it to the inbound caller. We do this in
	// the backend→client copy goroutine below — first thing it does
	// after the first RecvMsg succeeds.

	// Two halves of the proxy, each in its own goroutine. The errors
	// surface via channels so the main goroutine can collate them.
	clientToBackendErr := make(chan error, 1)
	go func() {
		clientToBackendErr <- copyMessages(ss, clientStream, false)
		// Half-close the backend send side so it knows the client is done
		// sending. The receive side stays open for the response.
		if cerr := clientStream.CloseSend(); cerr != nil {
			// CloseSend rarely fails; if it does, swallow — the
			// surfaced error from copyMessages already captures the
			// reason the stream ended.
			_ = cerr
		}
	}()

	backendToClientErr := make(chan error, 1)
	go func() {
		// Forward the backend's header to the inbound caller as soon as
		// we have it. Header() blocks until the backend sends its first
		// response (or closes the stream); we want that header in front
		// of any messages we forward back.
		header, herr := clientStream.Header()
		if herr == nil {
			_ = ss.SetHeader(header)
		}
		backendToClientErr <- copyMessages(clientStream, ss, true)
	}()

	// Wait for the backend→client direction to finish — that's when the
	// RPC is logically over from the caller's perspective. The
	// client→backend direction may already have terminated with EOF, or
	// may still be waiting for the caller to close its send side.
	backendErr := <-backendToClientErr
	clientErr := <-clientToBackendErr

	// Trailer propagation: the backend's status + trailer metadata
	// arrive once its stream is fully drained. SetTrailer must be
	// called before this function returns — gRPC sends trailers when
	// the handler exits.
	ss.SetTrailer(clientStream.Trailer())

	// Backend-originated errors dominate: if the backend returned a
	// status error, surface it verbatim so the caller sees the same
	// status code it would have without the broker in the path. If the
	// client-side copy hit a non-EOF error (rare; usually client cancel),
	// surface that.
	if backendErr != nil && !errors.Is(backendErr, io.EOF) {
		return backendErr
	}
	if clientErr != nil && !errors.Is(clientErr, io.EOF) {
		return clientErr
	}
	return nil
}

// copyMessages pumps frames from src to dst as opaque []byte payloads.
// The src is either a grpc.ServerStream (when copying client→backend) or
// a grpc.ClientStream (when copying backend→client); both satisfy
// {RecvMsg,SendMsg}(any) error, so we accept the minimal interface
// instead of pulling in both concrete types. isBackendSide is reserved
// for future divergent handling; currently both directions are identical.
type msgRecv interface {
	RecvMsg(m any) error
}
type msgSend interface {
	SendMsg(m any) error
}

func copyMessages(src msgRecv, dst msgSend, _ bool) error {
	for {
		var frame []byte
		if err := src.RecvMsg(&frame); err != nil {
			// EOF is the normal end of stream on the receive side.
			return err
		}
		if err := dst.SendMsg(&frame); err != nil {
			return err
		}
	}
}
