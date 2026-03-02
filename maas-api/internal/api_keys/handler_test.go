package api_keys //nolint:testpackage // Testing private helper methods requires same package

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/models-as-a-service/maas-api/internal/config"
	"github.com/opendatahub-io/models-as-a-service/maas-api/internal/logger"
	"github.com/opendatahub-io/models-as-a-service/maas-api/internal/token"
)

// mockAdminChecker is a simple mock for testing that checks if user has "admin-users" group.
type mockAdminChecker struct {
	adminGroups []string
}

func newMockAdminChecker() *mockAdminChecker {
	return &mockAdminChecker{
		adminGroups: []string{"admin-users"},
	}
}

func (m *mockAdminChecker) IsAdmin(userGroups []string) bool {
	for _, userGroup := range userGroups {
		if slices.Contains(m.adminGroups, userGroup) {
			return true
		}
	}
	return false
}

func TestIsAuthorizedForKey(t *testing.T) {
	h := &Handler{
		adminChecker: newMockAdminChecker(),
	}

	t.Run("OwnerCanAccess", func(t *testing.T) {
		user := &token.UserContext{Username: "alice", Groups: []string{"users"}}
		assert.True(t, h.isAuthorizedForKey(user, "alice"))
	})

	t.Run("NonOwnerCannotAccess", func(t *testing.T) {
		user := &token.UserContext{Username: "bob", Groups: []string{"users"}}
		assert.False(t, h.isAuthorizedForKey(user, "alice"))
	})

	t.Run("AdminCanAccessAnyKey", func(t *testing.T) {
		admin := &token.UserContext{Username: "admin", Groups: []string{"admin-users"}}
		assert.True(t, h.isAuthorizedForKey(admin, "alice"))
	})
}

func TestListAPIKeysPagination(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	store := NewMockStore()
	cfg := &config.Config{}
	service := NewServiceWithLogger(store, cfg, logger.Development())
	handler := NewHandler(logger.Development(), service, newMockAdminChecker())

	// Create test user and keys
	testUser := &token.UserContext{
		Username: "test-user",
		Groups:   []string{"system:authenticated"},
	}

	// Add 75 keys to test pagination
	for i := 1; i <= 75; i++ {
		keyID := fmt.Sprintf("key-%d", i)
		keyHash := fmt.Sprintf("hash-%d", i)
		name := fmt.Sprintf("Key %d", i)
		err := store.AddKey(context.Background(), testUser.Username, keyID, keyHash, name, "", []string{"system:authenticated"}, nil)
		require.NoError(t, err)
	}

	t.Run("DefaultPagination", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/api-keys", nil)
		c.Set("user", testUser)

		handler.ListAPIKeys(c)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListAPIKeysResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "list", response.Object)
		assert.Len(t, response.Data, 50, "should use default limit of 50")
		assert.True(t, response.HasMore, "should indicate more pages exist")
	})

	t.Run("InvalidLimit", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/api-keys?limit=abc", nil)
		c.Set("user", testUser)

		handler.ListAPIKeys(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]string
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Contains(t, response["error"], "invalid limit parameter")
	})
}

