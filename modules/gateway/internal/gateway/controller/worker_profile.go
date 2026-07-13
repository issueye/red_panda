package controller

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/service"
)

type WorkerProfileController struct {
	Services service.Set
}

type workerProfileCreateRequest struct {
	Key             string   `json:"key"`
	Name            string   `json:"name"`
	NameZH          string   `json:"name_zh"`
	Phase           string   `json:"phase"`
	Description     string   `json:"description"`
	SystemPrompt    string   `json:"system_prompt"`
	Provider        string   `json:"provider"`
	Model           string   `json:"model"`
	ToolAllowlist   []string `json:"tool_allowlist"`
	ToolDenylist    []string `json:"tool_denylist"`
	DefaultMaxTurns int      `json:"default_max_turns"`
	Enabled         *bool    `json:"enabled"`
	SortOrder       int      `json:"sort_order"`
	MetadataJSON    string   `json:"metadata_json"`
}

type workerProfileUpdateRequest struct {
	Name            *string   `json:"name"`
	NameZH          *string   `json:"name_zh"`
	Phase           *string   `json:"phase"`
	Description     *string   `json:"description"`
	SystemPrompt    *string   `json:"system_prompt"`
	Provider        *string   `json:"provider"`
	Model           *string   `json:"model"`
	ToolAllowlist   *[]string `json:"tool_allowlist"`
	ToolDenylist    *[]string `json:"tool_denylist"`
	DefaultMaxTurns *int      `json:"default_max_turns"`
	Enabled         *bool     `json:"enabled"`
	SortOrder       *int      `json:"sort_order"`
	MetadataJSON    *string   `json:"metadata_json"`
}

func (w WorkerProfileController) List(c *gin.Context) {
	items, err := w.Services.WorkerProfiles.List()
	if err != nil {
		writeWorkerProfileError(c, "list", err)
		return
	}
	c.JSON(http.StatusOK, envelope(c, items))
}

func (w WorkerProfileController) ListEnabled(c *gin.Context) {
	items, err := w.Services.WorkerProfiles.ListEnabled()
	if err != nil {
		writeWorkerProfileError(c, "list", err)
		return
	}
	c.JSON(http.StatusOK, envelope(c, items))
}

func (w WorkerProfileController) Get(c *gin.Context) {
	item, err := w.Services.WorkerProfiles.Get(c.Param("id"))
	if err != nil {
		writeWorkerProfileError(c, "get", err)
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (w WorkerProfileController) Create(c *gin.Context) {
	var req workerProfileCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid worker_profile payload"}})
		return
	}
	item, err := w.Services.WorkerProfiles.Create(service.WorkerProfileCreate{
		Key: req.Key, Name: req.Name, NameZH: req.NameZH, Phase: req.Phase,
		Description: req.Description, SystemPrompt: req.SystemPrompt,
		Provider: req.Provider, Model: req.Model, ToolAllowlist: req.ToolAllowlist,
		ToolDenylist: req.ToolDenylist, DefaultMaxTurns: req.DefaultMaxTurns,
		Enabled: req.Enabled, SortOrder: req.SortOrder, MetadataJSON: req.MetadataJSON,
	})
	if err != nil {
		writeWorkerProfileError(c, "create", err)
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (w WorkerProfileController) Update(c *gin.Context) {
	var req workerProfileUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid worker_profile payload"}})
		return
	}
	item, err := w.Services.WorkerProfiles.Update(c.Param("id"), service.WorkerProfileUpdate{
		Name: req.Name, NameZH: req.NameZH, Phase: req.Phase,
		Description: req.Description, SystemPrompt: req.SystemPrompt,
		Provider: req.Provider, Model: req.Model, ToolAllowlist: req.ToolAllowlist,
		ToolDenylist: req.ToolDenylist, DefaultMaxTurns: req.DefaultMaxTurns,
		Enabled: req.Enabled, SortOrder: req.SortOrder, MetadataJSON: req.MetadataJSON,
	})
	if err != nil {
		writeWorkerProfileError(c, "update", err)
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (w WorkerProfileController) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := w.Services.WorkerProfiles.Delete(id); err != nil {
		writeWorkerProfileError(c, "delete", err)
		return
	}
	c.JSON(http.StatusOK, envelope(c, gin.H{"deleted": true, "id": id}))
}

func writeWorkerProfileError(c *gin.Context, operation string, err error) {
	status := http.StatusInternalServerError
	code := "worker_profile_" + operation + "_failed"
	if errors.Is(err, gorm.ErrRecordNotFound) {
		status = http.StatusNotFound
		code = "worker_profile_not_found"
	} else if operation == "create" || operation == "update" || operation == "delete" {
		status = http.StatusBadRequest
	}
	c.JSON(status, gin.H{"ok": false, "error": gin.H{"code": code, "message": err.Error()}})
}
