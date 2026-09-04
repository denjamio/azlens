package cmd

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

const (
	githubRepo = "denjamio/azlens"
)

var (
	upgradeCheckOnly bool
	upgradeForce     bool
)

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// upgradeCmd represents the canonical upgrade command (Section 6.7).
var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade AzLens to a newer version",
	Long: `Checks GitHub Releases for a newer version of AzLens, downloads the binary
for your current operating system and architecture, verifies its SHA256 checksum
against the release checksums.txt, and atomically replaces the running executable.`,
	GroupID: "supporting",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		color.Cyan("🔍 Checking for updates at github.com/%s...", githubRepo)

		latest, err := fetchLatestRelease(ctx)
		if err != nil {
			return fmt.Errorf("failed checking for updates: %w", err)
		}

		latestVersion := strings.TrimPrefix(latest.TagName, "v")
		currentVersion := strings.TrimPrefix(Version, "v")

		fmt.Printf("Current version: %s\n", color.YellowString("v"+currentVersion))
		fmt.Printf("Latest release:  %s\n\n", color.GreenString(latest.TagName))

		if latestVersion == currentVersion && !upgradeForce {
			color.Green("✓ AzLens is already up to date!")
			return nil
		}

		if upgradeCheckOnly {
			if latestVersion != currentVersion {
				color.Yellow("💡 A new version is available! Run 'azlens upgrade' to upgrade.")
			}
			return nil
		}

		// Locate appropriate asset
		targetAssetURL, assetName, err := findMatchingAsset(latest)
		if err != nil {
			return err
		}

		executablePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to locate current executable path: %w", err)
		}
		executablePath, err = filepath.EvalSymlinks(executablePath)
		if err != nil {
			return fmt.Errorf("failed to resolve symlink: %w", err)
		}
		workDir := filepath.Dir(executablePath)

		// Download the release asset to a temp file
		color.Cyan("⬇️  Downloading %s...", assetName)
		assetFile, err := os.CreateTemp(workDir, "azlens-asset-*")
		if err != nil {
			return fmt.Errorf("permission denied writing to %s (try running with sudo): %w", workDir, err)
		}
		defer func() { _ = os.Remove(assetFile.Name()) }()

		if err := downloadFile(ctx, targetAssetURL, assetFile); err != nil {
			_ = assetFile.Close()
			return fmt.Errorf("failed downloading upgrade: %w", err)
		}
		if err := assetFile.Close(); err != nil {
			return fmt.Errorf("failed finalizing downloaded asset: %w", err)
		}

		// Verify checksum
		color.Cyan("🔒 Verifying SHA256 checksum...")
		if err := verifyChecksum(ctx, latest, assetFile.Name(), assetName); err != nil {
			return err
		}

		// Extract verified binary
		tempFile, err := os.CreateTemp(workDir, "azlens-upgrade-*")
		if err != nil {
			return fmt.Errorf("permission denied writing to %s (try running with sudo): %w", workDir, err)
		}
		defer func() { _ = os.Remove(tempFile.Name()) }()

		if err := extractBinary(assetFile.Name(), assetName, tempFile); err != nil {
			_ = tempFile.Close()
			return err
		}
		if err := tempFile.Close(); err != nil {
			return fmt.Errorf("failed finalizing downloaded binary: %w", err)
		}

		// Set executable permissions
		if err := os.Chmod(tempFile.Name(), 0755); err != nil {
			return fmt.Errorf("failed setting executable permissions: %w", err)
		}

		if err := replaceBinary(tempFile.Name(), executablePath); err != nil {
			return fmt.Errorf("failed replacing binary (try running with sudo): %w", err)
		}

		color.Green("✓ Successfully upgraded AzLens to %s!", latest.TagName)
		return nil
	},
}

func fetchLatestRelease(ctx context.Context) (*githubRelease, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", githubRepo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "azlens-updater")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API returned HTTP %d", resp.StatusCode)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}

	return &rel, nil
}

func findMatchingAsset(rel *githubRelease) (string, string, error) {
	osName := runtime.GOOS
	archName := runtime.GOARCH

	for _, a := range rel.Assets {
		name := strings.ToLower(a.Name)
		if strings.Contains(name, osName) && (strings.Contains(name, archName) || (archName == "amd64" && strings.Contains(name, "x86_64"))) {
			return a.BrowserDownloadURL, a.Name, nil
		}
	}

	return "", "", fmt.Errorf("no compatible release binary found for %s/%s in release %s", osName, archName, rel.TagName)
}

func downloadFile(ctx context.Context, url string, destFile *os.File) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "azlens-updater")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download of %s returned HTTP %d", url, resp.StatusCode)
	}

	_, err = io.Copy(destFile, resp.Body)
	return err
}

func verifyChecksum(ctx context.Context, rel *githubRelease, assetPath, assetName string) error {
	var checksumURL string
	for _, a := range rel.Assets {
		if strings.EqualFold(a.Name, "checksums.txt") {
			checksumURL = a.BrowserDownloadURL
			break
		}
	}
	if checksumURL == "" {
		return fmt.Errorf("release %s does not publish checksums.txt: refusing to install an unverifiable binary (see https://github.com/%s/releases)", rel.TagName, githubRepo)
	}

	tmp, err := os.CreateTemp("", "azlens-checksums-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if err := downloadFile(ctx, checksumURL, tmp); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed downloading checksums.txt: %w", err)
	}
	_ = tmp.Close()

	content, err := os.ReadFile(tmp.Name())
	if err != nil {
		return err
	}

	var expected string
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.EqualFold(fields[1], assetName) {
			expected = fields[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("checksums.txt does not contain an entry for %s", assetName)
	}

	actual, err := fileSHA256(assetPath)
	if err != nil {
		return fmt.Errorf("failed computing asset checksum: %w", err)
	}

	if subtle.ConstantTimeCompare([]byte(strings.ToLower(actual)), []byte(strings.ToLower(expected))) != 1 {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", assetName, expected, actual)
	}

	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func extractBinary(assetPath, assetName string, destFile *os.File) error {
	if !strings.HasSuffix(assetName, ".tar.gz") && !strings.HasSuffix(assetName, ".tgz") {
		src, err := os.Open(assetPath)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(destFile, src)
		return err
	}

	f, err := os.Open(assetPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if hdr.Name == "azlens" || strings.HasSuffix(hdr.Name, "/azlens") {
			_, err = io.Copy(destFile, tr)
			return err
		}
	}
	return fmt.Errorf("azlens binary not found inside tarball")
}

func replaceBinary(sourcePath, targetPath string) error {
	if err := os.Rename(sourcePath, targetPath); err == nil {
		return nil
	} else if runtime.GOOS != "windows" {
		return err
	}

	if err := os.Remove(targetPath); err != nil {
		return err
	}
	return os.Rename(sourcePath, targetPath)
}

func init() {
	upgradeCmd.Flags().BoolVar(&upgradeCheckOnly, "check", false, "Only check if an upgrade is available without downloading")
	upgradeCmd.Flags().BoolVarP(&upgradeForce, "force", "f", false, "Force re-download even if already up to date")

	RootCmd.AddCommand(upgradeCmd)
}
