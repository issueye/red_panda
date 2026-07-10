package service

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	protocolmcp "redpanda/protocol/mcp"
)

var ErrInvalidMCPServerConfig = errors.New("invalid MCP server config")

var mcpServerNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

const redactedMCPEnvValue = "****"

type MCPServerConfigService struct {
	repos repository.Set
}

type MCPServerConfigUpdate struct {
	Name          *string
	Command       *string
	Args          *[]string
	Env           *map[string]string
	CWD           *string
	Enabled       *bool
	Timeouts      *protocolmcp.MCPTimeouts
	ToolAllowlist *[]string
	RiskOverrides *map[string]string
}

func NewMCPServerConfigService(repos repository.Set) MCPServerConfigService {
	return MCPServerConfigService{repos: repos}
}

func (s MCPServerConfigService) Create(input protocolmcp.MCPServerConfig) (protocolmcp.MCPServerResponse, error) {
	normalized, err := validateAndNormalizeMCPServerConfig(input)
	if err != nil {
		return protocolmcp.MCPServerResponse{}, err
	}
	row, err := s.repos.MCPServers.Create(mcpServerModel(normalized))
	if err != nil {
		return protocolmcp.MCPServerResponse{}, err
	}
	return mcpServerResponse(row), nil
}

func (s MCPServerConfigService) Get(id string) (protocolmcp.MCPServerResponse, error) {
	row, err := s.repos.MCPServers.Get(id)
	if err != nil {
		return protocolmcp.MCPServerResponse{}, err
	}
	return mcpServerResponse(row), nil
}

func (s MCPServerConfigService) List() (protocolmcp.MCPServerListResponse, error) {
	rows, err := s.repos.MCPServers.List(100)
	if err != nil {
		return protocolmcp.MCPServerListResponse{}, err
	}
	servers := make([]protocolmcp.MCPServerResponse, 0, len(rows))
	for _, row := range rows {
		servers = append(servers, mcpServerResponse(row))
	}
	return protocolmcp.MCPServerListResponse{Servers: servers}, nil
}

func (s MCPServerConfigService) Update(id string, input MCPServerConfigUpdate) (protocolmcp.MCPServerResponse, error) {
	current, err := s.repos.MCPServers.Get(id)
	if err != nil {
		return protocolmcp.MCPServerResponse{}, err
	}
	next := mcpServerProtocolConfig(current, false)
	if input.Name != nil {
		next.Name = *input.Name
	}
	if input.Command != nil {
		next.Command = *input.Command
	}
	if input.Args != nil {
		next.Args = cloneStrings(*input.Args)
	}
	if input.Env != nil {
		next.Env = cloneStringMap(*input.Env)
	}
	if input.CWD != nil {
		next.CWD = *input.CWD
	}
	if input.Enabled != nil {
		next.Enabled = *input.Enabled
	}
	if input.Timeouts != nil {
		next.Timeouts = *input.Timeouts
	}
	if input.ToolAllowlist != nil {
		next.ToolAllowlist = cloneStrings(*input.ToolAllowlist)
	}
	if input.RiskOverrides != nil {
		next.RiskOverrides = cloneStringMap(*input.RiskOverrides)
	}
	normalized, err := validateAndNormalizeMCPServerConfig(next)
	if err != nil {
		return protocolmcp.MCPServerResponse{}, err
	}
	modelConfig := mcpServerModel(normalized)
	row, err := s.repos.MCPServers.Update(repository.MCPServerConfigUpdate{
		ID:            id,
		Name:          &modelConfig.Name,
		Command:       &modelConfig.Command,
		Args:          &modelConfig.Args,
		Env:           &modelConfig.Env,
		CWD:           &modelConfig.CWD,
		Enabled:       &modelConfig.Enabled,
		Timeouts:      &modelConfig.Timeouts,
		ToolAllowlist: &modelConfig.ToolAllowlist,
		RiskOverrides: &modelConfig.RiskOverrides,
	})
	if err != nil {
		return protocolmcp.MCPServerResponse{}, err
	}
	return mcpServerResponse(row), nil
}

func (s MCPServerConfigService) Delete(id string) error {
	return s.repos.MCPServers.Delete(id)
}

func validateAndNormalizeMCPServerConfig(input protocolmcp.MCPServerConfig) (protocolmcp.MCPServerConfig, error) {
	if !mcpServerNamePattern.MatchString(input.Name) || strings.Contains(input.Name, "__") {
		return protocolmcp.MCPServerConfig{}, invalidMCPConfig("name must be lowercase ASCII and may not contain whitespace, __, path separators, or shell metacharacters")
	}
	if err := validateMCPCommand(input.Command); err != nil {
		return protocolmcp.MCPServerConfig{}, err
	}
	for key := range input.Env {
		if strings.TrimSpace(key) == "" {
			return protocolmcp.MCPServerConfig{}, invalidMCPConfig("env keys must be non-empty")
		}
	}
	for _, toolName := range input.ToolAllowlist {
		if strings.TrimSpace(toolName) == "" {
			return protocolmcp.MCPServerConfig{}, invalidMCPConfig("tool_allowlist entries must be non-empty")
		}
	}
	for toolName, risk := range input.RiskOverrides {
		if strings.TrimSpace(toolName) == "" {
			return protocolmcp.MCPServerConfig{}, invalidMCPConfig("risk_overrides keys must be non-empty")
		}
		if risk != "low" && risk != "medium" && risk != "high" {
			return protocolmcp.MCPServerConfig{}, invalidMCPConfig("risk_overrides values must be low, medium, or high")
		}
	}
	if err := validateMCPTimeouts(input.Timeouts); err != nil {
		return protocolmcp.MCPServerConfig{}, err
	}
	input.Timeouts = input.Timeouts.Normalized()
	input.Args = cloneStrings(input.Args)
	input.Env = cloneStringMap(input.Env)
	input.ToolAllowlist = cloneStrings(input.ToolAllowlist)
	input.RiskOverrides = cloneStringMap(input.RiskOverrides)
	return input, nil
}

