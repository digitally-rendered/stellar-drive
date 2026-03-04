// Package policy provides access-control policy evaluation. The OPAEvaluator
// in this file is a self-contained, rule-based evaluator that mirrors the
// interface of Open Policy Agent without requiring the OPA runtime as a
// dependency.
//
// Real OPA integration note: to use the embedded OPA Go library replace the
// rule-matching logic in Evaluate with a call to rego.New(...).PrepareForEval
// and supply the compiled Rego modules loaded via LoadPolicy. Add
// github.com/open-policy-agent/opa to go.mod before doing so.
package policy

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
)

// PolicyRule is a programmatic rule used by OPAEvaluator when the full OPA
// runtime is not available. Each rule specifies which actions and schemas it
// covers, and an optional Condition that receives the full PolicyInput for
// fine-grained checks.
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

// OPAEvaluator implements port.PolicyEvaluator using a set of in-memory
// PolicyRules. It is safe for concurrent use.
//
// LoadPolicy accepts opaque policy bytes whose format is intentionally
// unspecified in this stub; a real OPA integration would parse and compile
// Rego source here.
type OPAEvaluator struct {
	mu       sync.RWMutex
	rules    []PolicyRule
	policies map[string][]byte // stored but not yet interpreted in stub
}

// NewOPAEvaluator returns an empty OPAEvaluator with no rules loaded.
func NewOPAEvaluator() *OPAEvaluator {
	return &OPAEvaluator{
		policies: make(map[string][]byte),
	}
}

// AddRule appends a programmatic rule to the evaluator. Rules are evaluated in
// the order they were added; the first matching rule wins.
func (e *OPAEvaluator) AddRule(rule PolicyRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = append(e.rules, rule)
}

// Evaluate checks whether input.Action on input.SchemaName is allowed by any
// loaded rule. It returns (true, "", nil) on success. When no rule permits the
// action, it returns (false, reason, nil). A non-nil error signals an
// evaluation fault (e.g. policy compilation failure in the real OPA path).
//
// Evaluation order:
//  1. Rules are tested in the order added (first match wins).
//  2. If no rule matches the action+schema, the default decision is deny.
func (e *OPAEvaluator) Evaluate(_ context.Context, input *port.PolicyInput) (bool, string, error) {
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

// LoadPolicy stores the named policy document. In this stub the bytes are
// retained for future use but not interpreted. A real OPA implementation would
// compile the Rego source and add the resulting module to an internal rego.Rego
// instance.
func (e *OPAEvaluator) LoadPolicy(_ context.Context, name string, policy []byte) error {
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
