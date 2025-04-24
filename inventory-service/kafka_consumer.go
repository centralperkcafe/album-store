// kafka consumer logic for inventory

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// OrderCreatedEvent represents an order creation event from Kafka
type OrderCreatedEvent struct {
	OrderID    string    `json:"orderId"`
	UserID     string    `json:"userId"`
	AlbumID    string    `json:"albumId"`
	Quantity   int       `json:"quantity"`
	Timestamp  time.Time `json:"timestamp"`
}

// PaymentProcessedEvent represents a payment processed event from Kafka
type PaymentProcessedEvent struct {
	OrderID    string    `json:"orderId"`
	Status     string    `json:"status"`
	Timestamp  time.Time `json:"timestamp"`
}

// Order represents an order from the order service
type Order struct {
	ID          int64     `json:"id"`
	UserID      string    `json:"userId"`
	AlbumID     string    `json:"albumId"`
	Quantity    int       `json:"quantity"`
	TotalPrice  float64   `json:"totalPrice"`
	Status      string    `json:"status"`
	CreatedAt   string    `json:"createdAt"`
}

// InventoryUpdatedEvent represents an inventory update event for Kafka
type InventoryUpdatedEvent struct {
	AlbumID            string    `json:"albumId"`
	QuantityAvailable  int       `json:"quantityAvailable"`
	Timestamp          time.Time `json:"timestamp"`
}

// Error definitions
var (
	errNoInventory           = fmt.Errorf("no inventory record found")
	errInsufficientInventory = fmt.Errorf("insufficient inventory")
)

// OrderMessage defines the structure for messages consumed from Kafka
type OrderMessage struct {
	OrderID   string `json:"orderId"`
	AlbumID   string `json:"albumId"`
	Quantity  int    `json:"quantity"`
	UserID    string `json:"userId"`
	Timestamp string `json:"timestamp"`
}

// AlbumCreatedEvent represents the event consumed when an album is created
// Ensure this matches the structure produced by album-service
type AlbumCreatedEvent struct {
	AlbumID         string    `json:"albumId"`
	Title           string    `json:"title"` // Optional, but good for logging
	Artist          string    `json:"artist"` // Optional, but good for logging
	Timestamp       time.Time `json:"timestamp"`
	InitialQuantity *int      `json:"initialQuantity,omitempty"` // Mirror definition from album-service
}

// OrderFailedEvent represents the event published when an order fails due to inventory
type OrderFailedEvent struct {
	OrderID   string    `json:"orderId"`
	Reason    string    `json:"reason"` // e.g., "INSUFFICIENT_STOCK"
	Timestamp time.Time `json:"timestamp"`
}

// OrderSucceededEvent represents the event published when inventory is successfully deducted
type OrderSucceededEvent struct {
	OrderID   string    `json:"orderId"`
	Timestamp time.Time `json:"timestamp"`
}

var consumerGroupID = "inventory-service-consumers"

// startOrderConsumer initializes and runs the Kafka consumer loop for order creation events.
func startOrderConsumer(kafkaBroker string) {
	orderCreatedTopic := "order-created"

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{kafkaBroker},
		Topic:   orderCreatedTopic,
		GroupID: consumerGroupID,
		MinBytes: 10e3,
		MaxBytes: 10e6,
	})

	log.Printf("Kafka consumer started for topic '%s', group '%s', broker '%s'", 
			   reader.Config().Topic, reader.Config().GroupID, kafkaBroker)

	defer reader.Close()

	for {
		msg, err := reader.ReadMessage(context.Background())
		if err != nil {
			log.Printf("Error reading message (%s): %v", orderCreatedTopic, err)
			continue
		}
		
		if err := processOrderCreated(db, msg); err != nil {
			log.Printf("Failed to process order created message: %v. Offset: %d", err, msg.Offset)
		} else {
			if err := reader.CommitMessages(context.Background(), msg); err != nil {
				log.Printf("Failed to commit message offset %d (%s): %v", msg.Offset, orderCreatedTopic, err)
			} else {
				log.Printf("Successfully committed message for offset %d (%s)", msg.Offset, orderCreatedTopic)
			}
		}
	}
}

