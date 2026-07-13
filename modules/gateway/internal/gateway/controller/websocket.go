package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"redpanda/gateway/internal/gateway/infra/eventhub"
	"redpanda/gateway/internal/gateway/service"
	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
	protows "redpanda/protocol/ws"
)

type WebSocketController struct {
	Services service.Set
	Hub      *eventhub.Hub
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type wsSession struct {
	conn          *websocket.Conn
	services      service.Set
	send          chan protows.Envelope
	subscriptions map[string]func()
	lastSentSeq   map[string]uint64
	mu            sync.Mutex
}

type cancelPayload struct {
	RunID  string `json:"run_id"`
	Reason string `json:"reason,omitempty"`
}

func (w WebSocketController) Connect(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	session := &wsSession{
		conn:          conn,
		services:      w.Services,
		send:          make(chan protows.Envelope, 128),
		subscriptions: map[string]func(){},
		lastSentSeq:   map[string]uint64{},
	}
	defer session.close()

	go session.writeLoop(ctx)

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg protows.Envelope
		if err := json.Unmarshal(raw, &msg); err != nil {
			session.enqueue(errorMessage("", "invalid_json", "invalid websocket message"))
			continue
		}
		session.handle(ctx, msg)
	}
}

func (s *wsSession) handle(ctx context.Context, msg protows.Envelope) {
	ok := true
	switch msg.Type {
	case protows.TypePing:
		s.enqueue(protows.Envelope{ID: msg.ID, Type: protows.TypePong, OK: &ok, Payload: msg.Payload})
	case protows.TypeAuth:
		s.enqueue(response(msg.ID, map[string]any{"authenticated": true}))
	case protows.TypeRequest:
		s.handleRequest(ctx, msg)
	default:
		s.enqueue(errorMessage(msg.ID, "unsupported_message_type", "unsupported websocket message type"))
	}
}

func (s *wsSession) handleRequest(ctx context.Context, msg protows.Envelope) {
	switch msg.Method {
	case protows.MethodAgentStatus:
		s.enqueue(response(msg.ID, s.services.Run.RuntimeStatus()))
	case protows.MethodRunStart:
		s.handleRunStart(ctx, msg)
	case protows.MethodRunSubscribe:
		s.handleRunSubscribe(msg)
	case protows.MethodRunResume:
		s.handleRunResume(msg)
	case protows.MethodRunCancel:
		s.handleRunCancel(ctx, msg)
	case protows.MethodWorkerList:
		s.handleWorkerList(ctx, msg)
	case protows.MethodAssignmentCancel:
		s.handleAssignmentCancel(ctx, msg)
	case protows.MethodPermissionResolve:
		s.handlePermissionResolve(ctx, msg)
	default:
		s.enqueue(errorMessage(msg.ID, "method_not_implemented", "websocket method not implemented"))
	}
}

func (s *wsSession) handleRunStart(ctx context.Context, msg protows.Envelope) {
	var payload protows.RunStartPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		s.enqueue(errorMessage(msg.ID, "invalid_payload", "invalid run.start payload"))
		return
	}
	result, err := s.services.Run.Start(ctx, payload)
	if err != nil {
		s.enqueue(errorMessage(msg.ID, "run_start_failed", err.Error()))
		return
	}
	if payload.Subscribe {
		s.subscribe(result.RunID)
		s.replay(result.RunID, 0)
	}
	s.enqueue(response(msg.ID, result))
}

func (s *wsSession) handleRunSubscribe(msg protows.Envelope) {
	var payload protows.RunSubscribePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		s.enqueue(errorMessage(msg.ID, "invalid_payload", "invalid run.subscribe payload"))
		return
	}
	for _, cursor := range payload.Runs {
		s.subscribe(cursor.RunID)
		s.replay(cursor.RunID, cursor.AfterSeq)
	}
	s.enqueue(response(msg.ID, map[string]any{"subscribed": len(payload.Runs)}))
}

func (s *wsSession) handleRunResume(msg protows.Envelope) {
	var payload protows.RunResumePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		s.enqueue(errorMessage(msg.ID, "invalid_payload", "invalid run.resume payload"))
		return
	}
	for runID, afterSeq := range payload.LastSeen {
		s.subscribe(runID)
		s.replay(runID, afterSeq)
	}
	s.enqueue(response(msg.ID, map[string]any{"resumed": len(payload.LastSeen)}))
}

