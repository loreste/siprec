package http

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
	"siprec-server/pkg/cluster"
)

// ClusterHandler handles cluster administration API endpoints
type ClusterHandler struct {
	logger       *logrus.Logger
	orchestrator *cluster.ClusterOrchestrator
}

// NewClusterHandler creates a new cluster handler
func NewClusterHandler(logger *logrus.Logger, orchestrator *cluster.ClusterOrchestrator) *ClusterHandler {
	return &ClusterHandler{
		logger:       logger,
		orchestrator: orchestrator,
	}
}

// RegisterHandlers registers all cluster API endpoints
func (h *ClusterHandler) RegisterHandlers(server *Server) {
	server.RegisterHandler("/api/cluster/status", h.handleStatus)
	server.RegisterHandler("/api/cluster/nodes", h.handleNodes)
	server.RegisterHandler("/api/cluster/health", h.handleHealth)
	server.RegisterHandler("/api/cluster/drain", h.handleDrain)
	server.RegisterHandler("/api/cluster/migrations", h.handleMigrations)
	server.RegisterHandler("/api/cluster/split-brain", h.handleSplitBrain)
	server.RegisterHandler("/api/cluster/split-brain/check", h.handleForceQuorumCheck)
	server.RegisterHandler("/api/cluster/traces", h.handleTraces)
	server.RegisterHandler("/api/cluster/rtp-states", h.handleRTPStates)
	h.logger.Info("Cluster admin API handlers registered")
}

func (h *ClusterHandler) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *ClusterHandler) writeError(w http.ResponseWriter, status int, msg string) {
	h.writeJSON(w, status, map[string]string{"error": msg})
}

// handleStatus returns comprehensive cluster status
func (h *ClusterHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.orchestrator == nil {
		h.writeError(w, http.StatusServiceUnavailable, "clustering not enabled")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	status := h.orchestrator.GetClusterStatus(ctx)
	h.writeJSON(w, http.StatusOK, status)
}

// handleNodes lists all cluster nodes
func (h *ClusterHandler) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.orchestrator == nil {
		h.writeError(w, http.StatusServiceUnavailable, "clustering not enabled")
		return
	}
	mgr := h.orchestrator.GetManager()
	if mgr == nil {
		h.writeError(w, http.StatusServiceUnavailable, "cluster manager not available")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	nodes, err := mgr.ListNodes(ctx)
	if err != nil {
		h.logger.WithError(err).Error("Failed to list cluster nodes")
		h.writeError(w, http.StatusInternalServerError, "failed to list nodes")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"nodes": nodes,
		"count": len(nodes),
	})
}

// handleHealth returns cluster health summary
func (h *ClusterHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.orchestrator == nil {
		h.writeError(w, http.StatusServiceUnavailable, "clustering not enabled")
		return
	}
	healthy := h.orchestrator.HasQuorum() && !h.orchestrator.IsFenced()
	status := "healthy"
	if !healthy {
		status = "degraded"
	}
	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      status,
		"is_leader":   h.orchestrator.IsLeader(),
		"has_quorum":  h.orchestrator.HasQuorum(),
		"is_fenced":   h.orchestrator.IsFenced(),
		"partitioned": h.orchestrator.IsPartitioned(),
		"node_id":     h.orchestrator.GetNodeID(),
	})
}

