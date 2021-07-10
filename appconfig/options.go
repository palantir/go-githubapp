package appconfig

import (
	"github.com/palantir/go-githubapp/githubapp"
)

type Option func(*Loader)

// WithRemoteRefParser sets the parser for encoded RemoteRefs. The default
// parser uses YAML. Set a nil parser to disable remote references.
func WithRemoteRefParser(parser RemoteRefParser) Option {
	return func(ld *Loader) {
		ld.parser = parser
	}
}

// WithOwnerDefault sets the owner repository and paths to check when a
// repository does not define its own configuration. By default, the repository
// name is ".github" and the paths are those passed to the loader with the
// ".github/" prefix removed. Set an empty repository name to disable
// owner defaults.
func WithOwnerDefault(name string, paths []string) Option {
	return func(ld *Loader) {
		ld.defaultName = name
		ld.defaultPaths = paths
	}
}

// WithPrivateRemotes enables loading remote configuration from private
// repositories in different organizations. By default, only public
// repositories can be remote targets.
func WithPrivateRemotes(cc githubapp.ClientCreator, installs githubapp.InstallationsService) Option {
	// TODO(bkeyes): implement this, if this functionality is valuable
	// See https://github.com/palantir/policy-bot/issues/111
	panic("TODO(bkeyes): unimplemented")
}
