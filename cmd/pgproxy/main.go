// pgproxy: PostgreSQL SNI routing proxy.
//
// Handles the PostgreSQL SSLRequest/response step, then peeks at the TLS
// ClientHello to extract SNI — but does NOT terminate TLS. The TLS stream is
// passed through transparently so client↔CNPG TLS is end-to-end, which avoids
// the SCRAM channel-binding check that fails on a MITM proxy.
//
// For sslmode=disable connections (no TLS), routes by the database field in
// the startup packet (must be the 32-char project UUID without dashes).
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"
)

const (
	sslRequestCode = 0x04d2162f
	listenAddr     = ":5432"
	cnpgDB         = "app"
)

var pgNamespace string

func main() {
	pgNamespace = os.Getenv("PG_NAMESPACE")
	if pgNamespace == "" {
		pgNamespace = "postgres-instances"
	}

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	log.Printf("pgproxy listening on %s (ns=%s)", listenAddr, pgNamespace)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		go handle(conn)
	}
}

func handle(client net.Conn) {
	defer client.Close()

	first, err := readPGMessage(client)
	if err != nil {
		return
	}

	var projectID string
	// Bytes to prepend to the upstream connection after SSLRequest negotiation.
	var upstreamPrefix []byte

	if isSSLRequest(first) {
		// Step 1: Tell the client we support SSL.
		if _, err := client.Write([]byte{'S'}); err != nil {
			return
		}

		// Step 2: Read the TLS ClientHello (peek only — do NOT terminate TLS).
		clientHello, err := readTLSRecord(client)
		if err != nil {
			return
		}

		// Step 3: Extract SNI from the ClientHello without doing a handshake.
		sni := sniFromClientHello(clientHello)
		projectID = projectFromSNI(sni)

		// Step 4: The TLS ClientHello must be forwarded to CNPG as-is after
		// CNPG's own SSLRequest exchange (below). Save it as the prefix.
		upstreamPrefix = clientHello

	} else {
		// No TLS — route by database field (must be 32-hex project ID).
		params, err := parseStartupParams(first)
		if err != nil {
			return
		}
		db := params["database"]
		if len(db) == 32 {
			projectID = db
		}
		// For plain connections we must rewrite the database and forward the
		// (possibly modified) startup packet to CNPG.
		if projectID != "" {
			params["database"] = cnpgDB
			upstreamPrefix = buildStartupMessage(params)
		}
	}

	if projectID == "" {
		writeError(client, "cannot route: SNI must be db-{32hexchars}.postgres.{domain} or database must be the 32-char project ID")
		return
	}

	projectNamespace := "pg-" + projectID
	backendAddr := fmt.Sprintf("db-%s-rw.%s.svc.cluster.local:5432", projectID, projectNamespace)
	upstream, err := net.DialTimeout("tcp", backendAddr, 5*time.Second)
	if err != nil {
		legacyAddr := fmt.Sprintf("db-%s-rw.%s.svc.cluster.local:5432", projectID, pgNamespace)
		backendAddr = legacyAddr
		upstream, err = net.DialTimeout("tcp", backendAddr, 5*time.Second)
		if err != nil {
			log.Printf("dial %s: %v", backendAddr, err)
			writeError(client, "cannot connect to project database")
			return
		}
	}
	defer upstream.Close()

	// Negotiate SSL with CNPG backend.
	sslReq := make([]byte, 8)
	binary.BigEndian.PutUint32(sslReq[0:], 8)
	binary.BigEndian.PutUint32(sslReq[4:], sslRequestCode)
	if _, err := upstream.Write(sslReq); err != nil {
		return
	}
	resp := make([]byte, 1)
	if _, err := io.ReadFull(upstream, resp); err != nil {
		return
	}
	if resp[0] != 'S' {
		writeError(client, "backend does not support SSL")
		return
	}
	// CNPG responded 'S' and is now waiting for the TLS ClientHello.
	// Forward the saved prefix (ClientHello for TLS, or startup packet for plain).
	if len(upstreamPrefix) > 0 {
		if _, err := upstream.Write(upstreamPrefix); err != nil {
			return
		}
	}

	log.Printf("proxying project=%s…", projectID[:8])

	// Transparent bidirectional proxy — TLS handshake and all data flow through.
	go io.Copy(upstream, client)
	io.Copy(client, upstream)
}

