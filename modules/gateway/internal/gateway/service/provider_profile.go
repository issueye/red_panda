package service

import (
	"fmt"
	"strings"
	"time"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
)

type ProviderProfileService struct {
	repos repository.Set
}

type ProviderProfileDTO struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Provider     string    `json:"provider"`
	BaseURL      string    `json:"base_url"`
	Model        string    `json:"model"`
	MaxTokens    int       `json:"max_tokens"`
	APIKeySet    bool      `json:"api_key_set"`
	APIKeyMasked string    `json:"api_key_masked,omitempty"`
	IsDefault    bool      `json:"is_default"`
	Stream       bool      `json:"stream"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type ProviderProfileCreate struct {
	Name      string
	Provider  string
	BaseURL   string
	Model     string
	MaxTokens int
	APIKey    string
	IsDefault bool
	Stream    *bool
}

type ProviderProfileUpdate struct {
	Name      *string
	Provider  *string
	BaseURL   *string
	Model     *string
	MaxTokens *int
	APIKey    *string
	IsDefault *bool
	Stream    *bool
	Active    *bool
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
	maxTokens, err := normalizeMaxTokens(input.MaxTokens)
	if err != nil {
		return ProviderProfileDTO{}, err
	}
	stream := true
	if input.Stream != nil {
		stream = *input.Stream
	}
	row, err := s.repos.Providers.Create(model.ProviderProfile{
		Name:         strings.TrimSpace(input.Name),
		Provider:     profile,
		BaseURL:      strings.TrimRight(baseURL, "/"),
		Model:        strings.TrimSpace(input.Model),
		MaxTokens:    maxTokens,
		APIKeySecret: input.APIKey,
		IsDefault:    input.IsDefault,
		Stream:       stream,
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
		ID:           id,
		Name:         current.Name,
		Provider:     current.Provider,
		BaseURL:      current.BaseURL,
		Model:        current.Model,
		MaxTokens:    current.MaxTokens,
		APIKeySecret: current.APIKeySecret,
		IsDefault:    current.IsDefault,
		Stream:       current.Stream,
		Active:       current.Active,
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
	if input.APIKey != nil {
		next.APIKeySecret = *input.APIKey
	}
	if input.IsDefault != nil {
		next.IsDefault = *input.IsDefault
	}
	if input.Stream != nil {
		next.Stream = *input.Stream
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

func providerProfileDTO(row model.ProviderProfile) ProviderProfileDTO {
	return ProviderProfileDTO{
		ID:           row.ID,
		Name:         row.Name,
		Provider:     row.Provider,
		BaseURL:      row.BaseURL,
		Model:        row.Model,
		MaxTokens:    row.MaxTokens,
		APIKeySet:    row.APIKeySecret != "",
		APIKeyMasked: maskSecret(row.APIKeySecret),
		IsDefault:    row.IsDefault,
		Stream:       row.Stream,
		Active:       row.Active,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
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
