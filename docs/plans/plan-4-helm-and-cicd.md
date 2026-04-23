# Plan 4: Helm Chart + CI/CD + Public Registry

## Goal

Make the repo public, publish a multi-arch Docker image to `ghcr.io`, create a Helm chart
installable on any Kubernetes cluster, and wire GitHub Actions for CI (security checks on
every PR) and CD (image + chart release on every tag).

## Context

- Repo: `github.com/vutruongduc/kube-scaledown` (currently private)
- Registry: `ghcr.io/vutruongduc/kube-scaledown` (free for public repos, uses GITHUB_TOKEN)
- Helm repo: GitHub Pages (`gh-pages` branch) via `helm/chart-releaser-action`
- Current image in cluster: `atherlabs.azurecr.io/kube-scaledown:0.1.0`
- CRD: `config/crd/bases/downscaler.sipher.gg_downscaleschedules.yaml`
- RBAC: `config/rbac/role.yaml`, `leader_election_role.yaml`, bindings, service account

## Tasks

### Task 1: Make Repo Public

```bash
gh repo edit vutruongduc/kube-scaledown --visibility public --yes
```

Verify: `gh repo view vutruongduc/kube-scaledown --json visibility`

### Task 2: Create Helm Chart

Create `charts/kube-scaledown/` with the following structure:

```
charts/kube-scaledown/
  Chart.yaml
  values.yaml
  crds/
    downscaler.sipher.gg_downscaleschedules.yaml   (copy from config/crd/bases/)
  templates/
    _helpers.tpl
    deployment.yaml
    serviceaccount.yaml
    clusterrole.yaml
    clusterrolebinding.yaml
    role.yaml                (leader election)
    rolebinding.yaml         (leader election)
    NOTES.txt
```

**`Chart.yaml`**:
```yaml
apiVersion: v2
name: kube-scaledown
description: Kubernetes controller that scales down workloads (Deployments, StatefulSets, Fleets, CronJobs) on a schedule.
type: application
version: 0.1.0
appVersion: "0.1.0"
keywords:
  - kubernetes
  - controller
  - scaling
  - agones
home: https://github.com/vutruongduc/kube-scaledown
sources:
  - https://github.com/vutruongduc/kube-scaledown
maintainers:
  - name: vutruongduc
```

**`values.yaml`**:
```yaml
replicaCount: 1

image:
  repository: ghcr.io/vutruongduc/kube-scaledown
  pullPolicy: IfNotPresent
  tag: ""  # defaults to Chart.appVersion

imagePullSecrets: []

serviceAccount:
  create: true
  name: ""
  annotations: {}

podAnnotations: {}

podSecurityContext:
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault

securityContext:
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
  capabilities:
    drop:
      - ALL

resources:
  limits:
    cpu: 500m
    memory: 128Mi
  requests:
    cpu: 10m
    memory: 64Mi

leaderElect: true

nodeSelector: {}
tolerations: []
affinity: {}
```

**`templates/_helpers.tpl`**: Standard Helm helpers (name, fullname, labels, selectorLabels, serviceAccountName).

**`templates/deployment.yaml`**: Mirrors `config/manager/manager.yaml` but templated with values (image, resources, leaderElect, securityContext, serviceAccountName).

**`templates/serviceaccount.yaml`**: Conditional on `serviceAccount.create`.

**`templates/clusterrole.yaml`**: Full content of `config/rbac/role.yaml` with Helm labels.

**`templates/clusterrolebinding.yaml`**: Binds ClusterRole to ServiceAccount.

**`templates/role.yaml`**: Leader election Role from `config/rbac/leader_election_role.yaml`.

**`templates/rolebinding.yaml`**: Leader election RoleBinding.

**`templates/NOTES.txt`**:
```
kube-scaledown has been installed.

Create a DownscaleSchedule to start managing workloads:

  kubectl apply -f https://raw.githubusercontent.com/vutruongduc/kube-scaledown/master/config/samples/downscaleschedule_working_hours.yaml

Docs: https://github.com/vutruongduc/kube-scaledown
```

**Verify**: `helm lint charts/kube-scaledown` and `helm template charts/kube-scaledown | kubectl apply --dry-run=client -f -`

### Task 3: Add golangci-lint Config

Create `.golangci.yml` at repo root:
```yaml
run:
  timeout: 5m

linters:
  enable:
    - gosec
    - govet
    - errcheck
    - staticcheck
    - unused
    - misspell
    - gofmt

linters-settings:
  gosec:
    excludes:
      - G601  # implicit memory aliasing (fixed in Go 1.22)
```

