# Frankenstein-CI
Miniature end-to-end container factory

## Milestone 1: The Raw OCI Puller (Registry → Disk)

> Forget the `docker` CLI exists. A registry is just a web server returning JSON and `.tar.gz` files.

*   **The Goal:** Write a Go CLI that talks to Docker Hub (or `cgr.dev`), downloads an image, and unpacks its filesystem locally.
*   **Requirements (Pure Go):**
    1. Make an HTTP `GET` request to the registry API to fetch the OCI Manifest for an image (e.g., `alpine:latest` or `cgr.dev/chainguard/static:latest`). *Note: You will need to handle the Bearer token auth handshake first.*
    2. Parse the JSON manifest to extract the list of layers (identified by their SHA256 digests).
    3. Iterate through the layers and download the `.tar.gz` blobs using HTTP `GET`.
    4. Use Go's `archive/tar` and `compress/gzip` to extract them in order into a local directory (e.g., `./rootfs`).
*   **Success Criteria:** You can `cd` into `./rootfs` and see `bin/`, `etc/`, `usr/`, etc.

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

## Milestone 4: Cryptographic Provenance (Sign & Verify)

> A supply chain is only secure if it's verifiable.

*   **The Goal:** Sign your newly built image and verify the signature to prove it hasn't been tampered with.
*   **Requirements:**
    1. Push your apko-built image to a temporary registry. (Use `ttl.sh`—it's an anonymous, ephemeral registry perfect for testing. E.g., push to `ttl.sh/rafael-test-image:1h`).
    2. Install `cosign`.
    3. Generate a keypair: `cosign generate-key-pair`.
    4. Sign the image in the registry using your private key: `cosign sign --key cosign.key ttl.sh/rafael-test-image:1h`.
    5. Verify the image using your public key.
*   **Success Criteria:** The `cosign verify` command returns a JSON payload showing the cryptographic signature and the exact digest of the image, proving the provenance of the artifact.