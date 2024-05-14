package handshake

import (
	"crypto/sha256"
	"fmt"
	"net"
	"time"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/lib"
	"ergo.services/ergo/net/edf"
)

func (h *handshake) Accept(node gen.NodeHandshake, conn net.Conn, options gen.HandshakeOptions) (gen.HandshakeResult, error) {
	var result gen.HandshakeResult
	result.HandshakeVersion = h.Version()

	v, tail, err := h.readMessage(conn, time.Second, nil)
	if err != nil {
		return result, err
	}
	switch m := v.(type) {
	case MessageHello:
		hash := sha256.New()
		hash.Write([]byte(fmt.Sprintf("%s:%s", m.Salt, options.Cookie)))

		if m.Digest != fmt.Sprintf("%x", hash.Sum(nil)) {
			return result, fmt.Errorf("incorrect digest")
		}

		salt := lib.RandomString(64)
		hash = sha256.New()
		hash.Write([]byte(fmt.Sprintf("%s:%s:%s", salt, m.Digest, options.Cookie)))
		hello := MessageHello{
			Salt:   salt,
			Digest: fmt.Sprintf("%x", hash.Sum(nil)),
		}
		if err := h.writeMessage(conn, hello); err != nil {
			return result, err
		}

	case MessageJoin:
		result.Peer = m.Node
		hash := sha256.New()
		hash.Write([]byte(fmt.Sprintf("%s:%s", m.ConnectionID, options.Cookie)))
		if m.Digest != fmt.Sprintf("%x", hash.Sum(nil)) {
			return result, fmt.Errorf("incorrect join digest")
		}
		result.ConnectionID = m.ConnectionID
		result.Custom = ConnectionOptions{}
		if err := h.writeMessage(conn, MessageAccept{}); err != nil {
			return result, err
		}
		if len(h.atom_mapping) > 0 {
			result.AtomMapping = make(map[gen.Atom]gen.Atom)
			for k, v := range h.atom_mapping {
				result.AtomMapping[k] = v
			}
		}
		return result, nil

	default:
		return result, fmt.Errorf("malformed handshake Hello/Join message")
	}

	// wait for the introduce message
	v, tail, err = h.readMessage(conn, time.Second, nil)
	if err != nil {
		return result, err
	}

	intro, ok := v.(MessageIntroduce)
	if ok == false {
		return result, fmt.Errorf("malformed handshake Introduce message")
	}

	if intro.Node == node.Name() {
		return result, fmt.Errorf("malformed handshake Introduce message (same name)")
	}

	accept := MessageAccept{}
	accept.ID = lib.RandomString(32)
	accept.PoolSize = h.poolsize
	accept.PoolDSN = append(accept.PoolDSN, conn.LocalAddr().String())
	if err := h.writeMessage(conn, accept); err != nil {
		return result, err
	}

	intro2 := MessageIntroduce{
		Node:     node.Name(),
		Version:  node.Version(),
		Flags:    options.Flags,
		Creation: node.Creation(),

		MaxMessageSize: options.MaxMessageSize,

		AtomCache: edf.GetAtomCache(),
		RegCache:  edf.GetRegCache(),
		ErrCache:  edf.GetErrCache(),
	}
	if err := h.writeMessage(conn, intro2); err != nil {
		return result, err
	}

	// wait for the accept message
	v, tail, err = h.readMessage(conn, time.Second, tail)
	if err != nil {
		return result, err
	}

	if _, ok := v.(MessageAccept); ok == false {
		return result, fmt.Errorf("malformed handshake Accept message")
	}

	result.ConnectionID = accept.ID
	result.Peer = intro.Node
	result.PeerVersion = intro.Version
	result.PeerCreation = intro.Creation
	result.PeerFlags = intro.Flags
	result.PeerMaxMessageSize = intro.MaxMessageSize
	result.NodeFlags = options.Flags
	result.NodeMaxMessageSize = options.MaxMessageSize
	result.Tail = tail

	custom := ConnectionOptions{
		PoolSize:        h.poolsize,
		EncodeAtomCache: h.makeEncodeAtomCache(intro2.AtomCache),
		EncodeRegCache:  h.makeEncodeRegCache(intro2.RegCache),
		EncodeErrCache:  h.makeEncodeErrCache(intro2.ErrCache),
		DecodeAtomCache: h.makeDecodeAtomCache(intro.AtomCache),
		DecodeRegCache:  h.makeDecodeRegCache(intro.RegCache),
		DecodeErrCache:  h.makeDecodeErrCache(intro2.ErrCache, intro.ErrCache),
	}
	result.Custom = custom

	return result, nil
}
