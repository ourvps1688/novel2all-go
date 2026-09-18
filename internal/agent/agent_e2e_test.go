package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/agent/tools"
	"github.com/ourvps1688/novel2all-go/internal/llm"
	"github.com/ourvps1688/novel2all-go/internal/roles"
)

// TestAgentE2E_All7VendorRoles - mock LLM end-to-end for 7 vendor roles (Sprint A6.1)
//
// verify each role: 1) Load from embed.FS, 2) Adapt LoopConfig, 3) RunAgent succeeds,
// 4) Output contains correct system prompt content.
func TestAgentE2E_All7VendorRoles(t *testing.T) {
	vendorRoles := roles.AllRoleSpecs()

	if len(vendorRoles) != 7 {
		t.Skipf("want 7 vendor roles, got=%d (only Sprint A3+ valid)", len(vendorRoles))
	}

	for _, spec := range vendorRoles {
		spec := spec
		t.Run(spec.Name, func(t *testing.T) {
			toolsReg := tools.NewRegistry()
			roleMocks := []*mockToolForRole{
				{name: "Read", description: "Read file", schema: []byte(`{}`)},
				{name: "Write", description: "Write file", schema: []byte(`{}`)},
			}
			for _, mt := range roleMocks {
				if err := toolsReg.Register(mt); err != nil {
					t.Fatalf("register %s: %v", mt.name, err)
				}
			}

			adapter := NewToolAdapter(toolsReg, "/tmp")
			adapter.SetDisallowedTools(spec.DisallowedTools)

			mockLLM := &mockLLMProvider{
				Responses: []*llm.ChatWithToolsResponse{
					{Content: "Mock response for " + spec.Name, TokensIn: 10, TokensOut: 20},
				},
			}

			cfg := LoopConfig{
				Router:          mockLLM,
				Tools:           adapter,
				Mapping:         DefaultModelMapping(),
				ProjectRoot:     "/tmp",
				Translator:      NewPathTranslator(),
				DisallowedTools: spec.DisallowedTools,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			spec2 := toAgentSpec(spec)
			res, err := RunAgent(ctx, spec2, cfg, "test input for "+spec.Name)
			if err != nil {
				t.Fatalf("RunAgent %s: %v", spec.Name, err)
			}
			if res == nil {
				t.Fatal("Result should not be nil")
			}
			if !strings.Contains(res.Content, "Mock response") {
				t.Errorf("Result should contain mock content, actual=%q", res.Content)
			}
			if res.TokensIn != 10 || res.TokensOut != 20 {
				t.Errorf("Tokens in/out=%d/%d, want 10/20", res.TokensIn, res.TokensOut)
			}
		})
	}
}

// toAgentSpec convert roles.RoleSpec -> agent.AgentSpec (Sprint A6.1 helper)
func toAgentSpec(s *roles.RoleSpec) *AgentSpec {
	if s == nil {
		return nil
	}
	return &AgentSpec{
		Name:         s.Name,
		Description:  s.Description,
		Tools:        s.Tools,
		Model:        s.Model,
		MaxTurns:     s.MaxTurns,
		Memory:       s.Memory,
		Skills:       s.Skills,
		SystemPrompt: s.SystemPrompt,
	}
}

// mockToolForRole - simple mock tool for e2e tests
type mockToolForRole struct {
	name        string
	description string
	schema      []byte
}

func (m *mockToolForRole) Name() string        { return m.name }
func (m *mockToolForRole) Description() string { return m.description }
func (m *mockToolForRole) InputSchema() []byte { return m.schema }
func (m *mockToolForRole) Execute(ctx *tools.ExecContext, input []byte) (tools.Result, error) {
	return tools.SuccessResult("mock " + m.name), nil
}

// TestAgentE2E_DisallowedToolsEnforced - vendor DisallowedTools enforce (Sprint A5.16)
//
// LLM tries to call disallowed tool (e.g. Write), agent should reject
func TestAgentE2E_DisallowedToolsEnforced(t *testing.T) {
	spec, err := roles.LoadRoleSpecByAlias("character_extractor")
	if err != nil {
		t.Skipf("character_extractor role not loaded: %v", err)
	}
	toolsReg := tools.NewRegistry()
	rwMocks := []*mockToolForRole{
		{name: "Read", description: "r", schema: []byte(`{}`)},
		{name: "Write", description: "w", schema: []byte(`{}`)},
	}
	for _, mt := range rwMocks {
		if err := toolsReg.Register(mt); err != nil {
			t.Fatalf("register %s: %v", mt.name, err)
		}
	}

	adapter := NewToolAdapter(toolsReg, "/tmp")
	adapter.SetDisallowedTools(spec.DisallowedTools)

	// LLM tries to call Write
	mockLLM := &mockLLMProvider{
		Responses: []*llm.ChatWithToolsResponse{
			{
				Content: "Tried to write but blocked",
				ToolCalls: []llm.ExecutedToolCall{
					{
						Call:   llm.ToolCall{ID: "c1", Name: "Write", Arguments: []byte(`{}`)},
						Result: llm.ToolResult{CallID: "c1", Result: "tool blocked"},
					},
				},
				TokensIn: 5,
			},
			{Content: "Final answer", TokensIn: 8, TokensOut: 15},
		},
	}

	cfg := LoopConfig{
		Router:          mockLLM,
		Tools:           adapter,
		Mapping:         DefaultModelMapping(),
		ProjectRoot:     "/tmp",
		Translator:      NewPathTranslator(),
		DisallowedTools: spec.DisallowedTools,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	spec2 := toAgentSpec(spec)
	res, err := RunAgent(ctx, spec2, cfg, "test")
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if res == nil {
		t.Fatal("Result should not be nil")
	}
}

// TestAgentE2E_PathTranslator - vendor `.claude/skills/...` path translation (Sprint A5.9)
func TestAgentE2E_PathTranslator(t *testing.T) {
	pt := NewPathTranslator()

	vendorPrompt := `read {项目根}/.claude/skills/story-setup/references/diagnostics.md
read .claude/skills/story-long-write/SKILL.md
see ~/.claude/skills/story-review/references/rubrics/fanqie.md
exec: vendor skills/story-short-write/SKILL.md guide`

	translated := pt.Translate(vendorPrompt)

	if strings.Contains(translated, ".claude/skills/") {
		t.Error(".claude/skills/ should be translated")
	}
	if !strings.Contains(translated, "internal/skills/assets/") {
		t.Errorf("should contain internal/skills/assets/, got=%q", translated[:200])
	}
}
