/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webhook

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"
)

func TestMaaSSubscriptionValidator_ValidateCreate(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = maasv1alpha1.AddToScheme(scheme)

	tests := []struct {
		name         string
		subscription *maasv1alpha1.MaaSSubscription
		namespace    *corev1.Namespace
		tenant       *maasv1alpha1.Tenant
		wantErr      bool
		errContains  string
	}{
		{
			name: "allow subscription in namespace with legacy Tenant CR",
			subscription: &maasv1alpha1.MaaSSubscription{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sub",
					Namespace: "ai-tenant-redteam",
				},
			},
			tenant: &maasv1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default-tenant",
					Namespace: "ai-tenant-redteam",
				},
			},
			wantErr: false,
		},
		{
			name: "reject subscription in namespace without tenant config CR",
			subscription: &maasv1alpha1.MaaSSubscription{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sub",
					Namespace: "random-namespace",
				},
			},
			wantErr:     true,
			errContains: "not enabled for MaaS tenant resources",
		},
		{
			name: "allow create with short rate-limit identity",
			subscription: &maasv1alpha1.MaaSSubscription{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sub",
					Namespace: "ai-tenant-redteam",
					Annotations: map[string]string{
						annotationRateLimitIdentity: rateLimitIdentityShort,
					},
				},
			},
			tenant: &maasv1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default-tenant",
					Namespace: "ai-tenant-redteam",
				},
			},
			wantErr: false,
		},
		{
			name: "reject create with unknown rate-limit identity",
			subscription: &maasv1alpha1.MaaSSubscription{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sub",
					Namespace: "ai-tenant-redteam",
					Annotations: map[string]string{
						annotationRateLimitIdentity: "bogus",
					},
				},
			},
			tenant: &maasv1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default-tenant",
					Namespace: "ai-tenant-redteam",
				},
			},
			wantErr:     true,
			errContains: annotationRateLimitIdentity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objs []runtime.Object
			if tt.namespace != nil {
				objs = append(objs, tt.namespace)
			}
			if tt.tenant != nil {
				objs = append(objs, tt.tenant)
			}
			client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()

			validator := &MaaSSubscriptionValidator{
				Client: client,
				Validator: &TenantNamespaceValidator{
					Client: client,
				},
			}

			_, err := validator.ValidateCreate(context.Background(), tt.subscription)

			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCreate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.errContains != "" && (err == nil || !contains(err.Error(), tt.errContains)) {
				t.Errorf("ValidateCreate() error = %v, want error containing %q", err, tt.errContains)
			}
		})
	}
}

func TestMaaSSubscriptionValidator_ValidateUpdate(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = maasv1alpha1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	validator := &MaaSSubscriptionValidator{
		Client: client,
		Validator: &TenantNamespaceValidator{
			Client: client,
		},
	}

	withAnn := func(v string) *maasv1alpha1.MaaSSubscription {
		sub := &maasv1alpha1.MaaSSubscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sub",
				Namespace: "ai-tenant-test",
			},
		}
		if v != "" {
			sub.Annotations = map[string]string{annotationRateLimitIdentity: v}
		}
		return sub
	}

	tests := []struct {
		name        string
		old         *maasv1alpha1.MaaSSubscription
		new         *maasv1alpha1.MaaSSubscription
		wantErr     bool
		errContains string
	}{
		{
			name: "update without identity annotation",
			old:  withAnn(""),
			new:  withAnn(""),
		},
		{
			name: "allow short to legacy opt-in",
			old:  withAnn(rateLimitIdentityShort),
			new:  withAnn(rateLimitIdentityLegacy),
		},
		{
			name:        "reject short identity removal",
			old:         withAnn(rateLimitIdentityShort),
			new:         withAnn(""),
			wantErr:     true,
			errContains: annotationRateLimitIdentity,
		},
		{
			name:        "reject short to garbage via ValidateUpdate",
			old:         withAnn(rateLimitIdentityShort),
			new:         withAnn("bogus"),
			wantErr:     true,
			errContains: annotationRateLimitIdentity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validator.ValidateUpdate(context.Background(), tt.old, tt.new)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateUpdate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errContains != "" && (err == nil || !contains(err.Error(), tt.errContains)) {
				t.Fatalf("ValidateUpdate() error = %v, want error containing %q", err, tt.errContains)
			}
		})
	}
}

