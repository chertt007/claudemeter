//go:build !windows

package watch

// watchTree is a no-op outside Windows: the UI falls back to periodic polling.
func watchTree(dir string, emit func(string)) (func(), error) {
	return func() {}, nil
}
