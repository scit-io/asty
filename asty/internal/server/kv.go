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

// provisionKVBuckets creates all KV buckets declared in svc.KV before
// the service starts. Returns a map of env vars to inject into the
// process: A_KV_{UPPER_BUCKET} = bucket_name.
func (s *Server) provisionKVBuckets(svc *types.ServiceDefinition) (map[string]string, error) {
	if len(svc.KV) == 0 {
		return nil, nil
	}

	js, err := jetstream.New(s.nc)
	if err != nil {
		return nil, fmt.Errorf("jetstream init: %w", err)
	}

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
		if errors.As(err, &jsErr) && jsErr.APIError() != nil && jsErr.APIError().ErrorCode == 10005 && replicas > 1 {
			log.Warn().Int("replicas", replicas).Str("bucket", decl.Bucket).Msg("no peers for placement, reducing replicas")
			replicas--
			continue
		}

		return err
	}
}

// autoReplicas determines the number of KV replicas from the NATS
// cluster size via DiscoveredServers(). Single-node returns 1.
func (s *Server) autoReplicas() int {
	const maxReplicas = 3
	servers := s.nc.DiscoveredServers()
	n := len(servers)
	if n < 1 {
		return 1
	}
	// DiscoveredServers() returns connect_urls which includes all peers
	// but NOT self (unlike Servers()). Add 1 for the current node.
	n++
	if n > maxReplicas {
		return maxReplicas
	}
	return n
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

// DiscoveredServersCount returns the number of NATS cluster peers
// visible via the INFO protocol. Exported for testing.
func (s *Server) DiscoveredServersCount() int {
	return len(s.nc.DiscoveredServers()) + 1
}
