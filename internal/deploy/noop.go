package deploy

import "context"

func init() {
	Register("noop", newNoopDeployer)
}

// noopDeployer is for projects released by something else, such as a platform that deploys on
// push. It performs no release, so the deploy watch still runs its verify window.
type noopDeployer struct{}

func newNoopDeployer(Config) (Deployer, error) {
	return noopDeployer{}, nil
}

// Deploy records nothing and returns a handle whose ID is "noop-" plus ref.
func (noopDeployer) Deploy(_ context.Context, ref string) (Handle, error) {
	return Handle{ID: "noop-" + ref}, nil
}

// Status always reports ready.
func (noopDeployer) Status(context.Context, string) (Status, error) {
	return Status{State: StateReady}, nil
}

// Rollback does nothing.
func (noopDeployer) Rollback(context.Context, string, string) error {
	return nil
}
