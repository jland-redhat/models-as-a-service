package api_keys

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/opendatahub-io/models-as-a-service/maas-api/internal/logger"
	"github.com/opendatahub-io/models-as-a-service/maas-api/internal/token"
)

// AdminChecker is an interface for checking if a user is an admin.
// This allows for different implementations (e.g., Auth CR-based, hardcoded, mock for testing).
type AdminChecker interface {
	IsAdmin(userGroups []string) bool
}

type Handler struct {
	service      *Service
	logger       *logger.Logger
	adminChecker AdminChecker
}

func NewHandler(log *logger.Logger, service *Service, adminChecker AdminChecker) *Handler {
	if log == nil {
		log = logger.Production()
	}
	return &Handler{
		service:      service,
		logger:       log,
		adminChecker: adminChecker,
	}
}

// getUserContext extracts and validates the user context from the Gin context.
// Returns the user context on success, or responds with an error and returns nil.
func (h *Handler) getUserContext(c *gin.Context) *token.UserContext {
	userCtx, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "User context not found"})
		return nil
	}

	user, ok := userCtx.(*token.UserContext)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user context type"})
		return nil
	}

	return user
}

// isAdmin checks if the user has admin privileges based on Auth CR (services.opendatahub.io/v1alpha1).
// The Auth CR defines adminGroups that are allowed to perform admin operations.
// Returns true if the user belongs to at least one admin group, false otherwise.
func (h *Handler) isAdmin(user *token.UserContext) bool {
	return h.adminChecker.IsAdmin(user.Groups)
}

// isAuthorizedForKey checks if the user is authorized to access the API key.
// User is authorized if they own the key or are an admin.
func (h *Handler) isAuthorizedForKey(user *token.UserContext, keyOwner string) bool {
	// Check if user owns the key
	if user.Username == keyOwner {
		return true
	}

	// Check if user is admin
	return h.isAdmin(user)
}

// parsePaginationParams extracts and validates pagination query parameters.
func (h *Handler) parsePaginationParams(c *gin.Context) (PaginationParams, error) {
	const (
		defaultLimit = 50
		maxLimit     = 100
	)

	params := PaginationParams{
		Limit:  defaultLimit,
		Offset: 0,
	}

	// Parse limit
	if limitStr := c.Query("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			return params, errors.New("invalid limit parameter: must be a number")
		}
		if limit < 1 {
			return params, errors.New("invalid limit parameter: must be at least 1")
		}
		// Silently cap at maximum (user-friendly)
		if limit > maxLimit {
			limit = maxLimit
		}
		params.Limit = limit
	}

	// Parse offset
	if offsetStr := c.Query("offset"); offsetStr != "" {
		offset, err := strconv.Atoi(offsetStr)
		if err != nil {
			return params, errors.New("invalid offset parameter: must be a number")
		}
		if offset < 0 {
			return params, errors.New("invalid offset parameter: must be non-negative")
		}
		params.Offset = offset
	}

	return params, nil
}


func (h *Handler) ListAPIKeys(c *gin.Context) {
	user := h.getUserContext(c)
	if user == nil {
		return
	}

	// Check if user is admin
	isAdmin := h.isAdmin(user)

	// Parse filter parameters
	filterUsername := c.Query("username")
	filterStatus := c.Query("status")

	// Determine target username for filtering
	var targetUsername string
	if isAdmin {
		// Admin behavior: default to ALL users (empty string), or filter if provided
		targetUsername = filterUsername // Empty string = all users
	} else {
		// Regular user behavior: always filter to own keys only
		if filterUsername != "" && filterUsername != user.Username {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "non-admin users can only view their own API keys",
			})
			return
		}
		targetUsername = user.Username // Always their own username
	}

	// Parse status filters
	var statusFilters []string
	if filterStatus != "" {
		statusFilters = strings.Split(filterStatus, ",")
		// Validate each status
		for _, status := range statusFilters {
			trimmed := strings.TrimSpace(status)
			if trimmed != "active" && trimmed != "revoked" && trimmed != "expired" {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "invalid status filter: must be active, revoked, or expired",
				})
				return
			}
		}
	}

	// Parse pagination parameters
	params, err := h.parsePaginationParams(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get paginated results with filters
	result, err := h.service.List(c.Request.Context(), targetUsername, params, statusFilters)
	if err != nil {
		h.logger.Error("Failed to list API keys",
			"error", err,
			"username", targetUsername,
			"limit", params.Limit,
			"offset", params.Offset,
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list api keys"})
		return
	}

	// Build response
	response := ListAPIKeysResponse{
		Object:  "list",
		Data:    result.Keys,
		HasMore: result.HasMore,
	}

	c.JSON(http.StatusOK, response)
}

func (h *Handler) GetAPIKey(c *gin.Context) {
	tokenID := c.Param("id")
	if tokenID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Token ID required"})
		return
	}

	// Extract user context for authorization
	user := h.getUserContext(c)
	if user == nil {
		return
	}

	// Get the API key to check ownership
	tok, err := h.service.GetAPIKey(c.Request.Context(), tokenID)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "API key not found"})
			return
		}
		h.logger.Error("Failed to get API key",
			"error", err,
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve API key"})
		return
	}

	// Check authorization - user must own the key or be admin
	if !h.isAuthorizedForKey(user, tok.Username) {
		h.logger.Warn("Unauthorized API key access attempt",
			"requestingUser", user.Username,
			"keyOwner", tok.Username,
			"keyId", tokenID,
		)
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied: you can only view your own API keys"})
		return
	}

	c.JSON(http.StatusOK, tok)
}


