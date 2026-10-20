package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/frankra/frankenstein-ci/pkg/host"
)

type Layer struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int    `json:"size"`
}
type Manifest struct {
	Layers []Layer `json:"layers"`
}

type ManifestMetadata struct {
	Digest   string `json:"digest"`
	Platform struct {
		Architecture string `json:"architecture"`
		OS           string `json:"os"`
	} `json:"platform"`
}
type ManifestsResponse struct {
	Manifests []ManifestMetadata `json:"manifests"`
}

type AuthResponse struct {
	Token       string    `json:"token"`
	AccessToken string    `json:"access_token"`
	ExpiresIn   int       `json:"expires_in"`
	IssuedAt    time.Time `json:"issued_at"`
}

func main() {
	imageName := "alpine" //TODO: make param
	client := http.Client{}

	auth, err := getAuth(client, imageName)
	if err != nil {
		panic(fmt.Sprintf("failed to auth, %s", err.Error()))
	}
	manifests, err := getManifests(client, auth, imageName)
	if err != nil {
		panic(fmt.Sprintf("failed to fetch manifests, %s", err.Error()))
	}

	manifestMetadata, err := findManifestForCurrentArch(manifests)
	if err != nil {
		panic(err)
	}

	manifest, err := getManifest(client, auth, imageName, manifestMetadata)
	if err != nil {
		panic(err)
	}

	downloadLayers(client, auth, imageName, manifest.Layers)
}

func findManifestForCurrentArch(resp ManifestsResponse) (ManifestMetadata, error) {
	hostArch := host.GetMachineArch()
	for _, manifest := range resp.Manifests {
		if manifest.Platform.Architecture == hostArch {
			return manifest, nil
		}
	}

	return ManifestMetadata{}, fmt.Errorf("could not find a manifest for arch %s", hostArch)
}

func getAuth(client http.Client, imageName string) (AuthResponse, error) {
	auth := AuthResponse{}
	authUrl := fmt.Sprintf("https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/%s:pull", imageName)
	res, err := client.Get(authUrl)
	if err != nil {
		return auth, fmt.Errorf("failed to authenticate, %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return auth, fmt.Errorf("failed to read the body, %w", err)
	}

	err = json.Unmarshal(body, &auth)
	if err != nil {
		return auth, fmt.Errorf("failed to unmarshal the body as object, %w", err)
	}

	return auth, err
}

func getManifests(client http.Client, auth AuthResponse, imageName string) (ManifestsResponse, error) {
	manifest := ManifestsResponse{}
	url := fmt.Sprintf("https://registry-1.docker.io/v2/library/%s/manifests/latest", imageName)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return manifest, fmt.Errorf("failed to create request, %w", err)
	}
	req.Header.Add("Authorization", "Bearer "+auth.AccessToken)
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.v2+json")

	res, err := client.Do(req)
	if err != nil {
		return manifest, fmt.Errorf("failed to get the manifests, %w", err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return manifest, fmt.Errorf("failed to read the body, %w", err)
	}

	err = json.Unmarshal(body, &manifest)
	if err != nil {
		return manifest, fmt.Errorf("failed to unmarshal the body as object, %w", err)
	}

	return manifest, err
}

func getManifest(client http.Client, auth AuthResponse, imageName string, metadata ManifestMetadata) (Manifest, error) {
	manifest := Manifest{}
	url := fmt.Sprintf("https://registry-1.docker.io/v2/library/%s/blobs/%s", imageName, metadata.Digest)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return manifest, fmt.Errorf("failed to create request, %w", err)
	}
	req.Header.Add("Authorization", "Bearer "+auth.AccessToken)
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.v2+json")

	res, err := client.Do(req)
	if err != nil {
		return manifest, fmt.Errorf("failed to get the manifest, %w", err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return manifest, fmt.Errorf("failed to read the body, %w", err)
	}

	err = json.Unmarshal(body, &manifest)
	if err != nil {
		return manifest, fmt.Errorf("failed to unmarshal the body as object, %w", err)
	}

	return manifest, err
}

func downloadLayers(client http.Client, auth AuthResponse, imageName string, layers []Layer) error {
	extractDir := "./rootfs"
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return fmt.Errorf("failed to create rootfs dir: %w", err)
	}

	for _, layer := range layers {
		url := fmt.Sprintf("https://registry-1.docker.io/v2/library/%s/blobs/%s", imageName, layer.Digest)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+auth.Token)
		req.Header.Set("Accept", layer.MediaType)

		res, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to download layer %s: %w", layer, err)
		}

		// Defer closing the body inside the loop via an anonymous function
		// to avoid leaking file descriptors if we parallelize later.
		err = func() error {
			defer res.Body.Close()

			if res.StatusCode != http.StatusOK {
				return fmt.Errorf("unexpected status code: %d", res.StatusCode)
			}

			gzf, err := gzip.NewReader(res.Body)
			if err != nil {
				return fmt.Errorf("failed to create gzip reader: %w", err)
			}
			defer gzf.Close()

			tarReader := tar.NewReader(gzf)

			for {
				header, err := tarReader.Next()
				if err == io.EOF {
					break // End of this layer's archive
				}
				if err != nil {
					return fmt.Errorf("tar reader error: %w", err)
				}

				targetPath := filepath.Join(extractDir, header.Name)

				// Some tarballs omit explicit directory entries, so we ensure the parent exists
				if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
					return fmt.Errorf("failed to create parent dir: %w", err)
				}

				switch header.Typeflag {
				case tar.TypeDir:
					if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
						return fmt.Errorf("failed to create directory: %w", err)
					}
				case tar.TypeReg:
					outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode))
					if err != nil {
						return fmt.Errorf("failed to create file: %w", err)
					}
					if _, err := io.Copy(outFile, tarReader); err != nil {
						outFile.Close()
						return fmt.Errorf("failed to extract file: %w", err)
					}
					outFile.Close()
				case tar.TypeSymlink:
					// If a symlink already exists from a previous layer, remove it
					os.Remove(targetPath)
					if err := os.Symlink(header.Linkname, targetPath); err != nil {
						return fmt.Errorf("failed to create symlink: %w", err)
					}
				case tar.TypeLink:
					os.Remove(targetPath)
					// Hardlinks in tar point to paths relative to the tar root, so we prepend extractDir
					linkTarget := filepath.Join(extractDir, header.Linkname)
					if err := os.Link(linkTarget, targetPath); err != nil {
						return fmt.Errorf("failed to create hardlink: %w", err)
					}
				}
			}
			return nil
		}()

		if err != nil {
			return err
		}
	}
	return nil
}
