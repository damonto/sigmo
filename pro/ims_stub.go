//go:build !ims

package main

import "context"

func configureIMS(context.Context, *proApp) error {
	return nil
}
