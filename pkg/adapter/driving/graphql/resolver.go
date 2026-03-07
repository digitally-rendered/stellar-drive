package graphql

import (
	"encoding/json"
	"fmt"

	gql "github.com/graphql-go/graphql"

	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
	"github.com/digitally-rendered/stellar-drive/pkg/core/query"
)

// flattenDocument merges Document audit fields with its Data map so the
// GraphQL resolver can return a single map[string]any that satisfies both the
// generated object type fields and the stored business-logic fields.
func flattenDocument(doc *model.Document) map[string]any {
	out := make(map[string]any, len(doc.Data)+8)

	// Copy user-defined data fields first so audit fields can never be
	// shadowed by user data.
	for k, v := range doc.Data {
		out[k] = v
	}

	// Audit / system fields always win.
	out["id"] = doc.ID
	out["entity_id"] = doc.EntityID
	out["record_version"] = doc.RecordVersion
	out["schema_version"] = doc.SchemaVersion
	out["created_at"] = doc.CreatedAt
	out["updated_at"] = doc.UpdatedAt
	out["etag"] = doc.ETag

	if doc.DeletedAt != nil {
		out["deleted_at"] = *doc.DeletedAt
	} else {
		out["deleted_at"] = nil
	}

	return out
}

// makeGetResolver returns a FieldResolveFn that fetches a single document by
// its entity ID. The resolver expects an "entityId" argument of type ID.
func makeGetResolver(schemaName string, svc port.Service) gql.FieldResolveFn {
	return func(p gql.ResolveParams) (any, error) {
		entityID, ok := p.Args["entityId"].(string)
		if !ok || entityID == "" {
			return nil, fmt.Errorf("entityId is required")
		}

		doc, err := svc.FindByID(p.Context, schemaName, entityID)
		if err != nil {
			return nil, err
		}

		return flattenDocument(doc), nil
	}
}

// makeListResolver returns a FieldResolveFn that lists documents with optional
// filtering, sorting, and pagination. Arguments:
//
//   - where  (String) — JSON object in the stellar-drive query DSL
//   - sort   (String) — comma-separated sort expression, e.g. "+name,-created_at"
//   - limit  (Int)    — maximum records to return
//   - offset (Int)    — zero-based record offset
func makeListResolver(schemaName string, svc port.Service) gql.FieldResolveFn {
	return func(p gql.ResolveParams) (any, error) {
		q := &query.Query{}

		// Parse the where argument into a FilterNode tree.
		if whereArg, ok := p.Args["where"].(string); ok && whereArg != "" {
			filter, err := query.Parse(json.RawMessage(whereArg))
			if err != nil {
				return nil, fmt.Errorf("invalid where argument: %w", err)
			}
			q.Filter = filter
		}

		// Parse the sort argument into []SortField.
		if sortArg, ok := p.Args["sort"].(string); ok && sortArg != "" {
			q.Sort = query.ParseSortString(sortArg)
		}

		// Pagination: limit defaults are handled by the service layer.
		if limit, ok := p.Args["limit"].(int); ok && limit > 0 {
			q.Limit = limit
		}
		if offset, ok := p.Args["offset"].(int); ok && offset >= 0 {
			q.Offset = offset
		}

		result, err := svc.List(p.Context, schemaName, q)
		if err != nil {
			return nil, err
		}

		// Build the list response map.
		items := make([]any, 0, len(result.Items))
		for _, doc := range result.Items {
			items = append(items, flattenDocument(doc))
		}

		// Build edges for the Relay connection type.
		edges := make([]any, 0, len(result.Items))
		var startCursor, endCursor string
		for i, doc := range result.Items {
			cursor := model.EncodeCursor(doc.EntityID, doc.RecordVersion)
			if i == 0 {
				startCursor = cursor
			}
			endCursor = cursor
			edges = append(edges, map[string]any{
				"node":   flattenDocument(doc),
				"cursor": cursor,
			})
		}

		hasPreviousPage := q.Offset > 0
		hasNextPage := result.HasMore

		pageInfo := map[string]any{
			"hasNextPage":     hasNextPage,
			"hasPreviousPage": hasPreviousPage,
			"startCursor":     startCursor,
			"endCursor":       endCursor,
			"total":           int(result.Total),
		}

		return map[string]any{
			"edges":    edges,
			"pageInfo": pageInfo,
			// Also expose flat items list for convenience via the list query.
			"items":   items,
			"total":   int(result.Total),
			"hasMore": result.HasMore,
		}, nil
	}
}

// makeCreateResolver returns a FieldResolveFn that creates a new document.
// The resolver expects an "input" argument that is a JSON string or a map.
func makeCreateResolver(schemaName string, svc port.Service) gql.FieldResolveFn {
	return func(p gql.ResolveParams) (any, error) {
		input, err := extractInputMap(p.Args, "input")
		if err != nil {
			return nil, fmt.Errorf("create %s: %w", schemaName, err)
		}

		doc, err := svc.Create(p.Context, schemaName, input)
		if err != nil {
			return nil, err
		}

		return flattenDocument(doc), nil
	}
}

// makeUpdateResolver returns a FieldResolveFn that partially updates an
// existing document. The resolver expects "entityId" and "input" arguments.
func makeUpdateResolver(schemaName string, svc port.Service) gql.FieldResolveFn {
	return func(p gql.ResolveParams) (any, error) {
		entityID, ok := p.Args["entityId"].(string)
		if !ok || entityID == "" {
			return nil, fmt.Errorf("entityId is required")
		}

		input, err := extractInputMap(p.Args, "input")
		if err != nil {
			return nil, fmt.Errorf("update %s: %w", schemaName, err)
		}

		doc, err := svc.Update(p.Context, schemaName, entityID, input)
		if err != nil {
			return nil, err
		}

		return flattenDocument(doc), nil
	}
}

