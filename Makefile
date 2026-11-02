# Frankenstein-CI Makefile
# Automates the end-to-end Chainguard container build & provenance pipeline:
#   1. melange: Compile Go source -> signed Wolfi .apk package
#   2. apko:    Assemble .apk + Wolfi base -> distroless OCI image + SPDX SBOMs
#   3. cosign:  Cryptographically sign & verify the OCI image and attach SBOM attestations

SHELL := /bin/bash

ARCH ?= $(shell uname -m)
IMAGE_TAG ?= ttl.sh/frankenstein-runtime:1h
LOCAL_IMAGE_TAG ?= frankenstein-runtime:latest

MELANGE_DIR := experiments/melange
APKO_DIR    := experiments/apko
COSIGN_DIR  := experiments/cosign
RUNTIME_DIR := experiments/runtime

MELANGE_IMAGE := cgr.dev/chainguard/melange:latest
APKO_IMAGE    := cgr.dev/chainguard/apko:latest

.PHONY: help setup keys build-apk build-image load-image run-image publish-image sign verify attest verify-attestation all clean

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ==============================================================================
# 1. Environment & Key Setup
# ==============================================================================

setup: ## Pull Chainguard melange & apko images and check cosign installation
	@echo "==> Pulling Chainguard melange & apko container images..."
	docker pull $(MELANGE_IMAGE)
	docker pull $(APKO_IMAGE)
	@if ! command -v cosign >/dev/null 2>&1; then \
		echo "==> Installing cosign via go install..."; \
		go install github.com/sigstore/cosign/v2/cmd/cosign@latest; \
	else \
		echo "==> cosign is already installed: $$(cosign version | head -n 1)"; \
	fi

keys: ## Generate signing keypairs for melange (.apk) and cosign (OCI provenance)
	@if [ ! -f "$(MELANGE_DIR)/melange.rsa" ]; then \
		echo "==> Generating melange RSA keypair for APK signing..."; \
		docker run --rm -v "$(CURDIR):/work" -w /work $(MELANGE_IMAGE) keygen $(MELANGE_DIR)/melange.rsa; \
	else \
		echo "==> Melange keypair already exists at $(MELANGE_DIR)/melange.rsa"; \
	fi
	@if [ ! -f "$(COSIGN_DIR)/cosign.key" ]; then \
		echo "==> Generating cosign keypair (using empty password for local automation)..."; \
		mkdir -p $(COSIGN_DIR); \
		COSIGN_PASSWORD="" cosign generate-key-pair --output-key-prefix="$(COSIGN_DIR)/cosign"; \
	else \
		echo "==> Cosign keypair already exists at $(COSIGN_DIR)/cosign.key"; \
	fi

# ==============================================================================
# 2. Milestone 3: Declarative Build (melange + apko)
# ==============================================================================

build-apk: keys ## Compile experiments/runtime/main.go into a signed Wolfi .apk package via melange
	@echo "==> Building frankenstein-runtime .apk with melange (arch: x86_64,aarch64)..."
	docker run --rm --privileged \
		-v "$(CURDIR):/work" \
		-w /work \
		$(MELANGE_IMAGE) build $(MELANGE_DIR)/melange.yaml \
		--arch x86_64,aarch64 \
		--source-dir /work/$(RUNTIME_DIR) \
		--signing-key /work/$(MELANGE_DIR)/melange.rsa \
		--out-dir /work/$(MELANGE_DIR)/packages
	@echo "==> Built APK packages in $(MELANGE_DIR)/packages/"

build-image: build-apk ## Assemble distroless OCI image tarball + SPDX SBOMs via apko
	@echo "==> Assembling OCI image tarball and SPDX SBOMs with apko..."
	docker run --rm \
		-v "$(CURDIR):/work" \
		-w /work \
		$(APKO_IMAGE) build \
		/work/$(APKO_DIR)/apko.yaml \
		$(LOCAL_IMAGE_TAG) \
		/work/$(APKO_DIR)/image.tar \
		--sbom-path /work/$(APKO_DIR)
	@echo "==> Image tarball written to $(APKO_DIR)/image.tar"

load-image: build-image ## Load the apko-built OCI image tarball into local Docker
	@echo "==> Loading $(APKO_DIR)/image.tar into Docker..."
	docker load < $(APKO_DIR)/image.tar

run-image: load-image ## Run the distroless Wolfi container locally on port 8080
	@echo "==> Starting container on http://localhost:8080 ..."
	docker run --rm -p 8080:8080 $(LOCAL_IMAGE_TAG)

# ==============================================================================
# 3. Milestone 5: Publish & Cryptographic Provenance (apko publish + cosign)
# ==============================================================================

publish-image: build-apk ## Publish multi-arch OCI image directly to registry (default: ttl.sh) without Docker
	@echo "==> Publishing OCI image directly to $(IMAGE_TAG) via apko publish..."
	docker run --rm \
		-v "$(CURDIR):/work" \
		-w /work \
		$(APKO_IMAGE) publish \
		/work/$(APKO_DIR)/apko.yaml \
		$(IMAGE_TAG) \
		--sbom-path /work/$(APKO_DIR) > $(COSIGN_DIR)/published-image.ref
	@echo "==> Published image digest: $$(cat $(COSIGN_DIR)/published-image.ref)"

sign: keys publish-image ## Sign the published OCI image digest using cosign
	@IMAGE_DIGEST=$$(cat $(COSIGN_DIR)/published-image.ref); \
	echo "==> Signing $$IMAGE_DIGEST with cosign..."; \
	COSIGN_PASSWORD="" cosign sign --yes --key $(COSIGN_DIR)/cosign.key "$$IMAGE_DIGEST"

verify: ## Verify the cryptographic signature on the published OCI image
	@IMAGE_DIGEST=$$(cat $(COSIGN_DIR)/published-image.ref); \
	echo "==> Verifying signature for $$IMAGE_DIGEST..."; \
	cosign verify --key $(COSIGN_DIR)/cosign.pub "$$IMAGE_DIGEST"

attest: keys ## Attach the apko SPDX SBOM as a signed in-toto attestation on the image
	@IMAGE_DIGEST=$$(cat $(COSIGN_DIR)/published-image.ref); \
	echo "==> Attaching SPDX SBOM attestation to $$IMAGE_DIGEST..."; \
	COSIGN_PASSWORD="" cosign attest --yes \
		--key $(COSIGN_DIR)/cosign.key \
		--type spdxjson \
		--predicate $(APKO_DIR)/sbom-index.spdx.json \
		"$$IMAGE_DIGEST"

verify-attestation: ## Verify the signed SPDX SBOM attestation on the published image
	@IMAGE_DIGEST=$$(cat $(COSIGN_DIR)/published-image.ref); \
	echo "==> Verifying SPDX SBOM attestation for $$IMAGE_DIGEST..."; \
	cosign verify-attestation \
		--key $(COSIGN_DIR)/cosign.pub \
		--type spdxjson \
		"$$IMAGE_DIGEST" | head -c 500 && echo "..."

all: sign verify attest verify-attestation ## Run full end-to-end pipeline: keys -> melange -> apko publish -> cosign sign & attest -> verify

clean: ## Clean generated packages, tarballs, and temporary files
	rm -rf $(MELANGE_DIR)/packages $(APKO_DIR)/image.tar $(COSIGN_DIR)/published-image.ref
