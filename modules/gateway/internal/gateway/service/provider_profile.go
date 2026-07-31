package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
)

type ProviderProfileService struct {
	repos repository.Set
}

type ProviderProfileDTO struct {
	ID             string             `json:"id"`
	Name           string             `json:"name"`
	Provider       string             `json:"provider"`
	BaseURL        string             `json:"base_url"`
	Model          string             `json:"model"`
	MaxTokens      int                `json:"max_tokens"`
	Models         []ProviderModelDTO `json:"models"`
	APIKeySet      bool               `json:"api_key_set"`
	APIKeyMasked   string             `json:"api_key_masked,omitempty"`
	IsDefault      bool               `json:"is_default"`
	Stream         bool               `json:"stream"`
	Active         bool               `json:"active"`
	SupportsVision bool               `json:"supports_vision"`
	HTTPProxy      string             `json:"http_proxy,omitempty"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
}

type ProviderModelDTO struct {
	Model           string `json:"model"`
	Label           string `json:"label,omitempty"`
	MaxTokens       int    `json:"max_tokens,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

type ProviderModelInput struct {
	Model           string `json:"model"`
	Label           string `json:"label,omitempty"`
	MaxTokens       int    `json:"max_tokens,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

type ProviderModelListInput struct {
	ProfileID string
	BaseURL   string
	APIKey    string
	HTTPProxy string
}

type ProviderProfileCreate struct {
	Name           string
	Provider       string
	BaseURL        string
	Model          string
	MaxTokens      int
	Models         []ProviderModelInput
	APIKey         string
	IsDefault      bool
	Stream         *bool
	SupportsVision *bool
	HTTPProxy      string
}

type ProviderProfileUpdate struct {
	Name           *string
	Provider       *string
	BaseURL        *string
	Model          *string
	MaxTokens      *int
	Models         *[]ProviderModelInput
	APIKey         *string
	IsDefault      *bool
	Stream         *bool
	Active         *bool
	SupportsVision *bool
	HTTPProxy      *string
}

func NewProviderProfileService(repos repository.Set) ProviderProfileService {
	return ProviderProfileService{repos: repos}
}

func (s ProviderProfileService) Create(input ProviderProfileCreate) (ProviderProfileDTO, error) {
	profile, err := normalizeProvider(input.Provider)
	if err != nil {
		return ProviderProfileDTO{}, err
	}
	baseURL := strings.TrimSpace(input.BaseURL)
	if baseURL == "" {
		return ProviderProfileDTO{}, fmt.Errorf("base_url is required")
	}
	models, defaultModel, maxTokens, err := normalizeProviderModels(input.Models, input.Model, input.MaxTokens)
	if err != nil {
		return ProviderProfileDTO{}, err
	}
	stream := true
	if input.Stream != nil {
		stream = *input.Stream
	}
	supportsVision := false
	if input.SupportsVision != nil {
		supportsVision = *input.SupportsVision
	}
	proxy, err := normalizeProviderHTTPProxy(input.HTTPProxy)
	if err != nil {
		return ProviderProfileDTO{}, err
	}
	row, err := s.repos.Providers.Create(model.ProviderProfile{
		Name:           strings.TrimSpace(input.Name),
		Provider:       profile,
		BaseURL:        strings.TrimRight(baseURL, "/"),
		Model:          defaultModel,
		MaxTokens:      maxTokens,
		Models:         models,
		APIKeySecret:   input.APIKey,
		IsDefault:      input.IsDefault,
		Stream:         stream,
		SupportsVision: supportsVision,
		HTTPProxy:      proxy,
	})
	if err != nil {
		return ProviderProfileDTO{}, err
	}
	return providerProfileDTO(row), nil
}

func (s ProviderProfileService) Update(id string, input ProviderProfileUpdate) (ProviderProfileDTO, error) {
	current, err := s.repos.Providers.Get(id)
	if err != nil {
		return ProviderProfileDTO{}, err
	}
	next := model.ProviderProfile{
		ID:             id,
		Name:           current.Name,
		Provider:       current.Provider,
		BaseURL:        current.BaseURL,
		Model:          current.Model,
		MaxTokens:      current.MaxTokens,
		Models:         current.Models,
		APIKeySecret:   current.APIKeySecret,
		IsDefault:      current.IsDefault,
		Stream:         current.Stream,
		Active:         current.Active,
		SupportsVision: current.SupportsVision,
		HTTPProxy:      current.HTTPProxy,
	}
	if input.Name != nil {
		next.Name = strings.TrimSpace(*input.Name)
	}
	if input.Provider != nil {
		profile, err := normalizeProvider(*input.Provider)
		if err != nil {
			return ProviderProfileDTO{}, err
		}
		next.Provider = profile
	}
	if input.BaseURL != nil {
		next.BaseURL = strings.TrimRight(strings.TrimSpace(*input.BaseURL), "/")
		if next.BaseURL == "" {
			return ProviderProfileDTO{}, fmt.Errorf("base_url is required")
		}
	}
	if input.Model != nil {
		next.Model = strings.TrimSpace(*input.Model)
	}
	if input.MaxTokens != nil {
		maxTokens, err := normalizeMaxTokens(*input.MaxTokens)
		if err != nil {
			return ProviderProfileDTO{}, err
		}
		next.MaxTokens = maxTokens
	}
	if input.Models != nil {
		models, defaultModel, maxTokens, err := normalizeProviderModels(*input.Models, next.Model, next.MaxTokens)
		if err != nil {
			return ProviderProfileDTO{}, err
		}
		next.Models = models
		next.Model = defaultModel
		next.MaxTokens = maxTokens
	} else if input.Model != nil {
		models, defaultModel, maxTokens, err := normalizeProviderModels(nil, next.Model, next.MaxTokens)
		if err != nil {
			return ProviderProfileDTO{}, err
		}
		next.Models = models
		next.Model = defaultModel
		next.MaxTokens = maxTokens
	}
	if input.APIKey != nil {
		next.APIKeySecret = *input.APIKey
	}
	if input.IsDefault != nil {
		next.IsDefault = *input.IsDefault
	}
	if input.Stream != nil {
		next.Stream = *input.Stream
	}
	if input.SupportsVision != nil {
		next.SupportsVision = *input.SupportsVision
	}
	if input.HTTPProxy != nil {
		proxy, err := normalizeProviderHTTPProxy(*input.HTTPProxy)
		if err != nil {
			return ProviderProfileDTO{}, err
		}
		next.HTTPProxy = proxy
	}
	if input.Active != nil {
		next.Active = *input.Active
	}
	row, err := s.repos.Providers.Update(next)
	if err != nil {
		return ProviderProfileDTO{}, err
	}
	return providerProfileDTO(row), nil
}

func (s ProviderProfileService) Get(id string) (ProviderProfileDTO, error) {
	row, err := s.repos.Providers.Get(id)
	if err != nil {
		return ProviderProfileDTO{}, err
	}
	return providerProfileDTO(row), nil
}

func (s ProviderProfileService) List() ([]ProviderProfileDTO, error) {
	rows, err := s.repos.Providers.List(100)
	if err != nil {
		return nil, err
	}
	items := make([]ProviderProfileDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, providerProfileDTO(row))
	}
	return items, nil
}

func (s ProviderProfileService) Delete(id string) error {
	return s.repos.Providers.Delete(id)
}

func (s ProviderProfileService) ListModels(ctx context.Context, input ProviderModelListInput) ([]string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	apiKey := strings.TrimSpace(input.APIKey)
	proxyURL := input.HTTPProxy
	if input.ProfileID != "" && apiKey == "" {
		profile, err := s.repos.Providers.Get(input.ProfileID)
		if err != nil {
			return nil, fmt.Errorf("load provider profile: %w", err)
		}
		if baseURL != strings.TrimRight(strings.TrimSpace(profile.BaseURL), "/") ||
			strings.TrimSpace(proxyURL) != strings.TrimSpace(profile.HTTPProxy) {
			return nil, fmt.Errorf("api_key is required after changing base_url or http_proxy")
		}
		apiKey = strings.TrimSpace(profile.APIKeySecret)
	}
	if baseURL == "" {
		return nil, fmt.Errorf("base_url is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("base_url must be a valid http(s) URL")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("api_key is required")
	}
	client, err := providerModelHTTPClient(proxyURL)
	if err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create model list request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request model list: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("read model list: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("model list returned HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode model list: %w", err)
	}
	seen := make(map[string]struct{}, len(payload.Data))
	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, id)
	}
	sort.Strings(models)
	return models, nil
}

func providerModelHTTPClient(rawProxy string) (*http.Client, error) {
	proxyURL, err := normalizeProviderHTTPProxy(rawProxy)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxyURL == "" {
		return &http.Client{Transport: transport}, nil
	}
	parsed, _ := url.Parse(proxyURL)
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		transport.Proxy = http.ProxyURL(parsed)
	case "socks5", "socks5h":
		var auth *xproxy.Auth
		if parsed.User != nil {
			password, _ := parsed.User.Password()
			auth = &xproxy.Auth{User: parsed.User.Username(), Password: password}
		}
		dialer, err := xproxy.SOCKS5("tcp", parsed.Host, auth, &net.Dialer{Timeout: 15 * time.Second})
		if err != nil {
			return nil, fmt.Errorf("configure socks5 proxy: %w", err)
		}
		transport.Proxy = nil
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialer.Dial(network, address)
		}
	}
	return &http.Client{Transport: transport}, nil
}

func normalizeProvider(value string) (string, error) {
	provider := strings.ToLower(strings.TrimSpace(value))
	if provider == "" {
		provider = "openai_compatible"
	}
	switch provider {
	case "openai_compatible", "http_compatible":
		return "openai_compatible", nil
	case "openai_responses", "anthropic":
		return provider, nil
	default:
		return "", fmt.Errorf("unsupported provider %q", value)
	}
}

// normalizeProviderHTTPProxy trims and validates an optional HTTP(S)/SOCKS5
// proxy URL. Empty is allowed (falls back to environment proxy). Mirrors the
// schemes accepted by the Agent Runtime provider transport.
func normalizeProviderHTTPProxy(raw string) (string, error) {
	proxy := strings.TrimSpace(raw)
	if proxy == "" {
		return "", nil
	}
	// 兼容粘贴时的全角标点。
	proxy = strings.Map(func(r rune) rune {
		switch r {
		case '：':
			return ':'
		case '／':
			return '/'
		default:
			return r
		}
	}, proxy)
	if !strings.Contains(proxy, "://") {
		proxy = "http://" + proxy
	}
	parsed, err := url.Parse(proxy)
	if err != nil {
		return "", fmt.Errorf("invalid http_proxy: %w", err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks5", "socks5h":
		// 支持的协议。
	default:
		return "", fmt.Errorf("unsupported http_proxy scheme %q (use http://, https://, or socks5://)", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("http_proxy host is required")
	}
	return proxy, nil
}

func providerProfileDTO(row model.ProviderProfile) ProviderProfileDTO {
	models := providerModelsForRead(row)
	modelItems := make([]ProviderModelDTO, 0, len(models))
	for _, item := range models {
		modelItems = append(modelItems, ProviderModelDTO{
			Model: item.Model, Label: item.Label, MaxTokens: item.MaxTokens,
			ReasoningEffort: item.ReasoningEffort,
		})
	}
	return ProviderProfileDTO{
		ID:             row.ID,
		Name:           row.Name,
		Provider:       row.Provider,
		BaseURL:        row.BaseURL,
		Model:          row.Model,
		MaxTokens:      row.MaxTokens,
		Models:         modelItems,
		APIKeySet:      row.APIKeySecret != "",
		APIKeyMasked:   maskSecret(row.APIKeySecret),
		IsDefault:      row.IsDefault,
		Stream:         row.Stream,
		Active:         row.Active,
		SupportsVision: row.SupportsVision,
		HTTPProxy:      row.HTTPProxy,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

func providerModelsForRead(row model.ProviderProfile) []model.ProviderModel {
	if len(row.Models) > 0 {
		return row.Models
	}
	if strings.TrimSpace(row.Model) == "" {
		return []model.ProviderModel{}
	}
	return []model.ProviderModel{{Model: strings.TrimSpace(row.Model), MaxTokens: row.MaxTokens}}
}

func normalizeProviderModels(input []ProviderModelInput, legacyModel string, legacyMaxTokens int) ([]model.ProviderModel, string, int, error) {
	defaultModel := strings.TrimSpace(legacyModel)
	if len(input) == 0 {
		maxTokens, err := normalizeMaxTokens(legacyMaxTokens)
		if err != nil {
			return nil, "", 0, err
		}
		if defaultModel == "" {
			return []model.ProviderModel{}, "", maxTokens, nil
		}
		return []model.ProviderModel{{Model: defaultModel, MaxTokens: maxTokens}}, defaultModel, maxTokens, nil
	}

	items := make([]model.ProviderModel, 0, len(input))
	seen := map[string]struct{}{}
	for index, raw := range input {
		name := strings.TrimSpace(raw.Model)
		if name == "" {
			return nil, "", 0, fmt.Errorf("models[%d].model is required", index)
		}
		if _, exists := seen[name]; exists {
			return nil, "", 0, fmt.Errorf("duplicate model %q", name)
		}
		seen[name] = struct{}{}
		maxTokens, err := normalizeMaxTokens(raw.MaxTokens)
		if err != nil {
			return nil, "", 0, fmt.Errorf("models[%d]: %w", index, err)
		}
		effort, err := NormalizeReasoningEffort(raw.ReasoningEffort)
		if err != nil {
			return nil, "", 0, fmt.Errorf("models[%d]: %w", index, err)
		}
		items = append(items, model.ProviderModel{
			Model: name, Label: strings.TrimSpace(raw.Label), MaxTokens: maxTokens, ReasoningEffort: effort,
		})
	}
	if defaultModel == "" {
		defaultModel = items[0].Model
	}
	for _, item := range items {
		if item.Model == defaultModel {
			return items, defaultModel, item.MaxTokens, nil
		}
	}
	return nil, "", 0, fmt.Errorf("default model %q is not in models", defaultModel)
}

func NormalizeReasoningEffort(value string) (string, error) {
	effort := strings.ToLower(strings.TrimSpace(value))
	switch effort {
	case "", "low", "medium", "high", "xhigh":
		return effort, nil
	default:
		return "", fmt.Errorf("unsupported reasoning_effort %q", value)
	}
}

const maxProviderContextTokens = 2_000_000

func normalizeMaxTokens(value int) (int, error) {
	if value < 0 {
		return 0, fmt.Errorf("max_tokens must be >= 0")
	}
	if value > maxProviderContextTokens {
		return 0, fmt.Errorf("max_tokens must be <= %d", maxProviderContextTokens)
	}
	return value, nil
}

func maskSecret(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return "****"
	}
	return "****" + value[len(value)-4:]
}
