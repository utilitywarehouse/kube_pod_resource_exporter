# kube_pod_resource_exporter

It exports two metrics, for each container in the cluster:
- `container_resources_cpu_milli`
- `container_resources_memory_bytes`

These metrics have the following labels:
- `namespace`
- `pod_name`
- `container_name`
- `type` (can be either `request` or `limit`)