// makeDeleteResolver returns a FieldResolveFn that soft-deletes a document.
// The resolver expects an "entityId" argument of type ID. On success it
// returns a map with a "deleted" boolean and the entity ID.
func makeDeleteResolver(schemaName string, svc port.Service) gql.FieldResolveFn {
	return func(p gql.ResolveParams) (any, error) {
		entityID, ok := p.Args["entityId"].(string)
		if !ok || entityID == "" {
			return nil, fmt.Errorf("entityId is required")
		}

		if err := svc.Delete(p.Context, schemaName, entityID); err != nil {
			return nil, err
		}

		return map[string]any{
			"deleted":  true,
			"entityId": entityID,
		}, nil
	}
}

// makeBulkCreateResolver returns a FieldResolveFn that creates multiple
// documents in one call. The resolver expects an "inputs" argument that is a
// list of JSON objects (map[string]any). On success it returns a map with
// "items", "succeeded", and "failed" keys.
func makeBulkCreateResolver(schemaName string, svc port.Service) gql.FieldResolveFn {
	return func(p gql.ResolveParams) (any, error) {
		rawList, ok := p.Args["inputs"].([]any)
		if !ok {
			return nil, fmt.Errorf("bulkCreate %s: inputs must be a list", schemaName)
		}

		inputs := make([]map[string]any, 0, len(rawList))
		for i, elem := range rawList {
			m, ok := elem.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("bulkCreate %s: inputs[%d] must be a JSON object", schemaName, i)
			}
			inputs = append(inputs, m)
		}

		docs, err := svc.BulkCreate(p.Context, schemaName, inputs)
		if err != nil {
			return nil, err
		}

		items := make([]any, 0, len(docs))
		for _, doc := range docs {
			items = append(items, flattenDocument(doc))
		}

		return map[string]any{
			"items":     items,
			"succeeded": len(docs),
			"failed":    0,
		}, nil
	}
}

// makeBulkUpdateResolver returns a FieldResolveFn that updates multiple
// documents in one call. The resolver expects an "items" argument that is a
// list of JSON objects, each with an "entity_id" string and a "data" object.
// On success it returns a map with "items", "succeeded", and "failed" keys.
func makeBulkUpdateResolver(schemaName string, svc port.Service) gql.FieldResolveFn {
	return func(p gql.ResolveParams) (any, error) {
		rawList, ok := p.Args["items"].([]any)
		if !ok {
			return nil, fmt.Errorf("bulkUpdate %s: items must be a list", schemaName)
		}

		bulkItems := make([]model.BulkUpdateItem, 0, len(rawList))
		for i, elem := range rawList {
			m, ok := elem.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("bulkUpdate %s: items[%d] must be a JSON object", schemaName, i)
			}

			entityID, ok := m["entity_id"].(string)
			if !ok || entityID == "" {
				return nil, fmt.Errorf("bulkUpdate %s: items[%d].entity_id is required", schemaName, i)
			}

			data, ok := m["data"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("bulkUpdate %s: items[%d].data must be a JSON object", schemaName, i)
			}

			bulkItems = append(bulkItems, model.BulkUpdateItem{
				EntityID: entityID,
				Data:     data,
			})
		}

		docs, err := svc.BulkUpdate(p.Context, schemaName, bulkItems)
		if err != nil {
			return nil, err
		}

		items := make([]any, 0, len(docs))
		for _, doc := range docs {
			items = append(items, flattenDocument(doc))
		}

		return map[string]any{
			"items":     items,
			"succeeded": len(docs),
			"failed":    0,
		}, nil
	}
}

// makeBulkDeleteResolver returns a FieldResolveFn that soft-deletes multiple
// documents in one call. The resolver expects an "ids" argument that is a
// list of entity ID strings. On success it returns a map with "succeeded" and
// "failed" keys.
func makeBulkDeleteResolver(schemaName string, svc port.Service) gql.FieldResolveFn {
	return func(p gql.ResolveParams) (any, error) {
		rawList, ok := p.Args["ids"].([]any)
		if !ok {
			return nil, fmt.Errorf("bulkDelete %s: ids must be a list", schemaName)
		}

		ids := make([]string, 0, len(rawList))
		for i, elem := range rawList {
			id, ok := elem.(string)
			if !ok || id == "" {
				return nil, fmt.Errorf("bulkDelete %s: ids[%d] must be a non-empty string", schemaName, i)
			}
			ids = append(ids, id)
		}

		if err := svc.BulkDelete(p.Context, schemaName, ids); err != nil {
			return nil, err
		}

		return map[string]any{
			"succeeded": len(ids),
			"failed":    0,
		}, nil
	}
}

// extractInputMap coerces the named argument into a map[string]any.
// The argument can arrive as a map[string]any (from variable substitution) or
// as a JSON string.
func extractInputMap(args map[string]any, key string) (map[string]any, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil, fmt.Errorf("%s argument is required", key)
	}

	switch v := raw.(type) {
	case map[string]any:
		return v, nil
	case string:
		var m map[string]any
		if err := json.Unmarshal([]byte(v), &m); err != nil {
			return nil, fmt.Errorf("%s must be a valid JSON object: %w", key, err)
		}
		return m, nil
	default:
		return nil, fmt.Errorf("%s must be a JSON object or string, got %T", key, raw)
	}
}
