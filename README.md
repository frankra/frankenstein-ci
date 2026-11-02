# Frankenstein-CI
Miniature end-to-end container factory

## Milestone 1: The Raw OCI Puller (Registry → Disk)

> Forget the `docker` CLI exists. A registry is just a web server returning JSON and `.tar.gz` files.

*   **The Goal:** Write a Go CLI that talks to Chainguard's registry (`cgr.dev`) or Docker Hub, downloads an image, and unpacks its filesystem locally.
*   **Requirements (Pure Go):**
    1. Make an HTTP `GET` request to the registry API to fetch the OCI Manifest for an image (e.g., `cgr.dev/chainguard/static:latest` or `cgr.dev/chainguard/wolfi-base:latest`). *Note: You will need to handle the Bearer token auth handshake first.*
    2. Parse the JSON manifest to extract the list of layers (identified by their SHA256 digests).
    3. Iterate through the layers and download the `.tar.gz` blobs using HTTP `GET`.
    4. Use Go's `archive/tar` and `compress/gzip` to extract them in order into a local directory (e.g., `./images/<name>`).
*   **Success Criteria:** You can `cd` into `./images/static` and inspect `/etc/apko.json`, `/etc/passwd`, and `/usr/lib/apk/db/installed`.

---

## Milestone 2: The Nano-Runtime (Disk → Container)

> Now that you have a root filesystem on your disk, you are going to turn it into a container using Go system calls.

*   **The Goal:** Write a Go program that isolates a process inside the `rootfs` folder you just downloaded.
*   **Requirements (Pure Go):**
    1. Use `os/exec` to execute a command (like `/bin/sh`) but configure the `SysProcAttr` to spawn it in new namespaces: `syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS`.
    2. Before the command runs, the Go program must `syscall.Chroot("./rootfs")` and `syscall.Chdir("/")`.
    3. Mount the `proc` filesystem inside the chroot so processes can be tracked (`syscall.Mount("proc", "proc", "proc", 0, "")`).
*   **Success Criteria:** When you run your Go program, it drops you into a shell. If you type `ps`, you only see your shell (PID 1). If you type `hostname`, you can change it without affecting your actual host machine. You have just built Docker.

---

## Milestone 3: The Declarative Builder (Code → OCI Image)

> Now we throw away imperative Dockerfiles and use Chainguard's actual tooling to build an image from scratch.

*   **The Goal:** Compile a simple app and package it as an OCI image without ever running `docker build`.
*   **Requirements:**
    1. Write a simple 20-line Go HTTP server (`main.go`).
    2. Write a `melange.yaml` file. Use it to compile your Go code into an Alpine/Wolfi package (`.apk`).
    3. Write an `apko.yaml` file. Define your base system, add your newly built `.apk`, and use the `apko build` CLI to assemble them into a raw OCI image tarball.
*   **Success Criteria:** You end up with a file named `image.tar`. If you load it into Docker (`docker load < image.tar`) and run it, your Go HTTP server boots up.

---

## Milestone 4: The Pocket OCI Registry (OCI Distribution Spec Server)

> A container registry is just a content-addressable blob store (`sha256`) and a manifest pointer table over HTTP.

*   **The Goal:** Write a compliant OCI Distribution Spec registry server in Go (`cmd/registry/main.go`) that stores blobs and manifests on disk and supports `docker push`, `apko publish`, `cosign`, and `cmd/pull.go`.
*   **Requirements (Pure Go):**
    1. Implement `GET /v2/` version check handshake (`Docker-Distribution-API-Version: registry/2.0`).
    2. Implement content-addressable blob storage (`HEAD /v2/<name>/blobs/<digest>` and `GET /v2/<name>/blobs/<digest>`, plus HTTP `Range` requests for lazy loading).
    3. Implement monolithic & chunked blob uploads (`POST /v2/<name>/blobs/uploads/`, `PATCH`, and `PUT ?digest=sha256:...`), verifying the `sha256` digest on the fly before committing the blob to disk.
    4. Implement manifest storage & tag resolution (`PUT` and `GET /v2/<name>/manifests/<reference>`), supporting OCI Image Index, OCI Image Manifest, and the **OCI 1.1 Referrers API** (`GET /v2/<name>/referrers/<digest>`) for attaching SBOMs and signatures.
*   **Success Criteria:** You can push your `apko`-built image directly to `localhost:5000/my-app:latest`, pull it back with `cmd/pull.go` or `docker pull`, and see the deduplicated `sha256` blobs on disk.

---

## Milestone 5: Cryptographic Provenance (Sign & Verify)

> A supply chain is only secure if it's verifiable.

*   **The Goal:** Sign your newly built image in your own OCI registry and verify the signature and SBOM attestation to prove it hasn't been tampered with.
*   **Requirements:**
    1. Push your `apko`-built image to your local OCI registry (`localhost:5000/my-app:latest`) or an ephemeral registry (`ttl.sh`).
    2. Install `cosign`.
    3. Generate a keypair: `cosign generate-key-pair`.
    4. Sign the image in the registry using your private key: `cosign sign --key cosign.key localhost:5000/my-app:latest`.
    5. Verify the image using your public key (`cosign verify --key cosign.pub localhost:5000/my-app:latest`), and inspect how `cosign` stored the signature artifact in your registry!
*   **Success Criteria:** The `cosign verify` command returns a JSON payload showing the cryptographic signature and the exact digest of the image, and you can inspect the `.sig` / Referrers manifest directly inside your registry's storage folder.
