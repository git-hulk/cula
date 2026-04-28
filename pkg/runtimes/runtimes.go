// Package runtimes exposes constructors for cula's built-in CLI runtimes.
//
// The runtime implementations themselves live under internal/runtime so they
// remain unexported; this package is the public seam external consumers use
// to obtain a Runtime they can register with cula.NewRegistry.
//
//	import (
//	    cula "github.com/git-hulk/cula/pkg"
//	    "github.com/git-hulk/cula/pkg/runtimes"
//	)
//
//	reg := cula.NewRegistry(
//	    runtimes.NewClaudeCode(),
//	    runtimes.NewCodex(),
//	    runtimes.NewOpenCode(),
//	)
package runtimes

import (
	"github.com/git-hulk/cula/internal/runtime/claudecode"
	"github.com/git-hulk/cula/internal/runtime/codex"
	"github.com/git-hulk/cula/internal/runtime/opencode"
	cula "github.com/git-hulk/cula/pkg"
)

// DefaultRegistry returns a cula.Registry preloaded with every built-in
// runtime (Claude Code, Codex, OpenCode), each configured by the given
// RuntimeOptions. Use this when you want all supported runtimes available
// without enumerating them yourself.
func DefaultRegistry(opts ...cula.RuntimeOption) *cula.Registry {
	return cula.NewRegistry(
		NewClaudeCode(opts...),
		NewCodex(opts...),
		NewOpenCode(opts...),
	)
}

// NewClaudeCode returns the built-in Claude Code runtime, configured by the
// given RuntimeOptions.
func NewClaudeCode(opts ...cula.RuntimeOption) cula.Runtime {
	return claudecode.New(buildConfig(opts))
}

// NewCodex returns the built-in Codex runtime.
func NewCodex(opts ...cula.RuntimeOption) cula.Runtime {
	return codex.New(buildConfig(opts))
}

// NewOpenCode returns the built-in OpenCode runtime.
func NewOpenCode(opts ...cula.RuntimeOption) cula.Runtime {
	return opencode.New(buildConfig(opts))
}

func buildConfig(opts []cula.RuntimeOption) cula.Config {
	var cfg cula.Config
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
