package at

import (
	"bufio"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

var commandTimeout = 10 * time.Second

type AT struct {
	f          *os.File
	reader     *bufio.Reader
	oldTermios *unix.Termios
	mutex      sync.Mutex
}

func Open(device string) (*AT, error) {
	var at AT
	var err error
	if at.f, err = os.OpenFile(device, os.O_RDWR|unix.O_NOCTTY, 0666); err != nil {
		return nil, err
	}
	if err := at.setTermios(); err != nil {
		_ = at.f.Close()
		return nil, err
	}
	at.reader = bufio.NewReader(at.f)
	return &at, nil
}

func (a *AT) setTermios() error {
	fd := int(a.f.Fd())
	var err error
	if a.oldTermios, err = unix.IoctlGetTermios(fd, unix.TCGETS); err != nil {
		return err
	}
	t := *a.oldTermios
	t.Ispeed = unix.B9600
	t.Ospeed = unix.B9600
	t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	t.Oflag &^= unix.OPOST
	t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Cflag &^= unix.CSIZE | unix.PARENB
	t.Cflag |= unix.CS8 | unix.CREAD | unix.CLOCAL
	t.Cc[unix.VMIN] = 1
	t.Cc[unix.VTIME] = 0
	return unix.IoctlSetTermios(fd, unix.TCSETS, &t)
}

func (a *AT) Run(command string) (string, error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := a.f.SetDeadline(time.Now().Add(commandTimeout)); err != nil {
		if !errors.Is(err, os.ErrNoDeadline) {
			return "", err
		}
	} else {
		defer func() {
			_ = a.f.SetDeadline(time.Time{})
		}()
	}
	if _, err := a.f.WriteString(command + "\r\n"); err != nil {
		return "", err
	}
	return readResponse(a.reader, command)
}

func readResponse(reader *bufio.Reader, command string) (string, error) {
	var sb strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimSpace(line)
		if line == "" || line == command {
			continue
		}
		switch {
		case line == "OK":
			return strings.TrimSpace(sb.String()), nil
		case terminalError(line):
			return "", errors.New(line)
		default:
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
	}
}

func terminalError(line string) bool {
	switch line {
	case "ERROR", "NO CARRIER", "NO DIALTONE", "BUSY", "NO ANSWER":
		return true
	default:
		return strings.HasPrefix(line, "+CME ERROR:") || strings.HasPrefix(line, "+CMS ERROR:")
	}
}

func (a *AT) Support(command string) bool {
	_, err := a.Run(command)
	return err == nil
}

func (a *AT) Close() error {
	var restoreErr error
	if a.oldTermios != nil {
		restoreErr = unix.IoctlSetTermios(int(a.f.Fd()), unix.TCSETS, a.oldTermios)
	}
	return errors.Join(restoreErr, a.f.Close())
}
