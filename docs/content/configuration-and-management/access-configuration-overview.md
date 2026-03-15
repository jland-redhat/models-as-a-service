# Access Configuration Overview

Access control in MaaS is configured via **MaaSAuthPolicy**. A policy defines **who** (subjects: groups/users) can access **which models** (by MaaSModelRef name). Users must match a policy's subjects to pass the access gate; they also need a matching subscription for quota.

## Example Configuration

```yaml
apiVersion: maas.opendatahub.io/v1alpha1
kind: MaaSAuthPolicy
metadata:
  name: data-science-access
  namespace: opendatahub
spec:
  modelRefs:
    - granite-3b-instruct
    - gpt-4-turbo
  subjects:
    groups:
      - name: data-science-team
    users:
      - name: service-account-a
```

This policy grants the `data-science-team` group and the `service-account-a` user access to the `granite-3b-instruct` and `gpt-4-turbo` models. Subjects use **OR logic**—a user matching any group or user in the list gets access. The `modelRefs` must match MaaSModelRef `metadata.name` values for models already registered on the cluster.

## Key Concepts

- **modelRefs** — List of model names (MaaSModelRef `metadata.name`) this policy grants access to
- **subjects** — Groups and/or users; **OR logic** — any match grants access
- **Multiple policies per model** — You can create multiple MaaSAuthPolicies that reference the same model. The controller aggregates them; a user matching any policy gets access.

## Related Documentation

For detailed configuration steps and field reference, see [MaaSAuthPolicy Configuration](maas-auth-policy-configuration.md).
