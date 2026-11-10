package config

import "path/filepath"

// Runtime settings use seconds and bytes. Zero values select conservative defaults.
type Runtime struct {
	ToolWaitSeconds     int    `json:"tool_wait_seconds"`
	ToolTimeoutSeconds  int    `json:"tool_timeout_seconds"`
	NoOutputSeconds     int    `json:"no_output_seconds"` // <=0 disables idle termination; quiet tools can be healthy.
	MCPConcurrency      int    `json:"mcp_concurrency"`
	MCPFailureThreshold int    `json:"mcp_failure_threshold"`
	MCPCooldownSeconds  int    `json:"mcp_cooldown_seconds"`
	RecipeDir           string `json:"recipe_dir"`
}

func RuntimeOptions() Runtime {
	r := Load().Runtime
	if r.ToolWaitSeconds <= 0 {
		r.ToolWaitSeconds = 120
	}
	if r.ToolTimeoutSeconds <= 0 {
		r.ToolTimeoutSeconds = 1800
	}
	if r.MCPConcurrency <= 0 {
		r.MCPConcurrency = 5
	}
	if r.MCPFailureThreshold <= 0 {
		r.MCPFailureThreshold = 5
	}
	if r.MCPCooldownSeconds <= 0 {
		r.MCPCooldownSeconds = 60
	}
	if r.RecipeDir == "" {
		r.RecipeDir = filepath.Join(BaseDir(), "tools")
	}
	return r
}
