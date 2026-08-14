//go:build !esim_transfer

package main

import "context"

func configureESIMTransfer(context.Context, *proApp) error {
	return nil
}