// handleDrain initiates graceful stream migration off this node
func (h *ClusterHandler) handleDrain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.orchestrator == nil {
		h.writeError(w, http.StatusServiceUnavailable, "clustering not enabled")
		return
	}

	var req struct {
		TargetNodeID string `json:"target_node_id"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}

	if req.TargetNodeID == "" {
		// Auto-select a peer node
		mgr := h.orchestrator.GetManager()
		if mgr != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			nodes, err := mgr.ListNodes(ctx)
			cancel()
			if err == nil {
				for _, node := range nodes {
					if node.ID != h.orchestrator.GetNodeID() {
						req.TargetNodeID = node.ID
						break
					}
				}
			}
		}
	}

	if req.TargetNodeID == "" {
		h.writeError(w, http.StatusBadRequest, "no target node available for migration")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	h.logger.WithField("target_node", req.TargetNodeID).Info("Initiating stream drain via API")
	if err := h.orchestrator.MigrateAllStreams(ctx, req.TargetNodeID); err != nil {
		h.logger.WithError(err).Error("Stream drain failed")
		h.writeError(w, http.StatusInternalServerError, "drain failed: "+err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":       "draining",
		"target_node":  req.TargetNodeID,
	})
}

// handleMigrations lists or initiates stream migrations
func (h *ClusterHandler) handleMigrations(w http.ResponseWriter, r *http.Request) {
	if h.orchestrator == nil {
		h.writeError(w, http.StatusServiceUnavailable, "clustering not enabled")
		return
	}
	migrator := h.orchestrator.GetStreamMigrator()
	if migrator == nil {
		h.writeError(w, http.StatusServiceUnavailable, "stream migration not enabled")
		return
	}

	switch r.Method {
	case http.MethodGet:
		pending := migrator.ListPendingMigrations()
		h.writeJSON(w, http.StatusOK, map[string]interface{}{
			"migrations": pending,
			"count":      len(pending),
		})

	case http.MethodPost:
		var req struct {
			CallUUID     string `json:"call_uuid"`
			TargetNodeID string `json:"target_node_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.CallUUID == "" || req.TargetNodeID == "" {
			h.writeError(w, http.StatusBadRequest, "call_uuid and target_node_id are required")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		task, err := migrator.InitiateMigration(ctx, req.CallUUID, req.TargetNodeID)
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "migration failed: "+err.Error())
			return
		}
		h.writeJSON(w, http.StatusAccepted, task)

	default:
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleSplitBrain returns split-brain detection status
func (h *ClusterHandler) handleSplitBrain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.orchestrator == nil {
		h.writeError(w, http.StatusServiceUnavailable, "clustering not enabled")
		return
	}
	detector := h.orchestrator.GetSplitBrainDetector()
	if detector == nil {
		h.writeError(w, http.StatusServiceUnavailable, "split-brain detection not enabled")
		return
	}
	status := detector.GetStatus()
	h.writeJSON(w, http.StatusOK, status)
}

// handleForceQuorumCheck triggers an immediate quorum check
func (h *ClusterHandler) handleForceQuorumCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.orchestrator == nil {
		h.writeError(w, http.StatusServiceUnavailable, "clustering not enabled")
		return
	}
	detector := h.orchestrator.GetSplitBrainDetector()
	if detector == nil {
		h.writeError(w, http.StatusServiceUnavailable, "split-brain detection not enabled")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	status, err := detector.ForceQuorumCheck(ctx)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "quorum check failed: "+err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, status)
}

// handleTraces retrieves distributed traces
func (h *ClusterHandler) handleTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.orchestrator == nil {
		h.writeError(w, http.StatusServiceUnavailable, "clustering not enabled")
		return
	}
	tracer := h.orchestrator.GetTracer()
	if tracer == nil {
		h.writeError(w, http.StatusServiceUnavailable, "distributed tracing not enabled")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Query by trace ID or call UUID
	traceID := r.URL.Query().Get("id")
	callUUID := r.URL.Query().Get("call")

	if traceID != "" {
		spans, err := tracer.GetTrace(ctx, traceID)
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "failed to get trace: "+err.Error())
			return
		}
		h.writeJSON(w, http.StatusOK, map[string]interface{}{
			"trace_id": traceID,
			"spans":    spans,
		})
		return
	}

	if callUUID != "" {
		spans, err := tracer.GetTraceByCallUUID(ctx, callUUID)
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "failed to get trace: "+err.Error())
			return
		}
		h.writeJSON(w, http.StatusOK, map[string]interface{}{
			"call_uuid": callUUID,
			"spans":     spans,
		})
		return
	}

	// Return tracer stats if no specific query
	stats := tracer.GetStats()
	h.writeJSON(w, http.StatusOK, stats)
}

// handleRTPStates lists replicated RTP stream states
func (h *ClusterHandler) handleRTPStates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.orchestrator == nil {
		h.writeError(w, http.StatusServiceUnavailable, "clustering not enabled")
		return
	}
	stateManager := h.orchestrator.GetRTPStateManager()
	if stateManager == nil {
		h.writeError(w, http.StatusServiceUnavailable, "RTP state replication not enabled")
		return
	}

	nodeID := r.URL.Query().Get("node")

	// If no node filter, return local streams
	if nodeID == "" {
		streams := stateManager.ListLocalStreams()
		h.writeJSON(w, http.StatusOK, map[string]interface{}{
			"streams": streams,
			"count":   len(streams),
			"node_id": h.orchestrator.GetNodeID(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	streams, err := stateManager.ListStreams(ctx, nodeID)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to list streams: "+err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"streams": streams,
		"count":   len(streams),
		"node_id": nodeID,
	})
}