func TestValidateRateLimitIdentityAnnotationSticky(t *testing.T) {
	t.Parallel()

	withAnn := func(v string) *maasv1alpha1.MaaSSubscription {
		sub := &maasv1alpha1.MaaSSubscription{
			ObjectMeta: metav1.ObjectMeta{Name: "sub", Namespace: "ns"},
		}
		if v != "" {
			sub.Annotations = map[string]string{annotationRateLimitIdentity: v}
		}
		return sub
	}

	tests := []struct {
		name    string
		old     *maasv1alpha1.MaaSSubscription
		new     *maasv1alpha1.MaaSSubscription
		wantErr bool
	}{
		{name: "unset stays unset", old: withAnn(""), new: withAnn(""), wantErr: false},
		{name: "controller may stamp short", old: withAnn(""), new: withAnn(rateLimitIdentityShort), wantErr: false},
		{name: "controller may stamp legacy", old: withAnn(""), new: withAnn(rateLimitIdentityLegacy), wantErr: false},
		{name: "short stays short", old: withAnn(rateLimitIdentityShort), new: withAnn(rateLimitIdentityShort), wantErr: false},
		{name: "legacy may opt into short", old: withAnn(rateLimitIdentityLegacy), new: withAnn(rateLimitIdentityShort), wantErr: false},
		{name: "short may switch to legacy", old: withAnn(rateLimitIdentityShort), new: withAnn(rateLimitIdentityLegacy), wantErr: false},
		{name: "reject short removal", old: withAnn(rateLimitIdentityShort), new: withAnn(""), wantErr: true},
		{name: "reject legacy removal", old: withAnn(rateLimitIdentityLegacy), new: withAnn(""), wantErr: true},
		{name: "reject short to garbage", old: withAnn(rateLimitIdentityShort), new: withAnn("bogus"), wantErr: true},
		{name: "reject legacy to garbage", old: withAnn(rateLimitIdentityLegacy), new: withAnn("garbage"), wantErr: true},
		{name: "reject unset to garbage", old: withAnn(""), new: withAnn("nope"), wantErr: true},
		{
			name: "reject short removal via nil annotations map",
			old:  withAnn(rateLimitIdentityShort),
			new: &maasv1alpha1.MaaSSubscription{
				ObjectMeta: metav1.ObjectMeta{Name: "sub", Namespace: "ns"},
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateRateLimitIdentityAnnotationSticky(tc.old, tc.new)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr && err != nil && !contains(err.Error(), annotationRateLimitIdentity) {
				t.Fatalf("error = %v, want annotation name in message", err)
			}
		})
	}
}

func TestValidateRateLimitIdentityAnnotationValue(t *testing.T) {
	t.Parallel()
	assertNil := func(t *testing.T, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	assertErr := func(t *testing.T, err error) {
		t.Helper()
		if err == nil {
			t.Fatal("expected error")
		}
	}
	assertNil(t, validateRateLimitIdentityAnnotationValue(""))
	assertNil(t, validateRateLimitIdentityAnnotationValue(rateLimitIdentityShort))
	assertNil(t, validateRateLimitIdentityAnnotationValue(rateLimitIdentityLegacy))
	assertErr(t, validateRateLimitIdentityAnnotationValue("bogus"))
}

func TestMaaSSubscriptionValidator_ValidateUpdateOrganizationAssociation(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = maasv1alpha1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	validator := &MaaSSubscriptionValidator{
		Client: client,
		Validator: &TenantNamespaceValidator{
			Client: client,
		},
	}

	tests := []struct {
		name    string
		oldID   *string
		newID   *string
		wantErr bool
	}{
		{name: "legacy adoption", oldID: nil, newID: stringPointer("grid-org-a")},
		{name: "unchanged association", oldID: stringPointer("grid-org-a"), newID: stringPointer("grid-org-a")},
		{name: "association cannot change", oldID: stringPointer("grid-org-a"), newID: stringPointer("grid-org-b"), wantErr: true},
		{name: "association cannot be removed", oldID: stringPointer("grid-org-a"), newID: nil, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldSub := subscriptionWithOrganization(tt.oldID)
			newSub := subscriptionWithOrganization(tt.newID)

			_, err := validator.ValidateUpdate(context.Background(), oldSub, newSub)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateUpdate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && (err == nil || err.Error() != "organization.id is immutable") {
				t.Fatalf("ValidateUpdate() error = %v, want organization.id is immutable", err)
			}
		})
	}
}

func TestMaaSSubscriptionValidator_ValidateDelete(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = maasv1alpha1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	validator := &MaaSSubscriptionValidator{
		Client: client,
		Validator: &TenantNamespaceValidator{
			Client: client,
		},
	}

	sub := &maasv1alpha1.MaaSSubscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sub",
			Namespace: "ai-tenant-test",
		},
	}

	// Delete should not validate
	_, err := validator.ValidateDelete(context.Background(), sub)
	if err != nil {
		t.Errorf("ValidateDelete() unexpected error: %v", err)
	}
}

func subscriptionWithOrganization(id *string) *maasv1alpha1.MaaSSubscription {
	sub := &maasv1alpha1.MaaSSubscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sub",
			Namespace: "ai-tenant-test",
		},
	}
	if id != nil {
		sub.Spec.Organization = &maasv1alpha1.SubscriptionOrganization{ID: *id}
	}
	return sub
}

func stringPointer(value string) *string {
	return &value
}
