package api_keys

import "github.com/opendatahub-io/models-as-a-service/maas-api/internal/token"

// APIKey represents a full API key with token and metadata.
// It embeds token.Token and adds API key-specific fields.
// Used for legacy K8s ServiceAccount tokens.
type APIKey struct {
	token.Token

	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// PermanentAPIKey represents a user-controlled API key (sk-oai-* format).
// Per Feature Refinement "Hash-Only Storage": stores hash-only (plaintext never stored).
type PermanentAPIKey struct {
	ID          string `json:"id"`
	KeyPrefix   string `json:"keyPrefix"` // Display prefix: sk-oai-abc123...
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Username    string `json:"username"`
	CreatedAt   string `json:"createdAt"`
	Status      string `json:"status"` // "active", "revoked"
}

// ApiKeyMetadata represents metadata for a single API key (without the token itself).
// Used for listing and retrieving API key metadata from the database.
// Note: KeyPrefix is NOT included - it's only shown once at creation (show-once pattern).
// Users should identify keys by name/description for security.
type ApiKeyMetadata struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Description        string   `json:"description,omitempty"`
	Username           string   `json:"username,omitempty"`
	OriginalUserGroups []string `json:"originalUserGroups,omitempty"` // User's groups at creation (audit only)
	CreationDate       string   `json:"creationDate"`
	ExpirationDate     string   `json:"expirationDate,omitempty"` // Empty for permanent keys
	Status             string   `json:"status"`                   // "active", "expired", "revoked"
	LastUsedAt         string   `json:"lastUsedAt,omitempty"`     // Tracks when key was last used for validation
}

// ValidationResult holds the result of API key validation (for Authorino HTTP callback).
type ValidationResult struct {
	Valid    bool     `json:"valid"`
	UserID   string   `json:"userId,omitempty"`
	Username string   `json:"username,omitempty"`
	KeyID    string   `json:"keyId,omitempty"`
	Groups   []string `json:"groups,omitempty"` // User groups for subscription-based authorization
	Reason   string   `json:"reason,omitempty"` // If invalid: "key not found", "revoked", etc.
}

// PaginationParams holds pagination parameters.
type PaginationParams struct {
	Limit  int `json:"limit"`  // Default 50, max 100
	Offset int `json:"offset"` // Default 0
}

// PaginatedResult holds the result of a paginated query.
type PaginatedResult struct {
	Keys    []ApiKeyMetadata
	HasMore bool
}

// ListAPIKeysResponse is the HTTP response for GET /v1/api-keys.
type ListAPIKeysResponse struct {
	Object  string           `json:"object"` // Always "list"
	Data    []ApiKeyMetadata `json:"data"`
	HasMore bool             `json:"has_more"`
}

// ============================================================
// SEARCH REQUEST/RESPONSE TYPES
// ============================================================

// SearchAPIKeysRequest for POST /v1/api-keys/search.
type SearchAPIKeysRequest struct {
	Filters    *SearchFilters    `json:"filters,omitempty"`
	Sort       *SortParams       `json:"sort,omitempty"`
	Pagination *PaginationParams `json:"pagination,omitempty"`
}

// SearchFilters holds all filter criteria for API key search.
type SearchFilters struct {
	// Phase 1: Core filters
	Username string   `json:"username,omitempty"` // Admin-only filter
	Status   []string `json:"status,omitempty"`   // active, revoked, expired

	// Phase 2: Date range filters (future)
	CreatedAfter  *string `json:"createdAfter,omitempty"`  // RFC3339
	CreatedBefore *string `json:"createdBefore,omitempty"` // RFC3339
	ExpiresAfter  *string `json:"expiresAfter,omitempty"`  // RFC3339
	ExpiresBefore *string `json:"expiresBefore,omitempty"` // RFC3339
	LastUsedAfter *string `json:"lastUsedAfter,omitempty"` // RFC3339

	// Phase 3: Text search (future)
	NameContains        *string `json:"nameContains,omitempty"`
	DescriptionContains *string `json:"descriptionContains,omitempty"`

	// Phase 4: Boolean filters (future)
	HasExpiration *bool `json:"hasExpiration,omitempty"` // true = expiring, false = permanent
	HasBeenUsed   *bool `json:"hasBeenUsed,omitempty"`   // true = used, false = never used
}

// SortParams specifies sorting criteria.
type SortParams struct {
	By    string `json:"by"`    // created_at, expires_at, last_used_at, name
	Order string `json:"order"` // asc, desc
}

// Default values.
const (
	DefaultSortBy    = "created_at"
	DefaultSortOrder = "desc"
	SortOrderAsc     = "asc"
	SortOrderDesc    = "desc"
	DefaultLimit     = 50
	MaxLimit         = 100
)

// ValidSortFields prevents SQL injection via allowlist.
var ValidSortFields = map[string]bool{
	"created_at":   true,
	"expires_at":   true,
	"last_used_at": true,
	"name":         true,
}

// ValidSortOrders allowlist for sort direction.
var ValidSortOrders = map[string]bool{
	"asc":  true,
	"desc": true,
}

// ValidStatuses allowlist for status filtering.
var ValidStatuses = map[string]bool{
	"active":  true,
	"revoked": true,
	"expired": true,
}

// SearchAPIKeysResponse is the HTTP response for POST /v1/api-keys/search.
type SearchAPIKeysResponse struct {
	Object  string           `json:"object"` // Always "list"
	Data    []ApiKeyMetadata `json:"data"`
	HasMore bool             `json:"has_more"`
}

// ============================================================
// BULK REVOKE TYPES
// ============================================================

// BulkRevokeRequest for POST /v1/api-keys/bulk-revoke.
type BulkRevokeRequest struct {
	Username string `binding:"required" json:"username"`
}

// BulkRevokeResponse returns count of revoked keys.
type BulkRevokeResponse struct {
	RevokedCount int    `json:"revokedCount"`
	Message      string `json:"message"`
}
