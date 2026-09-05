package appserverprobe

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
)

const maxRolloutLineBytes = 64 * 1024 * 1024

// rolloutModel reads the thread's original model from its session rollout
// record. The authoritative value is session_meta.base_instructions
// .provenance.model (the model the thread was created with); threads without
// instructions fall back to the last turn_context.model. It never consults
// model/list: the app-server default must not replace the thread's own model.
func rolloutModel(path string) (string, error) {
	if path == "" {
		return "", errors.New("thread/read response did not include a rollout path")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxRolloutLineBytes)
	var lastTurnModel string
	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.Contains(line, []byte(`"model"`)) {
			continue
		}
		var record struct {
			Type    string `json:"type"`
			Payload struct {
				Model            string `json:"model"`
				BaseInstructions struct {
					Provenance struct {
						Model string `json:"model"`
					} `json:"provenance"`
				} `json:"base_instructions"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &record); err != nil {
			continue
		}
		switch record.Type {
		case "session_meta":
			if model := strings.TrimSpace(record.Payload.BaseInstructions.Provenance.Model); model != "" {
				return model, nil
			}
		case "turn_context":
			if model := strings.TrimSpace(record.Payload.Model); model != "" {
				lastTurnModel = model
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if lastTurnModel != "" {
		return lastTurnModel, nil
	}
	return "", errors.New("rollout record has no model")
}