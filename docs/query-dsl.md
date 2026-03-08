# QueryDSL Reference

Stellar-drive uses a Hasura-style JSON query language for filtering, sorting, and pagination. This guide covers the complete QueryDSL syntax and includes practical examples.

## Overview

The QueryDSL allows you to express complex filter conditions using JSON syntax. Filters are passed as the `where` query parameter (URL-encoded JSON), while sorting and pagination use dedicated parameters.

### Key Characteristics

- **JSON-based**: Filter logic expressed as JSON objects
- **Composable**: Combine operators into complex trees
- **Type-safe**: Validators ensure filter depth and operator validity
- **Efficient**: Translates directly to MongoDB aggregation pipelines and SQL WHERE clauses
- **Max depth**: 5 levels of nesting to prevent abuse

## Comparison Operators

Each comparison operator operates on a single field and value:

| Operator | Description | JSON Example | SQL Equivalent | Result Type |
|----------|-------------|-------------|----------------|-------------|
| `_eq` | Equals | `{"status": {"_eq": "active"}}` | `status = 'active'` | Match if exact equal |
| `_neq` | Not equals | `{"status": {"_neq": "deleted"}}` | `status <> 'deleted'` | Match if not equal |
| `_gt` | Greater than | `{"age": {"_gt": 18}}` | `age > 18` | Match if greater |
| `_gte` | Greater than or equal | `{"age": {"_gte": 21}}` | `age >= 21` | Match if greater or equal |
| `_lt` | Less than | `{"score": {"_lt": 100}}` | `score < 100` | Match if less |
| `_lte` | Less than or equal | `{"score": {"_lte": 50}}` | `score <= 50` | Match if less or equal |
| `_in` | In list | `{"status": {"_in": ["active","pending"]}}` | `status IN ('active','pending')` | Match if value in array |
| `_nin` | Not in list | `{"status": {"_nin": ["deleted"]}}` | `status NOT IN ('deleted')` | Match if value not in array |
| `_like` | Pattern match | `{"name": {"_like": "Ali%"}}` | `name LIKE 'Ali%'` | String pattern (SQL wildcards) |
| `_ilike` | Case-insensitive LIKE | `{"name": {"_ilike": "ali%"}}` | `name ILIKE 'ali%'` | String pattern (case-insensitive) |
| `_contains` | Contains substring | `{"bio": {"_contains": "engineer"}}` | `bio LIKE '%engineer%'` | Match if contains substring |
| `_starts_with` | Starts with prefix | `{"name": {"_starts_with": "Al"}}` | `name LIKE 'Al%'` | Match if starts with string |
| `_ends_with` | Ends with suffix | `{"name": {"_ends_with": "son"}}` | `name LIKE '%son'` | Match if ends with string |
| `_exists` | Field exists | `{"phone": {"_exists": true}}` | `phone IS NOT NULL` | Match if field is not null |
| `_is_null` | Null check | `{"deleted_at": {"_is_null": true}}` | `deleted_at IS NULL` | Match if field is null |
| `_regex` | Regex pattern | `{"email": {"_regex": "^[^@]+@.*\\.com$"}}` | `email ~ '^[^@]+@.*\\.com$'` | Regex match (MongoDB/PostgreSQL) |
| `_array_contains` | Array contains element | `{"tags": {"_array_contains": "featured"}}` | JSON array search | Element in array field |
| `_array_length` | Array length | `{"items": {"_array_length": {"_gt": 5}}}` | Array length comparison | Array size comparison |

### Comparison Operator Examples

Simple equality:
```json
{
  "status": {"_eq": "active"}
}
```

Multiple comparisons on same field:
```json
{
  "age": {
    "_gte": 18,
    "_lt": 65
  }
}
```

List membership:
```json
{
  "priority": {"_in": [1, 2, 3]},
  "category": {"_nin": ["archived", "deleted"]}
}
```

String patterns:
```json
{
  "email": {"_like": "%@example.com"},
  "name": {"_ilike": "alice%"}
}
```

Null checks:
```json
{
  "deleted_at": {"_is_null": true},
  "phone": {"_exists": false}
}
```

## Logical Operators

Combine multiple filter conditions using logical operators:

### AND Operator (`_and`)

All conditions must be true:
```json
{
  "_and": [
    {"status": {"_eq": "active"}},
    {"age": {"_gt": 18}},
    {"verified": {"_eq": true}}
  ]
}
```

