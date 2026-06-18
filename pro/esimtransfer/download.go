//go:build esim_transfer

package esimtransfer

import (
	"context"
	"errors"
	"fmt"

	elpa "github.com/damonto/euicc-go/lpa"
	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/damonto/ts43-go"

	esimcore "github.com/damonto/sigmo/internal/pkg/esim"
	ilpa "github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func (s *Service) downloadEnableAndComplete(ctx context.Context, session *session, active *activeSession, result *ts43.Result, downloadConfig ts43.DownloadConfig) (*ts43.Result, error) {
	iccid, err := s.downloadProfile(ctx, session, active.target, active.targetClient, downloadConfig)
	active.CloseTarget()
	if err != nil {
		return result, err
	}
	session.sendIfConnected(serverMessage{Type: wsTypeProgress, Stage: stageEnabling})
	if err := s.enableTargetProfile(ctx, active.target, iccid); err != nil {
		return result, err
	}
	session.sendIfConnected(serverMessage{Type: wsTypeProgress, Stage: stageCompleting})
	next, err := active.client.CompleteActivation(ctx, result, ts43.ActivationResult{ICCID: iccid.String()})
	if err != nil {
		active.Logger().Warn("complete TS.43 activation", "iccid", iccid.String(), "error", err)
	}
	return next, err
}

func (s *Service) downloadProfile(ctx context.Context, session *session, target *mmodem.Modem, client *ilpa.LPA, downloadConfig ts43.DownloadConfig) (sgp22.ICCID, error) {
	ac, err := activationCode(downloadConfig)
	if err != nil {
		return nil, err
	}
	if ac.IMEI == "" {
		imei, err := target.ThreeGPP().IMEI(ctx)
		if err != nil {
			return nil, fmt.Errorf("read target IMEI: %w", err)
		}
		ac.IMEI = imei
	}
	session.sendIfConnected(serverMessage{Type: wsTypeProgress, Stage: stageDownloading})
	result, err := client.DownloadProfile(ctx, ac, &elpa.DownloadOptions{
		OnProgress: func(stage elpa.DownloadStage) {
			session.sendIfConnected(serverMessage{Type: wsTypeProgress, Stage: stage.String()})
		},
		OnConfirm: func(info *sgp22.ProfileInfo) bool {
			preview := profilePreviewFrom(info)
			session.sendIfConnected(serverMessage{
				Type:    wsTypePreview,
				Profile: &preview,
			})
			return true
		},
	})
	if err != nil {
		return nil, fmt.Errorf("download transferred profile: %w", err)
	}
	var iccid sgp22.ICCID
	if result != nil && result.Notification != nil {
		iccid = result.Notification.ICCID
		if result.Notification.SequenceNumber > 0 {
			if err := client.SendNotification(result.Notification.SequenceNumber, false); err != nil {
				return nil, fmt.Errorf("send install notification: %w", err)
			}
		}
	}
	if len(iccid) == 0 && downloadConfig.ProfileICCID != "" {
		parsed, err := sgp22.NewICCID(downloadConfig.ProfileICCID)
		if err != nil {
			return nil, fmt.Errorf("parse transferred ICCID: %w", err)
		}
		iccid = parsed
	}
	if len(iccid) == 0 {
		return nil, errors.New("transferred profile ICCID is missing")
	}
	return iccid, nil
}

func (s *Service) enableTargetProfile(ctx context.Context, target *mmodem.Modem, iccid sgp22.ICCID) error {
	if s.enableProfile == nil {
		return errors.New("enable profile dependency is missing")
	}
	return s.enableProfile(ctx, target, iccid)
}

func activationCode(downloadConfig ts43.DownloadConfig) (*elpa.ActivationCode, error) {
	if downloadConfig.ActivationCode != "" {
		var ac elpa.ActivationCode
		if err := ac.UnmarshalText([]byte(downloadConfig.ActivationCode)); err != nil {
			return nil, err
		}
		ac.IMEI = downloadConfig.IMEI
		return &ac, nil
	}
	var smdp esimcore.SMDPAddress
	if err := smdp.UnmarshalText([]byte(downloadConfig.SMDPFQDN)); err != nil {
		return nil, err
	}
	return &elpa.ActivationCode{
		SMDP:       smdp.URL(),
		MatchingID: downloadConfig.MatchingID,
		IMEI:       downloadConfig.IMEI,
	}, nil
}
