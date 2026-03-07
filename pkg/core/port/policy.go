package port

import "context"

// PolicyInput is the evaluation request passed to PolicyEvaluator.Evaluate.
// It encodes the full authorisation context in a single, self-contained value
// so that policy engines (OPA, Cedar, Casbin, etc.) receive a uniform input
// regardless of how the caller assembled it.
type PolicyInput struct {
	// Action is the operation being authorised (e.g. "create", "delete",
	// "bulk_update"). Conventionally matches the Service method name in lower
	// snake_case.
	Action string `json:"action"`

	// SchemaName identifies the resource type being operated on.
	SchemaName string `json:"schema_name"`

	// Resource is the document or partial data being acted upon. For pre-create
	// events it carries the proposed input; for other operations it carries the
	// existing document data.
	Resource map[string]any `json:"resource"`

	// Subject describes the authenticated principal (user ID, roles, tenant,
	// etc.). The exact shape is application-specific.
	Subject map[string]any `json:"subject"`

	// Context carries ambient request-scoped data (IP address, request ID,
	// environment, feature flags, etc.) that may influence policy decisions.
	Context map[string]any `json:"context"`
}

// PolicyEvaluator is the secondary port for access-control policy evaluation.
// Implementations wrap a policy engine (OPA, Cedar, Casbin, in-process rule
// sets, etc.) and expose a uniform evaluation surface.
type PolicyEvaluator interface {
	// Evaluate returns whether the action described by input is allowed.
	// When allowed is false, reason contains a human-readable explanation
	// suitable for inclusion in a Forbidden error message.
	Evaluate(ctx context.Context, input *PolicyInput) (allowed bool, reason string, err error)

	// LoadPolicy registers or replaces a named policy document. The format of
	// policy is implementation-defined (Rego source, Cedar policy text, etc.).
	// name is an opaque identifier used to update or remove the policy later.
	LoadPolicy(ctx context.Context, name string, policy []byte) error
}

// PolicyDecision carries the result of a policy evaluation. It is stored in
// the request context by the policy middleware so downstream handlers can
// inspect the decision.
type PolicyDecision struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}
