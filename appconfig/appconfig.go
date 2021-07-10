// Package appconfig loads repository configuration for GitHub apps. It
// supports loading directly from a file in a repository, loading from remote
// references, and loading an organization-level default. The config itself can
// be in any format.
package appconfig

import (
	"context"
	"strings"

	"github.com/google/go-github/v37/github"
)

// RemoteRefParser attempts to parse a RemoteRef from bytes. The parser should
// return nil with a nil error if b does not encode a RemoteRef and nil with a
// non-nil error if b encodes an invalid RemoteRef.
type RemoteRefParser func(path string, b []byte) (*RemoteRef, error)

// RemoteRef identifies a configuration file in a different repository.
type RemoteRef struct {
	// The repository in "owner/name" format.
	Remote string `yaml:"remote" json:"remote"`

	// The path to the config file in the repository.
	Path string `yaml:"path" json:"path"`

	// The reference (branch, tag, or SHA) to read in the repository. If empty,
	// use the default branch of the repository.
	Ref string `yaml:"ref" json:"ref"`
}

// Config contains unparsed configuration data and metadata about where it was found.
type Config struct {
	Content []byte

	Source   string
	Path     string
	IsRemote bool
}

// IsUndefined returns true if the Config's content is empty and there is no
// metadata giving a source.
func (c Config) IsUndefined() bool {
	return len(c.Content) == 0 && c.Source == "" && c.Path == ""
}

// Loader loads configuration for repositories.
type Loader struct {
	paths []string

	parser       RemoteRefParser
	defaultRepo  string
	defaultPaths string
}

// NewLoader creates a Loader that loads configuration from paths.
func NewLoader(paths []string, opts ...Option) *Loader {
	defaultPaths := make([]string, len(paths))
	for i, p := range paths {
		defaultPaths[i] = strings.TrimPrefix(p, ".github/")
	}

	ld := Loader{
		paths:        paths,
		parser:       YAMLRemoteRefParser,
		defaultRepo:  ".github",
		defaultPaths: defaultPaths,
	}

	for _, opt := range opts {
		opt(&ld)
	}

	return &ld
}

// LoadConfig loads configuration for the repository owner/repo. It first tries
// the Loader's paths in order, following remote references if they exist. If
// no configuration exists at any path in the repository, it tries to load
// default configuration defined by owner for all repositories. If no default
// configuration exists, it returns an undefined Config and a nil error.
//
// If error is non-nil, the Source and Path fields of the returned Config tell
// which file LoadConfig was processing when it encountered the error.
func (ld *Loader) LoadConfig(ctx context.Context, client *github.Client, owner, repo string) (Config, error) {
	// for each path:
	//   try loading
	//   if exists:
	//		try parsing as remote
	//		if remote:
	//		  load remote
	//		else:
	//		  return
	//
	// for each default path:
	//   try loading
	//   if exists:
	//     return
	panic("TODO(bkeyes): implement this")
}
