package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/frankra/frankenstein-ci/pkg/host"
	"github.com/frankra/frankenstein-ci/pkg/rest"
	"golang.org/x/sync/errgroup"
)

const (
	LAYERS_FOLDER string = ".layers" //contains the unzipped tar.gz layer files for the image
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

	dir, err := makeImageDir(imageName)
	if err != nil {
		panic(err)
	}

	err = parallelDownload(ctx, auth, imageName, manifest.Layers, dir)
	if err != nil {
		panic(err)
	}

	err = unzipLayers(manifest.Layers, dir)
	if err != nil {
		panic(err)
	}

	err = cleanupLayers(dir)
	if err != nil {
		panic(err)
	}
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

func parallelDownload(
	ctx context.Context,
	auth AuthResponse,
	imageName string,
	layers []Layer,
	dir string,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	g := errgroup.Group{}
	for _, layer := range layers {
		g.Go(func() error {
			return downloadLayer(
				ctx,
				auth,
				imageName,
				layer,
				dir,
			)
		})
	}

	err := g.Wait()
	if err != nil {
		cancel() //cancel other downloads
		return fmt.Errorf("failed to download layers, aborting, %w", err)
	}

	return nil
}

// makes the folder where the image content will be maintained
func makeImageDir(imageName string) (string, error) {
	dir := fmt.Sprintf("./images/%s", imageName)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return dir, fmt.Errorf("failed to create %s dir: %w", imageName, err)
	}
	layersFolder := fmt.Sprintf("%s/%s", dir, LAYERS_FOLDER)
	err = os.MkdirAll(layersFolder, 0755)
	if err != nil {
		return dir, fmt.Errorf("failed to create layers folder dir for %s : %w", imageName, err)
	}
	// return original base dir
	return dir, nil
}

func downloadLayer(
	ctx context.Context,
	auth AuthResponse,
	imageName string,
	layer Layer,
	dir string,
) error {
	client := http.Client{}
	url := fmt.Sprintf("https://registry-1.docker.io/v2/library/%s/blobs/%s", imageName, layer.Digest)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+auth.Token)
	req.Header.Set("Accept", layer.MediaType)

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download layer %s: %w", layer.Digest, err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}

	// Create a safe filename (digests usually contain 'sha256:', which is invalid on some file systems)
	safeFileName := strings.ReplaceAll(layer.Digest, ":", "-") + ".tar.gz"
	filePath := filepath.Join(dir, LAYERS_FOLDER, safeFileName)

	outFile, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file for layer %s: %w", layer.Digest, err)
	}
	defer outFile.Close()

	// io.Copy streams the data in chunks (default 32KB) directly to the file
	if _, err := io.Copy(outFile, res.Body); err != nil {
		return fmt.Errorf("failed to write layer %s to disk: %w", layer.Digest, err)
	}

	return nil
}

// unzipping layers has to be sequential as per OCI
func unzipLayers(layers []Layer, dir string) error {
	for _, layer := range layers {
		err := unzipLayer(layer, dir)
		if err != nil {
			return err
		}
	}

	return nil
}

func unzipLayer(layer Layer, dir string) error {
	filePath := buildLayerFilePath(dir, layer.Digest)
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	gzf, err := gzip.NewReader(file)
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
		targetPath := filepath.Join(dir, header.Name)

		// Docker whiteout handling
		fileName := filepath.Base(header.Name)
		if strings.HasPrefix(fileName, ".wh.") {
			targetToDelete := filepath.Join(filepath.Dir(targetPath), strings.TrimPrefix(fileName, ".wh."))
			os.RemoveAll(targetToDelete)
			continue // Skip creating the .wh. file
		}

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
			linkTarget := filepath.Join(dir, header.Linkname)
			if err := os.Link(linkTarget, targetPath); err != nil {
				return fmt.Errorf("failed to create hardlink: %w", err)
			}
		}
	}
	return nil
}

// builds the layer's file path
func buildLayerFilePath(dir string, digest string) string {
	// Create a safe filename (digests usually contain 'sha256:', which is invalid on some file systems)
	safeFileName := strings.ReplaceAll(digest, ":", "-") + ".tar.gz"
	return filepath.Join(dir, LAYERS_FOLDER, safeFileName)
}

// removes all content of .layers folder for the image downloaded
func cleanupLayers(dir string) error {
	return os.RemoveAll(filepath.Join(dir, LAYERS_FOLDER))
}
