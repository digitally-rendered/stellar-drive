// Package policy provides access-control policy evaluation adapters.
//
// InlineEvaluator is a self-contained, rule-based evaluator that uses
// programmatic Go functions for policy decisions. It is the Go equivalent
// of Python slip-stream's InlinePolicy.
//
// RemoteOPAEvaluator (in remote.go) calls a remote OPA server via its REST
// API for Rego-based policy evaluation.
package policy

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
)

// PolicyRule is a programmatic rule used by InlineEvaluator. Each rule
// specifies which actions and schemas it covers, and an optional Condition
// that receives the full PolicyInput for fine-grained checks.
type PolicyRule struct {
	// Name is a unique, human-readable identifier for this rule.
	Name string

	// Actions lists the action strings this rule applies to (e.g. "create",
	// "delete"). An empty slice means the rule applies to all actions.
	Actions []string

	// SchemaName scopes the rule to a specific schema. An empty string means
	// the rule applies to all schemas.
	SchemaName string

	// Condition is an optional predicate evaluated after action/schema
	// matching. If nil, the rule always permits. Returning false denies.
	Condition func(input *port.PolicyInput) bool
}

// InlineEvaluator implements port.PolicyEvaluator using a set of in-memory
// PolicyRules. It is the Go equivalent of Python's InlinePolicy — rules are
// registered as Go functions rather than Rego policies.
//
// All methods are safe for concurrent use.
type InlineEvaluator struct {
	mu       sync.RWMutex
	rules    []PolicyRule
	policies map[string][]byte
}

// NewInlineEvaluator returns an empty InlineEvaluator with no rules loaded.
func NewInlineEvaluator() *InlineEvaluator {
	return &InlineEvaluator{
		policies: make(map[string][]byte),
	}
}

// OPAEvaluator is an alias for InlineEvaluator, retained for backward
// compatibility.
//
// Deprecated: Use InlineEvaluator instead.
type OPAEvaluator = InlineEvaluator

// NewOPAEvaluator is an alias for NewInlineEvaluator, retained for backward
// compatibility.
//
// Deprecated: Use NewInlineEvaluator instead.
func NewOPAEvaluator() *InlineEvaluator {
	return NewInlineEvaluator()
}

// AddRule appends a programmatic rule to the evaluator. Rules are evaluated in
// the order they were added; the first matching rule wins.
func (e *InlineEvaluator) AddRule(rule PolicyRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = append(e.rules, rule)
}

// RegisterRule is a convenience method that constructs and appends a PolicyRule.
// It mirrors Python's InlinePolicy.register_rule.
func (e *InlineEvaluator) RegisterRule(name string, actions []string, schemaName string, fn func(*port.PolicyInput) bool) {
	e.AddRule(PolicyRule{
		Name:       name,
		Actions:    actions,
		SchemaName: schemaName,
		Condition:  fn,
	})
}

// Evaluate checks whether input.Action on input.SchemaName is allowed by any
// loaded rule. It returns (true, "", nil) on success. When no rule permits the
// action, it returns (false, reason, nil). A non-nil error signals an
// evaluation fault.
//
// Evaluation order:
//  1. Rules are tested in the order added (first match wins).
//  2. If no rule matches the action+schema, the default decision is deny.
func (e *InlineEvaluator) Evaluate(_ context.Context, input *port.PolicyInput) (bool, string, error) {
	e.mu.RLock()
	rules := make([]PolicyRule, len(e.rules))
	copy(rules, e.rules)
	e.mu.RUnlock()

	for _, rule := range rules {
		if !ruleMatchesAction(rule, input.Action) {
			continue
		}
		if !ruleMatchesSchema(rule, input.SchemaName) {
			continue
		}
		// Action and schema match — evaluate the optional condition.
		if rule.Condition == nil || rule.Condition(input) {
			return true, "", nil
		}
		// Condition failed for this rule; keep trying subsequent rules.
	}

	reason := fmt.Sprintf("policy: no rule permits action %q on schema %q", input.Action, input.SchemaName)
	return false, reason, nil
}

// LoadPolicy stores the named policy document. For the InlineEvaluator the
// bytes are retained but not interpreted — programmatic rules via AddRule or
// RegisterRule are the primary mechanism for inline policy evaluation.
func (e *InlineEvaluator) LoadPolicy(_ context.Context, name string, policy []byte) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("policy: LoadPolicy: name must not be empty")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	// Store a copy so the caller cannot mutate our internal state.
	stored := make([]byte, len(policy))
	copy(stored, policy)
	e.policies[name] = stored
	return nil
}

// ruleMatchesAction reports whether rule covers action. An empty Actions slice
// means the rule applies to all actions.
func ruleMatchesAction(rule PolicyRule, action string) bool {
	if len(rule.Actions) == 0 {
		return true
	}
	for _, a := range rule.Actions {
		if strings.EqualFold(a, action) {
			return true
		}
	}
	return false
}

// ruleMatchesSchema reports whether rule covers schemaName. An empty
// SchemaName means the rule applies to all schemas.
func ruleMatchesSchema(rule PolicyRule, schemaName string) bool {
	return rule.SchemaName == "" || rule.SchemaName == schemaName
}
