// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/compose"
)

func startAcceptanceStack(t *testing.T) string {
	t.Helper()
	stack, err := compose.NewDockerComposeWith(compose.WithStackFiles("../../integration/compose.yml"))
	if err != nil {
		t.Fatalf("create Kaneo stack: %s", err)
	}
	// Register cleanup before startup so partial starts are cleaned up too.
	t.Cleanup(func() {
		captureAcceptanceLogs(t, stack)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := stack.Down(ctx, compose.RemoveOrphans(true), compose.RemoveVolumes(true)); err != nil {
			t.Errorf("stop Kaneo stack: %s", err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	if err := stack.WithOsEnv().Up(ctx, compose.Wait(true)); err != nil {
		t.Fatalf("start Kaneo stack: %s", err)
	}
	container, err := stack.ServiceContainer(ctx, "kaneo")
	if err != nil {
		t.Fatalf("get Kaneo container: %s", err)
	}
	endpoint, err := container.PortEndpoint(ctx, "5173/tcp", "http")
	if err != nil {
		t.Fatalf("get Kaneo endpoint: %s", err)
	}
	return endpoint + "/api"
}

func captureAcceptanceLogs(t *testing.T, stack compose.ComposeStack) {
	t.Helper()
	logDir := t.ArtifactDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, service := range stack.Services() {
		container, err := stack.ServiceContainer(ctx, service)
		if err != nil {
			t.Logf("get %s container for logs: %s", service, err)
			continue
		}
		logs, err := container.Logs(ctx)
		if err != nil {
			t.Logf("get %s logs: %s", service, err)
			continue
		}
		data, err := io.ReadAll(logs)
		if err := logs.Close(); err != nil {
			t.Logf("close %s logs: %s", service, err)
		}
		if err != nil {
			t.Logf("read %s logs: %s", service, err)
		}
		if t.Failed() {
			t.Logf("%s logs:\n%s", service, data)
		}
		if err := os.WriteFile(filepath.Join(logDir, service+".log"), data, 0o600); err != nil {
			t.Errorf("save %s logs: %s", service, err)
		}
	}
}