// startAlbumCreatedConsumer initializes and runs the Kafka consumer loop for album creation events.
func startAlbumCreatedConsumer(kafkaBroker string) {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{kafkaBroker},
		Topic:   "album-created",
		GroupID: "inventory-service-album-init",
		MinBytes: 10e3,
		MaxBytes: 10e6,
	})

	log.Printf("Kafka consumer started for topic '%s', group '%s', broker '%s'", reader.Config().Topic, reader.Config().GroupID, kafkaBroker)

	defer reader.Close()

	for {
		msg, err := reader.ReadMessage(context.Background())
		if err != nil {
			log.Printf("Error reading message (album-created): %v", err)
			continue
		}
		
		if err := processAlbumCreatedEvent(db, msg); err != nil {
			log.Printf("Failed to process album created message: %v. Offset: %d", err, msg.Offset)
		} else {
			if err := reader.CommitMessages(context.Background(), msg); err != nil {
				log.Printf("Failed to commit message offset %d (album-created): %v", msg.Offset, err)
			} else {
				log.Printf("Successfully committed message for offset %d (album-created)", msg.Offset)
			}
		}
	}
}

// processAlbumCreatedEvent handles initializing inventory for a newly created album.
func processAlbumCreatedEvent(db *sql.DB, msg kafka.Message) error {
	log.Printf("Received Kafka message (album-created): Partition=%d, Offset=%d", msg.Partition, msg.Offset)

	// Extract trace context and start a new span
	ctx := ExtractTraceInfoFromKafkaMessage(context.Background(), msg.Headers)
	ctx, span := tracer.Start(ctx, "processAlbumCreatedEvent")
	defer span.End()
	
	// Set base Kafka message attributes
	span.SetAttributes(
		attribute.Int("kafka.partition", msg.Partition),
		attribute.Int64("kafka.offset", msg.Offset),
		attribute.String("kafka.topic", "album-created"),
	)

	// Parse album creation message
	var event AlbumCreatedEvent
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		log.Printf("Error parsing AlbumCreatedEvent JSON: %v. Message: %s", err, string(msg.Value))
		span.RecordError(err)
		span.SetStatus(codes.Error, "Failed to parse album created event")
		return fmt.Errorf("failed to parse AlbumCreatedEvent: %w", err)
	}

	// Log album details
	log.Printf("Processing album: AlbumID=%s, Title='%s', InitialQty=%v", 
		event.AlbumID, event.Title, event.InitialQuantity)
	span.SetAttributes(
		attribute.String("album.id", event.AlbumID),
		attribute.String("album.title", event.Title),
	)
	if event.InitialQuantity != nil {
		span.SetAttributes(attribute.Int("album.initial_quantity", *event.InitialQuantity))
	}

	// Determine initial inventory quantity
	quantityToInsert := 0 // default quantity
	if event.InitialQuantity != nil && *event.InitialQuantity >= 0 {
		quantityToInsert = *event.InitialQuantity
		log.Printf("Using initial quantity from event: %d", quantityToInsert)
	} else {
		log.Printf("Initial quantity not provided or invalid, defaulting to 0")
	}

	// Create child span for DB operation
	ctx, dbSpan := tracer.Start(ctx, "db.insert_inventory")
	
	// Insert initial inventory record
	_, err := db.ExecContext(ctx, `
		INSERT INTO inventory (album_id, quantity_available, last_updated)
		VALUES ($1, $2, NOW())
		ON CONFLICT (album_id) DO NOTHING`,
		event.AlbumID, quantityToInsert)
	
	if err != nil {
		log.Printf("Error inserting inventory: %v", err)
		dbSpan.RecordError(err)
		span.RecordError(err)
		dbSpan.End()
		span.SetStatus(codes.Error, "Database insert failed")
		return fmt.Errorf("database execution failed: %w", err)
	}
	
	dbSpan.End()
	log.Printf("Initialized inventory for AlbumID %s with quantity %d", event.AlbumID, quantityToInsert)
	span.SetStatus(codes.Ok, "Inventory initialized successfully")
	return nil
}

