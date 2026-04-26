package pkg

import (
	"context"
	"fmt"
	"sync"
)

type Registry struct {
	mu       sync.RWMutex
	runtimes map[RuntimeKind]Runtime
}

func NewRegistry(runtimes ...Runtime) *Registry {
	r := &Registry{runtimes: make(map[RuntimeKind]Runtime)}
	for _, rt := range runtimes {
		r.Register(rt)
	}
	return r
}

func (r *Registry) Register(rt Runtime) {
	if rt == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runtimes[rt.Kind()] = rt
}

func (r *Registry) Runtime(kind RuntimeKind) (Runtime, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rt, ok := r.runtimes[kind]
	return rt, ok
}

func (r *Registry) SpawnSession(ctx context.Context, input SessionInput) (Session, error) {
	rt, ok := r.Runtime(input.Runtime)
	if !ok {
		return nil, fmt.Errorf("runtime %q is not registered", input.Runtime)
	}
	return rt.SpawnSession(ctx, input)
}

func (r *Registry) DetectAll(ctx context.Context) []RuntimeInfo {
	r.mu.RLock()
	runtimes := make([]Runtime, 0, len(r.runtimes))
	for _, rt := range r.runtimes {
		runtimes = append(runtimes, rt)
	}
	r.mu.RUnlock()

	infos := make([]RuntimeInfo, 0, len(runtimes))
	for _, rt := range runtimes {
		info, err := rt.Detect(ctx)
		if err != nil {
			info = RuntimeInfo{
				Kind:       rt.Kind(),
				AuthStatus: AuthUnknown,
			}
		}
		infos = append(infos, info)
	}
	return infos
}
