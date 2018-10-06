// Copyright 2018 Palantir Technologies, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package githubapp

import (
	"github.com/google/go-github/github"
)

type BaseHandler struct {
	ClientCreator ClientCreator
}

func NewDefaultBaseHandler(c Config, opts ...ClientOption) (BaseHandler, error) {
	delegate := NewClientCreator(
		c.V3APIURL,
		c.V4APIURL,
		c.App.IntegrationID,
		[]byte(c.App.PrivateKey),
		opts...,
	)

	cc, err := NewCachingClientCreator(delegate, DefaultCachingClientCapacity)
	if err != nil {
		return BaseHandler{}, err
	}

	return BaseHandler{
		ClientCreator: cc,
	}, nil
}

type InstallationSource interface {
	GetInstallation() *github.Installation
}

func (b *BaseHandler) GetInstallationIDFromEvent(event InstallationSource) int64 {
	return event.GetInstallation().GetID()
}