// CreateAPIKeyRequest is the request body for creating an API key.
// Keys can be permanent (no expiresIn) or expiring (with expiresIn).
// Admins can optionally specify a username and groups to create keys for other users.
//
// TODO: Admin user-groups approach needs team consensus
// Current implementation requires admins to explicitly provide groups when creating keys
// for other users. Alternative approaches to consider:
// 1. Remove admin-create-for-user feature entirely (users create own keys)
// 2. Lookup user's live groups via impersonation/TokenReview
// 3. Add organizationId/costCenter fields instead of/alongside groups
// 4. Make groups optional and use default groups for the target user.
type CreateAPIKeyRequest struct {
	Name        string          `binding:"required"           json:"name"`
	Description string          `json:"description,omitempty"`
	ExpiresIn   *token.Duration `json:"expiresIn,omitempty"` // Optional - nil means permanent
	Username    string          `json:"username,omitempty"`  // Optional - admin can create for other users
	Groups      []string        `json:"groups,omitempty"`    // Optional - admin must provide when creating for others (TODO: needs consensus)
}

// CreateAPIKey handles POST /v1/api-keys
// Creates a new API key (sk-oai-* format) per Feature Refinement.
// Keys can be permanent (no expiresIn) or expiring (with expiresIn).
// Per "Keys Shown Only Once": key is returned ONCE at creation and never again.
func (h *Handler) CreateAPIKey(c *gin.Context) {
	var req CreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user := h.getUserContext(c)
	if user == nil {
		return
	}

	// Determine target username (admin can create for other users)
	targetUsername := req.Username
	if targetUsername == "" {
		// No username provided - use authenticated user
		targetUsername = user.Username
	}

	// Determine groups for the API key
	var keyGroups []string

	if targetUsername == user.Username {
		// Creating key for self - always use authenticated user's groups
		keyGroups = user.Groups
	} else {
		// Creating key for another user - admin only
		if !h.isAdmin(user) {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "only admins can create API keys for other users",
			})
			return
		}

		// TODO: Admin groups approach needs team consensus
		// Current: Admin must explicitly provide groups when creating keys for other users.
		// Problem: Admin may not know what groups the target user should have.
		// Alternative approaches:
		//   1. Remove this feature (users create own keys only)
		//   2. Lookup target user's current groups via impersonation/TokenReview
		//   3. Use organizationId/costCenter for billing instead of groups for access
		//   4. Allow empty groups and use sensible defaults (e.g., ["system:authenticated"])
		// Current implementation chosen for explicit control and avoiding privilege escalation.
		if len(req.Groups) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "groups must be provided when creating API keys for other users",
			})
			return
		}

		keyGroups = req.Groups
	}

	// Parse expiration duration if provided
	var expiresIn *time.Duration
	if req.ExpiresIn != nil {
		d := req.ExpiresIn.Duration
		expiresIn = &d
	}

	result, err := h.service.CreateAPIKey(c.Request.Context(), targetUsername, keyGroups, req.Name, req.Description, expiresIn)
	if err != nil {
		h.logger.Error("Failed to create API key", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.logger.Info("Created API key",
		"keyId", result.ID,
		"keyPrefix", result.KeyPrefix,
		"username", targetUsername,
		"groups", keyGroups,
		"createdBy", user.Username,
	)

	// Return the key - THIS IS THE ONLY TIME THE PLAINTEXT IS SHOWN
	c.JSON(http.StatusCreated, result)
}

// ValidateAPIKeyRequest is the request body for validating an API key.
type ValidateAPIKeyRequest struct {
	Key string `binding:"required" json:"key"`
}

// ValidateAPIKeyHandler handles POST /internal/v1/api-keys/validate
// This endpoint is called by Authorino via HTTP external auth callback
// Per Feature Refinement "Gateway Integration (Inference Flow)".
func (h *Handler) ValidateAPIKeyHandler(c *gin.Context) {
	var req ValidateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key is required"})
		return
	}

	result, err := h.service.ValidateAPIKey(c.Request.Context(), req.Key)
	if err != nil {
		h.logger.Error("API key validation failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "validation failed"})
		return
	}

	if !result.Valid {
		// Return 200 with validation result for Authorino
		// Per design doc section 7.7: invalid keys should return 200 with valid:false
		c.JSON(http.StatusOK, result)
		return
	}

	// Valid key - return user identity for Authorino to use
	c.JSON(http.StatusOK, result)
}

// RevokeAPIKey handles DELETE /v1/api-keys/:id
// Revokes a specific permanent API key (soft delete).
func (h *Handler) RevokeAPIKey(c *gin.Context) {
	keyID := c.Param("id")
	if keyID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "API key ID required"})
		return
	}

	// Extract user context for authorization
	user := h.getUserContext(c)
	if user == nil {
		return
	}

	// Get the API key to check ownership before revoking
	keyMetadata, err := h.service.GetAPIKey(c.Request.Context(), keyID)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "API key not found"})
			return
		}
		h.logger.Error("Failed to get API key for authorization check", "error", err, "keyId", keyID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve API key"})
		return
	}

	// Check authorization - user must own the key or be admin
	if !h.isAuthorizedForKey(user, keyMetadata.Username) {
		h.logger.Warn("Unauthorized API key revocation attempt",
			"requestingUser", user.Username,
			"keyOwner", keyMetadata.Username,
			"keyId", keyID,
		)
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied: you can only revoke your own API keys"})
		return
	}

	// Perform the revocation
	if err := h.service.RevokeAPIKey(c.Request.Context(), keyID); err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "API key not found"})
			return
		}
		h.logger.Error("Failed to revoke API key", "error", err, "keyId", keyID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to revoke API key"})
		return
	}

	h.logger.Info("Revoked API key", "keyId", keyID, "revokedBy", user.Username)
	c.Status(http.StatusNoContent)
}
