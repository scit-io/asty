package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"asty/asty/internal/core/types"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog/log"
)

const kvProvisionTimeout = 15 * time.Second

// JetStream API error codes we react to when provisioning a KV bucket.
// They both translate to "this request asked for more replicas than
// the local JetStream can place" — the difference is whether NATS is
// running clustered (10005, no eligible peers) or standalone (10074,
// non-clustered mode rejects replicas > 1 outright). In both cases we
// degrade by one and retry; the bucket lands with whatever the local
// JetStream can actually deliver.
const (
	jsErrNoPeersForPlacement     = 10005
	jsErrReplicasNotSupportedJSE = 10074
)

// provisionKVBuckets creates all KV buckets declared in svc.KV before
// the service starts. Returns a map of env vars to inject into the
// process: A_KV_{UPPER_BUCKET} = bucket_name.
func (s *Server) provisionKVBuckets(svc *types.ServiceDefinition) (map[string]string, error) {
	if len(svc.KV) == 0 {
		return nil, nil
	}

	js := s.js
	envVars := make(map[string]string, len(svc.KV))

	for _, decl := range svc.KV {
		if decl.Bucket == "" {
			continue
		}
		if err := s.ensureKVBucket(js, decl); err != nil {
			return nil, fmt.Errorf("kv bucket %q: %w", decl.Bucket, err)
		}
		envKey := "A_KV_" + strings.ToUpper(decl.Bucket)
		envVars[envKey] = decl.Bucket
	}

	return envVars, nil
}

func (s *Server) ensureKVBucket(js jetstream.JetStream, decl types.KVBucket) error {
	ctx, cancel := context.WithTimeout(context.Background(), kvProvisionTimeout)
	defer cancel()

	replicas := decl.Replicas
	if replicas <= 0 {
		replicas = s.autoReplicas()
	}
	requested := replicas

	history := decl.History
	if history <= 0 {
		history = 1
	}

	var ttl time.Duration
	if decl.TTL != "" {
		d, err := time.ParseDuration(decl.TTL)
		if err == nil {
			ttl = d
		}
	}

	for {
		_, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket:   decl.Bucket,
			Replicas: replicas,
			History:  uint8(history),
			TTL:      ttl,
			Storage:  jetstream.FileStorage,
		})
		if err == nil {
			event := log.Info()
			if requested-replicas >= 2 {
				event = log.Error().Int("requested", requested)
			}
			event.Str("bucket", decl.Bucket).Int("replicas", replicas).Msg("KV bucket created")
			return nil
		}

		if errors.Is(err, jetstream.ErrBucketExists) {
			log.Info().Str("bucket", decl.Bucket).Msg("KV bucket already exists")
			return nil
		}

		var jsErr jetstream.JetStreamError
		if replicas > 1 && errors.As(err, &jsErr) && jsErr.APIError() != nil {
			code := jsErr.APIError().ErrorCode
			if code == jsErrNoPeersForPlacement || code == jsErrReplicasNotSupportedJSE {
				log.Warn().Int("replicas", replicas).Str("bucket", decl.Bucket).Uint16("code", uint16(code)).Msg("reducing KV replicas: cluster cannot place requested count")
				replicas--
				continue
			}
		}

		return err
	}
}

// capReplicas clamps a desired replica count to [1, ceiling].
func capReplicas(size, ceiling int) int {
	if size > ceiling {
		size = ceiling
	}
	if size < 1 {
		return 1
	}
	return size
}

// autoReplicas / systemReplicas are the placement targets at the current
// cluster size — used at bucket creation. The ceilings come from config
// (cluster.app_kv_replicas / cluster.system_kv_replicas) — see ClusterConfig
// for the quorum-vs-latency rationale.
func (s *Server) autoReplicas() int {
	return capReplicas(s.clusterSize(), s.cfg.Cluster.AppKVReplicas)
}
func (s *Server) systemReplicas() int {
	return capReplicas(s.clusterSize(), s.cfg.Cluster.SystemKVReplicas)
}

// kvEnvForAllocation merges the KV env vars into the service definition
// env map so the agent passes them to the process. Mutates svc.Env in
// place — safe because the ServiceDefinition is freshly loaded for each
// deploy and not shared.
func kvEnvForAllocation(svc *types.ServiceDefinition, kvEnv map[string]string) {
	if len(kvEnv) == 0 {
		return
	}
	if svc.Env == nil {
		svc.Env = make(map[string]string, len(kvEnv))
	}
	for k, v := range kvEnv {
		svc.Env[k] = v
	}
}