### OR Operator (`_or`)

At least one condition must be true:
```json
{
  "_or": [
    {"status": {"_eq": "pending"}},
    {"status": {"_eq": "active"}},
    {"status": {"_eq": "review"}}
  ]
}
```

### NOT Operator (`_not`)

Inverts a single condition:
```json
{
  "_not": {
    "deleted_at": {"_is_null": false}
  }
}
```

### Complex Nested Logic

Combine operators for advanced queries:
```json
{
  "_and": [
    {
      "_or": [
        {"status": {"_eq": "active"}},
        {"status": {"_eq": "pending"}}
      ]
    },
    {
      "_not": {
        "deleted_at": {"_is_null": false}
      }
    },
    {"age": {"_gte": 18}}
  ]
}
```

This translates to:
```sql
(
  (status = 'active' OR status = 'pending')
  AND deleted_at IS NULL
  AND age >= 18
)
```

## Implicit AND

Multiple top-level fields create an implicit AND conjunction. These two queries are equivalent:

Query 1 - Implicit AND:
```json
{
  "name": {"_eq": "Alice"},
  "age": {"_gt": 18},
  "verified": {"_eq": true}
}
```

Query 2 - Explicit AND:
```json
{
  "_and": [
    {"name": {"_eq": "Alice"}},
    {"age": {"_gt": 18}},
    {"verified": {"_eq": true}}
  ]
}
```

Both result in:
```sql
name = 'Alice' AND age > 18 AND verified = true
```

## Shorthand Syntax

When a field value is a scalar (string, number, boolean) instead of an operator object, it is treated as an `_eq` operation.

These are equivalent:

Shorthand:
```json
{
  "status": "active",
  "priority": 1
}
```

Explicit:
```json
{
  "status": {"_eq": "active"},
  "priority": {"_eq": 1}
}
```

## Sort Syntax

Sorting is controlled via the `sort` query parameter. Use comma-separated field names with optional prefix:

| Prefix | Direction | Example |
|--------|-----------|---------|
| `-` | Descending | `sort=-created_at` |
| `+` | Ascending | `sort=+name` |
| (none) | Ascending (default) | `sort=name` |

### Single Field Sort

```bash
# Ascending
sort=name

# Descending
sort=-created_at
```

### Multiple Field Sort

Fields are sorted left-to-right by priority:

```bash
# Sort by status ascending, then by created_at descending
sort=status,-created_at

# Sort by priority descending, then by name ascending
sort=-priority,+name
```

Complete curl example:
```bash
curl -G http://localhost:8080/api/v1/pet \
  --data-urlencode 'sort=-created_at,+name'
```

## Pagination

Stellar-drive provides two pagination modes:

### Offset-Based Pagination

Use `limit` and `offset` query parameters:

| Parameter | Type | Default | Max | Description |
|-----------|------|---------|-----|-------------|
| `limit` | integer | 20 | 100 | Number of records per page |
| `offset` | integer | 0 | - | Number of records to skip |

Response includes:
- `data`: Array of documents
- `total`: Total count of matching records
- `limit`: Applied limit
- `offset`: Applied offset
- `has_more`: Boolean indicating if more records exist

Example request:
```bash
curl -G http://localhost:8080/api/v1/pet \
  --data-urlencode 'limit=10' \
  --data-urlencode 'offset=20'
```

Example response:
```json
{
  "data": [
    {"id": "pet-123", "name": "Buddy"},
    {"id": "pet-124", "name": "Max"}
  ],
  "total": 150,
  "limit": 10,
  "offset": 20,
  "has_more": true
}
```

### Cursor-Based Pagination

Use the `cursor` query parameter for efficient pagination of large datasets:

```bash
# First page (no cursor)
curl -G http://localhost:8080/api/v1/pet \
  --data-urlencode 'limit=10'

# Next page (use cursor from response)
curl -G http://localhost:8080/api/v1/pet \
  --data-urlencode 'limit=10' \
  --data-urlencode 'cursor=eyJpZCI6InBldC0xMjQiLCJ2ZXJzaW9uIjoyLCJkYXRhIjp7ImNyZWF0ZWRfYXQiOiIyMDI0LTA4LTA4VDEwOjMwOjAwWiJ9fQ=='
```

The cursor is a base64-encoded JSON object containing:
```json
{
  "id": "pet-124",
  "version": 2,
  "data": {
    "created_at": "2024-08-08T10:30:00Z"
  }
}
```

