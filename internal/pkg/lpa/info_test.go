package lpa

import (
	"strings"
	"testing"

	"github.com/damonto/euicc-go/bertlv"
)

func TestParseEUICCInfo2(t *testing.T) {
	freeSpace, err := bertlv.NewValue(bertlv.ContextSpecific.Primitive(2), []byte{0x01, 0x00}).MarshalBinary()
	if err != nil {
		t.Fatalf("marshal free space: %v", err)
	}
	valid := func() *bertlv.TLV {
		return bertlv.NewChildren(
			bertlv.ContextSpecific.Constructed(0),
			bertlv.NewValue(bertlv.Universal.Primitive(12), []byte("2.4.0")),
			bertlv.NewChildren(bertlv.ContextSpecific.Constructed(10)),
			bertlv.NewValue(bertlv.ContextSpecific.Primitive(4), freeSpace),
		)
	}

	tests := []struct {
		name    string
		tlv     func() *bertlv.TLV
		want    int32
		wantErr string
	}{
		{name: "valid", tlv: valid, want: 256},
		{name: "empty response", tlv: func() *bertlv.TLV { return nil }, wantErr: "eUICC info is empty"},
		{
			name: "missing SAS-UP",
			tlv: func() *bertlv.TLV {
				value := valid()
				value.Children = value.Children[1:]
				return value
			},
			wantErr: "read SAS-UP version: field is missing",
		},
		{
			name: "missing certificate list",
			tlv: func() *bertlv.TLV {
				value := valid()
				value.Children = append(value.Children[:1], value.Children[2:]...)
				return value
			},
			wantErr: "read certificate issuers: field is missing",
		},
		{
			name: "missing resource",
			tlv: func() *bertlv.TLV {
				value := valid()
				value.Children = value.Children[:2]
				return value
			},
			wantErr: "read free non-volatile memory: resource field is missing",
		},
		{
			name: "missing free-space integer",
			tlv: func() *bertlv.TLV {
				value := valid()
				value.Children[2] = bertlv.NewValue(bertlv.ContextSpecific.Primitive(4), nil)
				return value
			},
			wantErr: "read free non-volatile memory: field is missing",
		},
		{
			name: "oversized free-space integer",
			tlv: func() *bertlv.TLV {
				encoded, marshalErr := bertlv.NewValue(bertlv.ContextSpecific.Primitive(2), []byte{1, 2, 3, 4, 5}).MarshalBinary()
				if marshalErr != nil {
					t.Fatalf("marshal oversized free space: %v", marshalErr)
				}
				value := valid()
				value.Children[2] = bertlv.NewValue(bertlv.ContextSpecific.Primitive(4), encoded)
				return value
			},
			wantErr: "decode free non-volatile memory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &Info{EID: "89049032"}
			err := parseEUICCInfo2(info, tt.tlv())
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseEUICCInfo2() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseEUICCInfo2() error = %v", err)
			}
			if info.FreeSpace != tt.want {
				t.Fatalf("free space = %d, want %d", info.FreeSpace, tt.want)
			}
		})
	}
}
