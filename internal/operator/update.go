package operator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const defaultReleaseAPI = "https://api.github.com/repos/ceasarXuu/RA2A/releases/latest"

func Update(ctx context.Context) (version string, changed, deferred bool, err error) {
	api := os.Getenv("RA2A_RELEASE_API")
	if api == "" {
		api = defaultReleaseAPI
	}
	data, err := download(ctx, api, 4<<20)
	if err != nil {
		return "", false, false, fmt.Errorf("check GitHub Release: %w", err)
	}
	var release struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(data, &release); err != nil || release.Tag == "" {
		return "", false, false, fmt.Errorf("decode GitHub Release: %w", err)
	}
	if release.Tag == Version {
		return release.Tag, false, false, nil
	}
	name := fmt.Sprintf("ra2a-%s-%s-%s", release.Tag, runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	var binaryURL, checksumURL string
	for _, asset := range release.Assets {
		switch asset.Name {
		case name:
			binaryURL = asset.URL
		case name + ".sha256":
			checksumURL = asset.URL
		}
	}
	if binaryURL == "" || checksumURL == "" {
		return "", false, false, fmt.Errorf("release %s has no asset for %s/%s", release.Tag, runtime.GOOS, runtime.GOARCH)
	}
	binary, err := download(ctx, binaryURL, 64<<20)
	if err != nil {
		return "", false, false, err
	}
	checksum, err := download(ctx, checksumURL, 1024)
	if err != nil {
		return "", false, false, err
	}
	want := strings.Fields(string(checksum))
	got := fmt.Sprintf("%x", sha256.Sum256(binary))
	if len(want) == 0 || !strings.EqualFold(want[0], got) {
		return "", false, false, errors.New("release SHA-256 verification failed")
	}
	executable := os.Getenv("RA2A_EXECUTABLE")
	if executable == "" {
		executable, err = os.Executable()
		if err != nil {
			return "", false, false, err
		}
	}
	temporary := executable + ".new"
	if err := os.WriteFile(temporary, binary, 0o755); err != nil {
		return "", false, false, err
	}
	if runtime.GOOS == "windows" && os.Getenv("RA2A_EXECUTABLE") == "" {
		script := fmt.Sprintf(`Stop-ScheduledTask -TaskName RA2A -ErrorAction SilentlyContinue;Wait-Process -Id %d;Move-Item -LiteralPath '%s' -Destination '%s' -Force;Start-ScheduledTask -TaskName RA2A`, os.Getpid(), strings.ReplaceAll(temporary, "'", "''"), strings.ReplaceAll(executable, "'", "''"))
		if err := exec.Command("powershell.exe", "-NoProfile", "-Command", script).Start(); err != nil {
			_ = os.Remove(temporary)
			return "", false, false, err
		}
		return release.Tag, true, true, nil
	}
	if err := os.Rename(temporary, executable); err != nil {
		_ = os.Remove(temporary)
		return "", false, false, err
	}
	return release.Tag, true, false, nil
}

func download(ctx context.Context, url string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "ra2a/"+Version)
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %s", url, response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, limit))
}
