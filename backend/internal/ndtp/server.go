package ndtp

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type TelemetryHandler func(record *NavRecord)

type Server struct {
	port           int
	readTimeout    time.Duration
	handler        TelemetryHandler
	logger         zerolog.Logger
	listener       net.Listener
	mu             sync.Mutex
	activeConns    map[net.Conn]struct{}
	isShuttingDown bool
}

func NewServer(port int, readTimeout time.Duration, handler TelemetryHandler, logger zerolog.Logger) *Server {
	return &Server{
		port:        port,
		readTimeout: readTimeout,
		handler:     handler,
		logger:      logger.With().Str("component", "ndtp-server").Logger(),
		activeConns: make(map[net.Conn]struct{}),
	}
}

func (s *Server) Start(ctx context.Context) error {
	addr := fmt.Sprintf("0.0.0.0:%d", s.port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to bind NDTP TCP port %d: %w", s.port, err)
	}
	s.listener = l

	s.logger.Info().Str("listen_addr", addr).Msg("NDTP TCP receiver successfully bound")

	go func() {
		<-ctx.Done()
		s.Shutdown()
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			s.mu.Lock()
			closing := s.isShuttingDown
			s.mu.Unlock()
			if closing {
				return nil
			}
			s.logger.Error().Err(err).Msg("failed to accept incoming TCP connection")
			continue
		}

		s.trackConn(conn, true)
		go s.handleConnection(conn)
	}
}

func (s *Server) trackConn(c net.Conn, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if add {
		s.activeConns[c] = struct{}{}
	} else {
		delete(s.activeConns, c)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer func() {
		conn.Close()
		s.trackConn(conn, false)
	}()

	remoteAddr := conn.RemoteAddr().String()
	s.logger.Debug().Str("remote_addr", remoteAddr).Msg("client connected to NDTP server")

	headerBuf := make([]byte, NPLHeaderSize)

	for {
		_ = conn.SetReadDeadline(time.Now().Add(s.readTimeout))

		// 1. Читаем NPL-заголовок (ровно 15 байт)
		_, err := io.ReadFull(conn, headerBuf)
		if err != nil {
			if errorsIsEOF(err) {
				s.logger.Info().Str("remote_addr", remoteAddr).Msg("client closed connection")
			} else {
				s.logger.Warn().Err(err).Str("remote_addr", remoteAddr).Msg("connection read error, closing socket")
			}
			return
		}

		npl, err := ParseNPLHeader(headerBuf)
		if err != nil {
			s.logger.Error().Err(err).Msg("failed to parse NPL header, desyncing")
			return
		}

		// 2. Читаем полезную нагрузку (NPH + Body)
		dataBuf := make([]byte, npl.DataSize)
		_, err = io.ReadFull(conn, dataBuf)
		if err != nil {
			s.logger.Error().Err(err).Msg("failed to read NPH and payload body")
			return
		}

		// 3. Валидация контрольной суммы CRC-16
		if !CheckCRC(dataBuf, npl.CRC) {
			s.logger.Warn().
				Uint32("unit_id", npl.PeerAddress).
				Uint16("expected_crc", npl.CRC).
				Msg("packet CRC mismatch, skipping frame")
			continue
		}

		// 4. Разбор NPH
		nph, err := ParseNPHHeader(dataBuf)
		if err != nil {
			s.logger.Error().Err(err).Msg("failed to parse NPH header")
			continue
		}

		// Обработка Handshake
		if nph.ServiceID == ServiceGenericControls && nph.Type == TypeConnRequest {
			s.logger.Info().
				Uint32("unit_id", npl.PeerAddress).
				Str("remote_addr", remoteAddr).
				Msg("handshake (CONN_REQUEST) received from terminal")
			continue
		}

		// Обработка Realtime данных
		if nph.ServiceID == ServiceNavdata && nph.Type == TypeRealtime {
			body := dataBuf[NPHHeaderSize:]
			if len(body) == 0 {
				s.logger.Warn().Msg("empty realtime payload received")
				continue
			}

			navRecord, err := ParseNav00Cell(npl.PeerAddress, body)
			if err != nil {
				s.logger.Warn().Err(err).Uint32("unit_id", npl.PeerAddress).Msg("failed to parse Nav00 cell")
				continue
			}

			s.logger.Info().
				Uint32("unit_id", navRecord.UnitID).
				Float64("lat", navRecord.Latitude).
				Float64("lon", navRecord.Longitude).
				Uint16("speed", navRecord.SpeedAvg).
				Msg(">> NDTP NAV PACKET RECEIVED")

			if s.handler != nil {
				s.handler(navRecord)
			}
		}
	}
}

func (s *Server) Shutdown() {
	s.mu.Lock()
	s.isShuttingDown = true
	if s.listener != nil {
		_ = s.listener.Close()
	}
	for conn := range s.activeConns {
		_ = conn.Close()
	}
	s.mu.Unlock()
	s.logger.Info().Msg("NDTP TCP server shut down")
}

func errorsIsEOF(err error) bool {
	return err == io.EOF || err == io.ErrUnexpectedEOF
}
