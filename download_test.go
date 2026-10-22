package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mockVendor(t *testing.T, fn roundTripFunc) {
	t.Helper()
	old := vendorHTTP
	vendorHTTP = &http.Client{Transport: fn, CheckRedirect: old.CheckRedirect}
	t.Cleanup(func() { vendorHTTP = old })
}

func TestLatestNativeReleasesResolveBothArchitectures(t *testing.T) {
	for _, h := range []string{"claude", "codex", "opencode"} {
		for _, arch := range []string{"amd64", "arm64"} {
			t.Run(h+"/"+arch, func(t *testing.T) {
				mockVendor(t, func(r *http.Request) (*http.Response, error) {
					if r.Header.Get("Authorization") != "" {
						t.Fatal("credential sent to download host")
					}
					if h == "claude" {
						if strings.HasSuffix(r.URL.Path, "/latest") {
							return reply(200, "9.8.7\n"), nil
						}
						return reply(200, `{"platforms":{"win32-x64":{"checksum":"`+strings.Repeat("a", 64)+`"},"win32-arm64":{"checksum":"`+strings.Repeat("b", 64)+`"}}}`), nil
					}
					repo, asset, tag := "anomalyco/opencode", "opencode-windows-x64-baseline.zip", "v9.8.7"
					if arch == "arm64" {
						asset = "opencode-windows-arm64.zip"
					}
					if h == "codex" {
						repo, tag = "openai/codex", "rust-v9.8.7"
						cpu := "x86_64"
						if arch == "arm64" {
							cpu = "aarch64"
						}
						asset = "codex-package-" + cpu + "-pc-windows-msvc.tar.gz"
					}
					data, _ := json.Marshal(map[string]any{"tag_name": tag, "assets": []any{map[string]any{"name": asset, "digest": "sha256:" + strings.Repeat("a", 64), "browser_download_url": "https://github.com/" + repo + "/releases/download/" + tag + "/" + asset}}})
					return reply(200, string(data)), nil
				})
				r, err := latestRelease(h, arch)
				if err != nil || r.Version != "9.8.7" || len(r.SHA256) != 64 {
					t.Fatalf("release resolution failed: %v", err)
				}
				if h == "codex" && r.Executable != filepath.Join("bin", "codex.exe") {
					t.Fatal("Codex package layout lost")
				}
			})
		}
	}
}

func TestDownloadsRejectUntrustedHostsAndMismatchedChecksums(t *testing.T) {
	for _, raw := range []string{"http://github.com/x", "https://github.com.attacker.invalid/x", "https://user:pass@github.com/x", "https://github.com:443/x"} {
		if vendorURL(raw) {
			t.Fatal("untrusted URL accepted")
		}
	}
	mockVendor(t, func(r *http.Request) (*http.Response, error) {
		res := reply(200, "corrupt executable")
		res.ContentLength = -1
		return res, nil
	})
	err := downloadRelease(nativeRelease{URL: "https://github.com/vendor/release", SHA256: strings.Repeat("0", 64)}, filepath.Join(t.TempDir(), "download"))
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatal("corrupted download accepted", err)
	}
	if _, err := checksum(""); err == nil {
		t.Fatal("missing checksum accepted")
	}
	req, _ := http.NewRequest("GET", "https://attacker.invalid/payload", nil)
	if err := vendorHTTP.CheckRedirect(req, []*http.Request{{}}); err == nil {
		t.Fatal("untrusted redirect accepted")
	}
}

func TestGitHubApiQuotaFallsBackToChecksummedOfficialRelease(t *testing.T) {
	asset := "opencode-windows-x64-baseline.zip"
	href := "/anomalyco/opencode/releases/download/v9.8.7/" + asset
	sum := strings.Repeat("d", 64)
	mockVendor(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.github.com" {
			return reply(403, "quota exceeded"), nil
		}
		if strings.HasSuffix(r.URL.Path, "/latest") {
			res := reply(200, "")
			req, _ := http.NewRequest("GET", "https://github.com/anomalyco/opencode/releases/tag/v9.8.7", nil)
			res.Request = req
			return res, nil
		}
		return reply(200, `<li><a href="`+href+`">asset</a><clipboard-copy aria-label="Copy to clipboard digest for `+asset+`" value="sha256:`+sum+`"></clipboard-copy></li>`), nil
	})
	r, err := latestRelease("opencode", "amd64")
	if err != nil || r.SHA256 != sum || r.Version != "9.8.7" {
		t.Fatal("quota fallback failed", err)
	}
	if _, err := githubAssetChecksum(`<li><a href="`+href+`"></a></li><li><clipboard-copy aria-label="Copy to clipboard digest for `+asset+`" value="sha256:`+sum+`"></clipboard-copy></li>`, href, asset); err == nil {
		t.Fatal("checksum borrowed from another asset row")
	}
}

