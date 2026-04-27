// Copyright 2026 Ekaterina Godulyan
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ws

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// В разработке разрешаем все origin
		return true
	},
}

// Server представляет WebSocket‑сервер, который обрабатывает подключения и передаёт их хабу.
type Server struct {
	hub *Hub
}

// NewServer создаёт новый WebSocket‑сервер, использующий указанный хаб.
func NewServer(hub *Hub) *Server {
	return &Server{hub: hub}
}

// ServeHTTP обрабатывает HTTP‑запросы на эндпоинт /ws.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	// Регистрируем соединение в хабе
	s.hub.Register(conn)
	defer s.hub.Unregister(conn)

	// Читаем сообщения от клиента (пока не нужны, но поддерживаем ping/pong)
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			break
		}
		// Игнорируем входящие сообщения, так как клиент только слушает уведомления
	}
}