package ocsession

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Session struct {
	ID        string `json:"id"`
	Directory string `json:"directory"`
	Time      struct {
		Updated int64 `json:"updated"`
	} `json:"time"`
}

// Select pins a TUI to a session actually served by this OpenCode server.
// Its returned arguments replace --continue with an explicit --session ID.
func Select(ctx context.Context, baseURL string, args []string) (string, []string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", nil, err
	}
	var session string
	resume := false
	project := false
	kept := make([]string, 0, len(args)+2)
	for i := 0; i < len(args); i++ {
		name, value, hasValue := strings.Cut(args[i], "=")
		switch name {
		case "-s", "--session", "--dir":
			if !hasValue {
				i++
				if i >= len(args) {
					return "", nil, fmt.Errorf("%s requires a value", name)
				}
				value = args[i]
			}
			if name == "--dir" {
				directory, err = filepath.Abs(value)
				if err != nil {
					return "", nil, err
				}
				kept = append(kept, "--dir", directory)
				project = true
			} else {
				session = value
			}
		case "-c", "--continue":
			resume = !hasValue || (value != "false" && value != "0")
		case "--fork":
			return "", nil, fmt.Errorf("--fork is not supported with --ra2a: forked session ownership cannot be verified")
		default:
			if (name == "--log-level" || name == "--password" || name == "-p" ||
				name == "--username" || name == "-u" || name == "--replay-limit") && !hasValue {
				i++
				if i >= len(args) {
					return "", nil, fmt.Errorf("%s requires a value", name)
				}
				kept = append(kept, name, args[i])
				continue
			}
			if !strings.HasPrefix(args[i], "-") && !project {
				directory, err = filepath.Abs(args[i])
				if err != nil {
					return "", nil, err
				}
				kept = append(kept, "--dir", directory)
				project = true
				continue
			}
			kept = append(kept, args[i])
		}
	}
	endpoint := strings.TrimSuffix(baseURL, "/") + "/session?directory=" + url.QueryEscape(directory)
	if session == "" && resume {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return "", nil, err
		}
		response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
		if err != nil {
			return "", nil, err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return "", nil, fmt.Errorf("list OpenCode sessions: %s", response.Status)
		}
		var sessions []Session
		if err := json.NewDecoder(response.Body).Decode(&sessions); err != nil {
			return "", nil, err
		}
		var latest int64
		for _, item := range sessions {
			if item.Directory == directory && (session == "" || item.Time.Updated > latest) {
				session, latest = item.ID, item.Time.Updated
			}
		}
		if session == "" {
			return "", nil, fmt.Errorf("no OpenCode session to continue in %s", directory)
		}
	}
	if session == "" {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader([]byte(`{}`)))
		if err != nil {
			return "", nil, err
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
		if err != nil {
			return "", nil, err
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return "", nil, fmt.Errorf("create OpenCode session: %s", response.Status)
		}
		var created Session
		if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
			return "", nil, err
		}
		session = created.ID
	}
	if !strings.HasPrefix(session, "ses") {
		return "", nil, fmt.Errorf("invalid OpenCode session ID %q", session)
	}
	return session, append(kept, "--session", session), nil
}
