[![REUSE status](https://api.reuse.software/badge/github.com/openmcp-project/platform-service-gateway)](https://api.reuse.software/info/github.com/openmcp-project/platform-service-gateway)

# platform-service-gateway

## About this project

The Platform Service Gateway is responsible for enabling the [Gateway API](https://gateway-api.sigs.k8s.io/) on openMCP backend clusters, allowing webhooks from MCP resources to reach services in the platform or workload clusters.

### 📏 Architecture

![Architecture of the Platform Service Gateway in the context of openMCP](docs/architecture.excalidraw.svg)

### Dependencies

This platform service uses [Envoy Gateway](https://gateway.envoyproxy.io/) as the Gateway API implementation and [cert-manager](https://cert-manager.io/) to provision TLS certificates.

## 🏗️ Installation of the Platform Service Gateway

### Local Development

To run the platform-service-gateway locally, you need to first bootstrap an openMCP environment by using [openmcp-operator](https://github.com/openmcp-project/openmcp-operator) and [cluster-provider-kind](https://github.com/openmcp-project/cluster-provider-kind). A comprehensive guide will follow soon.

For current testing reasons, the platform-service-gateway needs to run in the cluster. To run the latest version of your changes in your local environment, you need to run:

```bash
task build:img:build
```

This will build the image of the platform-service-gateway locally and puts it into your local Docker registry.

```bash
docker images ghcr.io/openmcp-project/images/platform-service-gateway
```

You can then apply the `PlatformService` resource to your openMCP Platform cluster:

```yaml
apiVersion: openmcp.cloud/v1alpha1
kind: PlatformService
metadata:
  name: gateway
spec:
  image: ghcr.io/openmcp-project/images/platform-service-gateway:... # latest local docker image build
```

### OpenMCP Landscape

When you already have an openMCP environment set up, you can deploy the platform-service-gateway by applying the following manifest:

```yaml
apiVersion: openmcp.cloud/v1alpha1
kind: PlatformService
metadata:
  name: gateway
spec:
  image: ghcr.io/openmcp-project/images/platform-service-gateway:<latest-version> # latest upstream version
```

## 📖 Usage

### Configure a `GatewayServiceConfig`

A `GatewayServiceConfig` is an API where you can configure the platform-service-gateway.
The `GatewayServiceConfig` is stored in the Platform cluster and therefore in the responsibility realm of the platform owner.

```yaml
apiVersion: gateway.openmcp.cloud/v1alpha1
kind: GatewayServiceConfig
metadata:
  name: gateway # needs to match `PlatformService.metadata.name`
spec:
  envoyGateway:
    images:
      proxy: "ghcr.io/openmcp-project/components/github.com/openmcp-project/openmcp/images/envoy-proxy:distroless-v1.36.2"
      gateway: "ghcr.io/openmcp-project/components/github.com/openmcp-project/openmcp/images/envoy-gateway:v1.5.4"
      rateLimit: "ghcr.io/openmcp-project/components/github.com/openmcp-project/openmcp/images/envoy-ratelimit:99d85510"
    chart:
      url: "oci://ghcr.io/openmcp-project/components/github.com/openmcp-project/openmcp/charts/envoy-gateway"
      tag: "1.5.4"

  clusters:
    - selector:
        matchPurpose: platform
    - selector:
        matchPurpose: workload

  dns:
    baseDomain: dev.openmcp.example.com
```

## 📚 Documentation

More documentation for the platform-service-gateway can be found in the [docs](./docs) folder.

## 🧑‍💻 Development

### Building the binary locally

To build the binary locally, you can use the following command:

```bash
task build
```

### Build the image locally

To build the image locally, you can use the following command:

```bash
task build:img:build
```

### Run unit tests locally

To run the unit tests locally, you can use the following command:

```bash
task test
```

### Generating the CRDs, DeepCopy functions etc.

To generate the CRDs, DeepCopy functions, and other boilerplate code, you can use the following command:

```bash
task generate
```

## ❤️ Support, Feedback, Contributing

This project is open to feature requests/suggestions, bug reports etc. via [GitHub issues](https://github.com/openmcp-project/platform-service-gateway/issues). Contribution and feedback are encouraged and always welcome. For more information about how to contribute, the project structure, as well as additional contribution information, see our [Contribution Guidelines](CONTRIBUTING.md).

## 🔐 Security / Disclosure

If you find any bug that may be a security problem, please follow our instructions at [in our security policy](https://github.com/openmcp-project/platform-service-gateway/security/policy) on how to report it. Please do not create GitHub issues for security-related doubts or problems.

## 🤝 Code of Conduct

We as members, contributors, and leaders pledge to make participation in our community a harassment-free experience for everyone. By participating in this project, you agree to abide by its [Code of Conduct](https://github.com/openmcp-project/.github/blob/main/CODE_OF_CONDUCT.md) at all times.

## 📋 Licensing

Copyright 2025 SAP SE or an SAP affiliate company and platform-service-gateway contributors. Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/openmcp-project/platform-service-gateway).
