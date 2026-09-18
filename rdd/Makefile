# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: SUSE LLC
# SPDX-FileCopyrightText: The Rancher Desktop Authors
# SPDX-FileCopyrightText: The KCP Authors

EXE := $(if $(shell sh -c 'command -v winver.exe'),.exe,)
KUBE_VERSION := $(shell go$(EXE) list -m -f '{{.Version}}' 'k8s.io/kubernetes')
KUBE_MAJOR_VERSION := $(shell echo $(KUBE_VERSION) | sed 's/v\([0-9]*\).*/\1/')
KUBE_MINOR_VERSION := $(shell echo $(KUBE_VERSION) | sed "s/v[0-9]*\.\([0-9]*\).*/\1/")
GIT_COMMIT := $(shell git$(EXE) rev-parse --short HEAD || echo 'local')
GIT_DIRTY := $(shell git$(EXE) diff --quiet && echo 'clean' || echo 'dirty')
GIT_VERSION := $(KUBE_VERSION)+rdd-$(shell git$(EXE) describe --tags --match='v*' --abbrev=14 "$(GIT_COMMIT)^{commit}" 2>/dev/null || echo v0.0.0-$(GIT_COMMIT))
RDD_VERSION := $(shell git$(EXE) describe --match 'v[0-9]*' --dirty='.m' --always --tags)
RDD_VERSION_TRIMMED := $(RDD_VERSION:v%=%)
LIMA_VERSION := $(shell go$(EXE) list -m -f '{{.Version}}' 'github.com/lima-vm/lima/v2')
BUILD_DATE := $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
PACKAGE := github.com/rancher-sandbox/rancher-desktop-daemon
LDFLAGS := \
	-X k8s.io/client-go/pkg/version.gitCommit=${GIT_COMMIT} \
	-X k8s.io/client-go/pkg/version.gitTreeState=${GIT_DIRTY} \
	-X k8s.io/client-go/pkg/version.gitVersion=${GIT_VERSION} \
	-X k8s.io/client-go/pkg/version.gitMajor=${KUBE_MAJOR_VERSION} \
	-X k8s.io/client-go/pkg/version.gitMinor=${KUBE_MINOR_VERSION} \
	-X k8s.io/client-go/pkg/version.buildDate=${BUILD_DATE} \
	\
	-X k8s.io/component-base/version.gitCommit=${GIT_COMMIT} \
	-X k8s.io/component-base/version.gitTreeState=${GIT_DIRTY} \
	-X k8s.io/component-base/version.gitVersion=${GIT_VERSION} \
	-X k8s.io/component-base/version.gitMajor=${KUBE_MAJOR_VERSION} \
	-X k8s.io/component-base/version.gitMinor=${KUBE_MINOR_VERSION} \
	-X k8s.io/component-base/version.buildDate=${BUILD_DATE} \
	\
	-X $(PACKAGE)/pkg/version.Version=$(RDD_VERSION) \
	-X $(PACKAGE)/pkg/version.GitCommit=${GIT_COMMIT} \
	-X $(PACKAGE)/pkg/version.BuildDate=${BUILD_DATE} \
	-X github.com/lima-vm/lima/v2/pkg/version.Version=$(LIMA_VERSION) \
	-s -w

TAGS := sqlite_omit_load_extension
# CGO_CFLAGS is part of every cgo package's build cache key, so bin/rdd, test
# and lint-rdd all pass the same value. A target that left the flags out would
# recompile go-sqlite3 instead of reusing the others' build cache.
SQLITE_CFLAGS := -DSQLITE_ENABLE_DBSTAT_VTAB=1 -DSQLITE_USE_ALLOCA=1

# DISTRO_IMAGE is the guest distro image bin/rdd and bin/lima-controller embed
# under the with_distro tag. WSL2 imports a rootfs tarball, and Lima's other
# drivers boot a raw disk image.
#
# The build target is a GOOS or GOARCH set on the make command line, which GNU
# make 3.81 hides from $(shell), or else what `go env` reports. The recipes
# that build binaries pass the target to go explicitly, through WSLENV too, so
# bin/rdd and bin/lima-controller embed the image and guest agent staged for
# the target they are compiled for.
TARGET_OS := $(if $(filter command line,$(origin GOOS)),$(GOOS),$(shell go$(EXE) env GOOS))
TARGET_ARCH := $(if $(filter command line,$(origin GOARCH)),$(GOARCH),$(shell go$(EXE) env GOARCH))
HOST_OS := $(shell go$(EXE) env GOHOSTOS)
HOST_ARCH := $(shell go$(EXE) env GOHOSTARCH)
DISTRO_IMAGE := pkg/embedded/distro.$(if $(filter windows,$(TARGET_OS)),tar,raw).xz

