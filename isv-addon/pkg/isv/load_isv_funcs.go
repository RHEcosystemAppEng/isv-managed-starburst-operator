package isv

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	CommonISVInstance CommonISV
)

var isvCustomFuncs []func(ctx context.Context, cl client.Client, ns string) error
var isvCustomPatches []func(ctx context.Context, cl client.Client, ns string, subject map[string]interface{}) error

func LoadCustomFuncs(ctx context.Context, cl client.Client, ns string) error {
	for _, f := range isvCustomFuncs {
		if e := f(ctx, cl, ns); e != nil {
			return e
		}
	}

	return nil
}

func LoadCustomPatches(ctx context.Context, cl client.Client, ns string, subject map[string]interface{}) error {
	for _, p := range isvCustomPatches {
		if e := p(ctx, cl, ns, subject); e != nil {
			return e
		}
	}

	return nil
}

type CommonISV interface {
	GetISVPrefix() string
}
