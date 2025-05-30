/*
Copyright 2020 The Tekton Authors

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

// Code generated by client-gen. DO NOT EDIT.

package v1beta1

import (
	context "context"

	pipelinev1beta1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	scheme "github.com/tektoncd/pipeline/pkg/client/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	gentype "k8s.io/client-go/gentype"
)

// PipelineRunsGetter has a method to return a PipelineRunInterface.
// A group's client should implement this interface.
type PipelineRunsGetter interface {
	PipelineRuns(namespace string) PipelineRunInterface
}

// PipelineRunInterface has methods to work with PipelineRun resources.
type PipelineRunInterface interface {
	Create(ctx context.Context, pipelineRun *pipelinev1beta1.PipelineRun, opts v1.CreateOptions) (*pipelinev1beta1.PipelineRun, error)
	Update(ctx context.Context, pipelineRun *pipelinev1beta1.PipelineRun, opts v1.UpdateOptions) (*pipelinev1beta1.PipelineRun, error)
	// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
	UpdateStatus(ctx context.Context, pipelineRun *pipelinev1beta1.PipelineRun, opts v1.UpdateOptions) (*pipelinev1beta1.PipelineRun, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*pipelinev1beta1.PipelineRun, error)
	List(ctx context.Context, opts v1.ListOptions) (*pipelinev1beta1.PipelineRunList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *pipelinev1beta1.PipelineRun, err error)
	PipelineRunExpansion
}

// pipelineRuns implements PipelineRunInterface
type pipelineRuns struct {
	*gentype.ClientWithList[*pipelinev1beta1.PipelineRun, *pipelinev1beta1.PipelineRunList]
}

// newPipelineRuns returns a PipelineRuns
func newPipelineRuns(c *TektonV1beta1Client, namespace string) *pipelineRuns {
	return &pipelineRuns{
		gentype.NewClientWithList[*pipelinev1beta1.PipelineRun, *pipelinev1beta1.PipelineRunList](
			"pipelineruns",
			c.RESTClient(),
			scheme.ParameterCodec,
			namespace,
			func() *pipelinev1beta1.PipelineRun { return &pipelinev1beta1.PipelineRun{} },
			func() *pipelinev1beta1.PipelineRunList { return &pipelinev1beta1.PipelineRunList{} },
		),
	}
}
