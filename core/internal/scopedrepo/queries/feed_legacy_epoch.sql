SELECT e.version,n.namespace FROM resource_access_epoch e
JOIN scope_workspace_namespace n ON n.singleton=e.singleton WHERE e.singleton=1
