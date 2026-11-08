# aci-exporter Helm chart

Deploys [aci-exporter](https://github.com/opsdis/aci-exporter) to Kubernetes.

```shell
helm install aci-exporter oci://ghcr.io/opsdis/charts/aci-exporter -f my-values.yaml
```

## Minimal values

```yaml
config:
  fabrics:
    my_fabric:
      apic:
        - https://apic1.example.com

credentials:
  fabrics:
    my_fabric:
      username: monitor
      password: secret

queries:
  faults.yaml: |
    class_queries:
      faults:
        class_name: faultInst
        ...
```

- `config` is rendered as `config.yaml`. Everything from `example-config.yaml` can go here.
- `queries` become files in `config.d`. The `config.d` directory of the repository has ready to use queries.
- Credentials are passed as `ACI_EXPORTER_FABRICS_<FABRIC>_USERNAME/_PASSWORD` environment variables. Instead of
  `credentials.fabrics` an existing secret with these keys can be used with `credentials.existingSecret`, for example
  one managed by external-secrets.
- `config.httpclient.insecureHTTPS` is `false` in this chart. Mount the CA of the APIC certificates with
  `extraVolumes`/`extraVolumeMounts` (see `values.yaml`) or set it to `true`.

## Scraping

With the Prometheus Operator, `serviceMonitor.enabled` creates a ServiceMonitor for the exporter's own metrics and one
`/probe` endpoint per entry in `serviceMonitor.targets`. For service discovery via `/sd`, see the Prometheus
examples in the repository.

## OpenShift

The default `podSecurityContext` sets `runAsUser`/`runAsGroup` to 65534. On OpenShift set both to `null` to let the
SCC assign a user.