### Task 4: Create CI Workflow

Create `.github/workflows/ci.yaml`:

```yaml
name: CI

on:
  push:
    branches: [master, main]
  pull_request:
    branches: [master, main]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Run unit tests
        run: go test ./internal/scaler/... ./internal/schedule/...

  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - uses: golangci/golangci-lint-action@v6
        with:
          version: latest

  security:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: govulncheck
        run: |
          go install golang.org/x/vuln/cmd/govulncheck@latest
          govulncheck ./...

  helm-lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: azure/setup-helm@v4
      - name: Lint Helm chart
        run: helm lint charts/kube-scaledown
```

### Task 5: Create Release Workflow

Create `.github/workflows/release.yaml`:

Triggers on `v*` tag push. Two jobs:
1. **docker** — build multi-arch image and push to `ghcr.io`
2. **helm-release** — package Helm chart, publish to `gh-pages` via `helm/chart-releaser-action`

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

env:
  REGISTRY: ghcr.io
  IMAGE_NAME: ${{ github.repository }}

jobs:
  docker:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
    steps:
      - uses: actions/checkout@v4
      - uses: docker/setup-qemu-action@v3
      - uses: docker/setup-buildx-action@v3
      - name: Login to ghcr.io
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - name: Docker metadata
        id: meta
        uses: docker/metadata-action@v5
        with:
          images: ghcr.io/${{ github.repository }}
          tags: |
            type=semver,pattern={{version}}
            type=semver,pattern={{major}}.{{minor}}
            type=raw,value=latest
      - name: Build and push
        uses: docker/build-push-action@v6
        with:
          context: .
          platforms: linux/amd64,linux/arm64
          push: true
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
          cache-to: type=gha,mode=max

  trivy:
    runs-on: ubuntu-latest
    needs: docker
    permissions:
      contents: read
      security-events: write
    steps:
      - uses: actions/checkout@v4
      - name: Run Trivy vulnerability scanner
        uses: aquasecurity/trivy-action@master
        with:
          image-ref: ghcr.io/${{ github.repository }}:latest
          format: sarif
          output: trivy-results.sarif
          severity: CRITICAL,HIGH
      - name: Upload Trivy scan results to GitHub Security tab
        uses: github/codeql-action/upload-sarif@v3
        with:
          sarif_file: trivy-results.sarif

  helm-release:
    runs-on: ubuntu-latest
    needs: docker
    permissions:
      contents: write
      pages: write
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - name: Configure Git
        run: |
          git config user.name "$GITHUB_ACTOR"
          git config user.email "$GITHUB_ACTOR@users.noreply.github.com"
      - uses: azure/setup-helm@v4
      - name: Update chart appVersion from tag
        run: |
          TAG=${GITHUB_REF_NAME#v}
          sed -i "s/^version:.*/version: $TAG/" charts/kube-scaledown/Chart.yaml
          sed -i "s/^appVersion:.*/appVersion: \"$TAG\"/" charts/kube-scaledown/Chart.yaml
      - uses: helm/chart-releaser-action@v1.6.0
        env:
          CR_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

### Task 6: Update image reference and push initial tag

Update `config/manager/manager.yaml` image to `ghcr.io/vutruongduc/kube-scaledown:0.1.0` (was `atherlabs.azurecr.io/kube-scaledown:0.1.0`).

Push tag to trigger first release:
```bash
git tag v0.1.0
git push origin v0.1.0
```

Verify:
- GitHub Actions release workflow runs
- `ghcr.io/vutruongduc/kube-scaledown:0.1.0` is pushed
- `gh-pages` branch has `index.yaml` for Helm repo
- `helm repo add kube-scaledown https://vutruongduc.github.io/kube-scaledown` works

## Verification Checklist

- [ ] Repo is public: `gh repo view vutruongduc/kube-scaledown --json visibility`
- [ ] `helm lint charts/kube-scaledown` passes
- [ ] CI workflow runs green on master push
- [ ] Release workflow builds and pushes `ghcr.io/vutruongduc/kube-scaledown:0.1.0`
- [ ] Trivy scan shows no CRITICAL/HIGH vulns (or uploads to Security tab)
- [ ] `helm repo add kube-scaledown https://vutruongduc.github.io/kube-scaledown` works
- [ ] `helm install kube-scaledown kube-scaledown/kube-scaledown --dry-run` succeeds
