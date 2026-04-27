package pkg_test

import (
	"context"
	"testing"

	"github.com/git-hulk/cula/pkg"
)

type stubRuntime struct {
	kind pkg.RuntimeKind
}

func (s stubRuntime) Kind() pkg.RuntimeKind { return s.kind }

func (s stubRuntime) Detect(ctx context.Context) (pkg.RuntimeInfo, error) {
	return pkg.RuntimeInfo{Kind: s.kind, Name: string(s.kind), Installed: true, AuthStatus: pkg.AuthLoggedIn}, nil
}

func (s stubRuntime) SpawnSession(ctx context.Context, input pkg.SessionInput) (pkg.Session, error) {
	return nil, nil
}

func TestRegistryRuntimeLookup(t *testing.T) {
	registry := pkg.NewRegistry(stubRuntime{kind: "test"})
	if _, ok := registry.Runtime("test"); !ok {
		t.Fatal("runtime was not registered")
	}
	if _, ok := registry.Runtime(pkg.RuntimeCodex); ok {
		t.Fatal("unexpected codex runtime in custom registry")
	}
}
