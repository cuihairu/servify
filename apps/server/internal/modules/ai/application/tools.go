package application

import (
	"context"
	"fmt"

	"servify/apps/server/internal/platform/llm"
)

// Tool describes an executable capability exposed to AI orchestration.
type Tool interface {
	Name() string
	Description() string
	Schema() map[string]interface{}
	Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error)
}

// PermissionChecker validates whether a tool can be executed for the request.
type PermissionChecker func(req AIRequest, tool Tool) error

// ToolRegistry stores available tools by name.
type ToolRegistry struct {
	tools map[string]Tool
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

func (r *ToolRegistry) Register(tool Tool) {
	if tool == nil || tool.Name() == "" {
		return
	}
	r.tools[tool.Name()] = tool
}

func (r *ToolRegistry) Get(name string) (Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

func (r *ToolRegistry) List() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		out = append(out, tool)
	}
	return out
}

// ToolExecutor executes tools using registry and policy checks.
type ToolExecutor struct {
	registry          *ToolRegistry
	permissionChecker PermissionChecker
}

func NewToolExecutor(registry *ToolRegistry, permissionChecker PermissionChecker) *ToolExecutor {
	if registry == nil {
		registry = NewToolRegistry()
	}
	return &ToolExecutor{
		registry:          registry,
		permissionChecker: permissionChecker,
	}
}

func (e *ToolExecutor) Execute(ctx context.Context, req AIRequest, toolName string, input map[string]interface{}) (map[string]interface{}, error) {
	if !req.ToolPolicy.Enabled {
		return nil, fmt.Errorf("tool execution is disabled")
	}

	allowed := len(req.ToolPolicy.AllowedTools) == 0
	for _, name := range req.ToolPolicy.AllowedTools {
		if name == toolName {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, fmt.Errorf("tool %s is not allowed", toolName)
	}

	tool, ok := e.registry.Get(toolName)
	if !ok {
		return nil, fmt.Errorf("tool %s not found", toolName)
	}
	if e.permissionChecker != nil {
		if err := e.permissionChecker(req, tool); err != nil {
			return nil, err
		}
	}
	return tool.Execute(ctx, input)
}

// Definitions returns all registered tools as LLM tool definitions.
func (e *ToolExecutor) Definitions() []llm.ToolDefinition {
	tools := e.registry.List()
	out := make([]llm.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		out = append(out, llm.ToolDefinition{
			Name:        tool.Name(),
			Description: tool.Description(),
			InputSchema: tool.Schema(),
		})
	}
	return out
}
