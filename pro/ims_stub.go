//go:build !ims

package main

import "context"

var proIMS func(context.Context, *proApp) error