func TestAdminCanViewAllKeys_RegularUserOnlyOwn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewMockStore()
	cfg := &config.Config{}
	service := NewServiceWithLogger(store, cfg, logger.Development())
	handler := NewHandler(logger.Development(), service, newMockAdminChecker())

	ctx := context.Background()

	// Create keys for multiple users
	users := []string{"alice", "bob"}
	for _, username := range users {
		for i := 1; i <= 2; i++ {
			keyID := fmt.Sprintf("%s-key-%d", username, i)
			keyHash := fmt.Sprintf("%s-hash-%d", username, i)
			name := fmt.Sprintf("%s Key %d", username, i)
			err := store.AddKey(ctx, username, keyID, keyHash, name, "", []string{"system:authenticated"}, nil)
			require.NoError(t, err)
		}
	}

	t.Run("AdminSeesAllKeys", func(t *testing.T) {
		adminUser := &token.UserContext{
			Username: "admin",
			Groups:   []string{"admin-users"},
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/api-keys", nil)
		c.Set("user", adminUser)

		handler.ListAPIKeys(c)

		assert.Equal(t, http.StatusOK, w.Code)
		var response ListAPIKeysResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Len(t, response.Data, 4, "admin should see all 4 keys")
	})

	t.Run("RegularUserOnlySeesOwnKeys", func(t *testing.T) {
		regularUser := &token.UserContext{
			Username: "alice",
			Groups:   []string{"system:authenticated"},
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/api-keys", nil)
		c.Set("user", regularUser)

		handler.ListAPIKeys(c)

		assert.Equal(t, http.StatusOK, w.Code)
		var response ListAPIKeysResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Len(t, response.Data, 2, "regular user should only see own keys")
	})

	t.Run("RegularUserCannotFilterOtherUser", func(t *testing.T) {
		regularUser := &token.UserContext{
			Username: "alice",
			Groups:   []string{"system:authenticated"},
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/api-keys?username=bob", nil)
		c.Set("user", regularUser)

		handler.ListAPIKeys(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestStatusFiltering(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewMockStore()
	cfg := &config.Config{}
	service := NewServiceWithLogger(store, cfg, logger.Development())
	handler := NewHandler(logger.Development(), service, newMockAdminChecker())

	ctx := context.Background()
	testUser := &token.UserContext{
		Username: "test-user",
		Groups:   []string{"system:authenticated"},
	}

	// Create active and revoked keys
	err := store.AddKey(ctx, testUser.Username, "active-key", "active-hash", "Active Key", "", []string{"system:authenticated"}, nil)
	require.NoError(t, err)
	err = store.AddKey(ctx, testUser.Username, "revoked-key", "revoked-hash", "Revoked Key", "", []string{"system:authenticated"}, nil)
	require.NoError(t, err)
	err = store.Revoke(ctx, "revoked-key")
	require.NoError(t, err)

	t.Run("FiltersByStatus", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/api-keys?status=active", nil)
		c.Set("user", testUser)

		handler.ListAPIKeys(c)

		assert.Equal(t, http.StatusOK, w.Code)
		var response ListAPIKeysResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Len(t, response.Data, 1)
		assert.Equal(t, "active", response.Data[0].Status)
	})

	t.Run("InvalidStatusReturnsError", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/api-keys?status=invalid", nil)
		c.Set("user", testUser)

		handler.ListAPIKeys(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestAdminCanCreateForOtherUser_RegularUserCannot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewMockStore()
	cfg := &config.Config{}
	service := NewServiceWithLogger(store, cfg, logger.Development())
	handler := NewHandler(logger.Development(), service, newMockAdminChecker())

	t.Run("AdminCreatesForOtherUser", func(t *testing.T) {
		adminUser := &token.UserContext{
			Username: "admin",
			Groups:   []string{"admin-users", "system:authenticated"},
		}

		// Admin must provide groups when creating keys for other users
		requestBody := `{"name": "Alice's Key", "username": "alice", "groups": ["tier-premium", "system:authenticated"]}`

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/api-keys", nil)
		c.Request.Header.Set("Content-Type", "application/json")
		c.Request.Body = io.NopCloser(strings.NewReader(requestBody))
		c.Set("user", adminUser)

		handler.CreateAPIKey(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		var response CreateAPIKeyResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Verify key owned by alice, not admin
		meta, err := store.Get(context.Background(), response.ID)
		require.NoError(t, err)
		assert.Equal(t, "alice", meta.Username)
		// Verify groups were stored correctly
		assert.Equal(t, []string{"tier-premium", "system:authenticated"}, meta.OriginalUserGroups)
	})

	t.Run("RegularUserCannotCreateForOther", func(t *testing.T) {
		regularUser := &token.UserContext{
			Username: "bob",
			Groups:   []string{"system:authenticated"},
		}

		requestBody := `{"name": "Test Key", "username": "alice"}`

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/api-keys", nil)
		c.Request.Header.Set("Content-Type", "application/json")
		c.Request.Body = io.NopCloser(strings.NewReader(requestBody))
		c.Set("user", regularUser)

		handler.CreateAPIKey(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestRegularUserCanCreateOwnKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewMockStore()
	cfg := &config.Config{}
	service := NewServiceWithLogger(store, cfg, logger.Development())
	handler := NewHandler(logger.Development(), service, newMockAdminChecker())

	t.Run("ImplicitUsername", func(t *testing.T) {
		// Regular user creates key without specifying username (implicit self)
		// Note: requested groups are ignored - user's actual groups are always used
		testRegularUserCreateOwnKey(t, handler, store, `{"name": "my-key"}`)
	})

	t.Run("ExplicitUsername", func(t *testing.T) {
		// Regular user creates key with username=self
		// Note: requested groups are ignored - user's actual groups are always used
		testRegularUserCreateOwnKey(t, handler, store, `{"name": "my-key", "username": "alice"}`)
	})
}

// testRegularUserCreateOwnKey is a helper to test regular user creating their own key.
func testRegularUserCreateOwnKey(t *testing.T, handler *Handler, store *MockStore, requestBody string) {
	t.Helper()

	regularUser := &token.UserContext{
		Username: "alice",
		Groups:   []string{"tier-free", "system:authenticated"},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/api-keys", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Body = io.NopCloser(strings.NewReader(requestBody))
	c.Set("user", regularUser)

	handler.CreateAPIKey(c)

	assert.Equal(t, http.StatusCreated, w.Code)
	var response CreateAPIKeyResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	// Verify key is owned by alice with her full groups
	meta, err := store.Get(context.Background(), response.ID)
	require.NoError(t, err)
	assert.Equal(t, "alice", meta.Username)
	assert.Equal(t, []string{"tier-free", "system:authenticated"}, meta.OriginalUserGroups)
}

func TestAdminFiltersByUsernameAndStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewMockStore()
	cfg := &config.Config{}
	service := NewServiceWithLogger(store, cfg, logger.Development())
	handler := NewHandler(logger.Development(), service, newMockAdminChecker())

	ctx := context.Background()

	// Create 6 keys total: alice (2 active, 1 revoked), bob (2 active, 1 revoked)
	users := []string{"alice", "bob"}
	for _, username := range users {
		// Create 2 active keys
		for i := 1; i <= 2; i++ {
			keyID := fmt.Sprintf("%s-active-%d", username, i)
			keyHash := fmt.Sprintf("%s-hash-active-%d", username, i)
			name := fmt.Sprintf("%s Active Key %d", username, i)
			err := store.AddKey(ctx, username, keyID, keyHash, name, "", []string{"system:authenticated"}, nil)
			require.NoError(t, err)
		}
		// Create 1 revoked key
		keyID := fmt.Sprintf("%s-revoked", username)
		keyHash := fmt.Sprintf("%s-hash-revoked", username)
		name := fmt.Sprintf("%s Revoked Key", username)
		err := store.AddKey(ctx, username, keyID, keyHash, name, "", []string{"system:authenticated"}, nil)
		require.NoError(t, err)
		err = store.Revoke(ctx, keyID)
		require.NoError(t, err)
	}

	adminUser := &token.UserContext{
		Username: "admin",
		Groups:   []string{"admin-users"},
	}

	t.Run("AliceActiveKeys", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/api-keys?username=alice&status=active", nil)
		c.Set("user", adminUser)

		handler.ListAPIKeys(c)

		assert.Equal(t, http.StatusOK, w.Code)
		var response ListAPIKeysResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Len(t, response.Data, 2, "alice should have 2 active keys")
		for _, key := range response.Data {
			assert.Equal(t, "alice", key.Username)
			assert.Equal(t, "active", key.Status)
		}
	})

	t.Run("AliceRevokedKeys", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/api-keys?username=alice&status=revoked", nil)
		c.Set("user", adminUser)

		handler.ListAPIKeys(c)

		assert.Equal(t, http.StatusOK, w.Code)
		var response ListAPIKeysResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Len(t, response.Data, 1, "alice should have 1 revoked key")
		assert.Equal(t, "alice", response.Data[0].Username)
		assert.Equal(t, "revoked", response.Data[0].Status)
	})

	t.Run("BobActiveKeys", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/api-keys?username=bob&status=active", nil)
		c.Set("user", adminUser)

		handler.ListAPIKeys(c)

		assert.Equal(t, http.StatusOK, w.Code)
		var response ListAPIKeysResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Len(t, response.Data, 2, "bob should have 2 active keys")
		for _, key := range response.Data {
			assert.Equal(t, "bob", key.Username)
			assert.Equal(t, "active", key.Status)
		}
	})
}
