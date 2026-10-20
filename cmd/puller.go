package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/frankra/frankenstein-ci/pkg/host"
	"github.com/frankra/frankenstein-ci/pkg/rest"
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
	if len(os.Args) < 2 && os.Args[1] == "" {
		panic("command requires an image name as argument")
	}
	imageName := os.Args[1]

	ctx := context.Background()

	auth, err := getAuth(ctx, imageName)
	if err != nil {
		panic(fmt.Sprintf("failed to auth, %s", err.Error()))
	}
	manifests, err := getManifests(ctx, auth, imageName)
	if err != nil {
		panic(fmt.Sprintf("failed to fetch manifests, %s", err.Error()))
	}

	manifestMetadata, err := findManifestForCurrentArch(manifests)
	if err != nil {
		panic(err)
	}

	manifest, err := getManifest(ctx, auth, imageName, manifestMetadata)
	if err != nil {
		panic(err)
	}

	fmt.Print(manifest)

	// processLayers(client, auth, imageName, manifest.Layers)
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

func getAuth(ctx context.Context, imageName string) (AuthResponse, error) {
	authUrl := fmt.Sprintf("https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/%s:pull", imageName)

	client := rest.NewReadClient[AuthResponse]()
	auth, err := client.GetJSON(
		ctx,
		authUrl,
		nil,
	)
	if err != nil {
		return AuthResponse{}, fmt.Errorf("failed to get auth token, %w", err)
	}
	return *auth, nil
}

func getManifests(ctx context.Context, auth AuthResponse, imageName string) (ManifestsResponse, error) {
	url := fmt.Sprintf("https://registry-1.docker.io/v2/library/%s/manifests/latest", imageName)
	client := rest.NewReadClient[ManifestsResponse]()

	headers := map[string]string{
		"Authorization": "Bearer " + auth.AccessToken,
		"Accept":        "application/vnd.docker.distribution.manifest.v2+json",
	}

	res, err := client.GetJSON(ctx, url, headers)
	if err != nil {
		return ManifestsResponse{}, fmt.Errorf("failed to get manifests for image %s, %w", imageName, err)
	}
	return *res, err
}

func getManifest(ctx context.Context, auth AuthResponse, imageName string, metadata ManifestMetadata) (Manifest, error) {
	url := fmt.Sprintf("https://registry-1.docker.io/v2/library/%s/blobs/%s", imageName, metadata.Digest)
	client := rest.NewReadClient[Manifest]()

	headers := map[string]string{
		"Authorization": "Bearer " + auth.AccessToken,
		"Accept":        "application/vnd.docker.distribution.manifest.v2+json",
	}

	res, err := client.GetJSON(ctx, url, headers)
	if err != nil {
		return Manifest{}, fmt.Errorf("failed to get manifest for image %s, %w", imageName, err)
	}
	return *res, err
}

// func processLayers(client http.Client, auth AuthResponse, imageName string, layers []Layer) error {
// 	extractDir := fmt.Sprintf("./images/%s", imageName)
// 	err := os.MkdirAll(extractDir, 0755)
// 	if err != nil {
// 		return fmt.Errorf("failed to create %s dir: %w", imageName, err)
// 	}

// 	g := errgroup.Group{}
// 	for _, layer := range layers {
// 		g.Go(func() error {
// 			return pipeGzip(
// 				client,
// 				auth,
// 				imageName,
// 				layer,
// 				extractDir,
// 			)
// 		})
// 	}

// 	err = g.Wait()
// 	if err != nil {
// 		return fmt.Errorf("failed to download layers, %w", err)
// 	}

// 	return nil
// }

// func pipeGzip(
// 	client http.Client,
// 	auth AuthResponse,
// 	imageName string,
// 	layer Layer,
// 	extractDir string,
// ) error {
// 	url := fmt.Sprintf("https://registry-1.docker.io/v2/library/%s/blobs/%s", imageName, layer.Digest)

// 	req, err := http.NewRequest("GET", url, nil)
// 	if err != nil {
// 		return fmt.Errorf("failed to create request: %w", err)
// 	}
// 	req.Header.Set("Authorization", "Bearer "+auth.Token)
// 	req.Header.Set("Accept", layer.MediaType)

// 	res, err := client.Do(req)
// 	if err != nil {
// 		return fmt.Errorf("failed to download layer %s: %w", layer.Digest, err)
// 	}
// 	defer res.Body.Close()

// 	if res.StatusCode != http.StatusOK {
// 		return fmt.Errorf("unexpected status code: %d", res.StatusCode)
// 	}

// }

// func unzipLayer() {
// 	gzf, err := gzip.NewReader(res.Body)
// 	if err != nil {
// 		return fmt.Errorf("failed to create gzip reader: %w", err)
// 	}
// 	defer gzf.Close()

// 	tarReader := tar.NewReader(gzf)

// 	for {
// 		header, err := tarReader.Next()
// 		if err == io.EOF {
// 			break // End of this layer's archive
// 		}
// 		if err != nil {
// 			return fmt.Errorf("tar reader error: %w", err)
// 		}

// 		targetPath := filepath.Join(extractDir, header.Name)

// 		// Some tarballs omit explicit directory entries, so we ensure the parent exists
// 		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
// 			return fmt.Errorf("failed to create parent dir: %w", err)
// 		}

// 		switch header.Typeflag {
// 		case tar.TypeDir:
// 			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
// 				return fmt.Errorf("failed to create directory: %w", err)
// 			}
// 		case tar.TypeReg:
// 			outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode))
// 			if err != nil {
// 				return fmt.Errorf("failed to create file: %w", err)
// 			}
// 			if _, err := io.Copy(outFile, tarReader); err != nil {
// 				outFile.Close()
// 				return fmt.Errorf("failed to extract file: %w", err)
// 			}
// 			outFile.Close()
// 		case tar.TypeSymlink:
// 			// If a symlink already exists from a previous layer, remove it
// 			os.Remove(targetPath)
// 			if err := os.Symlink(header.Linkname, targetPath); err != nil {
// 				return fmt.Errorf("failed to create symlink: %w", err)
// 			}
// 		case tar.TypeLink:
// 			os.Remove(targetPath)
// 			// Hardlinks in tar point to paths relative to the tar root, so we prepend extractDir
// 			linkTarget := filepath.Join(extractDir, header.Linkname)
// 			if err := os.Link(linkTarget, targetPath); err != nil {
// 				return fmt.Errorf("failed to create hardlink: %w", err)
// 			}
// 		}
// 	}
// 	return nil
// }