Cursor benefits:
- Doesn't require total count (faster queries)
- Stable when records are added/deleted
- Works well with real-time data

## Filter Validation and Limits

### Maximum Nesting Depth

Filter trees are limited to 5 levels of nesting to prevent abuse and complexity. Example:

Level 1:
```json
{
  "_and": [
```

Level 2:
```json
    {
      "_or": [
```

Level 3:
```json
        {
          "_and": [
```

Level 4:
```json
            {
              "status": {"_eq": "active"}
            }
```

This filter is valid (depth = 4). A 6-level nested query would be rejected with a `400 Bad Request` error.

### Operator Validation

Invalid operators are rejected:
```json
{
  "name": {"_foo": "bar"}  // Error: unknown operator _foo
}
```

Invalid operator usage on incompatible types:
```json
{
  "age": {"_like": "%18%"}  // Warning/Error if age is integer, not string
}
```

## Complete Query Examples

### Example 1: Find Active Pets by Name

Filter for active pets whose name starts with "D":

```bash
curl -G http://localhost:8080/api/v1/pet \
  --data-urlencode 'where={"status":{"_eq":"active"},"name":{"_starts_with":"D"}}' \
  --data-urlencode 'sort=name' \
  --data-urlencode 'limit=20'
```

Decoded query:
```json
{
  "status": {"_eq": "active"},
  "name": {"_starts_with": "D"}
}
```

### Example 2: Complex Filters with Pagination

Find pets that are either available or pending adoption, updated in the last 30 days, limit 10:

```bash
curl -G http://localhost:8080/api/v1/pet \
  --data-urlencode 'where={"_and":[{"_or":[{"status":{"_eq":"available"}},{"status":{"_eq":"pending"}}]},{"updated_at":{"_gte":"2024-07-08T00:00:00Z"}}]}' \
  --data-urlencode 'sort=-updated_at' \
  --data-urlencode 'limit=10' \
  --data-urlencode 'offset=0'
```

Decoded:
```json
{
  "_and": [
    {
      "_or": [
        {"status": {"_eq": "available"}},
        {"status": {"_eq": "pending"}}
      ]
    },
    {
      "updated_at": {"_gte": "2024-07-08T00:00:00Z"}
    }
  ]
}
```

### Example 3: Array Operations

Find pets with tags containing "featured":

```bash
curl -G http://localhost:8080/api/v1/pet \
  --data-urlencode 'where={"tags":{"_array_contains":"featured"}}' \
  --data-urlencode 'sort=name'
```

### Example 4: Null and Existence Checks

Find pets without a delete timestamp (active records):

```bash
curl -G http://localhost:8080/api/v1/pet \
  --data-urlencode 'where={"deleted_at":{"_is_null":true}}'
```

### Example 5: Multiple Conditions with Sorting

Find users aged 18-65, not deleted, sorted by last login descending:

```bash
curl -G http://localhost:8080/api/v1/user \
  --data-urlencode 'where={"age":{"_gte":18,"_lte":65},"deleted_at":{"_is_null":true}}' \
  --data-urlencode 'sort=-last_login_at,name' \
  --data-urlencode 'limit=50'
```

## Error Responses

When QueryDSL validation fails, the API returns a 400 Bad Request:

```json
{
  "error": "ValidationFailed",
  "message": "Invalid filter syntax",
  "details": [
    {
      "field": "where",
      "reason": "Maximum nesting depth (5) exceeded"
    }
  ]
}
```

Common validation errors:
- `Maximum nesting depth exceeded` - Too many nested operators
- `Unknown operator: _foo` - Unrecognized operator
- `Invalid filter type` - Filter is not a JSON object
- `Invalid operator usage` - Operator misapplied (e.g., `_like` on integer)

## Performance Considerations

1. **Indexing**: Create database indexes on frequently filtered fields
2. **Sorting**: Indexes on sort fields dramatically improve performance
3. **Projection**: Use specific field selection when possible (via schema)
4. **Pagination**: Use cursor-based for large datasets to avoid full scans
5. **Limit**: Keep `limit` reasonable; default of 20 is recommended for most cases

## QueryDSL Specification Version

Current version: 1.0.0 (compatible with Hasura v2.0 QueryDSL syntax)

See https://hasura.io/docs/latest/queries/query-filters/ for additional reference.
