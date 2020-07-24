/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gke

import (
	"errors"

	container "google.golang.org/api/container/v1beta1"
)

const defaultGKEVersion = "latest"

// Request contains all settings collected for cluster creation
type Request struct {
	// GCPCredentialFile: the GCP credential file to use for the cluster operations
	GCPCredentialFile string

	// Project: name of the gcloud project for the cluster
	Project string

	// GKEVersion: GKE version of the cluster, default to be latest if not provided
	GKEVersion string

	// ReleaseChannel: GKE release channel. Only one of GKEVersion or ReleaseChannel can be
	// specified at a time.
	// https://cloud.google.com/kubernetes-engine/docs/concepts/release-channels
	ReleaseChannel string

	// ClusterName: name of the cluster
	ClusterName string

	// MinNodes: the minimum number of nodes of the cluster
	MinNodes int64

	// MaxNodes: the maximum number of nodes of the cluster
	MaxNodes int64

	// NodeType: node type of the cluster, e.g. e2-standard-4, e2-standard-8
	NodeType string

	// Region: region of the cluster, e.g. us-west1, us-central1
	Region string

	// Zone: default is none, must be provided together with region
	Zone string

	// Addons: cluster addons to be added to cluster, such as istio
	Addons []string

	// EnableWorkloadIdentity: whether to enable Workload Identity for this cluster
	EnableWorkloadIdentity bool

	// ServiceAccount: service account that will be used on this cluster
	ServiceAccount string
}

// DeepCopy will make a deepcopy of the request struct.
func (r *Request) DeepCopy() *Request {
	return &Request{
		Project:                r.Project,
		GKEVersion:             r.GKEVersion,
		ReleaseChannel:         r.ReleaseChannel,
		ClusterName:            r.ClusterName,
		MinNodes:               r.MinNodes,
		MaxNodes:               r.MaxNodes,
		NodeType:               r.NodeType,
		Region:                 r.Region,
		Zone:                   r.Zone,
		Addons:                 r.Addons,
		EnableWorkloadIdentity: r.EnableWorkloadIdentity,
		ServiceAccount:         r.ServiceAccount,
	}
}

// NewCreateClusterRequest returns a new CreateClusterRequest that can be used in gcloud SDK.
func NewCreateClusterRequest(request *Request) (*container.CreateClusterRequest, error) {
	if request.ClusterName == "" {
		return nil, errors.New("cluster name cannot be empty")
	}
	if request.MinNodes <= 0 {
		return nil, errors.New("min nodes must be larger than 1")
	}
	if request.MinNodes > request.MaxNodes {
		return nil, errors.New("min nodes cannot be larger than max nodes")
	}
	if request.NodeType == "" {
		return nil, errors.New("node type cannot be empty")
	}
	if request.EnableWorkloadIdentity && request.Project == "" {
		return nil, errors.New("project cannot be empty if you want Workload Identity")
	}
	if request.GKEVersion != "" && request.ReleaseChannel != "" {
		return nil, errors.New("can only specify one of GKE version or release channel (not both)")
	}

	ccr := &container.CreateClusterRequest{
		Cluster: &container.Cluster{
			NodePools: []*container.NodePool{
				{
					Name:             "default-pool",
					InitialNodeCount: request.MinNodes,
					Autoscaling: &container.NodePoolAutoscaling{
						Enabled:      true,
						MinNodeCount: request.MinNodes,
						MaxNodeCount: request.MaxNodes,
					},
					Config: &container.NodeConfig{
						MachineType: request.NodeType,
						// The set of Google API scopes to be made available on all
						// of the node VMs under the "default" service account.
						// If unspecified, no scopes are added, unless Cloud Logging or
						// Cloud Monitoring are enabled, in which case their required
						// scopes will be added.
						// `https://www.googleapis.com/auth/devstorage.read_only` is required
						// for communicating with **gcr.io**, and it's included in cloud-platform scope.
						// TODO(chizhg): give more fine granular scope based on the actual needs.
						OauthScopes: []string{container.CloudPlatformScope},
					},
				},
			},
			Name: request.ClusterName,
			// Installing addons after cluster creation takes at least 5
			// minutes, so install addons as part of cluster creation, which
			// doesn't seem to add much time on top of cluster creation
			AddonsConfig: GetAddonsConfig(request.Addons),
			// Equivalent to --enable-basic-auth, so that user:pass can be
			// later on retrieved for setting up cluster roles. Use the
			// default username from gcloud command, the password will be
			// automatically generated by GKE SDK
			MasterAuth: &container.MasterAuth{Username: "admin"},
		},
	}
	if request.EnableWorkloadIdentity {
		// Equivalent to --workload-pool=[PROJECT_ID].svc.id.goog, then
		// we can configure a Kubernetes service account to act as a Google
		// service account.
		ccr.Cluster.WorkloadIdentityConfig = &container.WorkloadIdentityConfig{
			WorkloadPool: request.Project + ".svc.id.goog",
		}
	}
	if request.ServiceAccount != "" {
		// The Google Cloud Platform Service Account to be used by the node VMs.
		// If a service account is specified, the cloud-platform and userinfo.email scopes are used.
		// If no Service Account is specified, the project default service account is used.
		ccr.Cluster.NodePools[0].Config.ServiceAccount = request.ServiceAccount
	}

	// Manage the GKE cluster version. Only one of initial cluster version or release channel can be specified.
	if request.ReleaseChannel != "" {
		ccr.Cluster.ReleaseChannel = &container.ReleaseChannel{Channel: request.ReleaseChannel}
	} else if request.GKEVersion != "" {
		ccr.Cluster.InitialClusterVersion = request.GKEVersion
	} else {
		// The default cluster version is not latest, has to explicitly
		// set it as "latest"
		ccr.Cluster.InitialClusterVersion = defaultGKEVersion
	}
	return ccr, nil
}