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
	"context"
	"encoding/json"
	"net/http"
	"net/http/pprof"
	"os"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	"github.com/kynoproj/kynomesh/pkg/shared/logging"
	sharedutil "github.com/kynoproj/kynomesh/pkg/shared/util"
)

// peerHashesFilePath is a test seam; production uses kmv1.PeerHashesFilePath.
var peerHashesFilePath = kmv1.PeerHashesFilePath

// peerHashesCache caches the peer-hashes file's parsed contents, keyed by the
// file's mtime, so a burst of /introspect requests doesn't re-read and
// re-parse the file on every call — it's only re-read when the agent SDK has
// actually written a new version of it.
var peerHashesCache struct {
	mu      sync.Mutex
	modTime time.Time
	hashes  map[string]string
}

// resetPeerHashesCache clears the cache. Test-only.
func resetPeerHashesCache() {
	peerHashesCache.mu.Lock()
	defer peerHashesCache.mu.Unlock()
	peerHashesCache.modTime = time.Time{}
	peerHashesCache.hashes = nil
}

// introspectResponse is the structured payload served by /introspect. It's
// deliberately a grab-bag of independent sections rather than one endpoint
// per topic, so new pod-internal insight can be added as another field here
// without introducing another endpoint.
type introspectResponse struct {
	// Host is the pod name.
	Host string `json:"host"`
	// PeerHashes is the peer-name-keyed AgentCard hash map.
	PeerHashes map[string]string `json:"peerHashes"`
}

// NewIntrospectionHandler serves /metrics, /healthz (liveness), /readyz, and
// /introspect.
func NewIntrospectionHandler(ctx context.Context, registry *prometheus.Registry, ready func() error) http.Handler {
	logger := logging.FromContext(ctx)
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		// /healthz is liveness only — agent reachability is /readyz.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if err := ready(); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	mux.HandleFunc("/introspect", func(w http.ResponseWriter, _ *http.Request) {
		peerHashes, err := readPeerHashes(logger)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(introspectResponse{
			Host:       os.Getenv(kmv1.EnvPodName),
			PeerHashes: peerHashes,
		})
	})
	pprofEnabled := sharedutil.LookupEnvBoolOr(kmv1.EnvPPROFEnabled, false)
	if pprofEnabled {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	} else {
		logger.Info("Not enabling pprof debug endpoints")
	}
	return mux
}

// readPeerHashes returns the peer-hashes file's contents, or an empty map if
// the file doesn't exist yet — the agent hasn't resolved any peer clients
// since last restart, not an error. The parsed result is cached by the
// file's mtime so repeated calls only re-read and re-parse the file when its
// contents have actually changed.
func readPeerHashes(logger *zap.SugaredLogger) (map[string]string, error) {
	info, err := os.Stat(peerHashesFilePath)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		logger.Errorw("Failed to stat peer-hashes file",
			zap.String("path", peerHashesFilePath),
			zap.Error(err))
		return nil, err
	}

	peerHashesCache.mu.Lock()
	defer peerHashesCache.mu.Unlock()
	if info.ModTime().Equal(peerHashesCache.modTime) && peerHashesCache.hashes != nil {
		return peerHashesCache.hashes, nil
	}

	raw, err := os.ReadFile(peerHashesFilePath)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		logger.Errorw("Failed to read peer-hashes file",
			zap.String("path", peerHashesFilePath),
			zap.Error(err))
		return nil, err
	}
	hashes := map[string]string{}
	if err := json.Unmarshal(raw, &hashes); err != nil {
		logger.Errorw("Failed to decode peer-hashes file",
			zap.String("path", peerHashesFilePath),
			zap.Error(err))
		return nil, err
	}

	peerHashesCache.modTime = info.ModTime()
	peerHashesCache.hashes = hashes
	return hashes, nil
}
