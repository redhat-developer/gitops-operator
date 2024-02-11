# `argocd admin proj generate-spec` Command Reference

## argocd admin proj generate-spec

Generate declarative config for a project

```
argocd admin proj generate-spec PROJECT [flags]
```

### Examples

```
  # Generate a YAML configuration for a project named "myproject"
  argocd admin projects generate-spec myproject
  
  # Generate a JSON configuration for a project named "anotherproject" and specify an output file
  argocd admin projects generate-spec anotherproject --output json --file config.json
  
  # Generate a YAML configuration for a project named "someproject" and write it back to the input file
  argocd admin projects generate-spec someproject --inline
```

### Options

| Option | Argument type | Description |
| ---------------- | ------ | ---- |
| --allow-cluster-resource | string Array| List of allowed cluster level resources |
| --allow-namespaced-resource | string Array| List of allowed namespaced resources |
| --deny-cluster-resource | string Array| List of denied cluster level resources |
| --deny-namespaced-resource | string Array| List of denied namespaced resources |
| --description | string | Project description |
| --orphaned-resources| Enables orphaned resources monitoring |
| --orphaned-resources-warn| Specifies if applications should have a warning condition when orphaned resources detected |
| --signature-keys | string s| GnuPG public key IDs for commit signature verification |
| --source-namespaces | string s| List of source namespaces for applications |

### Options inherited from parent commands

| Option | Argument type | Description |
| ---------------- | ------ | ---- |
| --auth-token | string | Authentication token |
| --client-crt | string | Client certificate file |
| --client-crt-key | string | Client certificate key file |
| --config | string | Path to Argo CD config (default "/home/user/.config/argocd/config") |
| --controller-name | string | Name of the Argo CD Application controller; set this or the ARGOCD_APPLICATION_CONTROLLER_NAME environment variable when the controller's name label differs from the default, for example when installing via the Helm chart (default "argocd-application-controller") |
| --core | |If set to true then CLI talks directly to Kubernetes instead of talking to Argo CD API server |
| --grpc-web | |Enables gRPC-web protocol. Useful if Argo CD server is behind proxy which does not support HTTP2. |
| --grpc-web-root-path | string | Enables gRPC-web protocol. Useful if Argo CD server is behind proxy which does not support HTTP2. Set web root. |
| --http-retry-max | int | Maximum number of retries to establish http connection to Argo CD server |
| --insecure | |Skip server certificate and domain verification |
| --kube-context | string | Directs the command to the given kube-context |
| --logformat | string | Set the logging format. One of: text|json (default "text") |
| --loglevel | string | Set the logging level. One of: debug|info|warn|error (default "info") |
| --plaintext | |Disable TLS |
| --port-forward | |Connect to a random argocd-server port using port forwarding |
| --port-forward-namespace | string | Namespace name which should be used for port forwarding |
| --redis-haproxy-name | string | Name of the Redis HA Proxy; set this or the ARGOCD_REDIS_HAPROXY_NAME environment variable when the HA Proxy's name label differs from the default, for example when installing via the Helm chart (default "argocd-redis-ha-haproxy") |
| --redis-name | string | Name of the Redis deployment; set this or the ARGOCD_REDIS_NAME environment variable when the Redis's name label differs from the default, for example when installing via the Helm chart (default "argocd-redis") |
| --repo-server-name | string | Name of the Argo CD Repo server; set this or the ARGOCD_REPO_SERVER_NAME environment variable when the server's name label differs from the default, for example when installing via the Helm chart (default "argocd-repo-server") |
| --server | string | Argo CD server address |
| --server-crt | string | Server certificate file |
| --server-name | string | Name of the Argo CD API server; set this or the ARGOCD_SERVER_NAME environment variable when the server's name label differs from the default, for example when installing via the Helm chart (default "argocd-server") |

### SEE ALSO

* [argocd admin proj](argocd_admin_proj.md)	 - Manage projects configuration