func validateMCPCommand(command string) error {
	if command == "" {
		return invalidMCPConfig("command is required")
	}
	if strings.TrimSpace(command) != command {
		return invalidMCPConfig("command must be a single executable string")
	}
	for _, char := range command {
		if unicode.IsSpace(char) || unicode.IsControl(char) || strings.ContainsRune(";&|<>`\"'$(){}[]!*?~^%", char) {
			return invalidMCPConfig("command must be a single executable string without shell syntax")
		}
	}
	return nil
}

func validateMCPTimeouts(timeouts protocolmcp.MCPTimeouts) error {
	values := []struct {
		name  string
		value int
	}{
		{"start_ms", timeouts.StartMS},
		{"initialize_ms", timeouts.InitializeMS},
		{"list_ms", timeouts.ListMS},
		{"call_ms", timeouts.CallMS},
		{"shutdown_ms", timeouts.ShutdownMS},
	}
	for _, item := range values {
		if item.value < 0 {
			return invalidMCPConfig("timeouts.%s must be positive when provided", item.name)
		}
	}
	return nil
}

func invalidMCPConfig(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidMCPServerConfig, fmt.Sprintf(format, args...))
}

func mcpServerModel(config protocolmcp.MCPServerConfig) model.MCPServerConfig {
	return model.MCPServerConfig{
		Name:          config.Name,
		Command:       config.Command,
		Args:          cloneStrings(config.Args),
		Env:           cloneStringMap(config.Env),
		CWD:           config.CWD,
		Enabled:       config.Enabled,
		Timeouts:      modelMCPTimeouts(config.Timeouts),
		ToolAllowlist: cloneStrings(config.ToolAllowlist),
		RiskOverrides: cloneStringMap(config.RiskOverrides),
	}
}

func mcpServerResponse(row model.MCPServerConfig) protocolmcp.MCPServerResponse {
	return protocolmcp.MCPServerResponse{
		ID:              row.ID,
		MCPServerConfig: mcpServerProtocolConfig(row, true),
		CreatedAt:       row.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:       row.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func mcpServerProtocolConfig(row model.MCPServerConfig, redactEnv bool) protocolmcp.MCPServerConfig {
	env := cloneStringMap(row.Env)
	if redactEnv {
		for key, value := range env {
			if value != "" && isSensitiveMCPEnvKey(key) {
				env[key] = redactedMCPEnvValue
			}
		}
	}
	return protocolmcp.MCPServerConfig{
		Name:          row.Name,
		Command:       row.Command,
		Args:          cloneStrings(row.Args),
		Env:           env,
		CWD:           row.CWD,
		Enabled:       row.Enabled,
		Timeouts:      protocolMCPTimeouts(row.Timeouts),
		ToolAllowlist: cloneStrings(row.ToolAllowlist),
		RiskOverrides: cloneStringMap(row.RiskOverrides),
	}
}

func isSensitiveMCPEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, marker := range []string{"TOKEN", "KEY", "SECRET", "PASSWORD", "CREDENTIAL"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}

func modelMCPTimeouts(timeouts protocolmcp.MCPTimeouts) model.MCPTimeouts {
	return model.MCPTimeouts{
		StartMS:      timeouts.StartMS,
		InitializeMS: timeouts.InitializeMS,
		ListMS:       timeouts.ListMS,
		CallMS:       timeouts.CallMS,
		ShutdownMS:   timeouts.ShutdownMS,
	}
}

func protocolMCPTimeouts(timeouts model.MCPTimeouts) protocolmcp.MCPTimeouts {
	return protocolmcp.MCPTimeouts{
		StartMS:      timeouts.StartMS,
		InitializeMS: timeouts.InitializeMS,
		ListMS:       timeouts.ListMS,
		CallMS:       timeouts.CallMS,
		ShutdownMS:   timeouts.ShutdownMS,
	}
}

func cloneStrings(items []string) []string {
	if items == nil {
		return nil
	}
	return append([]string(nil), items...)
}

func cloneStringMap(items map[string]string) map[string]string {
	if items == nil {
		return nil
	}
	copy := make(map[string]string, len(items))
	for key, value := range items {
		copy[key] = value
	}
	return copy
}
