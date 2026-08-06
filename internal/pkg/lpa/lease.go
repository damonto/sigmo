package lpa

import (
	"context"
	"log/slog"
	"sync"

	"github.com/damonto/euicc-go/bertlv"
	euicclpa "github.com/damonto/euicc-go/lpa"
	sgp22 "github.com/damonto/euicc-go/v2"
)

type leaseDisposition uint8

const (
	leaseReusable leaseDisposition = iota
	leaseInvalidated
)

// clientView exposes the eUICC operations needed by a lease while keeping the
// raw client private. Closing that raw client directly would skip lease
// release and leave a closed client in the pool.
type clientView struct {
	client *euicclpa.Client
}

func (c *clientView) EID() ([]byte, error) {
	return c.client.EID()
}

func (c *clientView) EUICCInfo2() (*bertlv.TLV, error) {
	return c.client.EUICCInfo2()
}

func (c *clientView) ListProfile(searchCriteria any, tags []bertlv.Tag) ([]*sgp22.ProfileInfo, error) {
	return c.client.ListProfile(searchCriteria, tags)
}

func (c *clientView) ListNotification(filters ...sgp22.NotificationEvent) ([]*sgp22.NotificationMetadata, error) {
	return c.client.ListNotification(filters...)
}

func (c *clientView) RetrieveNotificationList(searchCriteria any) ([]*sgp22.PendingNotification, error) {
	return c.client.RetrieveNotificationList(searchCriteria)
}

func (c *clientView) HandleNotification(notification *sgp22.PendingNotification) error {
	return c.client.HandleNotification(notification)
}

func (c *clientView) RemoveNotificationFromList(sequenceNumber sgp22.SequenceNumber) error {
	return c.client.RemoveNotificationFromList(sequenceNumber)
}

func (c *clientView) EnableProfile(identifier any, refresh bool) error {
	return c.client.EnableProfile(identifier, refresh)
}

func (c *clientView) DeleteProfile(identifier any) error {
	return c.client.DeleteProfile(identifier)
}

func (c *clientView) SetNickname(iccid sgp22.ICCID, nickname string) error {
	return c.client.SetNickname(iccid, nickname)
}

func (c *clientView) DownloadProfile(ctx context.Context, activationCode *euicclpa.ActivationCode, opts *euicclpa.DownloadOptions) (*sgp22.LoadBoundProfilePackageResponse, error) {
	return c.client.DownloadProfile(ctx, activationCode, opts)
}

// Lease grants serialized access to a pooled eUICC client. Close returns the
// client to the pool; Invalidate retires it after an operation that triggers a
// SIM refresh.
type Lease struct {
	*clientView
	owner   *Client
	release func(leaseDisposition) error

	closeOnce sync.Once
	closeErr  error
}

func newLease(owner *Client, release func(leaseDisposition) error) *Lease {
	var view *clientView
	if owner != nil {
		view = owner.clientView
	}
	return &Lease{clientView: view, owner: owner, release: release}
}

// Close returns the persistent client to the pool for reuse.
func (l *Lease) Close() error {
	return l.releaseWith(leaseReusable)
}

// Invalidate retires the persistent client without closing its logical channel.
// Some firmware stops answering UIM requests immediately after accepting a
// profile operation with refresh enabled. The lease is released even when the
// transport disconnect reports an error.
func (l *Lease) Invalidate() error {
	return l.releaseWith(leaseInvalidated)
}

func (l *Lease) releaseWith(disposition leaseDisposition) error {
	if l == nil {
		return nil
	}
	l.closeOnce.Do(func() {
		if l.release != nil {
			l.closeErr = l.release(disposition)
		}
	})
	return l.closeErr
}

func (l *Lease) Logger() *slog.Logger {
	if l == nil || l.owner == nil {
		return slog.Default()
	}
	return l.owner.Logger()
}

func (l *Lease) Info() (*Info, error) {
	return l.owner.Info()
}

func (l *Lease) Delete(id sgp22.ICCID) error {
	return l.owner.Delete(id)
}

func (l *Lease) SendNotification(searchCriteria any, delete bool) error {
	return l.owner.SendNotification(searchCriteria, delete)
}

func (l *Lease) Download(ctx context.Context, activationCode *euicclpa.ActivationCode, opts *euicclpa.DownloadOptions) error {
	return l.owner.Download(ctx, activationCode, opts)
}

func (l *Lease) Discovery(imei sgp22.IMEI) ([]*sgp22.EventEntry, error) {
	return l.owner.Discovery(imei)
}