// readTLSRecord reads one TLS record (5-byte header + payload) and returns all bytes.
func readTLSRecord(r io.Reader) ([]byte, error) {
	hdr := make([]byte, 5)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, err
	}
	if hdr[0] != 0x16 { // ContentType = Handshake
		return nil, fmt.Errorf("expected TLS handshake record, got 0x%02x", hdr[0])
	}
	payloadLen := int(binary.BigEndian.Uint16(hdr[3:]))
	if payloadLen > 1<<16 {
		return nil, fmt.Errorf("TLS record too large: %d", payloadLen)
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return append(hdr, payload...), nil
}

// sniFromClientHello parses raw TLS record bytes and returns the SNI hostname.
func sniFromClientHello(record []byte) string {
	if len(record) < 5 {
		return ""
	}
	data := record[5:] // skip TLS record header
	// Handshake: type(1) + length(3) + ClientHello body
	if len(data) < 4 || data[0] != 0x01 {
		return ""
	}
	// ClientHello: ProtocolVersion(2) + Random(32) = 34 bytes, then SessionID
	pos := 4 + 2 + 32
	if pos >= len(data) {
		return ""
	}
	sessLen := int(data[pos])
	pos += 1 + sessLen
	if pos+2 > len(data) {
		return ""
	}
	cipherLen := int(binary.BigEndian.Uint16(data[pos:]))
	pos += 2 + cipherLen
	if pos+1 > len(data) {
		return ""
	}
	compLen := int(data[pos])
	pos += 1 + compLen
	if pos+2 > len(data) {
		return ""
	}
	extTotal := int(binary.BigEndian.Uint16(data[pos:]))
	pos += 2
	end := pos + extTotal
	for pos+4 <= end && pos+4 <= len(data) {
		extType := binary.BigEndian.Uint16(data[pos:])
		extLen := int(binary.BigEndian.Uint16(data[pos+2:]))
		pos += 4
		if extType == 0x0000 && pos+extLen <= len(data) { // SNI extension
			p := pos + 2                                   // skip list length
			if p+3 <= pos+extLen && data[p] == 0x00 {     // NameType = host_name
				nameLen := int(binary.BigEndian.Uint16(data[p+1:]))
				if p+3+nameLen <= len(data) {
					return string(data[p+3 : p+3+nameLen])
				}
			}
		}
		pos += extLen
	}
	return ""
}

// projectFromSNI extracts the 32-hex project ID from db-{32hex}.postgres.{domain}.
func projectFromSNI(sni string) string {
	label := strings.SplitN(sni, ".", 2)[0] // "db-05105d77facd486e815a0a25a320987d"
	if strings.HasPrefix(label, "db-") {
		if id := label[3:]; len(id) == 32 {
			return id
		}
	}
	return ""
}

func readPGMessage(r io.Reader) ([]byte, error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, err
	}
	msgLen := int(binary.BigEndian.Uint32(hdr))
	if msgLen < 4 || msgLen > 1<<24 {
		return nil, fmt.Errorf("invalid pg message length: %d", msgLen)
	}
	body := make([]byte, msgLen-4)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return append(hdr, body...), nil
}

func isSSLRequest(buf []byte) bool {
	return len(buf) == 8 && binary.BigEndian.Uint32(buf[4:]) == sslRequestCode
}

func parseStartupParams(buf []byte) (map[string]string, error) {
	if len(buf) < 8 {
		return nil, fmt.Errorf("startup too short")
	}
	params := make(map[string]string)
	data := buf[8:] // skip length(4) + protocol version(4)
	for {
		i := bytes.IndexByte(data, 0)
		if i <= 0 {
			break
		}
		key := string(data[:i])
		data = data[i+1:]
		i = bytes.IndexByte(data, 0)
		if i < 0 {
			break
		}
		params[key] = string(data[:i])
		data = data[i+1:]
	}
	return params, nil
}

func buildStartupMessage(params map[string]string) []byte {
	var body []byte
	proto := make([]byte, 4)
	binary.BigEndian.PutUint32(proto, 0x00030000)
	body = append(body, proto...)
	for k, v := range params {
		body = append(body, k...)
		body = append(body, 0)
		body = append(body, v...)
		body = append(body, 0)
	}
	body = append(body, 0)
	hdr := make([]byte, 4)
	binary.BigEndian.PutUint32(hdr, uint32(len(body)+4))
	return append(hdr, body...)
}

func writeError(conn net.Conn, msg string) {
	fields := "SFATAL\x00" + "C08000\x00" + "M" + msg + "\x00\x00"
	buf := make([]byte, 1+4+len(fields))
	buf[0] = 'E'
	binary.BigEndian.PutUint32(buf[1:], uint32(4+len(fields)))
	copy(buf[5:], fields)
	conn.Write(buf) //nolint:errcheck
}