func (s *wsSession) handleRunCancel(ctx context.Context, msg protows.Envelope) {
	var payload cancelPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil || payload.RunID == "" {
		s.enqueue(errorMessage(msg.ID, "invalid_payload", "invalid run.cancel payload"))
		return
	}
	if err := s.services.Run.Cancel(ctx, payload.RunID, payload.Reason); err != nil {
		s.enqueue(errorMessage(msg.ID, "run_cancel_failed", err.Error()))
		return
	}
	s.enqueue(response(msg.ID, map[string]any{"accepted": true, "run_id": payload.RunID}))
}

func (s *wsSession) handleWorkerList(ctx context.Context, msg protows.Envelope) {
	var payload protows.WorkerListPayload
	if len(msg.Payload) > 0 {
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			s.enqueue(errorMessage(msg.ID, "invalid_payload", "invalid worker.list payload"))
			return
		}
	}
	result, err := s.services.Run.Workers(ctx, methods.WorkerListParams{
		RunID:        payload.RunID,
		WorkerID:     payload.WorkerID,
		AssignmentID: payload.AssignmentID,
	})
	if err != nil {
		s.enqueue(errorMessage(msg.ID, "worker_list_failed", err.Error()))
		return
	}
	s.enqueue(response(msg.ID, result))
}

func (s *wsSession) handleAssignmentCancel(ctx context.Context, msg protows.Envelope) {
	var payload protows.AssignmentCancelPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil || payload.RunID == "" || payload.AssignmentID == "" {
		s.enqueue(errorMessage(msg.ID, "invalid_payload", "invalid worker.assignment.cancel payload"))
		return
	}
	result, err := s.services.Run.CancelAssignment(ctx, methods.WorkerAssignmentCancelParams{
		RunID:        payload.RunID,
		AssignmentID: payload.AssignmentID,
		Reason:       payload.Reason,
	})
	if err != nil {
		s.enqueue(errorMessage(msg.ID, "assignment_cancel_failed", err.Error()))
		return
	}
	s.enqueue(response(msg.ID, result))
}

func (s *wsSession) handlePermissionResolve(ctx context.Context, msg protows.Envelope) {
	var payload permission.ResolveParams
	if err := json.Unmarshal(msg.Payload, &payload); err != nil || payload.PermissionID == "" {
		s.enqueue(errorMessage(msg.ID, "invalid_payload", "invalid permission.resolve payload"))
		return
	}
	result, err := s.services.Run.ResolvePermission(ctx, payload)
	if err != nil {
		s.enqueue(errorMessage(msg.ID, "permission_resolve_failed", err.Error()))
		return
	}
	s.enqueue(response(msg.ID, result))
}

func (s *wsSession) subscribe(rootRunID string) {
	if rootRunID == "" {
		return
	}
	s.mu.Lock()
	if _, exists := s.subscriptions[rootRunID]; exists {
		s.mu.Unlock()
		return
	}
	ch, cancel := s.services.Run.Subscribe(rootRunID)
	s.subscriptions[rootRunID] = cancel
	s.mu.Unlock()

	go func() {
		for event := range ch {
			s.sendEvent(event)
		}
	}()
}

func (s *wsSession) replay(rootRunID string, afterSeq uint64) {
	events, err := s.services.Run.Replay(rootRunID, afterSeq)
	if err != nil {
		s.enqueue(errorMessage("", "run_replay_failed", err.Error()))
		return
	}
	for _, event := range events {
		s.sendEvent(event)
	}
}

func (s *wsSession) sendEvent(event events.EnvelopeV2) {
	if !s.markEventSent(event.RunID, event.RunSeq) {
		return
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return
	}
	s.enqueue(protows.Envelope{
		Type:    protows.TypeEvent,
		Method:  protows.EventRun,
		Payload: raw,
		Meta: map[string]any{
			"run_id":  event.RunID,
			"run_seq": event.RunSeq,
		},
	})
}

func (s *wsSession) markEventSent(rootRunID string, rootSeq uint64) bool {
	if rootRunID == "" || rootSeq == 0 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if rootSeq <= s.lastSentSeq[rootRunID] {
		return false
	}
	s.lastSentSeq[rootRunID] = rootSeq
	return true
}

func (s *wsSession) enqueue(msg protows.Envelope) {
	select {
	case s.send <- msg:
	default:
	}
}

func (s *wsSession) writeLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-s.send:
			if err := s.conn.WriteJSON(msg); err != nil {
				return
			}
		case <-ticker.C:
			if err := s.conn.WriteJSON(protows.Envelope{Type: protows.TypePing}); err != nil {
				return
			}
		}
	}
}

func (s *wsSession) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for runID, cancel := range s.subscriptions {
		cancel()
		delete(s.subscriptions, runID)
	}
}
