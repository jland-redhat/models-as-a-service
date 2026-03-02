package auth

import (
	"errors"
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/cache"
)

// AdminChecker provides methods to check if a user is an admin based on Auth CR.
type AdminChecker interface {
	// IsAdmin checks if a user with the given groups is an admin.
	IsAdmin(userGroups []string) bool
	// GetAdminGroups returns the current list of admin groups from Auth CR.
	GetAdminGroups() ([]string, error)
}

// authChecker implements AdminChecker using the Auth CR from OpenDataHub operator.
type authChecker struct {
	authLister cache.GenericLister
}

// NewAdminChecker creates a new AdminChecker that queries the Auth CR.
// The Auth CR is a cluster-scoped singleton named "auth" from services.opendatahub.io/v1alpha1.
func NewAdminChecker(authLister cache.GenericLister) AdminChecker {
	return &authChecker{
		authLister: authLister,
	}
}

// IsAdmin checks if any of the user's groups match the admin groups defined in the Auth CR.
// Returns true if the user belongs to at least one admin group, false otherwise.
// If the Auth CR doesn't exist or can't be read, returns false (fail-closed).
func (a *authChecker) IsAdmin(userGroups []string) bool {
	adminGroups, err := a.GetAdminGroups()
	if err != nil {
		// Fail-closed: if we can't determine admin groups, deny admin access
		return false
	}

	// Check if any user group matches admin groups
	for _, userGroup := range userGroups {
		if slices.Contains(adminGroups, userGroup) {
			return true
		}
	}

	return false
}

// GetAdminGroups fetches the admin groups from the Auth CR.
// The Auth CR is cluster-scoped and must be named "auth".
// Returns empty slice and error if Auth CR doesn't exist or has invalid format.
func (a *authChecker) GetAdminGroups() ([]string, error) {
	// Auth CR is cluster-scoped, so we get it directly by name
	obj, err := a.authLister.Get("auth")
	if err != nil {
		return nil, fmt.Errorf("failed to get Auth CR: %w", err)
	}

	// Convert to unstructured to access fields
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("unexpected type for Auth CR: %T", obj)
	}

	// Extract spec.adminGroups field
	adminGroups, found, err := unstructured.NestedStringSlice(u.Object, "spec", "adminGroups")
	if err != nil {
		return nil, fmt.Errorf("failed to parse adminGroups from Auth CR: %w", err)
	}
	if !found {
		return nil, errors.New("adminGroups field not found in Auth CR spec")
	}

	return adminGroups, nil
}
