package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const claudeDownloads = "https://downloads.claude.ai/claude-code-releases"
const maxDownloadBytes int64 = 1 << 30

type nativeRelease struct {
	Version, URL, SHA256, Format, Executable string
}

func vendorURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	switch u.Hostname() {
	case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "downloads.claude.ai", "storage.googleapis.com":
		return true
	}
	return false
}

var vendorHTTP = &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
	if len(via) >= 6 || !vendorURL(req.URL.String()) {
		return errors.New("untrusted download redirect")
	}
	return nil
}}

func vendorGet(ctx context.Context, raw string) (*http.Response, error) {
	if !vendorURL(raw) {
		return nil, errors.New("untrusted vendor download URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, errors.New("invalid download URL")
	}
	req.Header.Set("User-Agent", "PhasesAI-Setup/"+version)
	res, err := vendorHTTP.Do(req)
	if err != nil {
		return nil, errors.New("vendor download failed; check your internet connection and retry setup")
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("vendor download returned HTTP %d; retry setup later", res.StatusCode)
	}
	return res, nil
}

func vendorMetadata(raw string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	res, err := vendorGet(ctx, raw)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 8<<20+1))
	if err != nil || len(data) > 8<<20 {
		return nil, errors.New("vendor metadata is incomplete or too large")
	}
	return data, nil
}

func checksum(value string) (string, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "sha256:")
	b, err := hex.DecodeString(value)
	if err != nil || len(b) != sha256.Size {
		return "", errors.New("vendor SHA-256 checksum is missing or invalid; nothing executed")
	}
	return strings.ToLower(value), nil
}

