package models

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	kservev1alpha1 "github.com/kserve/kserve/pkg/apis/serving/v1alpha1"
	kservelistersv1alpha1 "github.com/kserve/kserve/pkg/client/listers/serving/v1alpha1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/opendatahub-io/models-as-a-service/maas-api/internal/constant"
	"github.com/opendatahub-io/models-as-a-service/maas-api/internal/logger"
)

// AuthHandler handles model authorization requests from Gateway AuthPolicy.
type AuthHandler struct {
	llmIsvcLister kservelistersv1alpha1.LLMInferenceServiceLister
	logger        *logger.Logger
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(llmIsvcLister kservelistersv1alpha1.LLMInferenceServiceLister, log *logger.Logger) *AuthHandler {
	return &AuthHandler{
		llmIsvcLister: llmIsvcLister,
		logger:        log,
	}
}

// AuthorizeRequest represents the authorization request from Gateway AuthPolicy.
type AuthorizeRequest struct {
	Path string `binding:"required" json:"path"`
	Tier string `binding:"required" json:"tier"`
}

// ModelAuthorize handles POST /v1/models/authorize
// This endpoint is called by Gateway AuthPolicy to check if a user's tier matches the model's tier annotation
// Returns:
//   - 200 OK: User's tier matches model's tier requirement (authorized)
//   - 403 Forbidden: User's tier does not match model's tier requirement (denied)
//   - 400 Bad Request: Invalid request
//   - 404 Not Found: Model not found
//   - 500 Internal Server Error: Server error
func (h *AuthHandler) ModelAuthorize(c *gin.Context) {
	h.logger.Debug("ModelAuthorize request received")

	var req AuthorizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Debug("Failed to parse request body", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body: " + err.Error(),
		})
		return
	}

	h.logger.Debug("ModelAuthorize request parsed",
		"path", req.Path,
		"tier", req.Tier,
	)

	// Extract model name from path
	// Path format: /llm/{model-name}/v1/chat/completions or /llm/{model-name}/v1/completions
	pathParts := strings.Split(strings.TrimPrefix(req.Path, "/"), "/")
	h.logger.Debug("Extracted path parts", "path", req.Path, "parts", pathParts, "count", len(pathParts))

	if len(pathParts) < 2 || pathParts[0] != "llm" {
		h.logger.Debug("Invalid path format", "path", req.Path, "parts", pathParts)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid path format: expected /llm/{model-name}/...",
		})
		return
	}

	modelName := pathParts[1]
	if modelName == "" {
		h.logger.Debug("Model name is empty", "path", req.Path, "parts", pathParts)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "model name not found in path",
		})
		return
	}

	h.logger.Debug("Extracted model name from path", "modelName", modelName)

	// Search for LLMInferenceService across all namespaces
	// We need to search because we don't know the namespace from the path
	h.logger.Debug("Searching for LLMInferenceService", "modelName", modelName)

	allLLMs, err := h.llmIsvcLister.List(labels.Everything())
	if err != nil {
		h.logger.Error("Failed to list LLMInferenceServices", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to lookup model: " + err.Error(),
		})
		return
	}

	h.logger.Debug("Listed LLMInferenceServices", "count", len(allLLMs))

	var foundLLM *kservev1alpha1.LLMInferenceService
	for _, llm := range allLLMs {
		if llm.Name == modelName {
			foundLLM = llm
			h.logger.Debug("Found LLMInferenceService",
				"modelName", modelName,
				"namespace", llm.Namespace,
				"name", llm.Name,
			)
			break
		}
	}

	if foundLLM == nil {
		h.logger.Debug("LLMInferenceService not found", "modelName", modelName, "searchedCount", len(allLLMs))
		c.JSON(http.StatusNotFound, gin.H{
			"error": "model not found: " + modelName,
		})
		return
	}

	// Check tier annotation
	annotations := foundLLM.GetAnnotations()
	tierAnnotation := annotations[constant.AnnotationTiers]
	h.logger.Debug("Checking tier annotation",
		"model", modelName,
		"userTier", req.Tier,
		"tierAnnotation", tierAnnotation,
	)

	allowed := false

	if tierAnnotation == "" {
		// No tier annotation means all tiers can access
		h.logger.Debug("No tier annotation found - allowing all tiers", "model", modelName)
		allowed = true
	} else {
		// Parse tier annotation (JSON array)
		var allowedTiers []string
		if err := json.Unmarshal([]byte(tierAnnotation), &allowedTiers); err != nil {
			h.logger.Warn("Failed to parse tier annotation", "model", modelName, "annotation", tierAnnotation, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "invalid tier annotation format: " + err.Error(),
			})
			return
		}

		h.logger.Debug("Parsed tier annotation", "model", modelName, "allowedTiers", allowedTiers)

		// Empty array means all tiers can access
		if len(allowedTiers) == 0 {
			h.logger.Debug("Empty tier annotation array - allowing all tiers", "model", modelName)
			allowed = true
		} else {
			// Check if user's tier is in the allowed tiers list
			h.logger.Debug("Checking if user tier is in allowed tiers",
				"model", modelName,
				"userTier", req.Tier,
				"allowedTiers", allowedTiers,
			)
			for _, allowedTier := range allowedTiers {
				if allowedTier == req.Tier {
					h.logger.Debug("User tier matches allowed tier",
						"model", modelName,
						"userTier", req.Tier,
						"matchedTier", allowedTier,
					)
					allowed = true
					break
				}
			}
			if !allowed {
				h.logger.Debug("User tier not in allowed tiers list",
					"model", modelName,
					"userTier", req.Tier,
					"allowedTiers", allowedTiers,
				)
			}
		}
	}

	// Return JSON response with allowed boolean for metadata lookup
	// Authorino metadata evaluator will parse this and store it
	response := gin.H{
		"allowed": allowed,
	}

	if !allowed {
		h.logger.Debug("Access denied - user tier does not match model tier requirement",
			"model", modelName,
			"userTier", req.Tier,
		)
		response["reason"] = "user tier '" + req.Tier + "' not in model's allowed tiers"
	} else {
		h.logger.Debug("Access granted",
			"model", modelName,
			"userTier", req.Tier,
		)
	}

	h.logger.Debug("Returning authorization response", "model", modelName, "allowed", allowed, "response", response)
	c.JSON(http.StatusOK, response)
}