// processOrderCreated handles messages from the order-created topic.
// It attempts to deduct inventory atomically and sends an order-failed event if unsuccessful.
func processOrderCreated(db *sql.DB, msg kafka.Message) error {
	log.Printf("Received Kafka message (order-created): Partition=%d, Offset=%d", msg.Partition, msg.Offset)

	// Extract trace context and start a new span
	ctx := ExtractTraceInfoFromKafkaMessage(context.Background(), msg.Headers)
	ctx, span := tracer.Start(ctx, "processOrderCreated")
	defer span.End()
	
	// Set base Kafka message attributes
	span.SetAttributes(
		attribute.Int("kafka.partition", msg.Partition),
		attribute.Int64("kafka.offset", msg.Offset),
		attribute.String("kafka.topic", "order-created"),
	)
	
	// Parse order message
	var event OrderMessage
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		log.Printf("Error parsing OrderCreatedEvent JSON: %v. Message: %s", err, string(msg.Value))
		span.RecordError(err)
		span.SetStatus(codes.Error, "Failed to parse order message")
		return nil // For unparseable messages, still commit the offset
	}

	// Log order details
	log.Printf("Processing order: OrderID=%s, AlbumID=%s, Quantity=%d", 
		event.OrderID, event.AlbumID, event.Quantity)
	span.SetAttributes(
		attribute.String("order.id", event.OrderID),
		attribute.String("album.id", event.AlbumID),
		attribute.Int("order.quantity", event.Quantity),
		attribute.String("user.id", event.UserID),
	)

	// Try deducting inventory
	// Use transaction to ensure atomic operation
	ctx, dbSpan := tracer.Start(ctx, "db.update_inventory")
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("Error starting transaction: %v", err)
		dbSpan.RecordError(err)
		span.RecordError(err)
		dbSpan.End()
		span.SetStatus(codes.Error, "Database transaction error")
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Ensure rollback of uncommitted transaction on function exit

	// Perform atomic update; only succeeds if sufficient inventory exists
	result, err := tx.ExecContext(ctx,
		`UPDATE inventory
		 SET quantity_available = quantity_available - $1
		 WHERE album_id = $2 AND quantity_available >= $1`,
		event.Quantity, event.AlbumID)

	if err != nil {
		log.Printf("Error updating inventory: %v", err)
		dbSpan.RecordError(err)
		dbSpan.End()
		span.RecordError(err)
		span.SetStatus(codes.Error, "Database update failed")
		return fmt.Errorf("database update error: %w", err)
	}

	// Check if any rows were updated
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Printf("Error getting rows affected: %v", err)
		dbSpan.RecordError(err)
		dbSpan.End()
		span.RecordError(err)
		span.SetStatus(codes.Error, "Failed to get result info")
		return fmt.Errorf("database result error: %w", err)
	}
	
	// If rows were updated, inventory deduction succeeded
	if rowsAffected == 1 {
		// Commit transaction
		if err := tx.Commit(); err != nil {
			log.Printf("Error committing transaction: %v", err)
			dbSpan.RecordError(err)
			dbSpan.End()
			span.RecordError(err)
			span.SetStatus(codes.Error, "Transaction commit failed")
			return fmt.Errorf("transaction commit error: %w", err)
		}
		
		dbSpan.SetStatus(codes.Ok, "Inventory updated successfully")
		dbSpan.End()
		
		// Send order success event
		log.Printf("Inventory deducted successfully, sending success event")
		_, pubSpan := tracer.Start(ctx, "send_success_event")
		err = sendOrderSucceededEvent(event.OrderID)
		if err != nil {
			log.Printf("Failed to send success event: %v", err)
			pubSpan.RecordError(err)
		}
		pubSpan.End()
		
		span.SetStatus(codes.Ok, "Order processed successfully")
		return nil
	}
	
	// Insufficient inventory, order failed
	dbSpan.End()
	
	// Query current inventory for more detailed error information
	var currentQty int
	err = db.QueryRowContext(ctx, 
		"SELECT quantity_available FROM inventory WHERE album_id = $1", 
		event.AlbumID).Scan(&currentQty)
	
				if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("No inventory record found for AlbumID: %s", event.AlbumID)
			span.SetAttributes(attribute.Bool("inventory.exists", false))
				} else {
			log.Printf("Error querying inventory: %v", err)
			span.RecordError(err)
				}
			} else {
		log.Printf("Insufficient inventory. Requested: %d, Available: %d", 
			event.Quantity, currentQty)
		span.SetAttributes(
			attribute.Bool("inventory.exists", true),
			attribute.Int("inventory.available", currentQty),
		)
	}
	
	// Send order failure event and record tracking information
	err = sendOrderFailedEvent(event.OrderID, "INSUFFICIENT_INVENTORY")
	if err != nil {
		log.Printf("Failed to send failure event: %v", err)
		span.RecordError(err)
	}
	
	span.SetStatus(codes.Ok, "Order processed - insufficient inventory")
	return nil
}

