package domain

import "time"

// Notification é o registro de auditoria de um alerta enviado (RF-029).
type Notification struct {
	ID          int64
	EndpointID  int64
	IncidentID  *int64
	Channel     string // "email" | "webhook"
	Payload     string // JSON serializado (destino + conteúdo)
	DeliveredAt *time.Time
	Status      string // "sent" | "failed"
}

// NotificationResult é o retorno de entrega de um canal.
type NotificationResult struct {
	Channel     string
	Delivered   bool
	Error       string
	DeliveredAt time.Time
}
