//go:build !esim_transfer

package main

import "context"

var proESIMTransfer func(context.Context, *proApp) error
