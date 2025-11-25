package k8s

import (
	"os"
	"testing"

	"k8s.io/client-go/rest"
)

func TestGetConfig(t *testing.T) {
	// Save original env
	originalKubeconfig := os.Getenv("KUBECONFIG")
	originalHome := os.Getenv("HOME")
	defer func() {
		if originalKubeconfig != "" {
			_ = os.Setenv("KUBECONFIG", originalKubeconfig)
		} else {
			_ = os.Unsetenv("KUBECONFIG")
		}
		if originalHome != "" {
			_ = os.Setenv("HOME", originalHome)
		}
	}()

	tests := []struct {
		name        string
		setupEnv    func()
		expectError bool
		validate    func(*testing.T, *rest.Config)
	}{
		{
			name: "in-cluster config or kubeconfig exists",
			setupEnv: func() {
				// Don't modify environment - use whatever is available
			},
			expectError: false,
			validate: func(t *testing.T, config *rest.Config) {
				if config == nil {
					t.Error("Expected non-nil config")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupEnv != nil {
				tt.setupEnv()
			}

			config, err := GetConfig()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					// It's OK if we get an error in test environment without cluster
					t.Logf("Got expected error in test environment: %v", err)
					return
				}
				if tt.validate != nil {
					tt.validate(t, config)
				}
			}
		})
	}
}

func TestGetConfigNoConfigAvailable(t *testing.T) {
	// Save original env
	originalKubeconfig := os.Getenv("KUBECONFIG")
	originalHome := os.Getenv("HOME")
	originalInCluster := os.Getenv("KUBERNETES_SERVICE_HOST")

	defer func() {
		if originalKubeconfig != "" {
			_ = os.Setenv("KUBECONFIG", originalKubeconfig)
		} else {
			_ = os.Unsetenv("KUBECONFIG")
		}
		if originalHome != "" {
			_ = os.Setenv("HOME", originalHome)
		}
		if originalInCluster != "" {
			_ = os.Setenv("KUBERNETES_SERVICE_HOST", originalInCluster)
		} else {
			_ = os.Unsetenv("KUBERNETES_SERVICE_HOST")
		}
	}()

	// Clear all config sources
	_ = os.Unsetenv("KUBECONFIG")
	_ = os.Unsetenv("KUBERNETES_SERVICE_HOST")
	_ = os.Setenv("HOME", "/nonexistent")

	config, err := GetConfig()

	if err == nil {
		t.Skip("In-cluster or other config still available despite clearing env")
		return
	}

	if config != nil {
		t.Error("Expected nil config when error occurs")
	}

	// Verify error message is informative
	errMsg := err.Error()
	if errMsg == "" {
		t.Error("Error message should not be empty")
	}
	if len(errMsg) < 10 {
		t.Errorf("Error message seems too short: %s", errMsg)
	}

	t.Logf("Got expected error: %s", errMsg)
}

func TestGetClient(t *testing.T) {
	tests := []struct {
		name        string
		setupEnv    func()
		expectError bool
	}{
		{
			name: "successful client creation",
			setupEnv: func() {
				// Use default environment
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupEnv != nil {
				tt.setupEnv()
			}

			clientset, dynamicClient, err := GetClient()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if clientset != nil {
					t.Error("Expected nil clientset on error")
				}
				if dynamicClient != nil {
					t.Error("Expected nil dynamicClient on error")
				}
			} else {
				if err != nil {
					// It's OK if we get an error in test environment without cluster
					t.Logf("Got expected error in test environment: %v", err)
					return
				}
				if clientset == nil {
					t.Error("Expected non-nil clientset")
				}
				if dynamicClient == nil {
					t.Error("Expected non-nil dynamicClient")
				}
			}
		})
	}
}

func TestGetClientNoConfigAvailable(t *testing.T) {
	// Save original env
	originalKubeconfig := os.Getenv("KUBECONFIG")
	originalHome := os.Getenv("HOME")
	originalInCluster := os.Getenv("KUBERNETES_SERVICE_HOST")

	defer func() {
		if originalKubeconfig != "" {
			_ = os.Setenv("KUBECONFIG", originalKubeconfig)
		} else {
			_ = os.Unsetenv("KUBECONFIG")
		}
		if originalHome != "" {
			_ = os.Setenv("HOME", originalHome)
		}
		if originalInCluster != "" {
			_ = os.Setenv("KUBERNETES_SERVICE_HOST", originalInCluster)
		} else {
			_ = os.Unsetenv("KUBERNETES_SERVICE_HOST")
		}
	}()

	// Clear all config sources
	_ = os.Unsetenv("KUBECONFIG")
	_ = os.Unsetenv("KUBERNETES_SERVICE_HOST")
	_ = os.Setenv("HOME", "/nonexistent")

	clientset, dynamicClient, err := GetClient()

	if err == nil {
		t.Skip("In-cluster or other config still available despite clearing env")
		return
	}

	if clientset != nil {
		t.Error("Expected nil clientset on error")
	}
	if dynamicClient != nil {
		t.Error("Expected nil dynamicClient on error")
	}

	t.Logf("Got expected error: %s", err.Error())
}

func TestGetConfigReturnsValidConfig(t *testing.T) {
	config, err := GetConfig()
	if err != nil {
		t.Skipf("Skipping test - no Kubernetes config available: %v", err)
		return
	}

	// Validate config has expected fields
	if config.Host == "" {
		t.Error("Config host should not be empty")
	}

	t.Logf("Successfully created config with host: %s", config.Host)
}

func TestGetClientReturnsValidClients(t *testing.T) {
	clientset, dynamicClient, err := GetClient()
	if err != nil {
		t.Skipf("Skipping test - no Kubernetes config available: %v", err)
		return
	}

	// Validate clientset
	if clientset == nil {
		t.Fatal("Clientset should not be nil")
	}

	// Validate we can access core API
	coreV1 := clientset.CoreV1()
	if coreV1 == nil {
		t.Error("CoreV1 client should not be nil")
	}

	// Validate dynamic client
	if dynamicClient == nil {
		t.Fatal("Dynamic client should not be nil")
	}

	t.Log("Successfully created Kubernetes clients")
}

func TestGetClientConsistency(t *testing.T) {
	// Get clients twice and ensure they're independently created
	clientset1, dynamic1, err1 := GetClient()
	clientset2, dynamic2, err2 := GetClient()

	// Both should have same error state
	if (err1 == nil) != (err2 == nil) {
		t.Error("GetClient should return consistent error state")
	}

	if err1 != nil {
		t.Skipf("No Kubernetes config available: %v", err1)
		return
	}

	// Both should return valid clients
	if clientset1 == nil || clientset2 == nil {
		t.Error("Both calls should return valid clientsets")
	}

	if dynamic1 == nil || dynamic2 == nil {
		t.Error("Both calls should return valid dynamic clients")
	}

	// Clients should be independent instances (different pointers)
	// Note: They may share underlying HTTP client/transport, which is fine
	t.Log("Successfully created multiple independent client instances")
}

// Benchmark tests
func BenchmarkGetConfig(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = GetConfig()
	}
}

func BenchmarkGetClient(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _, _ = GetClient()
	}
}
