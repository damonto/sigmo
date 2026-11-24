package ims

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"github.com/pion/stun/v4"
	"github.com/things-go/go-socks5/statute"

	pinternet "github.com/damonto/sigmo/internal/pkg/internet"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestSOCKS5PacketConnRelaysBothIKEPortsAndDetectsControlClose(t *testing.T) {
	relay, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = relay.Close() })
	dropControl := make(chan struct{})
	underlay := startWiFiProxyTestServer(t, func(conn net.Conn, request statute.Request) {
		if request.Command != statute.CommandAssociate {
			t.Errorf("SOCKS5 command = %d, want UDP ASSOCIATE", request.Command)
			return
		}
		// Advertise a wildcard relay address to check the control-peer fallback.
		writeWiFiProxyTestReply(t, conn, statute.RepSuccess, "0.0.0.0", relay.LocalAddr().(*net.UDPAddr).Port)
		<-dropControl
	})
	defer closeIfOpen(dropControl)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	conn, _, err := underlay.openUDP(ctx, "[::]:0")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if local := conn.LocalAddr().(*net.UDPAddr); local.IP.IsUnspecified() {
		t.Errorf("local address = %v, want concrete IP for NAT detection", local)
	}
	deadline, _ := ctx.Deadline()
	if err := relay.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	payload := []byte{0xff, 0x00, 0x01}
	var buf [128]byte
	for _, endpoint := range []netip.AddrPort{
		netip.MustParseAddrPort("[2001:db8::20]:500"),
		netip.MustParseAddrPort("[2001:db8::20]:4500"),
	} {
		target := net.UDPAddrFromAddrPort(endpoint)
		if n, err := conn.WriteTo(payload, target); err != nil || n != len(payload) {
			t.Fatalf("WriteTo() = %d, %v", n, err)
		}
		n, client, err := relay.ReadFromUDP(buf[:])
		if err != nil {
			t.Fatal(err)
		}
		packet, err := statute.ParseDatagram(buf[:n])
		if err != nil || packet.DstAddr.String() != endpoint.String() {
			t.Fatalf("UDP target = %v, %v, want %v", packet.DstAddr.String(), err, endpoint)
		}
		if _, err := relay.WriteToUDP(buf[:n], client); err != nil {
			t.Fatal(err)
		}
		n, source, err := conn.ReadFrom(buf[:])
		if err != nil || n != len(payload) || source.String() != endpoint.String() {
			t.Fatalf("ReadFrom() = %d, %v, %v, want %v", n, source, err, endpoint)
		}
	}
	closeIfOpen(dropControl)
	if _, _, err := conn.ReadFrom(buf[:]); !errors.Is(err, errSOCKS5ControlLost) {
		t.Fatalf("ReadFrom() after control close = %v, want control lost", err)
	}
}

