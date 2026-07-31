package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"redpanda/gateway/internal/gateway/service"
)

type ProviderProfileController struct {
	Services service.Set
}

type providerProfileCreateRequest struct {
	Name              string                       `json:"name"`
	Provider          string                       `json:"provider"`
	BaseURL           string                       `json:"base_url"`
	Model             string                       `json:"model"`
	MaxTokens         int                          `json:"max_tokens"`
	Models            []service.ProviderModelInput `json:"models"`
	APIKey            string                       `json:"api_key"`
	IsDefault         bool                         `json:"is_default"`
	Stream            *bool                        `json:"stream"`
	SupportsVision    *bool                        `json:"supports_vision"`
	HTTPProxy         string                       `json:"http_proxy"`
	CacheMode         string                       `json:"cache_mode"`
	CacheKeySupported *bool                        `json:"cache_key_supported"`
	CacheRetention    string                       `json:"cache_retention"`
	MinCacheTokens    int                          `json:"min_cache_tokens"`
}

type providerProfileUpdateRequest struct {
	Name              *string                       `json:"name"`
	Provider          *string                       `json:"provider"`
	BaseURL           *string                       `json:"base_url"`
	Model             *string                       `json:"model"`
	MaxTokens         *int                          `json:"max_tokens"`
	Models            *[]service.ProviderModelInput `json:"models"`
	APIKey            *string                       `json:"api_key"`
	IsDefault         *bool                         `json:"is_default"`
	Stream            *bool                         `json:"stream"`
	Active            *bool                         `json:"active"`
	SupportsVision    *bool                         `json:"supports_vision"`
	HTTPProxy         *string                       `json:"http_proxy"`
	CacheMode         *string                       `json:"cache_mode"`
	CacheKeySupported *bool                         `json:"cache_key_supported"`
	CacheRetention    *string                       `json:"cache_retention"`
	MinCacheTokens    *int                          `json:"min_cache_tokens"`
}

type providerModelListRequest struct {
	ProfileID string `json:"profile_id"`
	BaseURL   string `json:"base_url"`
	APIKey    string `json:"api_key"`
	HTTPProxy string `json:"http_proxy"`
}

func (p ProviderProfileController) List(c *gin.Context) {
	items, err := p.Services.Provider.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "provider_profile_list_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, items))
}

func (p ProviderProfileController) ListModels(c *gin.Context) {
	var req providerModelListRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid model list payload"}})
		return
	}
	items, err := p.Services.Provider.ListModels(c.Request.Context(), service.ProviderModelListInput{
		ProfileID: req.ProfileID, BaseURL: req.BaseURL, APIKey: req.APIKey, HTTPProxy: req.HTTPProxy,
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"ok": false, "error": gin.H{"code": "provider_model_list_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, gin.H{"models": items}))
}

func (p ProviderProfileController) Create(c *gin.Context) {
	var req providerProfileCreateRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid provider profile payload"}})
		return
	}
	item, err := p.Services.Provider.Create(service.ProviderProfileCreate{
		Name:              req.Name,
		Provider:          req.Provider,
		BaseURL:           req.BaseURL,
		Model:             req.Model,
		MaxTokens:         req.MaxTokens,
		Models:            req.Models,
		APIKey:            req.APIKey,
		IsDefault:         req.IsDefault,
		Stream:            req.Stream,
		SupportsVision:    req.SupportsVision,
		HTTPProxy:         req.HTTPProxy,
		CacheMode:         req.CacheMode,
		CacheKeySupported: req.CacheKeySupported,
		CacheRetention:    req.CacheRetention,
		MinCacheTokens:    req.MinCacheTokens,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "provider_profile_create_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (p ProviderProfileController) Get(c *gin.Context) {
	item, err := p.Services.Provider.Get(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": gin.H{"code": "provider_profile_not_found", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (p ProviderProfileController) Update(c *gin.Context) {
	var req providerProfileUpdateRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid provider profile payload"}})
		return
	}
	item, err := p.Services.Provider.Update(c.Param("id"), service.ProviderProfileUpdate{
		Name:              req.Name,
		Provider:          req.Provider,
		BaseURL:           req.BaseURL,
		Model:             req.Model,
		MaxTokens:         req.MaxTokens,
		Models:            req.Models,
		APIKey:            req.APIKey,
		IsDefault:         req.IsDefault,
		Stream:            req.Stream,
		Active:            req.Active,
		SupportsVision:    req.SupportsVision,
		HTTPProxy:         req.HTTPProxy,
		CacheMode:         req.CacheMode,
		CacheKeySupported: req.CacheKeySupported,
		CacheRetention:    req.CacheRetention,
		MinCacheTokens:    req.MinCacheTokens,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "provider_profile_update_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (p ProviderProfileController) Delete(c *gin.Context) {
	if err := p.Services.Provider.Delete(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "provider_profile_delete_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, gin.H{"deleted": true}))
}
