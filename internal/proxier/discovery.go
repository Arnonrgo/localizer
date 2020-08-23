// Copyright 2020 Jared Allard
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
package proxier

import (
	"context"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	RemapAnnotationPrefix = "localizer.jaredallard.github.com/remap-"
)

// Service represents a Service running in Kubernetes
// that should be proxied local <-> remote
type Service struct {
	Name      string
	Namespace string
	Ports     []*ServicePort
}

// ServicePort defines a port that is exposed
// by a remote service.
type ServicePort struct {
	RemotePort uint
	LocalPort  uint
}

type Client struct {
	k kubernetes.Interface
}

// NewClient creates a new discovery client that is
// capable of finding remote services and creating proxies
func NewClient(k kubernetes.Interface) *Client {
	return &Client{
		k,
	}
}

func (c *Client) Discover(ctx context.Context) ([]*Service, error) {
	cont := ""

	s := make([]*Service, 0)
	for {
		l, err := c.k.CoreV1().Services("").List(ctx, metav1.ListOptions{Continue: cont})
		if kerrors.IsResourceExpired(err) {
			// we need a consistent list, so we just restart fetching
			s = make([]*Service, 0)
			cont = ""
			continue
		} else if err != nil {
			return nil, errors.Wrap(err, "failed to retrieve kubernetes services")
		}

		for _, kserv := range l.Items {
			serv := &Service{
				Name:      kserv.Name,
				Namespace: kserv.Namespace,
				Ports:     make([]*ServicePort, 0),
			}

			remaps := make(map[string]uint)
			for k, v := range kserv.Annotations {
				if !strings.HasPrefix(k, RemapAnnotationPrefix) {
					continue
				}

				// for now, skip invalid ports. We may want to expose
				// this someday in the future
				portOverride, err := strconv.ParseUint(v, 0, 6)
				if err != nil {
					continue
				}

				// TODO(jaredallard): determine if ToLower is really needed here.
				// for ease of use we transform this remap to lowercase here
				// when processing ports we also convert their name to lowercase
				// just in case. Though the spec may enforce this to begin with.
				portName := strings.ToLower(strings.TrimPrefix(k, RemapAnnotationPrefix))
				remaps[portName] = uint(portOverride)
			}

			// convert the Kubernetes ports into our own internal data model
			// we also handle overriding localPorts via the RemapAnnotation here.
			for _, p := range kserv.Spec.Ports {
				localPort := uint(p.Port)
				override := remaps[strings.ToLower(p.Name)]
				if override != 0 {
					localPort = override
				}

				serv.Ports = append(serv.Ports, &ServicePort{
					RemotePort: uint(p.Port),
					LocalPort:  localPort,
				})
			}

			s = append(s, serv)
		}

		// if we don't have a continue, then we break and return
		if l.Continue == "" {
			break
		}

		cont = l.Continue
	}

	return s, nil
}