func TestFullBundleExtractionAndTraversalDefense(t *testing.T) {
	for _, format := range []string{"zip", "tar.gz"} {
		t.Run(format, func(t *testing.T) {
			for _, unsafe := range []bool{false, true} {
				var buf bytes.Buffer
				members := map[string]string{"bin/codex.exe": "exe", "codex-resources/helper.exe": "helper", "codex-path/rg.exe": "rg"}
				if unsafe {
					members = map[string]string{"../escape.exe": "bad"}
				}
				if format == "zip" {
					z := zip.NewWriter(&buf)
					for name, data := range members {
						w, _ := z.Create(name)
						io.WriteString(w, data)
					}
					z.Close()
				} else {
					gz := gzip.NewWriter(&buf)
					w := tar.NewWriter(gz)
					for name, data := range members {
						w.WriteHeader(&tar.Header{Name: name, Mode: 0700, Size: int64(len(data)), Typeflag: tar.TypeReg})
						io.WriteString(w, data)
					}
					w.Close()
					gz.Close()
				}
				archive := filepath.Join(t.TempDir(), "archive")
				os.WriteFile(archive, buf.Bytes(), 0600)
				dest := t.TempDir()
				err := extractRelease(archive, dest, format)
				if unsafe {
					if err == nil {
						t.Fatal("archive escaped its root")
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				for name, want := range members {
					got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(name)))
					if err != nil || string(got) != want {
						t.Fatal("companion missing", name, err)
					}
				}
			}
		})
	}
}

func TestPersistentConfigsMergeAndRotateKeys(t *testing.T) {
	key := "rk_live_test_persistent"
	for _, h := range []string{"claude", "codex", "opencode"} {
		model := "claude-opus-5-5"
		if h == "codex" {
			model = "gpt-6.1-sol"
		}
		before := []byte(`{"theme":"existing","enabledPlugins":{"keep@example":true}}`)
		if h == "codex" {
			before = []byte("# keep\n[features]\nkeep=true\n")
		}
		out, err := renderPersistent(h, model, key, before)
		if err != nil {
			t.Fatal(err)
		}
		root, err := decodeConfig(h, out)
		if err != nil {
			t.Fatal(err)
		}
		if h == "codex" {
			if bytes.Contains(out, []byte(key)) || bytes.Contains(out, []byte("env_key")) {
				t.Fatal("Codex credentials belong in auth.json")
			}
			continue
		}
		if root["theme"] != "existing" {
			t.Fatal("user preference overwritten")
		}
		paths := [][]string{{"env", "ANTHROPIC_AUTH_TOKEN"}}
		if h == "opencode" {
			paths = [][]string{{"provider", "phasesai-anthropic", "options", "apiKey"}, {"provider", "phasesai-gpt", "options", "apiKey"}}
		}
		for _, p := range paths {
			v, _ := fieldValue(root, p)
			if v != key {
				t.Fatal("credential not persisted")
			}
		}
		again, err := renderPersistent(h, model, key, out)
		if err != nil || !bytes.Equal(out, again) {
			t.Fatal("rerun changed config", err)
		}
		rotated, err := renderPersistent(h, model, "rk_live_rotated", out)
		if err != nil || bytes.Contains(rotated, []byte(key)) {
			t.Fatal("old credential remains")
		}
	}
}

func TestMultiFileFailureRestoresEarlierFiles(t *testing.T) {
	home := t.TempDir()
	first := filepath.Join(home, "config.toml")
	second := filepath.Join(home, "auth.json")
	writeOwnedTestFile(first, []byte("original"), 0600)
	writeOwnedTestFile(second, []byte("changed concurrently"), 0600)
	err := applyPlans(home, []configPlan{{first, []byte("original"), []byte("updated"), true}, {second, []byte("stale"), []byte("new key"), true}})
	if err == nil {
		t.Fatal("concurrent edit ignored")
	}
	got, _ := os.ReadFile(first)
	if string(got) != "original" {
		t.Fatal("earlier file not restored")
	}
	got, _ = os.ReadFile(second)
	if string(got) != "changed concurrently" {
		t.Fatal("concurrent edit overwritten")
	}
}
