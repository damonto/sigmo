package lpa

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/damonto/euicc-go/driver"
)

var (
	estkProductAID = mustDecodeAID("A06573746B6D65FFFFFFFFFFFF6D6774")
	estkSE0AID     = mustDecodeAID("A06573746B6D65FFFF4953442D522030")
	estkSE1AID     = mustDecodeAID("A06573746B6D65FFFF4953442D522031")
	estkmeATRs     = [][]byte{
		{0x3B, 0x9F, 0x96, 0x80, 0x1F, 0xC7, 0x80, 0x31, 0xE0, 0x73, 0xFE, 0x21, 0x15, 0x57, 0x65, 0x73, 0x74, 0x6B, 0x2E, 0x6D, 0x65, 0xC1},
		{0x3B, 0x9F, 0x96, 0x80, 0x3F, 0xC7, 0x82, 0x80, 0x31, 0xE0, 0x73, 0xFE, 0x21, 0x15, 0x57, 0x65, 0x73, 0x74, 0x6B, 0x2E, 0x6D, 0x65, 0x63},
		{0x3B, 0xBF, 0x93, 0x00, 0x80, 0x1F, 0xC6, 0x80, 0x31, 0xE0, 0x73, 0xFE, 0x21, 0x13, 0x57, 0x65, 0x73, 0x74, 0x6B, 0x2E, 0x6D, 0x65, 0xE3},
	}
)

func estkmeSEs(channel driver.SmartCardChannel, logger *slog.Logger) ([]SE, bool) {
	skuName, err := readESTKSkuName(channel, logger)
	if err != nil {
		logger.Debug("read ESTKme SKU", "error", err)
		return nil, false
	}
	ses, ok := estkmeSEsForSKU(skuName)
	if !ok {
		logger.Debug("ESTKme SKU is not dual SE", "skuName", skuName)
	}
	return ses, ok
}

func isESTKmeATR(atr []byte) bool {
	return slices.ContainsFunc(estkmeATRs, func(known []byte) bool {
		return bytes.Equal(atr, known)
	})
}

func estkmeSEsForSKU(skuName string) ([]SE, bool) {
	if !isESTKmeDualSESKU(skuName) {
		return nil, false
	}
	return []SE{
		{ID: SEID0, Label: "SE1", AID: slices.Clone(estkSE0AID)},
		{ID: SEID1, Label: "SE2", AID: slices.Clone(estkSE1AID)},
	}, true
}

func isESTKmeDualSESKU(skuName string) bool {
	skuName = strings.TrimSpace(skuName)
	return slices.Contains([]string{"ESTKme Max", "ESTKme Plus+"}, skuName)
}

func readESTKSkuName(channel driver.SmartCardChannel, logger *slog.Logger) (string, error) {
	if channel == nil {
		return "", errors.New("smart card channel is required")
	}
	if err := channel.Connect(); err != nil {
		return "", fmt.Errorf("connect card: %w", err)
	}
	defer func() {
		if err := channel.Disconnect(); err != nil {
			logger.Debug("disconnect ESTKme product channel", "error", err)
		}
	}()

	logicalChannel, err := channel.OpenLogicalChannel(estkProductAID)
	if err != nil {
		return "", fmt.Errorf("open ESTKme product AID: %w", err)
	}
	defer func() {
		if err := channel.CloseLogicalChannel(logicalChannel); err != nil {
			logger.Debug("close ESTKme product logical channel", "channel", logicalChannel, "error", err)
		}
	}()

	command := []byte{0x00, 0x00, 0x03, 0x00, 0x00}
	setLogicalChannel(command, logicalChannel)
	response, err := channel.Transmit(command)
	if err != nil {
		return "", fmt.Errorf("read SKU name: %w", err)
	}
	value, ok := decodeESTKString(response)
	if !ok {
		return "", fmt.Errorf("read SKU name: unexpected response %X", response)
	}
	return strings.TrimSpace(value), nil
}

func decodeESTKString(response []byte) (string, bool) {
	if len(response) < 2 {
		return "", false
	}
	if response[len(response)-2] != 0x90 || response[len(response)-1] != 0x00 {
		return "", false
	}
	return string(response[:len(response)-2]), true
}

func setLogicalChannel(command []byte, channel byte) {
	if len(command) == 0 {
		return
	}
	if channel < 4 {
		command[0] = (command[0] & 0x9C) | channel
		return
	}
	if channel < 20 {
		command[0] = (command[0] & 0xB0) | 0x40 | (channel - 4)
	}
}

func mustDecodeAID(value string) []byte {
	aid, err := hex.DecodeString(value)
	if err != nil {
		panic(err)
	}
	return aid
}