// sendOrderFailedEvent publishes an event to the order-failed topic
func sendOrderFailedEvent(orderID string, reason string) error {
	return sendOrderEvent(orderID, reason, orderFailedTopic, kafkaFailedEventWriter)
}

// sendOrderSucceededEvent publishes an event to the order-succeeded topic
func sendOrderSucceededEvent(orderID string) error {
	return sendOrderEvent(orderID, "", orderSucceededTopic, kafkaSucceededEventWriter)
}

// sendOrderEvent handles sending events to Kafka with unified tracing logic
func sendOrderEvent(orderID string, reason string, topic string, writer *kafka.Writer) error {
	// Create a new context, not using tracing
	ctx := context.Background()
	
	var event []byte
	var err error
	
	// Build event based on topic type
	if topic == orderFailedTopic {
		failEvent := OrderFailedEvent{
			OrderID:   orderID,
			Reason:    reason,
			Timestamp: time.Now(),
		}
		event, err = json.Marshal(failEvent)
	} else if topic == orderSucceededTopic {
		succEvent := OrderSucceededEvent{
			OrderID:   orderID,
			Timestamp: time.Now(),
		}
		event, err = json.Marshal(succEvent)
	} else {
		return fmt.Errorf("unknown topic: %s", topic)
	}
	
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}
	
	// Send message to Kafka
	return writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(orderID),
		Value: event,
	})
}

// initProcessedOrdersTable creates the table to track processed orders if it doesn't exist.
func initProcessedOrdersTable() {
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS processed_orders (
		order_id VARCHAR(255) PRIMARY KEY,
		processed_at TIMESTAMP NOT NULL DEFAULT NOW()
	)`)
	if err != nil {
		log.Fatalf("Could not create processed_orders table: %v", err)
	}
}

// reserveInventory reserves inventory for an order
func reserveInventory(albumID string, quantity int) error {
	var currentQuantity int
	err := db.QueryRow("SELECT quantity_available FROM inventory WHERE album_id = $1", albumID).Scan(&currentQuantity)
	if err != nil {
		if err == sql.ErrNoRows {
			return errNoInventory
		}
		return err
	}

	if currentQuantity < quantity {
		return errInsufficientInventory
	}

	_, err = db.Exec(
		"UPDATE inventory SET quantity_available = quantity_available - $1, last_updated = $2 WHERE album_id = $3",
		quantity, time.Now(), albumID,
	)
	if err != nil {
		return err
	}

	var newQuantity int
	err = db.QueryRow("SELECT quantity_available FROM inventory WHERE album_id = $1", albumID).Scan(&newQuantity)
	if err != nil {
		return err
	}

	log.Printf("Inventory updated for albumId: %s, new quantity: %d", albumID, newQuantity)
	return nil
}
