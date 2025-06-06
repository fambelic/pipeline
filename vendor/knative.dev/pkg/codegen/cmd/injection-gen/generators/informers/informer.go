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

package informers

import (
	"io"

	clientgentypes "k8s.io/code-generator/cmd/client-gen/types"
	"k8s.io/gengo/v2/generator"
	"k8s.io/gengo/v2/namer"
	"k8s.io/gengo/v2/types"
	"k8s.io/klog/v2"

	gennamer "knative.dev/pkg/codegen/cmd/injection-gen/namer"
)

// injectionTestGenerator produces a file of listers for a given GroupVersion and
// type.
type injectionGenerator struct {
	generator.GoGenerator
	outputPackage               string
	groupVersion                clientgentypes.GroupVersion
	groupGoName                 string
	typeToGenerate              *types.Type
	imports                     namer.ImportTracker
	typedInformerPackage        string
	groupInformerFactoryPackage string
	disableInformerInit         bool
}

var _ generator.Generator = (*injectionGenerator)(nil)

func (g *injectionGenerator) Filter(c *generator.Context, t *types.Type) bool {
	// Only process the type for this informer generator.
	return t == g.typeToGenerate
}

func (g *injectionGenerator) Namers(c *generator.Context) namer.NameSystems {
	publicPluralNamer := &gennamer.ExceptionNamer{
		Exceptions: map[string]string{
			// these exceptions are used to deconflict the generated code
			// you can put your fully qualified package like
			// to generate a name that doesn't conflict with your group.
			// "k8s.io/apis/events/v1beta1.Event": "EventResource"
		},
		KeyFunc: func(t *types.Type) string {
			return t.Name.Package + "." + t.Name.Name
		},
		Delegate: namer.NewPublicPluralNamer(map[string]string{
			"Endpoints": "Endpoints",
		}),
	}

	return namer.NameSystems{
		"raw":          namer.NewRawNamer(g.outputPackage, g.imports),
		"publicPlural": publicPluralNamer,
	}
}

func (g *injectionGenerator) Imports(c *generator.Context) (imports []string) {
	imports = append(imports, g.imports.ImportLines()...)
	return
}

func (g *injectionGenerator) GenerateType(c *generator.Context, t *types.Type, w io.Writer) error {
	sw := generator.NewSnippetWriter(w, c, "{{", "}}")

	klog.V(5).Info("processing type ", t)

	m := map[string]interface{}{
		"groupGoName":               namer.IC(g.groupGoName),
		"versionGoName":             namer.IC(g.groupVersion.Version.String()),
		"type":                      t,
		"injectionRegisterInformer": c.Universe.Type(types.Name{Package: "knative.dev/pkg/injection", Name: "Default.RegisterInformer"}),
		"controllerInformer":        c.Universe.Type(types.Name{Package: "knative.dev/pkg/controller", Name: "Informer"}),
		"informersTypedInformer":    c.Universe.Type(types.Name{Package: g.typedInformerPackage, Name: t.Name.Name + "Informer"}),
		"factoryGet":                c.Universe.Type(types.Name{Package: g.groupInformerFactoryPackage, Name: "Get"}),
		"loggingFromContext": c.Universe.Function(types.Name{
			Package: "knative.dev/pkg/logging",
			Name:    "FromContext",
		}),
		"contextContext": c.Universe.Type(types.Name{
			Package: "context",
			Name:    "Context",
		}),
		"contextWithValue": c.Universe.Function(types.Name{
			Package: "context",
			Name:    "WithValue",
		}),
		"disableInformerInit": g.disableInformerInit,
	}

	sw.Do(injectionInformer, m)

	return sw.Error()
}

var injectionInformer = `
{{ if not .disableInformerInit }}
func init() {
	{{.injectionRegisterInformer|raw}}(withInformer)
}
{{ end }}

// Key is used for associating the Informer inside the context.Context.
type Key struct{}

{{ if .disableInformerInit }} func WithInformer {{ else }} func withInformer {{ end }} (ctx {{.contextContext|raw}}) ({{.contextContext|raw}}, {{.controllerInformer|raw}}) {
	f := {{.factoryGet|raw}}(ctx)
	inf := f.{{.groupGoName}}().{{.versionGoName}}().{{.type|publicPlural}}()
	return {{ .contextWithValue|raw }}(ctx, Key{}, inf), inf.Informer()
}

// Get extracts the typed informer from the context.
func Get(ctx {{.contextContext|raw}}) {{.informersTypedInformer|raw}} {
	untyped := ctx.Value(Key{})
	if untyped == nil {
		{{.loggingFromContext|raw}}(ctx).Panic(
			"Unable to fetch {{.informersTypedInformer}} from context.")
	}
	return untyped.({{.informersTypedInformer|raw}})
}
`
