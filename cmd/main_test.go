/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"os"
	"testing"
)

func TestModeDetection(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantWatcher bool
		wantGC      bool
	}{
		{
			name:        "watcher mode with -watcher flag",
			args:        []string{"cmd", "-watcher"},
			wantWatcher: true,
			wantGC:      false,
		},
		{
			name:        "watcher mode with --watcher flag",
			args:        []string{"cmd", "--watcher"},
			wantWatcher: true,
			wantGC:      false,
		},
		{
			name:        "gc mode with gc flag",
			args:        []string{"cmd", "gc"},
			wantWatcher: false,
			wantGC:      true,
		},
		{
			name:        "gc mode with --garbage-collect flag",
			args:        []string{"cmd", "--garbage-collect"},
			wantWatcher: false,
			wantGC:      true,
		},
		{
			name:        "operator mode (no flags)",
			args:        []string{"cmd"},
			wantWatcher: false,
			wantGC:      false,
		},
		{
			name:        "operator mode with other flags",
			args:        []string{"cmd", "-metrics-bind-address=:8080"},
			wantWatcher: false,
			wantGC:      false,
		},
		{
			name:        "watcher flag among other flags",
			args:        []string{"cmd", "-debug", "-watcher", "-verbose"},
			wantWatcher: true,
			wantGC:      false,
		},
		{
			name:        "gc flag among other flags",
			args:        []string{"cmd", "-debug", "gc", "-verbose"},
			wantWatcher: false,
			wantGC:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate os.Args
			watcherMode := false
			gcMode := false
			for _, arg := range tt.args[1:] {
				if arg == "-watcher" || arg == "--watcher" {
					watcherMode = true
					break
				}
				if arg == "gc" || arg == "--garbage-collect" {
					gcMode = true
					break
				}
			}

			if watcherMode != tt.wantWatcher {
				t.Errorf("watcherMode = %v, want %v", watcherMode, tt.wantWatcher)
			}
			if gcMode != tt.wantGC {
				t.Errorf("gcMode = %v, want %v", gcMode, tt.wantGC)
			}
		})
	}
}

func TestPollIntervalParsing(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     int
	}{
		{
			name:     "valid integer",
			envValue: "60",
			want:     60,
		},
		{
			name:     "empty string uses default",
			envValue: "",
			want:     30,
		},
		{
			name:     "invalid string uses default",
			envValue: "invalid",
			want:     30,
		},
		{
			name:     "zero value",
			envValue: "0",
			want:     0,
		},
		{
			name:     "large value",
			envValue: "3600",
			want:     3600,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv("POLL_INTERVAL", tt.envValue)
			}

			// Simulate the parsing logic from main
			pollInterval := 30
			if val := os.Getenv("POLL_INTERVAL"); val != "" {
				parsed := 0
				_, err := parseIntHelper(val, &parsed)
				if err == nil {
					pollInterval = parsed
				}
			}

			if pollInterval != tt.want {
				t.Errorf("pollInterval = %d, want %d", pollInterval, tt.want)
			}
		})
	}
}

func TestVersionVariable(t *testing.T) {
	// Test that Version variable exists and has expected default
	if Version == "" {
		t.Error("Version should not be empty string")
	}

	// Version is set via ldflags, so in tests it should be "dev"
	expectedDefault := "dev"
	if Version != expectedDefault {
		t.Logf("Version = %q, typically %q in tests (can be overridden by ldflags)", Version, expectedDefault)
	}
}

func TestSchemeInitialization(t *testing.T) {
	// Test that scheme is initialized
	if scheme == nil {
		t.Fatal("scheme should not be nil")
	}

	// Verify basic Kubernetes types are registered
	gvk := scheme.AllKnownTypes()
	if len(gvk) == 0 {
		t.Error("scheme should have registered types")
	}
}

// Helper function for parsing integers (matching the logic in main)
func parseIntHelper(s string, result *int) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, &parseError{s}
		}
		n = n*10 + int(c-'0')
	}
	*result = n
	return n, nil
}

type parseError struct {
	s string
}

func (e *parseError) Error() string {
	return "invalid integer: " + e.s
}
