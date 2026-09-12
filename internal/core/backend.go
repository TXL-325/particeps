package core

import "particeps/internal/incusx"

// IncusBackend describes the operations used by the Agent. Keeping the boundary
// explicit lets fault tests exercise real business rules without changing a host.
type IncusBackend interface {
	Ready() error
	EnsureProject() error
	EnsureNetwork(string, string, bool) error
	GetState(string) (*incusx.InstanceState, error)
	GetConfig(string) (map[string]any, error)
	CreateInstance(string, string, map[string]string, map[string]map[string]string) error
	SetState(string, string, bool) error
	DeleteInstance(string) error
	Rebuild(string, string) error
	BeginConfigUpdate(string, map[string]string, map[string]map[string]string) (incusx.ConfigOperation, error)
	WaitConfigOperation(string) (bool, error)
	ConfigOperationFinished(string) (bool, error)
	ListForwards(string) ([]incusx.Forward, error)
	GetForward(string, string) (incusx.Forward, string, error)
	CreateForward(string, incusx.Forward) error
	UpdateForward(string, incusx.Forward, string) error
	SetRootPassword(string, string) error
	InstallRootKey(string, string) error
}