# The guest agent's file name carries the target architecture, and
# pkg/guestagent embeds the one matching GOARCH, so a build for another
# architecture builds its own agent instead of reusing a stale one.
GUESTAGENT_BINARY := pkg/guestagent/lima-guestagent.$(TARGET_ARCH)
GUESTAGENT_GZ := $(GUESTAGENT_BINARY).gz

default: build-rdd
.PHONY: default

ldflags:
	@echo $(LDFLAGS)

build: build-rdd build-all-controllers
.PHONY: build

# Code signing for macOS - required for Virtualization.framework
# On macOS, sign binaries with virtualization entitlement using ad-hoc signature
# On other platforms, this is a no-op
ifeq ($(shell uname -s),Darwin)
define ATTACH_ENTITLEMENTS
codesign --force --verbose --entitlements macos-entitlements.plist --sign - $(1)
endef
else
define ATTACH_ENTITLEMENTS
@true
endef
endif

GOLANG_SOURCES := $(shell find . -name '*.go') go.mod go.sum
EMBEDDED_SOURCES := $(shell find ./cmd ./pkg -name '*.yaml' -o -name '*.json')

# Build the Lima guest agent from our Go dependency and compress it for embedding.
$(GUESTAGENT_BINARY): go.mod go.sum
	WSLENV=${WSLENV}:CGO_ENABLED:GOOS:GOARCH \
	CGO_ENABLED=0 GOOS=linux GOARCH=$(TARGET_ARCH) \
	go$(EXE) build -ldflags="-s -w" -o $@ \
		github.com/lima-vm/lima/v2/cmd/lima-guestagent

$(GUESTAGENT_GZ): $(GUESTAGENT_BINARY)
	gzip --to-stdout $< > $@

.INTERMEDIATE: $(GUESTAGENT_BINARY)

# The downloader runs on every build, because the staged file name has no
# architecture in it and only a rerun notices a build for a different one. It
# leaves an up-to-date file untouched, so make does not relink bin/rdd.
# Naming the host makes `go run` build the tool for this machine; an empty
# GOOS or GOARCH would fall back to a target saved with `go env -w`.
$(DISTRO_IMAGE): FORCE
	WSLENV=${WSLENV}:GOOS:GOARCH GOOS=$(HOST_OS) GOARCH=$(HOST_ARCH) \
	go$(EXE) run ./cmd/download-guest-deps --os $(TARGET_OS) --arch $(TARGET_ARCH)

FORCE:
.PHONY: FORCE

# Only bin/rdd and bin/lima-controller embed the distro. Tests, lint and the
# other standalone controllers build without with_distro and need no image.
bin/rdd$(EXE): $(GOLANG_SOURCES) $(EMBEDDED_SOURCES) $(GUESTAGENT_GZ) $(DISTRO_IMAGE)
	WSLENV=${WSLENV}:CGO_CFLAGS:CGO_ENABLED:GOOS:GOARCH \
	CGO_CFLAGS="$(SQLITE_CFLAGS)" CGO_ENABLED=1 \
	GOOS=$(TARGET_OS) GOARCH=$(TARGET_ARCH) \
	go$(EXE) build -tags="$(TAGS) with_distro" -gcflags="all=${GCFLAGS}" -ldflags="$(LDFLAGS)" -o $@ ./cmd/rdd
	$(call ATTACH_ENTITLEMENTS,$@)
	ls -lh $@
build-rdd: bin/rdd$(EXE)
.PHONY: build-rdd