func TestSOCKS5PacketConnLocalCloseReleasesBothSockets(t *testing.T) {
	relay, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	controlClosed := make(chan error, 1)
	underlay := startWiFiProxyTestServer(t, func(conn net.Conn, request statute.Request) {
		writeWiFiProxyTestReply(t, conn, statute.RepSuccess, "127.0.0.1", relay.LocalAddr().(*net.UDPAddr).Port)
		_, err := io.Copy(io.Discard, conn)
		controlClosed <- err
	})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	conn, _, err := underlay.openUDP(ctx, ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("repeated Close() = %v", err)
	}
	if _, _, err := conn.ReadFrom(make([]byte, 1)); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("ReadFrom() after local close = %v, want net.ErrClosed", err)
	}
	select {
	case err := <-controlClosed:
		if err != nil {
			t.Fatalf("SOCKS5 server did not receive EOF: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("SOCKS5 control connection stayed open")
	}
}

func TestSOCKS5UsernamePasswordAuthentication(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status byte
		want   error
	}{
		{name: "accepted"},
		{name: "rejected", status: 1, want: errSOCKS5Auth},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client, server := net.Pipe()
			defer client.Close()
			if err := client.SetDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer server.Close()
				method, err := statute.ParseMethodRequest(server)
				if err != nil || !bytes.Equal(method.Methods, []byte{statute.MethodUserPassAuth}) {
					t.Errorf("expected only username/password authentication, got %v, %v", method, err)
					return
				}
				if _, err := server.Write([]byte{statute.VersionSocks5, statute.MethodUserPassAuth}); err != nil {
					t.Error(err)
					return
				}
				request, err := statute.ParseUserPassRequest(server)
				if err != nil || string(request.User) != "user:name" || string(request.Pass) != "fixture@password" {
					t.Errorf("username/password authentication did not preserve the configured values: %v", err)
					return
				}
				_, _ = server.Write([]byte{1, tt.status})
			}()
			underlay := socks5Underlay{proxy: wifiProxy{WiFiProxySettings: WiFiProxySettings{
				Username: "user:name", Password: "fixture@password",
			}}}
			err := underlay.authenticate(client)
			_ = client.Close()
			<-done
			if !errors.Is(err, tt.want) {
				t.Fatalf("authenticate() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestWiFiProxyTCPOnlyCheckKeepsTCPResult(t *testing.T) {
	underlay := startWiFiProxyTestServer(t, func(conn net.Conn, request statute.Request) {
		if request.Command == statute.CommandAssociate {
			writeWiFiProxyTestReply(t, conn, statute.RepCommandNotSupported, "0.0.0.0", 0)
			return
		}
		if request.Command != statute.CommandConnect || request.DstAddr.FQDN != "echo.example" {
			t.Errorf("unexpected SOCKS5 CONNECT: %+v", request)
			return
		}
		writeWiFiProxyTestReply(t, conn, statute.RepSuccess, "127.0.0.1", 10000)
		var req [2048]byte
		if _, err := conn.Read(req[:]); err != nil {
			t.Errorf("read proxied HTTP request: %v", err)
			return
		}
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 12\r\nConnection: close\r\n\r\n203.0.113.10")
	})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	tcp := checkWiFiProxyTCP(ctx, underlay, func(ctx context.Context, client *http.Client) (pinternet.PublicIPInfo, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://echo.example/", nil)
		if err != nil {
			return pinternet.PublicIPInfo{}, err
		}
		resp, err := client.Do(request)
		if err != nil {
			return pinternet.PublicIPInfo{}, err
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		return pinternet.PublicIPInfo{IP: string(data)}, err
	})
	if tcp.ErrorCode != "" || tcp.IP.String() != "203.0.113.10" || tcp.ServerAddress == "" {
		t.Fatalf("TCP result = %+v", tcp)
	}
	udp := checkWiFiProxyUDP(ctx, underlay, "127.0.0.1:3478")
	if udp.ErrorCode != "udp_associate_rejected" || udp.ServerAddress == "" {
		t.Fatalf("UDP result = %+v", udp)
	}
}

func TestSOCKS5ResolvesDNSOverProxyTCP(t *testing.T) {
	underlay := startWiFiProxyTestServer(t, func(conn net.Conn, request statute.Request) {
		if request.Command != statute.CommandConnect || request.DstAddr.String() != "1.1.1.1:53" {
			t.Errorf("unexpected proxy DNS request: %+v", request)
			return
		}
		writeWiFiProxyTestReply(t, conn, statute.RepSuccess, "127.0.0.1", 10000)
		var length [2]byte
		if _, err := io.ReadFull(conn, length[:]); err != nil {
			t.Error(err)
			return
		}
		query := make([]byte, binary.BigEndian.Uint16(length[:]))
		if _, err := io.ReadFull(conn, query); err != nil {
			t.Error(err)
			return
		}
		// Copy the query header and the question, discarding the optional EDNS record.
		questionEnd := 12 + len("\x04epdg\x07example\x00") + 4
		if len(query) < questionEnd {
			t.Errorf("proxy DNS question is truncated: %d bytes", len(query))
			return
		}
		response := append([]byte(nil), query[:questionEnd]...)
		binary.BigEndian.PutUint16(response[2:4], 0x8180)
		binary.BigEndian.PutUint16(response[6:8], 1)
		binary.BigEndian.PutUint16(response[10:12], 0)
		response = append(response, 0xc0, 12, 0, 1, 0, 1, 0, 0, 0, 10, 0, 4, 192, 0, 2, 11)
		binary.BigEndian.PutUint16(length[:], uint16(len(response)))
		if _, err := conn.Write(append(length[:], response...)); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	addresses, err := underlay.LookupNetIP(ctx, "ip4", "epdg.example.")
	if err != nil || len(addresses) != 1 || addresses[0].String() != "192.0.2.11" {
		t.Fatalf("LookupNetIP() = %v, %v", addresses, err)
	}
}

func TestWiFiProxySTUNObservesUDPExit(t *testing.T) {
	relay, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	underlay := startWiFiProxyTestServer(t, func(conn net.Conn, request statute.Request) {
		if request.Command != statute.CommandAssociate {
			t.Errorf("unexpected command %d", request.Command)
			return
		}
		addr := relay.LocalAddr().(*net.UDPAddr)
		writeWiFiProxyTestReply(t, conn, statute.RepSuccess, addr.IP.String(), addr.Port)
		_, _ = io.Copy(io.Discard, conn)
	})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	if err := relay.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		var buf [2048]byte
		n, from, err := relay.ReadFromUDP(buf[:])
		if err != nil {
			t.Error(err)
			return
		}
		request, err := statute.ParseDatagram(buf[:n])
		if err != nil {
			t.Error(err)
			return
		}
		var message stun.Message
		if err := stun.Decode(request.Data, &message); err != nil {
			t.Error(err)
			return
		}
		response, err := stun.Build(stun.NewTransactionIDSetter(message.TransactionID), stun.BindingSuccess,
			stun.XORMappedAddress{IP: net.ParseIP("2001:db8::10"), Port: 42000})
		if err != nil {
			t.Error(err)
			return
		}
		request.Data = response.Raw
		if _, err := relay.WriteToUDP(request.Bytes(), from); err != nil {
			t.Error(err)
		}
	}()
	result := checkWiFiProxyUDP(ctx, underlay, "127.0.0.1:3478")
	<-done
	if result.ErrorCode != "" || result.IP.String() != "2001:db8::10" || result.Port != 42000 || result.RelayAddress == "" {
		t.Fatalf("UDP result = %+v", result)
	}
}

func TestWiFiProxyCatalogPreservesWriteOnlyPassword(t *testing.T) {
	store, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	catalog := &wifiProxyCatalog{store: store}
	settings := WiFiProxySettings{Name: "Relay", Host: "2001:db8::4", Username: "user", Password: "a:secret"}
	summary, err := catalog.save(t.Context(), "", settings)
	if err != nil || summary.Port != 1080 || !summary.HasPassword {
		t.Fatalf("save() = %+v, %v", summary, err)
	}
	body, err := json.Marshal(summary)
	if err != nil || bytes.Contains(body, []byte(`"password"`)) || bytes.Contains(body, []byte(settings.Password)) {
		t.Fatalf("summary exposes password or failed to encode: %s, %v", body, err)
	}
	settings.Name = "Updated relay"
	settings.Password = ""
	if _, err := catalog.save(t.Context(), summary.ID, settings); err != nil {
		t.Fatal(err)
	}
	saved, err := catalog.get(t.Context(), summary.ID)
	if err != nil || saved.Password != "a:secret" || saved.Name != "Updated relay" {
		t.Fatal("failed to update proxy while preserving the saved password")
	}
	settings.Username = ""
	if _, err := catalog.save(t.Context(), summary.ID, settings); err != nil {
		t.Fatal(err)
	}
	if saved, err := catalog.get(t.Context(), summary.ID); err != nil || saved.Password != "" {
		t.Fatal("switching to no authentication did not clear the saved password")
	}
}

func startWiFiProxyTestServer(t *testing.T, handle func(net.Conn, statute.Request)) socks5Underlay {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			serveWiFiProxyTestConnection(t, conn, handle)
		}
	}()
	t.Cleanup(func() { _ = listener.Close(); <-done })
	addr := listener.Addr().(*net.TCPAddr)
	return socks5Underlay{proxy: wifiProxy{WiFiProxySettings: WiFiProxySettings{Host: addr.IP.String(), Port: addr.Port}}}
}

func serveWiFiProxyTestConnection(t *testing.T, conn net.Conn, handle func(net.Conn, statute.Request)) {
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Error(err)
		return
	}
	if _, err := statute.ParseMethodRequest(conn); err != nil {
		t.Error(err)
		return
	}
	if _, err := conn.Write([]byte{statute.VersionSocks5, statute.MethodNoAuth}); err != nil {
		t.Error(err)
		return
	}
	request, err := statute.ParseRequest(conn)
	if err != nil {
		t.Error(err)
		return
	}
	handle(conn, request)
}

func writeWiFiProxyTestReply(t *testing.T, conn net.Conn, status byte, host string, port int) {
	t.Helper()
	reply := statute.Reply{
		Version: statute.VersionSocks5, Response: status,
		BndAddr: statute.AddrSpec{AddrType: statute.ATYPIPv4, IP: net.ParseIP(host), Port: port},
	}
	if _, err := conn.Write(reply.Bytes()); err != nil {
		t.Error(err)
	}
}

func closeIfOpen(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}
