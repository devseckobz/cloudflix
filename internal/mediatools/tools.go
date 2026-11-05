// Package mediatools supplies Cloudflix's private, self-contained media tools.
package mediatools

import (
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

//go:embed all:payload
var payload embed.FS

var bundled = manager{files: payload}

type manager struct {
	mu    sync.Mutex
	files fs.FS
	ready map[string]string
}

// Resolve unpacks the release's tool into a private directory in the data
// filesystem. The legacy updater only has to install the Cloudflix executable.
// Source-only development builds may use tools already installed on PATH.
func Resolve(dataDir, name string) (string, error) { return bundled.resolve(dataDir, name) }

func (m *manager) resolve(dataDir, name string) (string, error) {
	if name != "ffmpeg" && name != "ffprobe" {
		return "", errors.New("unsupported media tool")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	compressed, err := m.files.Open("payload/" + name + ".gz")
	if errors.Is(err, fs.ErrNotExist) {
		return exec.LookPath(name)
	}
	if err != nil {
		return "", err
	}
	defer compressed.Close()
	sum, err := fs.ReadFile(m.files, "payload/"+name+".sha256")
	if err != nil {
		return "", err
	}
	expected := strings.TrimSpace(string(sum))
	decoded, err := hex.DecodeString(expected)
	if err != nil || len(decoded) != sha256.Size {
		return "", errors.New("invalid embedded media checksum")
	}
	base, err := filepath.Abs(dataDir)
	if err != nil {
		return "", err
	}
	key := base + "/" + name + "/" + expected
	if ready := m.ready[key]; ready != "" {
		return ready, nil
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return "", err
	}
	defer root.Close()
	dir := "runtime-tools/" + expected
	if err := root.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := dir + "/" + name
	valid := false
	if info, err := root.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return "", errors.New("media tool cache is not a regular file")
		}
		file, err := root.Open(path)
		if err != nil {
			return "", err
		}
		hash := sha256.New()
		_, readErr := io.Copy(hash, file)
		file.Close()
		valid = readErr == nil && hex.EncodeToString(hash.Sum(nil)) == expected
		if !valid {
			if err := root.Remove(path); err != nil {
				return "", err
			}
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if !valid {
		reader, err := gzip.NewReader(compressed)
		if err != nil {
			return "", err
		}
		defer reader.Close()
		tmp := dir + "/." + name + "-" + rand.Text()
		file, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return "", err
		}
		defer root.Remove(tmp)
		hash := sha256.New()
		const maxToolSize = 256 << 20
		n, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(reader, maxToolSize+1))
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil || n > maxToolSize || hex.EncodeToString(hash.Sum(nil)) != expected {
			return "", fmt.Errorf("embedded %s failed integrity verification", name)
		}
		if err := root.Rename(tmp, path); err != nil {
			return "", err
		}
	}
	if err := root.Chmod(path, 0700); err != nil {
		return "", err
	}
	if m.ready == nil {
		m.ready = map[string]string{}
	}
	m.ready[key] = filepath.Join(base, filepath.FromSlash(path))
	return m.ready[key], nil
}