# API Group Controller management - Auto-discovery of API groups
CONTROLLERS := $(patsubst cmd/%-controller,%,$(wildcard cmd/*-controller))

# Per-controller CGO settings (default is 0)
# lima-controller requires CGO on macOS for the vz driver (Virtualization.framework)
ifeq ($(shell uname -s),Darwin)
CGO_ENABLED_lima := 1
endif

# The lima controller embeds the guest agent, and the distro it extracts for
# the App's VM.
TAGS_lima := with_distro
bin/lima-controller$(EXE): $(DISTRO_IMAGE) $(GUESTAGENT_GZ)

# Generate build targets for API group controllers
define CONTROLLER_TARGETS
bin/$(1)-controller$$(EXE): $$(GOLANG_SOURCES) $$(EMBEDDED_SOURCES)
	WSLENV=${WSLENV}:CGO_ENABLED:GOOS:GOARCH CGO_ENABLED=$$(or $$(CGO_ENABLED_$(1)),0) \
	GOOS=$(TARGET_OS) GOARCH=$(TARGET_ARCH) \
	go$$(EXE) build -tags="$$(strip $(TAGS) $$(TAGS_$(1)))" -gcflags="all=$${GCFLAGS}" -ldflags="$(LDFLAGS)" -o $$@ ./cmd/$(1)-controller
	$$(call ATTACH_ENTITLEMENTS,$$@)
	ls -lh $$@
build-$(1)-controller: bin/$(1)-controller$$(EXE)
.PHONY: build-$(1)-controller

test-$(1)-controllers:
	go$$(EXE) test -v ./pkg/controllers/$(1)/...
.PHONY: test-$(1)-controllers

run-$(1)-controller: bin/$(1)-controller$$(EXE)
	$$<
.PHONY: run-$(1)-controller
endef

# Generate targets for each API group
$(foreach controller,$(CONTROLLERS),$(eval $(call CONTROLLER_TARGETS,$(controller))))

# The mock controller depends on static test data.
bin/mock-controller$(EXE): $(wildcard pkg/controllers/mock/testdata/*.json)

# Meta targets
build-all-controllers: $(addprefix build-,$(addsuffix -controller,$(CONTROLLERS)))
.PHONY: build-all-controllers

test-all-controllers: $(addprefix test-,$(addsuffix -controllers,$(CONTROLLERS)))
.PHONY: test-all-controllers

run-all-controllers: $(addprefix run-,$(addsuffix -controller,$(CONTROLLERS)))
.PHONY: run-all-controllers

# Code generation targets
generate-deepcopy:
	scripts/generate-deepcopy.sh
.PHONY: generate-deepcopy

generate-crds:
	scripts/generate-crds.sh
.PHONY: generate-crds

# We do not run this as part of `make generate` as this requires the service
# to be buildable and runnable.
generate-typescript: bin/rdd$(EXE)
	../scripts/generate-rdd-client.sh
.PHONY: generate-typescript

generate: generate-deepcopy generate-crds
.PHONY: generate

verify-generate: generate generate-typescript
	@if [ -n "$$(git diff --name-only)" ]; then \
		printf "ERROR: Generated files are out of date. Run '%s' and commit the result.\n" \
			"make generate generate-typescript" >&2; \
		git diff --stat; \
		exit 1; \
	fi
.PHONY: verify-generate

run: bin/rdd$(EXE)
	$< service start
.PHONY: run

test: $(GOLANG_SOURCES) $(GUESTAGENT_GZ)
	CGO_CFLAGS="$(SQLITE_CFLAGS)" \
	go$(EXE) test -tags="$(TAGS)" ./...
.PHONY: test

lint-bats:
	$(MAKE) -C bats lint
.PHONY: lint-bats

# golangci-lint compiles the packages it analyses, so it fills the build cache
# that the other jobs restore.
lint-rdd: $(GUESTAGENT_GZ)
	CGO_CFLAGS="$(SQLITE_CFLAGS)" \
	go$(EXE) tool golangci-lint run --build-tags="$(TAGS)"
.PHONY: lint-rdd

lint: lint-bats lint-rdd
.PHONY: lint

format:
	go$(EXE) tool golangci-lint fmt
.PHONY: format

LTAG_EXCLUDES := \
	bats/lib \
	pkg/util/nxadmtail \
	$(GUESTAGENT_BINARY) \
	${NULL}
ltag:
	go$(EXE) tool ltag -v -t .ltag -path . --gitignore --excludes="${LTAG_EXCLUDES}"
check-ltag:
	go$(EXE) tool ltag -v -t .ltag -path . --gitignore --excludes="${LTAG_EXCLUDES}" --check
.PHONY: ltag check-ltag

BATS_TARGETS := $(shell MAKEFLAGS= $(MAKE) -C bats --print-data-base --question --no-builtin-variables | awk -F: '$$1 ~ /^bats-/ { print $$1 }')
$(BATS_TARGETS): bin/rdd$(EXE)
	@$(MAKE) -C bats $@
.PHONY: $(BATS_TARGETS)

check: test lint check-ltag
.PHONY: check

# clean's distro.*.xz* pattern also matches the .tmp-* file a killed
# downloader leaves beside the staged image.
clean:
	-rm -r bin
	-rm pkg/guestagent/lima-guestagent.*
	-rm pkg/embedded/distro.*.xz*
.PHONY: clean
