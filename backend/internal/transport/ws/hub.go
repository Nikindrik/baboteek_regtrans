package ws

import (
	"encoding/json"
	"net/http"
	"sync"

	"baboteek_regtrans/internal/domain"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Разрешаем подключение с любого Origin (для React/Vue)
	},
}

type WSEvent struct {
	Type    string `json:"type"` // "INIT", "VEHICLE_UPDATE", "INCIDENT"
	Payload any    `json:"payload"`
}

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	repo       domain.FleetRepository
	logger     zerolog.Logger
	mu         sync.RWMutex
}

func NewHub(repo domain.FleetRepository, logger zerolog.Logger) *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 1024),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		repo:       repo,
		logger:     logger.With().Str("component", "ws-hub").Logger(),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()

			initData := WSEvent{
				Type: "INIT",
				Payload: map[string]any{
					"vehicles":  h.repo.GetAllVehiclesSnapshot(),
					"incidents": h.repo.GetRecentIncidents(),
				},
			}
			bytes, _ := json.Marshal(initData)
			client.send <- bytes

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			var deadClients []*Client

			// 1. Только читаем под RLock
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					deadClients = append(deadClients, client)
				}
			}
			h.mu.RUnlock()

			// 2. Удаление зависших сокетов строго под эксклюзивным Lock!
			if len(deadClients) > 0 {
				h.mu.Lock()
				for _, client := range deadClients {
					if _, ok := h.clients[client]; ok {
						delete(h.clients, client)
						close(client.send)
					}
				}
				h.mu.Unlock()
			}
		}
	}
}

// BroadcastEvent сериализует событие и мгновенно рассылает всем вкладкам браузера
func (h *Hub) BroadcastEvent(eventType string, payload any) {
	evt := WSEvent{
		Type:    eventType,
		Payload: payload,
	}
	bytes, err := json.Marshal(evt)
	if err == nil {
		select {
		case h.broadcast <- bytes:
		default:
			h.logger.Warn().Msg("ws broadcast channel saturated, frame dropped")
		}
	}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to upgrade to websocket")
		return
	}
	client := &Client{hub: h, conn: conn, send: make(chan []byte, 256), logger: h.logger}
	h.register <- client

	go client.writePump()
	go client.readPump()
}
