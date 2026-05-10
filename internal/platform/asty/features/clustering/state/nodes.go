package state

import (
	"encoding/json"
	"fmt"
	"time"

	"asty/internal/platform/asty/core/types"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// UpdateNode updates node information in cluster state
func (cs *ClusterState) UpdateNode(node *types.NodeInfo) error {
	now := time.Now()
	node.LastSeen = now

	key := fmt.Sprintf("node.%s", node.ID)

	if node.CreatedAt.IsZero() {
		if existing, err := cs.bucket.Get(key); err == nil {
			var existingNode types.NodeInfo
			if json.Unmarshal(existing.Value(), &existingNode) == nil && !existingNode.CreatedAt.IsZero() {
				node.CreatedAt = existingNode.CreatedAt
			}
		}
		if node.CreatedAt.IsZero() {
			node.CreatedAt = now
		}
	}

	data, err := json.Marshal(node)
	if err != nil {
		return fmt.Errorf("failed to marshal node info: %w", err)
	}

	if _, err := cs.bucket.Put(key, data); err != nil {
		return fmt.Errorf("failed to put node info: %w", err)
	}

	return nil
}

// GetNode retrieves node information from cluster state
func (cs *ClusterState) GetNode(nodeID string) (*types.NodeInfo, error) {
	key := fmt.Sprintf("node.%s", nodeID)
	entry, err := cs.bucket.Get(key)
	if err != nil {
		if err == nats.ErrKeyNotFound {
			return nil, fmt.Errorf("node %s not found", nodeID)
		}
		return nil, fmt.Errorf("failed to get node info: %w", err)
	}

	var node types.NodeInfo
	if err := json.Unmarshal(entry.Value(), &node); err != nil {
		return nil, fmt.Errorf("failed to unmarshal node info: %w", err)
	}

	return &node, nil
}

// ListNodes returns all nodes in the cluster
func (cs *ClusterState) ListNodes() ([]*types.NodeInfo, error) {
	keys, err := cs.bucket.Keys()
	if err != nil {
		if err == nats.ErrNoKeysFound {
			return []*types.NodeInfo{}, nil
		}
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}

	nodes := make([]*types.NodeInfo, 0)
	for _, key := range keys {
		if len(key) < 5 || key[:5] != "node." {
			continue
		}

		entry, err := cs.bucket.Get(key)
		if err != nil {
			log.Warn().Err(err).Str("key", key).Msg("failed to get node entry")
			continue
		}

		var node types.NodeInfo
		if err := json.Unmarshal(entry.Value(), &node); err != nil {
			log.Warn().Err(err).Str("key", key).Msg("failed to unmarshal node")
			continue
		}

		nodes = append(nodes, &node)
	}

	return nodes, nil
}

// RemoveNode removes a node from cluster state
func (cs *ClusterState) RemoveNode(nodeID string) error {
	key := fmt.Sprintf("node.%s", nodeID)
	if err := cs.bucket.Delete(key); err != nil {
		return fmt.Errorf("failed to delete node: %w", err)
	}

	log.Info().Str("node_id", nodeID).Msg("node removed from cluster state")
	return nil
}
