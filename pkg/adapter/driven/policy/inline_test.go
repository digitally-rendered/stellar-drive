package policy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
)

func TestInlineEvaluator_BasicAllow(t *testing.T) {
	e := NewInlineEvaluator()
	e.AddRule(PolicyRule{Name: "allow-all"})

	allowed, reason, err := e.Evaluate(context.Background(), &port.PolicyInput{
		Action:     "create",
		SchemaName: "pet",
	})

	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Empty(t, reason)
}

func TestInlineEvaluator_DefaultDeny(t *testing.T) {
	e := NewInlineEvaluator()

	allowed, reason, err := e.Evaluate(context.Background(), &port.PolicyInput{
		Action:     "create",
		SchemaName: "pet",
	})

	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Contains(t, reason, "no rule permits")
}

func TestInlineEvaluator_ConditionalAllow(t *testing.T) {
	e := NewInlineEvaluator()
	e.AddRule(PolicyRule{
		Name: "admin-only",
		Condition: func(input *port.PolicyInput) bool {
			role, _ := input.Subject["role"].(string)
			return role == "admin"
		},
	})

	tests := []struct {
		name    string
		subject map[string]any
		want    bool
	}{
		{"admin allowed", map[string]any{"role": "admin"}, true},
		{"user denied", map[string]any{"role": "user"}, false},
		{"no role denied", map[string]any{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed, _, err := e.Evaluate(context.Background(), &port.PolicyInput{
				Action:  "delete",
				Subject: tt.subject,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, allowed)
		})
	}
}

func TestInlineEvaluator_ActionMatching(t *testing.T) {
	e := NewInlineEvaluator()
	e.AddRule(PolicyRule{
		Name:    "allow-reads",
		Actions: []string{"read", "list"},
	})

	tests := []struct {
		action string
		want   bool
	}{
		{"read", true},
		{"list", true},
		{"READ", true}, // case-insensitive
		{"create", false},
		{"delete", false},
	}

	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			allowed, _, err := e.Evaluate(context.Background(), &port.PolicyInput{Action: tt.action})
			require.NoError(t, err)
			assert.Equal(t, tt.want, allowed)
		})
	}
}

func TestInlineEvaluator_SchemaMatching(t *testing.T) {
	e := NewInlineEvaluator()
	e.AddRule(PolicyRule{
		Name:       "pet-only",
		SchemaName: "pet",
	})

	tests := []struct {
		schema string
		want   bool
	}{
		{"pet", true},
		{"order", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.schema, func(t *testing.T) {
			allowed, _, err := e.Evaluate(context.Background(), &port.PolicyInput{
				Action:     "create",
				SchemaName: tt.schema,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, allowed)
		})
	}
}

func TestInlineEvaluator_FirstMatchWins(t *testing.T) {
	e := NewInlineEvaluator()
	// Specific rule first: deny delete on pet.
	e.AddRule(PolicyRule{
		Name:       "deny-pet-delete",
		Actions:    []string{"delete"},
		SchemaName: "pet",
		Condition:  func(*port.PolicyInput) bool { return false },
	})
	// Broader rule second: allow everything.
	e.AddRule(PolicyRule{Name: "allow-all"})

	// Delete on pet: first rule matches action+schema but condition fails,
	// so falls through to allow-all.
	allowed, _, err := e.Evaluate(context.Background(), &port.PolicyInput{
		Action:     "delete",
		SchemaName: "pet",
	})
	require.NoError(t, err)
	assert.True(t, allowed)

	// Create on pet: first rule doesn't match action, falls through to allow-all.
	allowed, _, err = e.Evaluate(context.Background(), &port.PolicyInput{
		Action:     "create",
		SchemaName: "pet",
	})
	require.NoError(t, err)
	assert.True(t, allowed)
}

func TestInlineEvaluator_RegisterRule(t *testing.T) {
	e := NewInlineEvaluator()
	e.RegisterRule("admin-create", []string{"create"}, "pet", func(input *port.PolicyInput) bool {
		role, _ := input.Subject["role"].(string)
		return role == "admin"
	})

	allowed, _, err := e.Evaluate(context.Background(), &port.PolicyInput{
		Action:     "create",
		SchemaName: "pet",
		Subject:    map[string]any{"role": "admin"},
	})
	require.NoError(t, err)
	assert.True(t, allowed)
}

func TestInlineEvaluator_LoadPolicy(t *testing.T) {
	e := NewInlineEvaluator()

	err := e.LoadPolicy(context.Background(), "authz", []byte("package authz\ndefault allow = true"))
	require.NoError(t, err)

	// Verify stored.
	e.mu.RLock()
	stored := e.policies["authz"]
	e.mu.RUnlock()
	assert.Equal(t, "package authz\ndefault allow = true", string(stored))
}

func TestInlineEvaluator_LoadPolicy_EmptyName(t *testing.T) {
	e := NewInlineEvaluator()
	err := e.LoadPolicy(context.Background(), "", []byte("data"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name must not be empty")
}

func TestInlineEvaluator_BackwardCompat(t *testing.T) {
	// OPAEvaluator and NewOPAEvaluator should work as aliases.
	e := NewOPAEvaluator()
	e.AddRule(PolicyRule{Name: "allow-all"})

	allowed, _, err := e.Evaluate(context.Background(), &port.PolicyInput{Action: "create"})
	require.NoError(t, err)
	assert.True(t, allowed)

	// Type should be assignable.
	var _ *OPAEvaluator = e
}
