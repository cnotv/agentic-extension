# Dev workflow for the agentic controller against the local Rancher (k3s inside the rancher-16486 container).
REPO_ROOT    := $(abspath ..)
IMG          ?= agentic-controller:dev
ARCH         ?= arm64
NAMESPACE    ?= cattle-agentic-system
RANCHER      ?= rancher-16486
KUBECTL      := docker exec -i $(RANCHER) kubectl
CHART        := $(REPO_ROOT)/charts/agentic-controller
CONTROLLER_GEN := $(REPO_ROOT)/tmp/bin/controller-gen

export GOTOOLCHAIN := local
export DOCKER_HOST ?= unix://$(HOME)/.colima/default/docker.sock
export DOCKER_CONFIG ?= $(REPO_ROOT)/tmp/docker-config

# Go 1.22 binaries need the system linker on recent macOS (missing LC_UUID otherwise).
ifeq ($(shell uname -s),Darwin)
HOST_LDFLAGS := -ldflags=-linkmode=external
endif

.PHONY: all generate fmt vet test build build-linux image image-full load deploy restart dev undeploy logs example

all: vet test build

generate: ## Regenerate deepcopy code (controller-gen v0.15.0 in ../tmp/bin)
	$(CONTROLLER_GEN) object:headerFile=hack/boilerplate.go.txt paths=./api/...

fmt:
	gofmt -w .

vet:
	go vet ./...

test:
	go test $(HOST_LDFLAGS) ./...

build: ## Host binary
	go build $(HOST_LDFLAGS) -o bin/manager ./cmd

build-linux: ## Cross-compile for the cluster
	CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) go build -trimpath -ldflags="-s -w" -o bin/manager-linux ./cmd

image: build-linux ## Fast dev image from the cross-compiled binary
	@mkdir -p $(DOCKER_CONFIG) && [ -f $(DOCKER_CONFIG)/config.json ] || echo '{}' > $(DOCKER_CONFIG)/config.json
	docker build -f Dockerfile.dev -t $(IMG) .

image-full: ## Multi-stage image build (no host Go needed)
	docker build --build-arg TARGETARCH=$(ARCH) -t $(IMG) .

load: ## Import the image into the k3s containerd
	docker save $(IMG) | docker exec -i $(RANCHER) ctr -n k8s.io -a /run/k3s/containerd/containerd.sock images import -

deploy: ## Render the chart and apply it (CRDs included)
	$(KUBECTL) create namespace $(NAMESPACE) --dry-run=client -o yaml | $(KUBECTL) apply -f -
	helm template agentic-controller $(CHART) -n $(NAMESPACE) --include-crds | $(KUBECTL) apply -n $(NAMESPACE) -f -

restart: ## Pick up a re-imported image with the same tag
	$(KUBECTL) -n $(NAMESPACE) rollout restart deployment/agentic-controller
	$(KUBECTL) -n $(NAMESPACE) rollout status deployment/agentic-controller --timeout=120s

dev: image load deploy restart ## Build, import, apply and restart

undeploy:
	helm template agentic-controller $(CHART) -n $(NAMESPACE) | $(KUBECTL) delete -n $(NAMESPACE) --ignore-not-found -f -

logs:
	$(KUBECTL) -n $(NAMESPACE) logs deploy/agentic-controller -f

example: ## Apply the example AgentRepository (create the github-token Secret first, see examples/)
	$(KUBECTL) create namespace agentic-demo --dry-run=client -o yaml | $(KUBECTL) apply -f -
	$(KUBECTL) apply -f - < $(REPO_ROOT)/examples/agentrepository-dashboard.yaml
