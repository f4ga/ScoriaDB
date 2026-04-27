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
	"encoding/json"
	"log"
	"sync"

	"github.com/gorilla/websocket"
)

// Hub управляет всеми активными WebSocket‑соединениями и рассылкой сообщений.
type Hub struct {
	mu          sync.RWMutex
	connections map[*websocket.Conn]struct{}
	broadcast   chan []byte
}

// NewHub создаёт новый хаб.
func NewHub() *Hub {
	hub := &Hub{
		connections: make(map[*websocket.Conn]struct{}),
		broadcast:   make(chan []byte, 256),
	}
	go hub.run()
	return hub
}

// Register добавляет новое соединение в хаб.
func (h *Hub) Register(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.connections[conn] = struct{}{}
	log.Printf("WebSocket connection registered, total: %d", len(h.connections))
}

// Unregister удаляет соединение из хаба.
func (h *Hub) Unregister(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.connections, conn)
	log.Printf("WebSocket connection unregistered, total: %d", len(h.connections))
}

// Broadcast отправляет сообщение всем зарегистрированным соединениям.
func (h *Hub) Broadcast(msg []byte) {
	h.broadcast <- msg
}

// run обрабатывает рассылку сообщений.
func (h *Hub) run() {
	for msg := range h.broadcast {
		h.mu.RLock()
		for conn := range h.connections {
			err := conn.WriteMessage(websocket.TextMessage, msg)
			if err != nil {
				log.Printf("WebSocket write error: %v", err)
				conn.Close()
				h.mu.RUnlock()
				h.Unregister(conn)
				h.mu.RLock()
			}
		}
		h.mu.RUnlock()
	}
}

// Close закрывает хаб и все соединения.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.connections {
		conn.Close()
	}
	close(h.broadcast)
}

// Event представляет уведомление об изменении данных.
type Event struct {
	Type   string `json:"event"`          // "key_updated" или "key_deleted"
	Key    string `json:"key"`            // ключ
	Value  string `json:"value,omitempty"` // значение (только для key_updated)
	CF     string `json:"cf"`             // column family
}

// NotifyKeyUpdated отправляет уведомление об обновлении ключа.
func (h *Hub) NotifyKeyUpdated(cf, key string, value []byte) {
	event := Event{
		Type:  "key_updated",
		Key:   key,
		Value: string(value),
		CF:    cf,
	}
	msg, _ := json.Marshal(event)
	h.Broadcast(msg)
}

// NotifyKeyDeleted отправляет уведомление об удалении ключа.
func (h *Hub) NotifyKeyDeleted(cf, key string) {
	event := Event{
		Type: "key_deleted",
		Key:  key,
		CF:   cf,
	}
	msg, _ := json.Marshal(event)
	h.Broadcast(msg)
}