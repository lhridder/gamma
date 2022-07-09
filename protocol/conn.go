package protocol

import (
	"github.com/sandertv/go-raknet"
	"net"
)

type ProcessedConn struct {
	*raknet.Conn
	RemoteAddr net.Addr
	ServerAddr string
	Username   string
	ReadBytes  []byte
}

func (c ProcessedConn) Disconnect(msg string) error {
	defer c.Close()
	pk := Disconnect{
		HideDisconnectionScreen: msg == "",
		Message:                 msg,
	}

	b, err := MarshalPacket(&pk)
	if err != nil {
		return err
	}

	if _, err := c.Write(b); err != nil {
		return err
	}

	return nil
}
