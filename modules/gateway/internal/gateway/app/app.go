package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"

	"github.com/gin-gonic/gin"

	"redpanda/gateway/internal/gateway/controller"
	"redpanda/gateway/internal/gateway/infra/database"
	"redpanda/gateway/internal/gateway/infra/eventhub"
	"redpanda/gateway/internal/gateway/infra/runtimeclient"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/gateway/internal/gateway/service"
	"redpanda/protocol/events"
	"redpanda/protocol/methods"
)

type Config struct {
	Addr        string
	DatabaseDSN string
	Token       string
	Version     string

	AgentCommand string
	AgentArgs    []string
}

func Run(ctx context.Context, cfg Config) error {
	db, err := database.Open(cfg.DatabaseDSN)
	if err != nil {
		return err
	}
	if err := database.Migrate(db); err != nil {
		return err
	}

	hub := eventhub.New()
	repos := repository.NewSet(db)
	var services service.Set
	runtime := runtimeclient.New(agentCommand(cfg.AgentCommand), cfg.AgentArgs, cfg.Version, func(event events.Envelope) {
		services.Run.HandleRuntimeEvent(event)
	}, func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		switch method {
		case methods.MemoryToolExecute:
			var req methods.MemoryToolExecuteParams
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, fmt.Errorf("invalid memory tool params")
			}
			return services.Memory.ExecuteRuntimeTool(req)
		case methods.TodoToolExecute:
			var req methods.TodoToolExecuteParams
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, fmt.Errorf("invalid todo tool params")
			}
			return services.Todo.ExecuteRuntimeTool(req)
		case methods.GoalToolExecute:
			var req methods.GoalToolExecuteParams
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, fmt.Errorf("invalid goal tool params")
			}
			return services.Goal.ExecuteRuntimeTool(req)
		default:
			return nil, fmt.Errorf("method not found: %s", method)
		}
	})
	services = service.NewSet(service.Options{
		Version:       cfg.Version,
		Repos:         repos,
		Hub:           hub,
		RuntimeClient: runtime,
	})
	controllers := controller.NewSet(services, hub)

	router := NewRouter(cfg, controllers)
	server := &http.Server{Addr: cfg.Addr, Handler: router}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		_ = server.Shutdown(context.Background())
		return ctx.Err()
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func NewRouter(cfg Config, controllers controller.Set) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(requestID())
	r.Use(localDesktopCORS())
	r.GET("/healthz", controllers.Health.Healthz)
	r.GET("/readyz", controllers.Health.Readyz)

	api := r.Group("/api/v1")
	api.Use(auth(cfg.Token))
	api.GET("/app/bootstrap", controllers.App.Bootstrap)
	api.GET("/agent/status", controllers.Agent.Status)
	api.GET("/sessions", controllers.Session.List)
	api.POST("/sessions", controllers.Session.Create)
	api.DELETE("/sessions/:id", controllers.Session.Delete)
	api.GET("/sessions/:id/history", controllers.Session.History)
	api.POST("/sessions/:id/fork", controllers.Session.Fork)
	api.GET("/sessions/:id/compact", controllers.Session.CompactionState)
	api.POST("/sessions/:id/compact/preview", controllers.Session.CompactPreview)
	api.POST("/sessions/:id/compact", controllers.Session.Compact)
	api.GET("/sessions/:id/permissions", controllers.Permission.ListBySession)
	api.GET("/sessions/:id/runs", controllers.Run.ListBySession)
	api.GET("/sessions/:id/tools", controllers.Tool.ListBySession)
	api.GET("/sessions/:id/todos", controllers.Todo.ListBySession)
	api.GET("/sessions/:id/goals", controllers.Goal.ListBySession)
	api.POST("/sessions/:id/goals/start", controllers.Goal.Start)
	api.GET("/sessions/:id/goals/:goalId", controllers.Goal.Get)
	api.POST("/sessions/:id/goals/:goalId/cancel", controllers.Goal.Cancel)
	api.POST("/sessions/:id/goals/:goalId/continue", controllers.Goal.Continue)
	api.GET("/memory", controllers.Memory.List)
	api.POST("/memory", controllers.Memory.Create)
	api.PUT("/memory/:id", controllers.Memory.Update)
	api.DELETE("/memory/:id", controllers.Memory.Delete)
	api.POST("/memory/preview-run", controllers.Memory.PreviewRun)
	api.GET("/permissions/pending", controllers.Permission.Pending)
	api.GET("/permissions/:id", controllers.Permission.Get)
	api.GET("/provider-profiles", controllers.Provider.List)
	api.POST("/provider-profiles", controllers.Provider.Create)
	api.GET("/provider-profiles/:id", controllers.Provider.Get)
	api.PUT("/provider-profiles/:id", controllers.Provider.Update)
	api.DELETE("/provider-profiles/:id", controllers.Provider.Delete)
	api.GET("/mcp/servers", controllers.MCPServers.List)
	api.POST("/mcp/servers", controllers.MCPServers.Create)
	api.GET("/mcp/servers/:id", controllers.MCPServers.Get)
	api.PUT("/mcp/servers/:id", controllers.MCPServers.Update)
	api.DELETE("/mcp/servers/:id", controllers.MCPServers.Delete)
	api.POST("/mcp/servers/:id/discover", controllers.MCPServers.Discover)
	api.GET("/skills", controllers.Skills.List)
	api.POST("/skills", controllers.Skills.Create)
	api.GET("/skills/:name", controllers.Skills.Get)
	api.PUT("/skills/:name", controllers.Skills.Update)
	api.DELETE("/skills/:name", controllers.Skills.Delete)
	api.GET("/agents", controllers.Agents.List)
	api.GET("/agents/enabled", controllers.Agents.ListEnabled)
	api.POST("/agents", controllers.Agents.Create)
	api.GET("/agents/:id", controllers.Agents.Get)
	api.PUT("/agents/:id", controllers.Agents.Update)
	api.DELETE("/agents/:id", controllers.Agents.Delete)
	api.GET("/runs/:id", controllers.Run.Get)
	api.GET("/runs/:id/events", controllers.Run.Events)
	api.GET("/runs/:id/permissions", controllers.Permission.ListByRun)
	api.GET("/runs/:id/tools", controllers.Tool.ListByRun)
	api.POST("/workspaces/open", controllers.Workspace.Open)
	api.GET("/workspaces/current", controllers.Workspace.Current)
	api.GET("/workspaces/recent", controllers.Workspace.Recent)
	api.DELETE("/workspaces/:id", controllers.Workspace.Delete)
	api.GET("/workspaces/tree", controllers.Workspace.Tree)
	api.GET("/workspaces/file", controllers.Workspace.File)
	api.GET("/workspaces/diff", controllers.Workspace.Diff)
	api.GET("/ws", controllers.WebSocket.Connect)

	return r
}

func agentCommand(configured string) string {
	if configured != "" {
		return configured
	}
	exe, err := os.Executable()
	if err == nil {
		name := "red-panda-agent"
		if goruntime.GOOS == "windows" {
			name += ".exe"
		}
		return filepath.Join(filepath.Dir(exe), name)
	}
	return "red-panda-agent"
}