func latestRelease(harness, arch string) (nativeRelease, error) {
	if arch != "amd64" && arch != "arm64" {
		return nativeRelease{}, errors.New("Windows x64 or ARM64 is required")
	}
	if harness == "claude" {
		data, err := vendorMetadata(claudeDownloads + "/latest")
		if err != nil {
			return nativeRelease{}, err
		}
		v := strings.TrimSpace(string(data))
		if !stableVersion.MatchString(v) {
			return nativeRelease{}, errors.New("invalid Claude release version")
		}
		data, err = vendorMetadata(claudeDownloads + "/" + v + "/manifest.json")
		if err != nil {
			return nativeRelease{}, err
		}
		var manifest struct {
			Platforms map[string]struct {
				Checksum string `json:"checksum"`
			} `json:"platforms"`
		}
		if json.Unmarshal(data, &manifest) != nil {
			return nativeRelease{}, errors.New("invalid Claude release manifest")
		}
		platform := "win32-x64"
		if arch == "arm64" {
			platform = "win32-arm64"
		}
		sum, err := checksum(manifest.Platforms[platform].Checksum)
		if err != nil {
			return nativeRelease{}, err
		}
		return nativeRelease{v, claudeDownloads + "/" + v + "/" + platform + "/claude.exe", sum, "exe", "claude.exe"}, nil
	}
	repo, asset := "", ""
	switch harness {
	case "codex":
		repo = "openai/codex"
		cpu := "x86_64"
		if arch == "arm64" {
			cpu = "aarch64"
		}
		asset = "codex-package-" + cpu + "-pc-windows-msvc.tar.gz"
	case "opencode":
		repo = "anomalyco/opencode"
		// Baseline runs on older x64 CPUs as well as fresh contemporary PCs.
		asset = "opencode-windows-x64-baseline.zip"
		if arch == "arm64" {
			asset = "opencode-windows-arm64.zip"
		}
	default:
		return nativeRelease{}, errors.New("unsupported harness")
	}
	data, err := vendorMetadata("https://api.github.com/repos/" + repo + "/releases/latest")
	if err != nil {
		// Shared networks can exhaust GitHub's anonymous API quota. The official
		// release page exposes the same per-asset SHA-256 without an API token.
		return githubPageRelease(repo, asset, harness)
	}
	var release struct {
		Tag        string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if json.Unmarshal(data, &release) != nil || release.Prerelease {
		return nativeRelease{}, errors.New("invalid stable release metadata")
	}
	v := strings.TrimPrefix(strings.TrimPrefix(release.Tag, "rust-v"), "v")
	if !stableVersion.MatchString(v) {
		return nativeRelease{}, errors.New("invalid vendor release version")
	}
	for _, a := range release.Assets {
		if a.Name != asset {
			continue
		}
		if !strings.HasPrefix(a.URL, "https://github.com/"+repo+"/releases/download/"+release.Tag+"/") {
			return nativeRelease{}, errors.New("release asset URL does not match the vendor")
		}
		sum, err := checksum(a.Digest)
		if err != nil {
			return nativeRelease{}, err
		}
		format := "zip"
		exe := "opencode.exe"
		if harness == "codex" {
			format = "tar.gz"
			exe = filepath.Join("bin", "codex.exe")
		}
		return nativeRelease{v, a.URL, sum, format, exe}, nil
	}
	return nativeRelease{}, errors.New("the latest release has no supported Windows download")
}

func githubPageRelease(repo, asset, harness string) (nativeRelease, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	res, err := vendorGet(ctx, "https://github.com/"+repo+"/releases/latest")
	if err != nil {
		return nativeRelease{}, err
	}
	res.Body.Close()
	prefix := "https://github.com/" + repo + "/releases/tag/"
	if res.Request == nil || !strings.HasPrefix(res.Request.URL.String(), prefix) {
		return nativeRelease{}, errors.New("could not resolve the vendor's latest stable release")
	}
	tag := strings.TrimPrefix(res.Request.URL.String(), prefix)
	v := strings.TrimPrefix(strings.TrimPrefix(tag, "rust-v"), "v")
	if !stableVersion.MatchString(v) {
		return nativeRelease{}, errors.New("invalid vendor release version")
	}
	data, err := vendorMetadata("https://github.com/" + repo + "/releases/expanded_assets/" + tag)
	if err != nil {
		return nativeRelease{}, err
	}
	href := "/" + repo + "/releases/download/" + tag + "/" + asset
	sum, err := githubAssetChecksum(string(data), href, asset)
	if err != nil {
		return nativeRelease{}, err
	}
	format, exe := "zip", "opencode.exe"
	if harness == "codex" {
		format, exe = "tar.gz", filepath.Join("bin", "codex.exe")
	}
	return nativeRelease{v, "https://github.com" + href, sum, format, exe}, nil
}

func githubAssetChecksum(page, href, asset string) (string, error) {
	start := strings.Index(page, `href="`+html.EscapeString(href)+`"`)
	if start < 0 {
		return "", errors.New("latest release has no supported Windows download")
	}
	end := strings.Index(page[start:], "</li>")
	if end < 0 {
		return "", errors.New("invalid vendor asset metadata")
	}
	row := page[start : start+end]
	for _, element := range regexp.MustCompile(`<clipboard-copy\b[^>]*>`).FindAllString(row, -1) {
		if !strings.Contains(element, `aria-label="Copy to clipboard digest for `+html.EscapeString(asset)+`"`) {
			continue
		}
		match := regexp.MustCompile(`\bvalue="sha256:([a-f0-9]{64})"`).FindStringSubmatch(element)
		if len(match) == 2 {
			return checksum(match[1])
		}
	}
	return "", errors.New("vendor SHA-256 checksum missing; nothing executed")
}

func downloadRelease(release nativeRelease, destination string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	res, err := vendorGet(ctx, release.URL)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.ContentLength > maxDownloadBytes {
		return errors.New("vendor download exceeds size limit")
	}
	f, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	hash := sha256.New()
	p := &downloadProgress{total: res.ContentLength}
	n, err := io.Copy(io.MultiWriter(f, hash, p), io.LimitReader(res.Body, maxDownloadBytes+1))
	fmt.Println()
	if err != nil || n > maxDownloadBytes || (res.ContentLength >= 0 && n != res.ContentLength) {
		return errors.New("download incomplete; rerun setup")
	}
	if hex.EncodeToString(hash.Sum(nil)) != release.SHA256 {
		return errors.New("download checksum mismatch; nothing executed")
	}
	return f.Sync()
}

type downloadProgress struct {
	total, received int64
	last            time.Time
}

func (p *downloadProgress) Write(b []byte) (int, error) {
	p.received += int64(len(b))
	if time.Since(p.last) > time.Second {
		p.last = time.Now()
		if p.total > 0 {
			fmt.Printf("\r      Downloading %3d%%  (%d / %d MiB)", 100*p.received/p.total, p.received>>20, p.total>>20)
		} else {
			fmt.Printf("\r      Downloading %d MiB", p.received>>20)
		}
	}
	return len(b), nil
}

// Extract the whole bundle, including Codex's sandbox helpers, DLLs and rg.
// os.Root plus explicit member validation prevent archive path escapes.
func extractRelease(archive, destination, format string) error {
	root, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer root.Close()
	remaining := int64(2 << 30)
	count := 0
	write := func(name string, mode os.FileMode, size int64, reader io.Reader) error {
		name = filepath.FromSlash(name)
		if strings.Contains(name, "\\") && os.PathSeparator != '\\' {
			return errors.New("unsafe archive member")
		}
		if !filepath.IsLocal(name) || strings.Contains(name, ":") || mode&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return errors.New("unsafe archive member")
		}
		count++
		if count > 20000 || size < 0 || size > remaining {
			return errors.New("archive exceeds extraction limits")
		}
		if mode.IsDir() {
			return root.MkdirAll(name, 0700)
		}
		if !mode.IsRegular() {
			return errors.New("unsupported archive member")
		}
		if err := root.MkdirAll(filepath.Dir(name), 0700); err != nil {
			return err
		}
		f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
		if err != nil {
			return err
		}
		n, copyErr := io.Copy(f, io.LimitReader(reader, size+1))
		closeErr := f.Close()
		if copyErr != nil || n != size {
			return errors.New("archive member is incomplete")
		}
		remaining -= n
		return closeErr
	}
	if format == "zip" {
		z, err := zip.OpenReader(archive)
		if err != nil {
			return err
		}
		defer z.Close()
		for _, entry := range z.File {
			if entry.UncompressedSize64 > uint64(remaining) {
				return errors.New("archive exceeds extraction limits")
			}
			r, err := entry.Open()
			if err != nil {
				return err
			}
			err = write(entry.Name, entry.Mode(), int64(entry.UncompressedSize64), r)
			r.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}
	if format != "tar.gz" {
		return errors.New("unsupported archive format")
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	t := tar.NewReader(gz)
	for {
		h, err := t.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
			return errors.New("linked or special archive member refused")
		}
		if err := write(h.Name, h.FileInfo().Mode(), h.Size, t); err != nil {
			return err
		}
	}
}

func installNative(home, harness string, release nativeRelease) (string, error) {
	root, err := installRoot(home)
	if err != nil {
		return "", err
	}
	base := filepath.Join(root, "harnesses", harness)
	if err := safePath(home, filepath.Join(base, "placeholder")); err != nil {
		return "", err
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(base, ".download-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	archive := filepath.Join(stage, "download")
	if err := downloadRelease(release, archive); err != nil {
		return "", err
	}
	files := filepath.Join(stage, "files")
	if err := os.Mkdir(files, 0700); err != nil {
		return "", err
	}
	if release.Format == "exe" {
		err = os.Rename(archive, filepath.Join(files, release.Executable))
	} else {
		err = extractRelease(archive, files, release.Format)
	}
	if err != nil {
		return "", err
	}
	exe := filepath.Join(files, release.Executable)
	detected, err := checkHarness(exe, harness)
	if err != nil {
		return "", err
	}
	if detected != release.Version {
		return "", errors.New("downloaded harness version does not match its release metadata")
	}
	final := filepath.Join(base, release.Version+"-"+nonce())
	if err := os.Rename(files, final); err != nil {
		return "", err
	}
	return filepath.Join(final, release.Executable), nil
}
