package container

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"strings"
)

//go:embed image/Dockerfile image/*.sh image/*.conf image/proxy/main.go
var imageFiles embed.FS

// labelImageHash records which version of the embedded build context an
// image was built from, so outdated images are rebuilt automatically.
const labelImageHash = "exile.image-hash"

// BuildOptions controls BuildImage.
type BuildOptions struct {
	NoCache bool // rebuild all layers, e.g. to update the agents
	Pull    bool // pull a newer base image
}

// BuildImage builds image from the embedded Dockerfile.
func BuildImage(image string, opts BuildOptions) error {
	context, hash, err := imageContext()
	if err != nil {
		return err
	}
	args := []string{"build", "--tag", image, "--label", labelImageHash + "=" + hash}
	if opts.NoCache {
		args = append(args, "--no-cache")
	}
	if opts.Pull {
		args = append(args, "--pull")
	}
	cmd := exec.Command("docker", append(args, "-")...)
	cmd.Stdin = bytes.NewReader(context)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build: %w", err)
	}
	return nil
}

// ImageCurrent reports whether image exists and was built from the embedded
// build context.
func ImageCurrent(image string) (bool, error) {
	_, hash, err := imageContext()
	if err != nil {
		return false, err
	}
	out, err := exec.Command("docker", "image", "inspect",
		"--format", fmt.Sprintf("{{index .Config.Labels %q}}", labelImageHash), image).Output()
	return err == nil && strings.TrimSpace(string(out)) == hash, nil
}

// imageContext returns the embedded build context as a tar archive along
// with a hash of its contents.
func imageContext() ([]byte, string, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	h := sha256.New()
	err := fs.WalkDir(imageFiles, "image", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := imageFiles.ReadFile(p)
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(p, "image/")
		fmt.Fprintf(h, "%s\x00%d\x00", name, len(data))
		h.Write(data)

		mode := int64(0o644)
		if path.Ext(name) == ".sh" {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data))}); err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	if err := tw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), hex.EncodeToString(h.Sum(nil))[:16], nil
}
